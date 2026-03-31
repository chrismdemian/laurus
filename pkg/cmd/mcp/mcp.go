// Package mcp provides the cobra command for running the MCP server.
package mcp

import (
	"log"
	"os"

	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/chrismdemian/laurus/pkg/cmdutil"
	mcpserver "github.com/chrismdemian/laurus/pkg/mcp"
)

// NewCmdMCP returns the `laurus mcp` command group.
func NewCmdMCP(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server for AI assistant integration",
		Long:  "Model Context Protocol server that exposes Canvas LMS tools for AI assistants like Claude Desktop, Symphony, and Cursor.",
	}

	cmd.AddCommand(newCmdServe(f))
	return cmd
}

func newCmdServe(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server on stdio",
		Long:  "Start the MCP server using stdio transport. The server reads JSON-RPC from stdin and writes responses to stdout. All logging goes to stderr.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// MCP protocol uses stdout exclusively — redirect all logs to stderr
			log.SetOutput(os.Stderr)

			srv := mcpserver.NewServer(f)
			return server.ServeStdio(srv)
		},
	}
}
