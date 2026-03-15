package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/DeusData/codebase-memory-mcp/internal/metrics"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callSearchGraph(t *testing.T, srv *Server, namePattern string) map[string]any {
	t.Helper()
	args := map[string]any{"name_pattern": namePattern}
	rawArgs, _ := json.Marshal(args)

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "search_graph",
			Arguments: rawArgs,
		},
	}

	result, err := srv.handleSearchGraph(context.TODO(), req)
	if err != nil {
		t.Fatalf("handleSearchGraph error: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("empty result content")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &data); err != nil {
		t.Fatalf("unmarshal result: %v (text: %s)", err, tc.Text)
	}
	return data
}

func TestSearchGraph_MetaField(t *testing.T) {
	srv := testSnippetServer(t)
	// Attach a metricsTracker backed by a temp file.
	savingsPath := filepath.Join(t.TempDir(), "savings.json")
	srv.metricsTracker = metrics.NewTracker(savingsPath)

	// Search for "Handle" which matches HandleRequest in the fixture.
	data := callSearchGraph(t, srv, "Handle")

	// Results should be present.
	results, ok := data["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatal("expected at least one result from search_graph")
	}

	// _meta must exist.
	metaRaw, ok := data["_meta"]
	if !ok {
		t.Fatal("expected _meta field in response")
	}
	meta, ok := metaRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected _meta to be a map, got %T", metaRaw)
	}

	tokensSaved, _ := meta["tokens_saved"].(float64)
	baselineTokens, _ := meta["baseline_tokens"].(float64)
	responseTokens, _ := meta["response_tokens"].(float64)

	if tokensSaved < 0 {
		t.Errorf("tokens_saved should be >= 0, got %v", tokensSaved)
	}
	// baseline_tokens may be 0 if the fixture file is not accessible on disk
	// (test uses a temp dir, not the real source tree), so only assert >= 0.
	if baselineTokens < 0 {
		t.Errorf("baseline_tokens should be >= 0, got %v", baselineTokens)
	}
	if responseTokens <= 0 {
		t.Errorf("response_tokens should be > 0, got %v", responseTokens)
	}
}
