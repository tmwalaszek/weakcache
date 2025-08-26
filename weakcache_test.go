package weakcache

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWeakCache(t *testing.T) {
	w := NewWeakCache[int]()

	r, err := w.Do("key1", func() (int, error) {
		time.Sleep(time.Microsecond * 10)
		return 9, nil
	})
	if err != nil {
		t.Fatal("Got error")
	}

	var wg sync.WaitGroup

	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			_, err := w.Do("key1", func() (int, error) {
				return 10, nil
			})
			if err != nil {
				t.Error("fail")
				return
			}
		}()
	}

	wg.Wait()

	if _, ok := w.c["key1"]; !ok {
		t.Error("fail")
	}

	wcStats := w.Stats()

	assert.Equal(t, int64(11), wcStats.NumCalls.Load())
	assert.Equal(t, int64(10), wcStats.NumCacheHits.Load())
	assert.Equal(t, int64(1), wcStats.NumCacheMisses.Load())

	assert.Equal(t, int64(1), wcStats.SingleFlightStats.NumCalls.Load())
	assert.Equal(t, int64(0), wcStats.SingleFlightStats.NumSuppressedCalls.Load())

	runtime.GC()

	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			r1, err := w.Do("key1", func() (int, error) {
				time.Sleep(time.Microsecond * 100)
				return 10, nil
			})
			if err != nil {
				t.Errorf("error: %v", err)
				return
			}

			if r1 == r {
				t.Errorf("fail mismatch %d != %d", r1, r)
			}
		}()
	}

	wg.Wait()

	assert.Equal(t, int64(21), wcStats.NumCalls.Load())
	sfCalls := wcStats.SingleFlightStats.NumCalls.Load()

	cacheHit := 21 - sfCalls
	assert.Equal(t, cacheHit, wcStats.NumCacheHits.Load())
	assert.Equal(t, sfCalls, wcStats.NumCacheMisses.Load())
}
