package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/pkg/cmdutil"
)

// clientSpec describes where a client keeps its project-scoped MCP config and
// under which top-level key.
type clientSpec struct {
	file    string // relative to --dir
	rootKey string // "mcpServers" or "servers"
	typed   bool   // VS Code wants "type": "stdio"
}

var clients = map[string]clientSpec{
	"claude-code": {file: ".mcp.json", rootKey: "mcpServers"},
	"cursor":      {file: filepath.Join(".cursor", "mcp.json"), rootKey: "mcpServers"},
	"vscode":      {file: filepath.Join(".vscode", "mcp.json"), rootKey: "servers", typed: true},
}

func newCmdInstall(f *cmdutil.Factory) *cobra.Command {
	var (
		client   string
		dir      string
		readOnly bool
		printIt  bool
		name     string
	)

	cmd := &cobra.Command{
		Use:   "install --client <claude-code|cursor|vscode>",
		Short: "Write this server into a client's project MCP config",
		Long: `Add laurus to a client's project-scoped MCP config file, merging with any
servers already there:

  claude-code  .mcp.json            (mcpServers)
  cursor       .cursor/mcp.json     (mcpServers)
  vscode       .vscode/mcp.json     (servers, type stdio)

Only project files are written; user-level files such as ~/.claude.json are
never touched. Run 'laurus setup' first so the token is in the keychain, or
add CANVAS_TOKEN/CANVAS_URL to the entry's env yourself.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, ok := clients[client]
			if !ok {
				return fmt.Errorf("unknown --client %q (want claude-code, cursor, or vscode)", client)
			}
			entry := serverEntry(spec, readOnly)
			if printIt {
				out, _ := json.MarshalIndent(map[string]any{spec.rootKey: map[string]any{name: entry}}, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return nil
			}
			path := filepath.Join(dir, spec.file)
			if err := mergeServer(path, spec.rootKey, name, entry); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s (%s.%s)\n", path, spec.rootKey, name)
			return nil
		},
	}

	cmd.Flags().StringVar(&client, "client", "", "Client to configure: claude-code, cursor, or vscode")
	cmd.Flags().StringVar(&dir, "dir", ".", "Project directory to write the config into")
	cmd.Flags().StringVar(&name, "name", "laurus", "Server name to register under")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "Register the server with --read-only (no Canvas write tools)")
	cmd.Flags().BoolVar(&printIt, "print", false, "Print the JSON entry instead of writing a file")
	_ = cmd.MarkFlagRequired("client")

	return cmd
}

func serverEntry(spec clientSpec, readOnly bool) map[string]any {
	args := []string{"mcp", "serve"}
	if readOnly {
		args = append(args, "--read-only")
	}
	entry := map[string]any{"command": "laurus", "args": args}
	if spec.typed {
		entry["type"] = "stdio"
	}
	return entry
}

// mergeServer inserts or replaces rootKey.name in the JSON object at path,
// preserving every other key, and creates the file when it does not exist.
func mergeServer(path, rootKey, name string, entry map[string]any) error {
	doc := map[string]any{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(data) > 0 {
			if err := json.Unmarshal(data, &doc); err != nil {
				return fmt.Errorf("%s is not a JSON object; fix it or delete it first: %w", path, err)
			}
		}
	case !os.IsNotExist(err):
		return err
	}

	servers, _ := doc[rootKey].(map[string]any)
	if servers == nil {
		if doc[rootKey] != nil {
			return fmt.Errorf("%s: %q is not an object", path, rootKey)
		}
		servers = map[string]any{}
	}
	servers[name] = entry
	doc[rootKey] = servers

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0644)
}
