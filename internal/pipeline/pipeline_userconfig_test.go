package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeusData/codebase-memory-mcp/internal/discover"
	"github.com/DeusData/codebase-memory-mcp/internal/store"
)

func TestPipeline_UserExtension_FullMode(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)

	// Write user config mapping .blade.php -> php
	writeFile(t, filepath.Join(dir, ".codebase-memory.json"), `{
		"extra_extensions": {".blade.php": "php"}
	}`)

	// Create a .blade.php file with minimal PHP content
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "templates", "index.blade.php"), `<?php
function renderIndex() {
	return "hello";
}
`)

	s, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	p := New(context.Background(), s, dir, discover.ModeFull)
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}

	nodes, err := s.FindNodesByLabel(p.ProjectName, "File")
	if err != nil {
		t.Fatalf("FindNodesByLabel: %v", err)
	}

	for _, n := range nodes {
		if strings.HasSuffix(n.FilePath, ".blade.php") {
			lang, _ := n.Properties["language"].(string)
			if lang == "php" {
				return // found a php File node for the blade file
			}
			t.Errorf("found .blade.php File node but language=%q, want php", lang)
			return
		}
	}
	t.Errorf("no File node with .blade.php path found; total File nodes: %d", len(nodes))
}
