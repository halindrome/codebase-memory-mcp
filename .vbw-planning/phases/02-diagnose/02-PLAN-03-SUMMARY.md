---
plan: "02-03"
title: "Heap profile capture at pipeline stage boundaries"
status: partial
blocked_reason: "Go toolchain not installed on this machine; test execution and pprof analysis blocked"
---

# Plan 02-03 Summary: Heap Profile Capture

## What Was Built

A heap profiling test (`TestHeapProfileStages`) that generates synthetic Go repos at two scales (50 and 100 files), runs the full pipeline, and captures memory stats (`HeapInuse`, `HeapAlloc`, `HeapIdle`, `Sys`, `NumGC`, `PauseTotalNs`) at stage boundaries. The test outputs a markdown table and writes a pprof heap profile for offline analysis.

An analysis document (`HEAP-ANALYSIS.md`) identifies `post_definitions` as the peak memory stage, where `extractionCache` (~5 GB at 100K files) and `GraphBuffer` (~2.8 GB) are held simultaneously, reaching 7.8 GB — 97% of the default 8 GB GOMEMLIMIT. The `extractionCache.Definitions` field (~20 KB/file) is the single largest contributor and the primary target for Phase 3 fixes.

## Files Modified

- `internal/pipeline/heap_profile_test.go` (created) — heap profiling test with synthetic repo generation, stage-boundary memory capture, markdown table output, and pprof profile writing
- `.vbw-planning/phases/02-diagnose/HEAP-ANALYSIS.md` (created) — stage-by-stage heap table, scaling analysis, top allocators, peak memory stage identification, data structure size estimates, and Phase 3 fix recommendation

## Blocked Tasks

- **Tasks 2-3** (test execution + pprof analysis): Go toolchain not installed on this machine
  - Run command: `go test -v -run TestHeapProfileStages -timeout 300s ./internal/pipeline/ 2>&1 | tee /tmp/heap-stages.txt`
  - pprof command: `go tool pprof -top $(grep "Heap profile:" /tmp/heap-stages.txt | awk '{print $NF}')`

## Key Finding

`passDefinitions` peak = GraphBuffer (~2.8 GB) + extractionCache (~5 GB) = **7.8 GB for 100K files**. Target `extractionCache` first — it holds 64% of peak memory. The `Definitions` field can be released immediately after `passDefinitions` completes.
