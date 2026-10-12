package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigValidates(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	root := filepath.Join(dir, "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `
repo: myservice
root: ` + root + `
concurrency: 0
qdrant:
  host: localhost
  port: 6334
  collection: code_chunks
  dim: 768
ollama:
  base_url: http://localhost:11434
  model: nomic-embed-text
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Concurrency != 4 {
		t.Fatalf("concurrency default = %d", cfg.Concurrency)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Root != abs {
		t.Fatalf("root = %s, want %s", cfg.Root, abs)
	}
}

func TestLoadConfigMissingFields(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("repo: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadConfig(cfgPath)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "missing required config") {
		t.Fatalf("unexpected error: %v", err)
	}
}
