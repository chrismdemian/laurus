package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return doc
}

func TestMergeServer_CreatesAndPreserves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".vscode", "mcp.json")

	// Existing file with another server and an unrelated key.
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"inputs":[{"id":"x"}],"servers":{"other":{"command":"o"}}}`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := mergeServer(path, "servers", "laurus", serverEntry(clients["vscode"], true)); err != nil {
		t.Fatal(err)
	}
	doc := readDoc(t, path)
	if _, ok := doc["inputs"]; !ok {
		t.Error("unrelated top-level key was dropped")
	}
	servers := doc["servers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Error("existing server was dropped")
	}
	l := servers["laurus"].(map[string]any)
	if l["command"] != "laurus" || l["type"] != "stdio" {
		t.Errorf("entry = %v", l)
	}
	args := l["args"].([]any)
	if len(args) != 3 || args[2] != "--read-only" {
		t.Errorf("args = %v", args)
	}

	// Fresh file in a missing directory, claude-code shape, no type key.
	p2 := filepath.Join(dir, "proj", ".mcp.json")
	if err := mergeServer(p2, "mcpServers", "laurus", serverEntry(clients["claude-code"], false)); err != nil {
		t.Fatal(err)
	}
	e := readDoc(t, p2)["mcpServers"].(map[string]any)["laurus"].(map[string]any)
	if _, has := e["type"]; has {
		t.Error("claude-code entry must not carry a type key")
	}
	if len(e["args"].([]any)) != 2 {
		t.Errorf("args = %v", e["args"])
	}
}

func TestMergeServer_RefusesNonObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mcp.json")
	if err := os.WriteFile(path, []byte(`[1,2]`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := mergeServer(path, "mcpServers", "laurus", serverEntry(clients["claude-code"], false)); err == nil {
		t.Fatal("expected error for non-object JSON; must not clobber the file")
	}
	if data, _ := os.ReadFile(path); string(data) != `[1,2]` {
		t.Errorf("file was modified: %s", data)
	}
}
