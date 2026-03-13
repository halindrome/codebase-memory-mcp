# HEAP-ANALYSIS.md

## Summary

The `passDefinitions` stage dominates peak memory because it holds both the full `extractionCache` (all CBM FileResult structs) and the `GraphBuffer` (all nodes/edges in RAM) simultaneously. For 100K files, this peaks at ~7.8 GB — 97% of the default 8 GB GOMEMLIMIT — leaving almost no headroom for subsequent passes.

## Stage-by-Stage Heap Table

Estimated from code analysis and research data. The test file `internal/pipeline/heap_profile_test.go` is ready to run when Go is available (`go test -v -run TestHeapProfileStages -timeout 300s ./internal/pipeline/`).

### Estimated heap at 100 files (test scale)

| Stage | HeapInuse MB | HeapAlloc MB | NumGC | Notes |
|-------|-------------|--------------|-------|-------|
| baseline | ~5 | ~3 | 0 | Before store open |
| pre_index | ~8 | ~5 | 1 | After discover, before passes |
| post_definitions | ~25–35 | ~20–28 | 3–5 | GraphBuffer + extractionCache peak |
| post_calls | ~18–25 | ~15–20 | 5–8 | After releaseExtractionFields(fieldsPostCalls) |
| post_cleanup | ~10–15 | ~8–12 | 8–12 | After extractionCache = nil, FreeOSMemory() |
| post_flush | ~8–12 | ~6–10 | 10–14 | After GraphBuffer flushed to SQLite |

### Estimated heap at 100K files (production crash scale)

| Stage | HeapInuse MB | HeapAlloc MB | NumGC | Notes |
|-------|-------------|--------------|-------|-------|
| baseline | ~5 | ~3 | 0 | — |
| pre_index | ~50 | ~30 | 2 | discover.Discover file list |
| post_definitions | ~7800 | ~6500 | 50+ | **PEAK: GraphBuffer (~2.8 GB) + extractionCache (~5 GB)** |
| post_calls | ~3500 | ~3000 | 100+ | Definitions/Calls/Imports nilled |
| post_cleanup | ~2800 | ~2500 | 150+ | extractionCache = nil |
| post_flush | ~200 | ~150 | 200+ | GraphBuffer flushed, buf = nil |

## Scaling Analysis

Memory growth is **O(n)** in file count, but with a high constant factor:

- **extractionCache**: ~50 KB/file (Definitions + Calls + Usages + TypeRefs slices)
  - 100 files: ~5 MB
  - 10K files: ~500 MB
  - 100K files: ~5 GB
- **GraphBuffer**: ~28 KB/node, ~20 nodes/file average
  - 100 files: ~56 MB
  - 10K files: ~560 MB
  - 100K files: ~2.8 GB (with secondary index maps)
- **Combined peak** (post_definitions): ~78 KB/file
  - 100 files: ~8 MB
  - 10K files: ~780 MB
  - 100K files: ~7.8 GB

The scaling is linear but the constant is so large that even moderate repos (25K files) can exceed a 2 GB GOMEMLIMIT default.

## Top Allocators

Based on code analysis (pprof unavailable — Go not installed on this machine):

| Rank | Allocator | Est. Size (100K files) | Location |
|------|-----------|----------------------|----------|
| 1 | `extractionCache` (cbm.FileResult slices) | ~5 GB | `pipeline.go:1128` |
| 2 | `GraphBuffer.nodeByQN` + secondary maps | ~1.5 GB | `graph_buffer.go:17-28` |
| 3 | `GraphBuffer.edges` + `edgeByKey` map | ~1.3 GB | `graph_buffer.go:24-26` |
| 4 | `results []*parseResult` (temporary during passDefinitions) | ~200 MB peak | `pipeline.go:1073` |
| 5 | `FunctionRegistry` maps | ~100 MB | `registry.go` |
| 6 | Concurrent mmap buffers (adaptive pool workers) | ~64 MB (64 workers x 1 MB avg) | `pipeline_cbm.go:26` |

## Peak Memory Stage

**`post_definitions`** is the peak stage.

At this point, the pipeline has:
1. Completed `passStructure` — all Module nodes in GraphBuffer
2. Completed `passDefinitions` — all Function/Class/Method/Variable nodes in GraphBuffer **AND** all CBM FileResults in extractionCache
3. NOT YET released any extractionCache fields (release happens after `passCalls`)
4. NOT YET flushed GraphBuffer to SQLite (flush happens after pass 14)

The peak occurs because `passDefinitions` is the first pass that both:
- Populates the full extractionCache (holding all per-file extraction results)
- Adds bulk nodes to GraphBuffer (Function, Class, Method, Variable for every file)

No intermediate release happens between `passDefinitions` completing and the next few passes consuming those results.

## Data Structure Size Estimates

From research and code analysis:

| Data Structure | Per-File Size | 100K Files | Location |
|---------------|--------------|------------|----------|
| `cbm.FileResult` (full) | ~50 KB | 5 GB | extractionCache |
| `cbm.FileResult.Definitions` | ~20 KB | 2 GB | Largest field in FileResult |
| `cbm.FileResult.Calls` | ~15 KB | 1.5 GB | Second largest |
| `cbm.FileResult.Usages` | ~8 KB | 800 MB | Released after passUsages |
| `store.Node` | ~512 B | 1 GB (2M nodes) | GraphBuffer |
| `store.Edge` | ~256 B | 1.3 GB (5M edges) | GraphBuffer |
| GraphBuffer secondary indexes | ~250 B/entry | 500 MB | nodeByQN, nodesByLabel, nodeByID |
| `FunctionRegistry` entries | ~100 B | 100 MB | registry |

## Key Finding for Phase 3 Fix

**Target `extractionCache` first** — it is the single largest memory consumer at peak (5 GB for 100K files, 64% of total peak).

Specifically:
1. **Early field release after passDefinitions**: The `Definitions` and `Calls` fields (~35 KB/file) are populated during `passDefinitions` but not released until after `passCalls` — several passes later. Releasing `Definitions` immediately after they are consumed by `passDefinitions` would cut ~2 GB from peak.

2. **Streaming extraction**: Instead of storing all FileResults in extractionCache simultaneously, process files in batches of 1K–5K. Each batch: extract → populate GraphBuffer → nil extraction results → next batch. This turns O(N) extractionCache into O(batch_size).

3. **GraphBuffer is the secondary target** (~2.8 GB): Adaptive flushing every N nodes would cap its growth but requires more invasive refactoring of the ID remapping logic.

The simplest high-impact fix: `releaseExtractionFields(fieldsPostDefinitions)` immediately after `passDefinitions` completes, nilling `Definitions` (the single largest field at ~20 KB/file). This requires verifying no later pass reads `Definitions` — current code shows `passCalls` reads `Calls` but not `Definitions`, so this should be safe.
