package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"testing"

	"github.com/DeusData/codebase-memory-mcp/internal/discover"
	"github.com/DeusData/codebase-memory-mcp/internal/store"
)

// heapSnapshot captures memory stats at a pipeline stage boundary.
type heapSnapshot struct {
	Stage      string
	HeapInuse  uint64
	HeapAlloc  uint64
	HeapIdle   uint64
	Sys        uint64
	NumGC      uint32
	PauseTotNs uint64
}

func captureHeap(stage string) heapSnapshot {
	runtime.GC()
	debug.FreeOSMemory()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return heapSnapshot{
		Stage:      stage,
		HeapInuse:  m.HeapInuse,
		HeapAlloc:  m.HeapAlloc,
		HeapIdle:   m.HeapIdle,
		Sys:        m.Sys,
		NumGC:      m.NumGC,
		PauseTotNs: m.PauseTotalNs,
	}
}

// generateSyntheticGoRepo creates a Go repo with the given number of files
// spread across numPkgs packages. Each file has 5 functions with cross-file calls.
func generateSyntheticGoRepo(t *testing.T, dir string, numFiles, numPkgs int) {
	t.Helper()

	filesPerPkg := numFiles / numPkgs
	if filesPerPkg < 1 {
		filesPerPkg = 1
	}

	for pkg := 0; pkg < numPkgs; pkg++ {
		pkgName := fmt.Sprintf("pkg%d", pkg)
		pkgDir := filepath.Join(dir, pkgName)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatal(err)
		}

		for f := 0; f < filesPerPkg; f++ {
			fileIdx := pkg*filesPerPkg + f
			fileName := fmt.Sprintf("file%d.go", f)
			filePath := filepath.Join(pkgDir, fileName)

			var content string
			content += fmt.Sprintf("package %s\n\n", pkgName)

			// 5 functions per file
			for fn := 0; fn < 5; fn++ {
				funcName := fmt.Sprintf("Func%d_%d", fileIdx, fn)
				content += fmt.Sprintf("func %s(x int) int {\n", funcName)

				// Add cross-file calls (3 per function)
				for call := 0; call < 3; call++ {
					targetFile := (fileIdx + call + 1) % numFiles
					targetFn := call % 5
					targetFunc := fmt.Sprintf("Func%d_%d", targetFile, targetFn)
					content += fmt.Sprintf("\t_ = %s(x + %d)\n", targetFunc, call)
				}

				content += "\treturn x\n}\n\n"
			}

			if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}

	// .git marker so discover treats it as a repo root
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestHeapProfileStages generates a synthetic repo, runs the pipeline,
// and captures heap stats at each stage boundary. Outputs a markdown table
// and writes a heap profile for pprof analysis.
func TestHeapProfileStages(t *testing.T) {
	scales := []struct {
		name     string
		numFiles int
		numPkgs  int
	}{
		{"50files", 50, 5},
		{"100files", 100, 5},
	}

	for _, sc := range scales {
		t.Run(sc.name, func(t *testing.T) {
			runHeapProfileForScale(t, sc.numFiles, sc.numPkgs)
		})
	}
}

func runHeapProfileForScale(t *testing.T, numFiles, numPkgs int) {
	t.Helper()

	repoDir := t.TempDir()
	generateSyntheticGoRepo(t, repoDir, numFiles, numPkgs)

	snapshots := make([]heapSnapshot, 0, 8)

	// Capture baseline
	snapshots = append(snapshots, captureHeap("baseline"))

	// Open store
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	snapshots = append(snapshots, captureHeap("post_store_open"))

	// Run the pipeline
	p := New(context.Background(), st, repoDir, discover.ModeFull)
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}

	// The pipeline itself logs heap stats at pre_index, post_definitions,
	// post_calls, post_cleanup via logHeapStats(). We capture our own
	// post-run snapshot here.
	snapshots = append(snapshots, captureHeap("post_run"))

	// Force full GC and capture final state
	runtime.GC()
	debug.FreeOSMemory()
	snapshots = append(snapshots, captureHeap("post_gc"))

	// Print markdown table
	t.Logf("\n## Heap Profile — %d files, %d packages\n", numFiles, numPkgs)
	t.Logf("| Stage | HeapInuse MB | HeapAlloc MB | HeapIdle MB | Sys MB | NumGC | PauseTot ms |")
	t.Logf("|-------|-------------|--------------|-------------|--------|-------|-------------|")
	for _, s := range snapshots {
		t.Logf("| %s | %d | %d | %d | %d | %d | %.1f |",
			s.Stage,
			s.HeapInuse/(1<<20),
			s.HeapAlloc/(1<<20),
			s.HeapIdle/(1<<20),
			s.Sys/(1<<20),
			s.NumGC,
			float64(s.PauseTotNs)/1e6,
		)
	}

	// Growth analysis
	baseline := snapshots[0]
	postRun := snapshots[2]
	postGC := snapshots[3]

	t.Logf("\n## Growth Analysis")
	t.Logf("Baseline HeapInuse: %d MB", baseline.HeapInuse/(1<<20))
	t.Logf("Post-run HeapInuse: %d MB (delta: +%d MB)",
		postRun.HeapInuse/(1<<20),
		(postRun.HeapInuse-baseline.HeapInuse)/(1<<20))
	t.Logf("Post-GC  HeapInuse: %d MB (delta from baseline: +%d MB)",
		postGC.HeapInuse/(1<<20),
		(postGC.HeapInuse-baseline.HeapInuse)/(1<<20))

	if numFiles > 0 {
		perFile := (postRun.HeapInuse - baseline.HeapInuse) / uint64(numFiles)
		t.Logf("HeapInuse per file: ~%d KB", perFile/1024)
	}

	// Node/edge counts
	nc, _ := st.CountNodes(p.ProjectName)
	ec, _ := st.CountEdges(p.ProjectName)
	t.Logf("\nGraph: %d nodes, %d edges", nc, ec)
	if numFiles > 0 {
		t.Logf("Nodes/file: %.1f, Edges/file: %.1f",
			float64(nc)/float64(numFiles),
			float64(ec)/float64(numFiles))
	}

	// Write heap profile
	f, err := os.CreateTemp("", fmt.Sprintf("heap-%dfiles-*.prof", numFiles))
	if err != nil {
		t.Logf("Warning: could not create heap profile temp file: %v", err)
		return
	}
	runtime.GC()
	if err := pprof.WriteHeapProfile(f); err != nil {
		t.Logf("Warning: could not write heap profile: %v", err)
	}
	f.Close()
	t.Logf("Heap profile: %s", f.Name())
}
