package userconfig

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/DeusData/codebase-memory-mcp/internal/lang"
)

// UserConfig holds user-defined configuration overlays.
type UserConfig struct {
	// ExtraExtensions maps file extensions (with leading dot) to language identifiers.
	// Example: {".blade.php": "php", ".mjs": "javascript"}
	ExtraExtensions map[string]string `json:"extra_extensions"`
}

// Load reads user-defined config from the global config dir and the project root,
// merging them with project config taking precedence. It is fail-open: missing
// files are silently ignored; malformed JSON or unknown language values produce
// a warning log and are skipped.
func Load(repoPath string) (*UserConfig, error) {
	globalPath := ""
	if configDir, err := os.UserConfigDir(); err == nil {
		globalPath = filepath.Join(configDir, "codebase-memory-mcp", "config.json")
	} else {
		log.Printf("[userconfig] warning: could not determine user config dir: %v", err)
	}
	projectPath := filepath.Join(repoPath, ".codebase-memory.json")
	return load(globalPath, projectPath)
}

// load is the testable core: accepts explicit paths so tests can inject temp dirs.
func load(globalPath, projectPath string) (*UserConfig, error) {
	merged := &UserConfig{ExtraExtensions: map[string]string{}}

	for _, path := range []string{globalPath, projectPath} {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			log.Printf("[userconfig] warning: could not read %s: %v", path, err)
			continue
		}
		var cfg UserConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			log.Printf("[userconfig] warning: malformed JSON in %s: %v", path, err)
			continue
		}
		for ext, langStr := range cfg.ExtraExtensions {
			merged.ExtraExtensions[ext] = langStr
		}
	}

	// Validate and remove unknown language values.
	for ext, langStr := range merged.ExtraExtensions {
		if lang.ForLanguage(lang.Language(langStr)) == nil {
			log.Printf("[userconfig] warning: unknown language %q for extension %q — skipping", langStr, ext)
			delete(merged.ExtraExtensions, ext)
		}
	}

	return merged, nil
}
