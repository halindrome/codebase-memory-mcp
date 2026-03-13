package pipeline

import (
	"context"
	"runtime"
	"runtime/debug"
	"testing"
	"time"
)

// TestMemPressureWatcher_Throttles verifies that the watcher reduces pool limit
// when heap usage exceeds the high-water mark.
//
// We allocate enough memory to push HeapInuse above 80% of a small GOMEMLIMIT,
// then check that the pool limit drops to numCPU.
func TestMemPressureWatcher_Throttles(t *testing.T) {
	// Save and restore GOMEMLIMIT
	originalLimit := debug.SetMemoryLimit(0)
	defer debug.SetMemoryLimit(originalLimit)

	// Set a moderate limit (64 MB); we will allocate ~55 MB to exceed 80% (51.2 MB).
	const testLimit = 64 << 20 // 64 MB
	const highWater = testLimit * 80 / 100
	debug.SetMemoryLimit(testLimit)

	// Skip if current heap already exceeds the low-water mark (unusual environment).
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	if int64(before.HeapInuse) > testLimit*70/100 {
		t.Skip("current heap already exceeds low-water mark, test not meaningful")
	}

	pool := newAdaptivePool(4)
	defer pool.stop()
	pool.setLimit(pool.maxLimit)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	watcher := startMemPressureWatcher(ctx, pool)
	defer watcher.stop()

	// Allocate enough live memory to push HeapInuse above high-water mark.
	// Writing to each page forces physical allocation (bypasses lazy zeroing).
	// We keep a reference so the GC cannot collect it during the test.
	bufs := make([][]byte, 0, 60)
	for i := 0; i < 55; i++ {
		b := make([]byte, 1<<20) // 1 MB each
		for j := 0; j < len(b); j += 4096 {
			b[j] = byte(i) // touch every page
		}
		bufs = append(bufs, b)
	}
	runtime.GC() // ensure HeapInuse reflects live allocations

	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if int64(after.HeapInuse) <= highWater {
		runtime.KeepAlive(bufs)
		t.Skipf("could not push HeapInuse above high-water mark (HeapInuse=%d, highWater=%d)", after.HeapInuse, highWater)
	}

	// Wait up to 3 seconds for throttle to trigger.
	// bufs must stay live so HeapInuse remains above high-water during ticks.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pool.mu.Lock()
		limit := pool.limit
		pool.mu.Unlock()
		if limit <= pool.numCPU {
			t.Logf("throttled: pool.limit=%d == numCPU=%d", limit, pool.numCPU)
			runtime.KeepAlive(bufs)
			return // success
		}
		time.Sleep(200 * time.Millisecond)
	}
	runtime.KeepAlive(bufs)
	t.Errorf("watcher did not throttle pool within 3s; pool.limit=%d, numCPU=%d", pool.limit, pool.numCPU)
}

// TestMemPressureWatcher_SkipsWhenNoGOMEMLIMIT verifies that when GOMEMLIMIT is
// not set (math.MaxInt64), the watcher exits immediately and does not modify the pool.
func TestMemPressureWatcher_SkipsWhenNoGOMEMLIMIT(t *testing.T) {
	originalLimit := debug.SetMemoryLimit(0)
	defer debug.SetMemoryLimit(originalLimit)

	// math.MaxInt64 signals "not set"
	debug.SetMemoryLimit(int64(^uint64(0) >> 1)) // math.MaxInt64

	pool := newAdaptivePool(4)
	defer pool.stop()
	pool.setLimit(pool.maxLimit)
	initialLimit := pool.maxLimit

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	watcher := startMemPressureWatcher(ctx, pool)
	// Give watcher goroutine time to start and then exit
	time.Sleep(200 * time.Millisecond)
	watcher.stop()

	pool.mu.Lock()
	finalLimit := pool.limit
	pool.mu.Unlock()

	if finalLimit != initialLimit {
		t.Errorf("watcher modified pool limit (got %d, want %d) when GOMEMLIMIT=MaxInt64", finalLimit, initialLimit)
	}
}
