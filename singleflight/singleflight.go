// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package singleflight provides a duplicate function call suppression
// mechanism.
package singleflight

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// errGoexit indicates the runtime.Goexit was called in
// the user given function.
var errGoexit = errors.New("runtime.Goexit was called")

// A panicError is an arbitrary value recovered from a panic
// with the stack trace during the execution of given function.
type panicError struct {
	value interface{}
	stack []byte
}

// Error implements error interface.
func (p *panicError) Error() string {
	return fmt.Sprintf("%v\n\n%s", p.value, p.stack)
}

func (p *panicError) Unwrap() error {
	err, ok := p.value.(error)
	if !ok {
		return nil
	}

	return err
}

func newPanicError(v interface{}) error {
	stack := debug.Stack()

	// The first line of the stack trace is of the form "goroutine N [status]:"
	// but by the time the panic reaches Do the goroutine may no longer exist
	// and its status will have changed. Trim out the misleading line.
	if line := bytes.IndexByte(stack[:], '\n'); line >= 0 {
		stack = stack[line+1:]
	}
	return &panicError{value: v, stack: stack}
}

// call is an in-flight or completed singleflight.Do call
type call[T any] struct {
	cond chan struct{}
	dups atomic.Uint64

	res Result[T]
}

// set groups in-fligh calls.
type set[T any] struct {
	m map[string]*call[T]

	mx sync.Mutex
}

func (s *set[T]) loadOrStore(key string, c *call[T]) (*call[T], bool) {
	s.mx.Lock()
	defer s.mx.Unlock()

	val, ok := s.m[key]
	if !ok {
		s.m[key] = c
		val = s.m[key]
	}

	return val, ok
}

func (s *set[T]) compareAndDelete(key string, c *call[T]) bool {
	s.mx.Lock()
	defer s.mx.Unlock()

	if s.m[key] == c {
		delete(s.m, key)
		return true
	}

	return false
}

func NewGroup[T any]() *Group[T] {
	return &Group[T]{
		m: &set[T]{m: make(map[string]*call[T]), mx: sync.Mutex{}},
	}
}

// Group represents a class of work and forms a namespace in which units of work can be executed with duplicate suppression.
// Unlike the original singleflight Group, this version uses generics.
// A Group[T]’s Do method returns a Result[T], so the value has the same type T used to create the Group.
type Group[T any] struct {
	m     *set[T]
	stats Stats
}

// Stats contains statistics about the singleflight.Group.
// NumCalls is the number of calls to Do.
// NumSuppressedCalls is the number of calls to Do that were
// suppressed due to a previous call and are waiting for the results.
type Stats struct {
	NumCalls           atomic.Int64
	NumSuppressedCalls atomic.Int64
}

// Result is the return value of a singleflight.Do call.
//
// The shared field indicates whether the value was given to multiple callers.
// The initial field indicates whether this is the first call to Do for the given key.
type Result[T any] struct {
	Err error

	Shared  bool
	Initial bool

	Val T
}

// Do executes and returns the results of the given function, making
// sure that only one execution is in-flight for a given key at a
// time. If a duplicate comes in, the duplicate caller waits for the
// original to complete and receives the same results.
func (g *Group[T]) Do(key string, fn func() (T, error)) Result[T] {
	val, loaded := g.m.loadOrStore(key, &call[T]{
		cond: make(chan struct{}),
	})
	g.stats.NumCalls.Add(1)

	if loaded {
		g.stats.NumSuppressedCalls.Add(1)
		<-val.cond
		val.dups.Add(1)

		res := val.res

		var e *panicError
		if errors.As(val.res.Err, &e) {
			panic(e)
		}

		if errors.Is(val.res.Err, errGoexit) {
			runtime.Goexit()
		}

		res.Shared = val.dups.Load() > 0
		return res
	}

	g.doCall(val, key, fn)

	res := val.res
	res.Shared = val.dups.Load() > 0
	res.Initial = true
	return res
}

func (g *Group[T]) Stats() *Stats {
	return &g.stats
}

// doCall handles the single call for a key.
func (g *Group[T]) doCall(c *call[T], key string, fn func() (T, error)) {
	normalReturn := false
	recovered := false

	defer func() {
		if !normalReturn && !recovered {
			c.res.Err = errGoexit
		}

		close(c.cond)
		g.m.compareAndDelete(key, c)

		if e, ok := c.res.Err.(*panicError); ok {
			panic(e)
		}
	}()

	func() {
		defer func() {
			if !normalReturn {
				// Ideally, we would wait to take a stack trace until we've determined
				// whether this is a panic or a runtime.Goexit.
				//
				// Unfortunately, the only way we can distinguish the two is to see
				// whether the recover stopped the goroutine from terminating, and by
				// the time we know that, the part of the stack trace relevant to the
				// panic has been discarded.
				if r := recover(); r != nil {
					c.res.Err = newPanicError(r)
				}
			}
		}()

		c.res.Val, c.res.Err = fn()
		normalReturn = true
	}()

	if !normalReturn {
		recovered = true
	}
}
