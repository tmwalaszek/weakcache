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

	sfGroup *singleflight.Group[T]
}

type Stats struct {
	NumCalls       atomic.Int64
	NumCacheMisses atomic.Int64
	NumCacheHits   atomic.Int64

	SingleFlightStats *singleflight.Stats
}

func NewWeakCache[T any]() *WeakCache[T] {
	sfGroup := singleflight.NewGroup[T]()

	w := &WeakCache[T]{
		c:       make(map[string]weak.Pointer[T]),
		sfGroup: sfGroup,
	}
	w.stats.SingleFlightStats = sfGroup.Stats()
	return w
}

func (w *WeakCache[T]) get(key string) (T, bool) {
	w.mx.Lock()
	defer w.mx.Unlock()

	weakVal, ok := w.c[key]
	if !ok {
		var zero T
		return zero, false
	}

	val := weakVal.Value()
	if val == nil {
		delete(w.c, key)
		var zero T
		return zero, false
	}

	return *val, true
}

// set stores value under key. If key already exists, its weak entry is
// overwritten; the old entry's cleanup may still fire later but is a no-op
// because the cur.Value() == nil guard rejects deleting a live entry.
func (w *WeakCache[T]) set(key string, value *T) {
	w.mx.Lock()
	defer w.mx.Unlock()

	w.c[key] = weak.Make(value)

	// Attach the cleanup to the value itself so it runs when the value
	// is collected, not when this stack frame's locals go out of scope.
	runtime.AddCleanup(value, func(key string) {
		w.mx.Lock()
		defer w.mx.Unlock()

		if cur, ok := w.c[key]; ok && cur.Value() == nil {
			delete(w.c, key)
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
	return &w.stats
}
