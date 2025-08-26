# Overview
WeakCache provides duplicate function call suppression combined with a weakly referenced cache for storing results.

This means:

- Concurrent calls with the same key will be collapsed into a single execution making sure that only one execution is in-flight for a given key at a time.
- Results are cached and reused if available — but only as long as they are still held by a weak pointer. Once garbage collected, the result is gone from cache.

WeakCache use modified version of [Go’s `x/sync/singleflight`](https://cs.opensource.google/go/x/sync). The key differences of weakCache singleflight are:
- The main difference is that `Group` is now generic. This means that the function passed to `Do` has to return a type which was used to create `Group`.
- The `DoChan` method is removed. I might add it back in the future as a generic version.

# Example

```Go
w := NewWeakCache[int]()

r, err := w.Do("key", func() (int, error) {
    time.Sleep(10 * time.Microsecond)
    return 1, nil
})
```

- If another goroutine calls `Do("key", ...)` while the first one is still running, it will block and reuse the results from the first one. This is `singleflight` behavior. 
- Later calls to `Do("key", ...)` will reuse the cached result, as long as it hasn’t been collected by the GC.
