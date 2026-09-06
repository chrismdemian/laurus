package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// clientSpec describes where a client keeps its MCP config at each scope and
// how it wants the entry shaped.
type clientSpec struct {
	projectFile string                 // relative to --dir
	userFile    func() (string, error) // absolute path for --scope user
	rootKey     string                 // "mcpServers" or "servers"
	typed       bool                   // include "type": "stdio"
	envRef      string                 // how the client expands an env var in config
}

func homeJoin(parts ...string) func() (string, error) {
	return func() (string, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(append([]string{home}, parts...)...), nil
	}
}

// vscodeUserMCP is VS Code's dedicated user-level MCP file (the `mcp` key in
// settings.json is the older, deprecated location).
func vscodeUserMCP() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json"), nil
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "Code", "User", "mcp.json"), nil
		}
		return filepath.Join(home, "AppData", "Roaming", "Code", "User", "mcp.json"), nil
	default:
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, "Code", "User", "mcp.json"), nil
	}
}

var clients = map[string]clientSpec{
	"claude-code": {
		projectFile: ".mcp.json",
		userFile:    homeJoin(".claude.json"),
		rootKey:     "mcpServers",
		typed:       true,
		envRef:      "${CANVAS_TOKEN}",
	},
	"cursor": {
		projectFile: filepath.Join(".cursor", "mcp.json"),
		userFile:    homeJoin(".cursor", "mcp.json"),
		rootKey:     "mcpServers",
		envRef:      "${env:CANVAS_TOKEN}",
	},
	"vscode": {
		projectFile: filepath.Join(".vscode", "mcp.json"),
		userFile:    vscodeUserMCP,
		rootKey:     "servers",
		typed:       true,
		envRef:      "${env:CANVAS_TOKEN}",
	},
}

func newCmdInstall(f *cmdutil.Factory) *cobra.Command {
	var (
		client   string
		scope    string
		dir      string
		readOnly bool
		noEnv    bool
		printIt  bool
		name     string
	)

	cmd := &cobra.Command{
		Use:   "install --client <claude-code|cursor|vscode> [--scope project|user]",
		Short: "Write this server into a client's MCP config",
		Long: `Add laurus to an MCP client's config, merging with the servers already there.

  client       --scope project (default)   --scope user
  claude-code  .mcp.json                   ~/.claude.json
  cursor       .cursor/mcp.json            ~/.cursor/mcp.json
  vscode       .vscode/mcp.json            <VS Code user dir>/mcp.json

The entry runs "laurus mcp serve" and passes CANVAS_TOKEN through from the
environment using the client's own expansion syntax, so the token is never
written into the file. Use --no-env when the token is already in the OS
keychain from 'laurus setup'. Running it twice is a no-op. User-scope files
are backed up to <file>.bak before they are rewritten.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, ok := clients[client]
			if !ok {
				return fmt.Errorf("unknown --client %q (want claude-code, cursor, or vscode)", client)
			}
			entry := serverEntry(spec, readOnly, !noEnv)
			if printIt {
				out, _ := json.MarshalIndent(map[string]any{spec.rootKey: map[string]any{name: entry}}, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}

			var path string
			var backup bool
			switch scope {
			case "project":
				path = filepath.Join(dir, spec.projectFile)
			case "user":
				p, err := spec.userFile()
				if err != nil {
					return err
				}
				path, backup = p, true
			default:
				return fmt.Errorf("unknown --scope %q (want project or user)", scope)
			}

			changed, err := mergeServer(path, spec.rootKey, name, entry, backup)
			if err != nil {
				return err
			}
			if changed {
				fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s (%s.%s)\n", path, spec.rootKey, name)
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s already up to date (%s.%s)\n", path, spec.rootKey, name)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&client, "client", "", "Client to configure: claude-code, cursor, or vscode")
	cmd.Flags().StringVar(&scope, "scope", "project", "Where to write: project (a file in --dir) or user (the client's own config)")
	cmd.Flags().StringVar(&dir, "dir", ".", "Project directory for --scope project")
	cmd.Flags().StringVar(&name, "name", "laurus", "Server name to register under")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "Register the server with --read-only (no Canvas write tools)")
	cmd.Flags().BoolVar(&noEnv, "no-env", false, "Omit the CANVAS_TOKEN env passthrough (token comes from the keychain)")
	cmd.Flags().BoolVar(&printIt, "print", false, "Print the JSON entry instead of writing a file")
	_ = cmd.MarkFlagRequired("client")

	return cmd
}

func serverEntry(spec clientSpec, readOnly, withEnv bool) map[string]any {
	args := []string{"mcp", "serve"}
	if readOnly {
		args = append(args, "--read-only")
	}
	entry := map[string]any{"command": "laurus", "args": args}
	if spec.typed {
		entry["type"] = "stdio"
	}
	if withEnv {
		entry["env"] = map[string]any{"CANVAS_TOKEN": spec.envRef}
	}
	return entry
}

// mergeServer inserts or replaces rootKey.name in the JSON object at path,
// preserving every other key and every number exactly as written, and
// creates the file when it does not exist. It reports whether the file
// changed; an identical entry leaves the file untouched (idempotent). With
// backup set, an existing file is copied to <path>.bak before being
// rewritten, and the write is atomic (temp file + rename).
func mergeServer(path, rootKey, name string, entry map[string]any, backup bool) (bool, error) {
	doc := map[string]any{}
	data, err := os.ReadFile(path)
	existed := err == nil
	switch {
	case existed:
		if len(bytes.TrimSpace(data)) > 0 {
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.UseNumber() // keep 1788635585552 as-is, never 1.788635585552e+12
			if err := dec.Decode(&doc); err != nil {
				return false, fmt.Errorf("%s is not a JSON object; fix it or delete it first: %w", path, err)
			}
		}
	case !os.IsNotExist(err):
		return false, err
	}

	servers, _ := doc[rootKey].(map[string]any)
	if servers == nil {
		if doc[rootKey] != nil {
			return false, fmt.Errorf("%s: %q is not an object", path, rootKey)
		}
		servers = map[string]any{}
	}
	if existing, ok := servers[name]; ok && jsonEqual(existing, entry) {
		return false, nil
	}
	servers[name] = entry
	doc[rootKey] = servers

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	out = append(out, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	if existed && backup {
		if err := os.WriteFile(path+".bak", data, 0600); err != nil {
			return false, fmt.Errorf("writing backup: %w", err)
		}
	}
	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if backup {
		mode = 0600 // user-level files may hold other servers' secrets
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".laurus-mcp-*.tmp")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return false, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return false, err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		_ = os.Remove(tmpName)
		return false, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return false, err
	}
	return true, nil
}

// jsonEqual compares two values by their canonical JSON encoding, so a
// decoded entry (with json.Number, []any) matches a freshly built one.
func jsonEqual(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(x, y)
}
