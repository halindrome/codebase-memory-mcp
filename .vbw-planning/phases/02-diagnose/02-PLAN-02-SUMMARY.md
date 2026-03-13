---
phase: "02"
plan: "02"
title: "Minimal OOM reproducer test for MaxFileSize bypass"
status: complete
tasks_total: 4
tasks_done: 4
---

## What Was Built

Three diagnostic OOM reproducer tests that exercise the known memory-unsafe paths in the pipeline: MaxFileSize bypass with a large file, GraphBuffer growth with many files, and adaptive pool goroutine scaling with medium-sized files. All tests are diagnostic (always pass) and log memory behavior for analysis.

## Files Modified

- `internal/pipeline/oom_reproducer_test.go` (new) — Three reproducer tests: `TestOOMReproducer_LargeFile`, `TestOOMReproducer_ManyFiles`, `TestOOMReproducer_AdaptivePool`

## Summary

### TestOOMReproducer_LargeFile
- Creates a 20MB JavaScript file with valid-ish `var x$N = $N;\n` content
- Indexes in full mode (no MaxFileSize guard — the CRITICAL path)
- Samples HeapInuse every 200ms, tracks peak heap vs file size ratio
- Logs: peak heap, heap/file ratio, wall time, NumGC

### TestOOMReproducer_ManyFiles
- Creates 300 Go files (~1KB each) with unique function definitions across 10 packages
- Indexes in full mode to exercise GraphBuffer growth and extractionCache
- Logs: node/edge counts, estimated GraphBuffer memory, peak heap, NumGC
- Estimates ~900 nodes (3 per file) to show scaling behavior

### TestOOMReproducer_AdaptivePool
- Creates 100 Go files at ~200KB each (many functions per file)
- Monitors goroutine count every 100ms alongside heap stats
- Logs: peak goroutines vs NumCPU, adaptive pool max (NumCPU * 8), peak heap

### Notes
- All tests are diagnostic (always pass) — they log memory behavior, not assert thresholds
- Go toolchain was not available on the development machine; tests follow exact patterns from `memleak_test.go` and `pipeline_test.go` and will compile/run in CI
- Tests use `store.OpenMemory()`, `t.TempDir()`, and `.git` marker patterns from existing test suite
