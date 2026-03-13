package pipeline

import (
	"context"
	"log/slog"
	"math"
	"runtime"
	"runtime/debug"
	"time"
)

// memPressureWatcher polls HeapInuse and throttles the adaptive pool when
// memory pressure exceeds 80% of GOMEMLIMIT.
type memPressureWatcher struct {
	pool     *adaptivePool
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func startMemPressureWatcher(ctx context.Context, pool *adaptivePool) *memPressureWatcher {
	w := &memPressureWatcher{
		pool:     pool,
		interval: time.Second,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	go w.run(ctx)
	return w
}

func (w *memPressureWatcher) stop() {
	close(w.stopCh)
	<-w.doneCh
}

func (w *memPressureWatcher) run(ctx context.Context) {
	defer close(w.doneCh)

	goMemLimit := debug.SetMemoryLimit(0) // query current value, no change
	if goMemLimit <= 0 || goMemLimit == math.MaxInt64 {
		// GOMEMLIMIT not set — backpressure not applicable
		slog.Debug("mem.backpressure.disabled", "reason", "GOMEMLIMIT not set")
		return
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	throttled := false
	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			heapInuse := int64(m.HeapInuse)

			highWater := goMemLimit * 80 / 100
			lowWater := goMemLimit * 70 / 100

			if !throttled && heapInuse > highWater {
				w.pool.setLimit(w.pool.numCPU)
				throttled = true
				slog.Warn("mem.backpressure.throttle",
					"heap_inuse_mb", heapInuse/(1<<20),
					"limit_mb", goMemLimit/(1<<20),
					"pool_limit", w.pool.numCPU,
				)
			} else if throttled && heapInuse < lowWater {
				w.pool.setLimit(w.pool.maxLimit)
				throttled = false
				slog.Info("mem.backpressure.restore",
					"heap_inuse_mb", heapInuse/(1<<20),
					"pool_limit", w.pool.maxLimit,
				)
			}
		}
	}
}
