// Package render converts Canvas HTML content to styled terminal output.
package render

import (
	"bytes"
	"strings"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/charmbracelet/glamour"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// CanvasHTML converts Canvas HTML content to styled terminal output.
// width controls word wrapping; if width <= 0, defaults to 80.
func CanvasHTML(rawHTML string, width int) (string, error) {
	if strings.TrimSpace(rawHTML) == "" {
		return "", nil
	}

	if width <= 0 {
		width = 80
	}

	// Stage 1: Parse HTML
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		// Fallback: try direct html-to-markdown conversion
		return convertAndRender(rawHTML, width)
	}

	// Stage 2: Pre-process DOM
	preprocess(doc)

	// Stage 3: Serialize back to HTML (body children only)
	cleanHTML := renderBody(doc)

	// Stage 4-5: Convert to markdown, then render to terminal
	return convertAndRender(cleanHTML, width)
}

// convertAndRender converts HTML to markdown, then renders to terminal ANSI.
func convertAndRender(htmlContent string, width int) (string, error) {
	// Stage 4: HTML to Markdown
	md, err := htmltomd.ConvertString(htmlContent)
	if err != nil {
		// Fallback: return raw HTML stripped of tags
		return stripTags(htmlContent), nil
	}

	// Stage 5: Markdown to terminal ANSI with word wrapping
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		// Fallback: return plain markdown
		return md, nil
	}
	rendered, err := renderer.Render(md)
	if err != nil {
		return md, nil
	}

	return rendered, nil
}

// preprocess walks the DOM and transforms Canvas-specific elements.
func preprocess(n *html.Node) {
	// Process current node
	switch {
	case isEquationImage(n):
		replaceWithLatex(n)
		return
	case n.DataAtom == atom.Iframe:
		replaceWithVideoLink(n)
		return
	case n.DataAtom == atom.Script || n.DataAtom == atom.Style:
		removeNode(n)
		return
	}

	// Recurse into children (collect first to avoid mutation issues)
	var children []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		children = append(children, c)
	}
	for _, c := range children {
		preprocess(c)
	}
}

// isEquationImage checks if a node is a Canvas equation image.
func isEquationImage(n *html.Node) bool {
	if n.DataAtom != atom.Img {
		return false
	}
	for _, attr := range n.Attr {
		if attr.Key == "class" && strings.Contains(attr.Val, "equation_image") {
			return true
		}
	}
	return false
}

// replaceWithLatex replaces an equation image with its LaTeX content.
func replaceWithLatex(n *html.Node) {
	var latex string
	for _, attr := range n.Attr {
		if attr.Key == "data-equation-content" {
			latex = attr.Val
			break
		}
	}
	if latex == "" {
		// No equation content; try alt text
		for _, attr := range n.Attr {
			if attr.Key == "alt" && attr.Val != "" {
				latex = attr.Val
				break
			}
		}
	}
	if latex == "" {
		latex = "[equation]"
	}

	text := &html.Node{
		Type: html.TextNode,
		Data: "$" + latex + "$",
	}
	n.Parent.InsertBefore(text, n)
	n.Parent.RemoveChild(n)
}

// replaceWithVideoLink replaces an iframe with a video link placeholder.
func replaceWithVideoLink(n *html.Node) {
	var src string
	for _, attr := range n.Attr {
		if attr.Key == "src" {
			src = attr.Val
			break
		}
	}

	label := "[Video]"
	if src != "" {
		label = "[Video: " + src + "]"
	}

	text := &html.Node{
		Type: html.TextNode,
		Data: label,
	}
	n.Parent.InsertBefore(text, n)
	n.Parent.RemoveChild(n)
}

// removeNode removes a node from the DOM tree.
func removeNode(n *html.Node) {
	if n.Parent != nil {
		n.Parent.RemoveChild(n)
	}
}

// renderBody serializes the <body> children back to HTML, avoiding the
// full <html><head><body> wrapper that Go's parser adds.
func renderBody(doc *html.Node) string {
	body := findBody(doc)
	if body == nil {
		// Fallback: render the whole document
		var buf bytes.Buffer
		_ = html.Render(&buf, doc)
		return buf.String()
	}

	var buf bytes.Buffer
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&buf, c)
	}
	return buf.String()
}

// findBody finds the <body> node in the parsed document tree.
func findBody(n *html.Node) *html.Node {
	if n.DataAtom == atom.Body {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findBody(c); found != nil {
			return found
		}
	}
	return nil
}

// stripTags is a last-resort fallback that removes HTML tags.
func stripTags(s string) string {
	var buf bytes.Buffer
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}
