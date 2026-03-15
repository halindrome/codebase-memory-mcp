package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DeusData/codebase-memory-mcp/internal/metrics"
	"github.com/DeusData/codebase-memory-mcp/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) handleSearchGraph(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, err := parseArgs(req)
	if err != nil {
		return errResult(err.Error()), nil
	}

	params := &store.SearchParams{
		Label:              getStringArg(args, "label"),
		NamePattern:        getStringArg(args, "name_pattern"),
		QNPattern:          getStringArg(args, "qn_pattern"),
		FilePattern:        getStringArg(args, "file_pattern"),
		Relationship:       getStringArg(args, "relationship"),
		Direction:          getStringArg(args, "direction"),
		MinDegree:          getIntArg(args, "min_degree", -1),
		MaxDegree:          getIntArg(args, "max_degree", -1),
		Limit:              getIntArg(args, "limit", 10),
		Offset:             getIntArg(args, "offset", 0),
		ExcludeEntryPoints: getBoolArg(args, "exclude_entry_points"),
		IncludeConnected:   getBoolArg(args, "include_connected"),
		CaseSensitive:      getBoolArg(args, "case_sensitive"),
	}

	// Parse exclude_labels array; default to excluding Community nodes
	if rawLabels, ok := args["exclude_labels"]; ok {
		if labelArr, ok := rawLabels.([]any); ok {
			for _, l := range labelArr {
				if str, ok := l.(string); ok {
					params.ExcludeLabels = append(params.ExcludeLabels, str)
				}
			}
		}
	} else {
		params.ExcludeLabels = []string{"Community"}
	}

	params.SortBy = getStringArg(args, "sort_by")

	st, err := s.resolveStore(getStringArg(args, "project"))
	if err != nil {
		return errResult(fmt.Sprintf("resolve store: %v", err)), nil
	}

	projName := s.resolveProjectName(getStringArg(args, "project"))
	projects, _ := st.ListProjects()
	if len(projects) > 0 {
		projName = projects[0].Name
	}

	params.Project = projName
	output, searchErr := st.Search(params)
	if searchErr != nil {
		return errResult(fmt.Sprintf("search: %v", searchErr)), nil
	}

	type resultEntry struct {
		Project        string   `json:"project"`
		Name           string   `json:"name"`
		QualifiedName  string   `json:"qualified_name"`
		Label          string   `json:"label"`
		FilePath       string   `json:"file_path"`
		StartLine      int      `json:"start_line"`
		EndLine        int      `json:"end_line"`
		InDegree       int      `json:"in_degree"`
		OutDegree      int      `json:"out_degree"`
		ConnectedNames []string `json:"connected_names,omitempty"`
	}

	results := make([]resultEntry, 0, len(output.Results))
	for _, r := range output.Results {
		results = append(results, resultEntry{
			Project:        projName,
			Name:           r.Node.Name,
			QualifiedName:  r.Node.QualifiedName,
			Label:          r.Node.Label,
			FilePath:       r.Node.FilePath,
			StartLine:      r.Node.StartLine,
			EndLine:        r.Node.EndLine,
			InDegree:       r.InDegree,
			OutDegree:      r.OutDegree,
			ConnectedNames: r.ConnectedNames,
		})
	}

	responseData := map[string]any{
		"total":    output.Total,
		"limit":    params.Limit,
		"offset":   params.Offset,
		"has_more": params.Offset+params.Limit < output.Total,
		"results":  results,
	}
	s.addIndexStatus(responseData)

	result := jsonResult(responseData)
	// Token savings: baseline = sum of unique source file sizes in results page
	// (files the user would have read to find these symbols manually).
	if s.config == nil || s.config.GetBool(store.ConfigMetricsEnabled, true) {
		proj, _ := st.GetProject(projName)
		if proj != nil {
			seenFiles := make(map[string]struct{})
			baselineBytes := 0
			for _, r := range output.Results {
				if r.Node.FilePath == "" {
					continue
				}
				if _, seen := seenFiles[r.Node.FilePath]; !seen {
					seenFiles[r.Node.FilePath] = struct{}{}
					absPath := filepath.Join(proj.RootPath, r.Node.FilePath)
					if fi, statErr := os.Stat(absPath); statErr == nil {
						baselineBytes += int(fi.Size())
					}
				}
			}
			price := priceForConfig(s.config)
			responseJSON, _ := json.Marshal(responseData)
			meta := metrics.CalculateSavings(baselineBytes, len(responseJSON), price)
			result = resultWithMeta(responseData, meta, s.metricsTracker)
		}
	}
	s.addUpdateNotice(result)
	return result, nil
}
