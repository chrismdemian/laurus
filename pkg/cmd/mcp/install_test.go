package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		t.Fatalf("%s is not valid JSON: %v", path, err)
	}
	return doc
}

func TestMergeServer_CreatesAndPreserves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".vscode", "mcp.json")

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"inputs":[{"id":"x"}],"servers":{"other":{"command":"o"}}}`), 0644); err != nil {
		t.Fatal(err)
	}

	changed, err := mergeServer(path, "servers", "laurus", serverEntry(clients["vscode"], true, true), false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("first write must report changed")
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
	if env := l["env"].(map[string]any); env["CANVAS_TOKEN"] != "${env:CANVAS_TOKEN}" {
		t.Errorf("vscode env = %v, want ${env:CANVAS_TOKEN}", env)
	}
	if args := l["args"].([]any); len(args) != 3 || args[2] != "--read-only" {
		t.Errorf("args = %v", args)
	}

	// Fresh file in a missing directory, cursor shape: no type key, cursor env syntax.
	p2 := filepath.Join(dir, "proj", ".cursor", "mcp.json")
	if _, err := mergeServer(p2, "mcpServers", "laurus", serverEntry(clients["cursor"], false, true), false); err != nil {
		t.Fatal(err)
	}
	e := readDoc(t, p2)["mcpServers"].(map[string]any)["laurus"].(map[string]any)
	if _, has := e["type"]; has {
		t.Error("cursor entry must not carry a type key")
	}
	if e["env"].(map[string]any)["CANVAS_TOKEN"] != "${env:CANVAS_TOKEN}" {
		t.Errorf("cursor env = %v", e["env"])
	}

	// --no-env form has no env key at all.
	e3 := serverEntry(clients["claude-code"], false, false)
	if _, has := e3["env"]; has {
		t.Error("no-env entry must not carry env")
	}
	if e3["env"] == nil && clients["claude-code"].envRef != "${CANVAS_TOKEN}" {
		t.Errorf("claude-code envRef = %q", clients["claude-code"].envRef)
	}
}

func TestMergeServer_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mcp.json")
	entry := serverEntry(clients["claude-code"], true, true)

	if changed, err := mergeServer(path, "mcpServers", "laurus", entry, false); err != nil || !changed {
		t.Fatalf("first: changed=%v err=%v", changed, err)
	}
	first, _ := os.ReadFile(path)
	info1, _ := os.Stat(path)

	changed, err := mergeServer(path, "mcpServers", "laurus", entry, true)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("second identical write must report unchanged")
	}
	second, _ := os.ReadFile(path)
	if !bytes.Equal(first, second) {
		t.Errorf("file changed on repeat:\n%s\n---\n%s", first, second)
	}
	if info2, _ := os.Stat(path); !info2.ModTime().Equal(info1.ModTime()) {
		t.Error("file was rewritten on repeat")
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Error("no backup should be made when nothing changes")
	}

	// A different entry does change it, and the old bytes land in .bak.
	changed, err = mergeServer(path, "mcpServers", "laurus", serverEntry(clients["claude-code"], false, true), true)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if !bytes.Equal(bak, first) {
		t.Error("backup does not hold the previous contents")
	}
}

// ~/.claude.json holds large integers (timestamps) and other servers with
// secrets; a merge must reproduce them byte-for-byte in value.
func TestMergeServer_UserClaudeJSONPreservesNumbersAndServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	original := `{
  "firstStartTime": "2026-01-01T00:00:00Z",
  "changelogLastFetched": 1788635585552,
  "cachedUsageUtilization": {"ratio": 0.5, "big": 12345678901234567890},
  "mcpServers": {
    "context7": {"type": "stdio", "command": "npx", "args": ["-y", "ctx7"], "env": {"API_KEY": "secret-value"}}
  },
  "hasCompletedOnboarding": true
}
`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := mergeServer(path, "mcpServers", "laurus", serverEntry(clients["claude-code"], false, true), true); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	s := string(out)
	for _, want := range []string{`1788635585552`, `12345678901234567890`, `"secret-value"`, `"hasCompletedOnboarding": true`, `"ratio": 0.5`} {
		if !strings.Contains(s, want) {
			t.Errorf("rewritten file lost %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, "e+") {
		t.Errorf("a number was rewritten in exponent form:\n%s", s)
	}
	doc := readDoc(t, path)
	servers := doc["mcpServers"].(map[string]any)
	if _, ok := servers["context7"]; !ok {
		t.Error("existing server dropped")
	}
	if servers["laurus"].(map[string]any)["env"].(map[string]any)["CANVAS_TOKEN"] != "${CANVAS_TOKEN}" {
		t.Errorf("claude-code env = %v", servers["laurus"])
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0600 {
		t.Errorf("mode = %o, want 0600 preserved", info.Mode().Perm())
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Error("user-scope write must leave a .bak")
	}
}

func TestMergeServer_RefusesNonObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mcp.json")
	if err := os.WriteFile(path, []byte(`[1,2]`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := mergeServer(path, "mcpServers", "laurus", serverEntry(clients["claude-code"], false, true), true); err == nil {
		t.Fatal("expected error for non-object JSON; must not clobber the file")
	}
	if data, _ := os.ReadFile(path); string(data) != `[1,2]` {
		t.Errorf("file was modified: %s", data)
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Error("no backup should be written when the merge is refused")
	}
}

func TestInstallCommand_UserScopeWritesClientFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))

	cmd := newCmdInstall(nil)
	cmd.SetArgs([]string{"--client", "claude-code", "--scope", "user", "--read-only"})
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	doc := readDoc(t, filepath.Join(home, ".claude.json"))
	e := doc["mcpServers"].(map[string]any)["laurus"].(map[string]any)
	if e["type"] != "stdio" || e["env"].(map[string]any)["CANVAS_TOKEN"] != "${CANVAS_TOKEN}" {
		t.Errorf("entry = %v", e)
	}

	// Idempotent through the command as well.
	cmd2 := newCmdInstall(nil)
	errOut := new(bytes.Buffer)
	cmd2.SetArgs([]string{"--client", "claude-code", "--scope", "user", "--read-only"})
	cmd2.SetErr(errOut)
	if err := cmd2.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "already up to date") {
		t.Errorf("second run reported: %q", errOut.String())
	}

	// vscode user file resolves under the sandboxed dirs, never the real home.
	p, err := vscodeUserMCP()
	if err != nil || !strings.HasPrefix(p, home) {
		t.Errorf("vscodeUserMCP = %q, %v; want under %q", p, err, home)
	}

	bad := newCmdInstall(nil)
	bad.SetArgs([]string{"--client", "cursor", "--scope", "global"})
	bad.SetErr(new(bytes.Buffer))
	bad.SetOut(new(bytes.Buffer))
	if err := bad.Execute(); err == nil {
		t.Error("unknown scope must be an error")
	}
}
