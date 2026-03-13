package userconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCfg(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeCfg: %v", err)
	}
	return path
}

func TestLoad_GlobalOnly(t *testing.T) {
	dir := t.TempDir()
	global := writeCfg(t, dir, "global.json", `{"extra_extensions":{".blade.php":"php"}}`)
	cfg, err := load(global, filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ExtraExtensions[".blade.php"] != "php" {
		t.Errorf("expected .blade.php=php, got %v", cfg.ExtraExtensions)
	}
}

func TestLoad_ProjectOnly(t *testing.T) {
	dir := t.TempDir()
	project := writeCfg(t, dir, "project.json", `{"extra_extensions":{".mjs":"javascript"}}`)
	cfg, err := load(filepath.Join(dir, "missing.json"), project)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ExtraExtensions[".mjs"] != "javascript" {
		t.Errorf("expected .mjs=javascript, got %v", cfg.ExtraExtensions)
	}
}

func TestLoad_Cascade(t *testing.T) {
	dir := t.TempDir()
	global := writeCfg(t, dir, "global.json", `{"extra_extensions":{".ext1":"python",".ext3":"go"}}`)
	project := writeCfg(t, dir, "project.json", `{"extra_extensions":{".ext1":"php",".ext2":"go"}}`)
	cfg, err := load(global, project)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ExtraExtensions[".ext1"] != "php" {
		t.Errorf("project should win: .ext1 want php, got %s", cfg.ExtraExtensions[".ext1"])
	}
	if cfg.ExtraExtensions[".ext2"] != "go" {
		t.Errorf(".ext2 want go, got %s", cfg.ExtraExtensions[".ext2"])
	}
	if cfg.ExtraExtensions[".ext3"] != "go" {
		t.Errorf(".ext3 want go, got %s", cfg.ExtraExtensions[".ext3"])
	}
}

func TestLoad_MissingBothFiles(t *testing.T) {
	dir := t.TempDir()
	cfg, err := load(filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ExtraExtensions) != 0 {
		t.Errorf("expected empty map, got %v", cfg.ExtraExtensions)
	}
}

func TestLoad_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	global := writeCfg(t, dir, "global.json", `{not valid json`)
	cfg, err := load(global, filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ExtraExtensions) != 0 {
		t.Errorf("expected empty on malformed JSON, got %v", cfg.ExtraExtensions)
	}
}

func TestLoad_UnknownLanguage(t *testing.T) {
	dir := t.TempDir()
	project := writeCfg(t, dir, "project.json", `{"extra_extensions":{".foo":"notareallanguage"}}`)
	cfg, err := load(filepath.Join(dir, "missing.json"), project)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.ExtraExtensions[".foo"]; ok {
		t.Error("unknown language should be dropped from result")
	}
}
