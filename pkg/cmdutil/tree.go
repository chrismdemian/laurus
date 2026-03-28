package cmdutil

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/chrismdemian/laurus/internal/iostreams"
)

// TreeNode represents a node in a tree with optional styling and detail text.
type TreeNode struct {
	Label    string
	Style    lipgloss.Style
	Detail   string // extra info shown after the label (e.g., size, due date)
	Children []*TreeNode
}

// RenderTree writes a tree structure to ios.Out using box-drawing characters.
func RenderTree(ios *iostreams.IOStreams, roots []*TreeNode) error {
	for _, root := range roots {
		// Print root label
		label := root.Style.Render(root.Label)
		line := label
		if root.Detail != "" {
			line += "  " + root.Detail
		}
		if _, err := fmt.Fprintln(ios.Out, line); err != nil {
			return err
		}

		// Print root's children with tree connectors
		for i, child := range root.Children {
			if err := renderNode(ios, child, "", i == len(root.Children)-1); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderNode(ios *iostreams.IOStreams, node *TreeNode, prefix string, isLast bool) error {
	var connector string
	if isLast {
		connector = "└── "
	} else {
		connector = "├── "
	}

	label := node.Style.Render(node.Label)
	line := prefix + connector + label
	if node.Detail != "" {
		line += "  " + node.Detail
	}

	if _, err := fmt.Fprintln(ios.Out, line); err != nil {
		return err
	}

	// Build child prefix
	var childPrefix string
	if isLast {
		childPrefix = prefix + "    "
	} else {
		childPrefix = prefix + "│   "
	}

	for i, child := range node.Children {
		if err := renderNode(ios, child, childPrefix, i == len(node.Children)-1); err != nil {
			return err
		}
	}

	return nil
}

// FormatFileSize returns a human-readable file size string.
func FormatFileSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)

	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
