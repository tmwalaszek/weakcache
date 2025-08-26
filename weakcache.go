package weakcache

import (
	"runtime"
	"sync"
	"sync/atomic"
	"weak"

	"github.com/tmwalaszek/weakcache/singleflight"
)

type WeakCache[T any] struct {
	c  map[string]weak.Pointer[T]
	mx sync.Mutex

	stats Stats

	sfGroup singleflight.Group[T]
}

type Stats struct {
	NumCalls       atomic.Int64
	NumCacheMisses atomic.Int64
	NumCacheHits   atomic.Int64

	SingleFlightStats *singleflight.Stats
}

func NewWeakCache[T any]() WeakCache[T] {
	sfGroup := singleflight.NewGroup[T]()

	return WeakCache[T]{
		c:       make(map[string]weak.Pointer[T]),
		mx:      sync.Mutex{},
		sfGroup: *sfGroup,
	}
}

func (w *WeakCache[T]) get(key string) (T, bool) {
	w.mx.Lock()
	defer w.mx.Unlock()

	var weakVal weak.Pointer[T]

	var b bool
	var value T
	weakVal, b = w.c[key]
	if !b {
		return value, false
	}

	val := weakVal.Value()
	if val == nil {
		delete(w.c, key)
		return value, false
	}

	return *val, true
}

// XXX What to do when we have key?
func (w *WeakCache[T]) set(key string, value *T) {
	w.mx.Lock()
	defer w.mx.Unlock()

	v := weak.Make(value)
	w.c[key] = v

	runtime.AddCleanup(&v, func(key string) {
		w.mx.Lock()
		defer w.mx.Unlock()

		if cur, ok := w.c[key]; ok {
			if cur.Value() == nil {
				delete(w.c, key)
			}
		}
	}, key)
}

func (w *WeakCache[T]) Do(key string, fn func() (T, error)) (T, error) {
	var value T

	w.stats.NumCalls.Add(1)

	v, got := w.get(key)
	if !got {
		w.stats.NumCacheMisses.Add(1)
		r := w.sfGroup.Do(key, fn)
		if r.Err != nil {
			return value, r.Err
		}

		// If r.Initial equal true then it's first result from the singleflight.Do and we need to store it in the cache
		if r.Initial {
			w.set(key, &r.Val)
		}

		return r.Val, nil
	}

	w.stats.NumCacheHits.Add(1)
	return v, nil
}

func (w *WeakCache[T]) Stats() *Stats {
	w.stats.SingleFlightStats = w.sfGroup.Stats()
	return &w.stats
}
