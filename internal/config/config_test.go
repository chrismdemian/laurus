package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPath(t *testing.T) {
	p, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error: %v", err)
	}
	if filepath.Base(p) != "config.toml" {
		t.Errorf("expected filename config.toml, got %s", filepath.Base(p))
	}
	parent := filepath.Base(filepath.Dir(p))
	if parent != "laurus" {
		t.Errorf("expected parent dir laurus, got %s", parent)
	}
}

func TestLoadFrom_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.toml")

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() error: %v", err)
	}

	if cfg.Theme != "auto" {
		t.Errorf("Theme = %q, want 'auto'", cfg.Theme)
	}
	if cfg.SyncDir != "~/School" {
		t.Errorf("SyncDir = %q, want '~/School'", cfg.SyncDir)
	}
	if cfg.CanvasURL != "" {
		t.Errorf("CanvasURL = %q, want empty", cfg.CanvasURL)
	}

	// File should have been created
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected config file to be created")
	}
}

func TestLoadFrom_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `canvas_url = "https://q.utoronto.ca"
sync_dir = "~/Documents/School"
theme = "dark"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() error: %v", err)
	}

	if cfg.CanvasURL != "https://q.utoronto.ca" {
		t.Errorf("CanvasURL = %q", cfg.CanvasURL)
	}
	if cfg.SyncDir != "~/Documents/School" {
		t.Errorf("SyncDir = %q", cfg.SyncDir)
	}
	if cfg.Theme != "dark" {
		t.Errorf("Theme = %q", cfg.Theme)
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	original := &Config{
		CanvasURL: "https://canvas.example.com",
		SyncDir:   "~/MySchool",
		Theme:     "light",
		Aliases:   map[string]string{"cs": "CSC108"},
	}

	if err := SaveTo(original, path); err != nil {
		t.Fatalf("SaveTo() error: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() error: %v", err)
	}

	if loaded.CanvasURL != original.CanvasURL {
		t.Errorf("CanvasURL = %q, want %q", loaded.CanvasURL, original.CanvasURL)
	}
	if loaded.SyncDir != original.SyncDir {
		t.Errorf("SyncDir = %q, want %q", loaded.SyncDir, original.SyncDir)
	}
	if loaded.Theme != original.Theme {
		t.Errorf("Theme = %q, want %q", loaded.Theme, original.Theme)
	}
	if loaded.Aliases["cs"] != "CSC108" {
		t.Errorf("Aliases[cs] = %q, want CSC108", loaded.Aliases["cs"])
	}
}

func TestLoadFrom_PartialConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `canvas_url = "https://canvas.example.com"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() error: %v", err)
	}

	if cfg.CanvasURL != "https://canvas.example.com" {
		t.Errorf("CanvasURL = %q", cfg.CanvasURL)
	}
	if cfg.Theme != "auto" {
		t.Errorf("Theme = %q, want 'auto' (default)", cfg.Theme)
	}
	if cfg.SyncDir != "~/School" {
		t.Errorf("SyncDir = %q, want '~/School' (default)", cfg.SyncDir)
	}
}

func TestLoadFrom_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() error: %v", err)
	}

	if cfg.Theme != "auto" {
		t.Errorf("Theme = %q, want 'auto'", cfg.Theme)
	}
	if cfg.SyncDir != "~/School" {
		t.Errorf("SyncDir = %q, want '~/School'", cfg.SyncDir)
	}
}
