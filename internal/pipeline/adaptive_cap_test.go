package pipeline

import (
	"os"
	"runtime"
	"testing"
)

func TestAdaptivePool_MaxLimitCap(t *testing.T) {
	// Default: maxLimit must be <= numCPU * 2
	numCPU := runtime.NumCPU()
	p := newAdaptivePool(numCPU)
	p.stop()

	if p.maxLimit > numCPU*2 {
		t.Errorf("maxLimit=%d exceeds numCPU*2=%d", p.maxLimit, numCPU*2)
	}
	if p.maxLimit < numCPU {
		t.Errorf("maxLimit=%d is below minLimit numCPU=%d", p.maxLimit, numCPU)
	}
}

func TestAdaptivePool_MaxLimitEnvOverride(t *testing.T) {
	t.Setenv("CODEBASE_POOL_MAX_MULTIPLIER", "4")
	numCPU := runtime.NumCPU()
	p := newAdaptivePool(numCPU)
	p.stop()

	if p.maxLimit != numCPU*4 {
		t.Errorf("expected maxLimit=%d with multiplier=4, got %d", numCPU*4, p.maxLimit)
	}
}

func TestAdaptivePool_MaxLimitInvalidEnv(t *testing.T) {
	// Invalid env value must fall back to default cap (numCPU * 2)
	t.Setenv("CODEBASE_POOL_MAX_MULTIPLIER", "notanumber")
	numCPU := runtime.NumCPU()
	p := newAdaptivePool(numCPU)
	p.stop()

	if p.maxLimit != numCPU*2 {
		t.Errorf("expected default maxLimit=%d, got %d", numCPU*2, p.maxLimit)
	}
}

func TestAdaptivePool_MinCPU1(t *testing.T) {
	// Edge case: single-CPU host must still have valid limits
	os.Unsetenv("CODEBASE_POOL_MAX_MULTIPLIER")
	p := newAdaptivePool(1)
	p.stop()

	if p.minLimit != 1 {
		t.Errorf("expected minLimit=1, got %d", p.minLimit)
	}
	if p.maxLimit < 1 {
		t.Errorf("expected maxLimit>=1, got %d", p.maxLimit)
	}
}
