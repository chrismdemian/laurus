package render

import (
	"strings"
	"testing"
)

func TestCanvasHTML_SimpleHTML(t *testing.T) {
	result, err := CanvasHTML("<p>Hello <strong>world</strong></p>", 80)
	if err != nil {
		t.Fatalf("CanvasHTML error: %v", err)
	}
	if !strings.Contains(result, "Hello") || !strings.Contains(result, "world") {
		t.Errorf("result = %q, want to contain 'Hello' and 'world'", result)
	}
}

func TestCanvasHTML_EquationImage(t *testing.T) {
	html := `<p>The formula is <img class="equation_image" data-equation-content="x^2+y^2=z^2" alt="Pythagorean theorem"> in math.</p>`
	result, err := CanvasHTML(html, 80)
	if err != nil {
		t.Fatalf("CanvasHTML error: %v", err)
	}
	if !strings.Contains(result, "$x^2+y^2=z^2$") {
		t.Errorf("result = %q, want to contain LaTeX equation", result)
	}
}

func TestCanvasHTML_EquationImage_FallbackToAlt(t *testing.T) {
	html := `<p><img class="equation_image" alt="e=mc^2"></p>`
	result, err := CanvasHTML(html, 80)
	if err != nil {
		t.Fatalf("CanvasHTML error: %v", err)
	}
	if !strings.Contains(result, "$e=mc^2$") {
		t.Errorf("result = %q, want to contain $e=mc^2$", result)
	}
}

func TestCanvasHTML_Iframe(t *testing.T) {
	html := `<p>Watch this:</p><iframe src="https://youtube.com/embed/xyz"></iframe>`
	result, err := CanvasHTML(html, 80)
	if err != nil {
		t.Fatalf("CanvasHTML error: %v", err)
	}
	if !strings.Contains(result, "[Video: https://youtube.com/embed/xyz]") {
		t.Errorf("result = %q, want video link placeholder", result)
	}
}

func TestCanvasHTML_IframeNoSrc(t *testing.T) {
	html := `<iframe></iframe>`
	result, err := CanvasHTML(html, 80)
	if err != nil {
		t.Fatalf("CanvasHTML error: %v", err)
	}
	if !strings.Contains(result, "[Video]") {
		t.Errorf("result = %q, want [Video] placeholder", result)
	}
}

func TestCanvasHTML_ScriptStripped(t *testing.T) {
	html := `<p>Content</p><script>alert('xss')</script>`
	result, err := CanvasHTML(html, 80)
	if err != nil {
		t.Fatalf("CanvasHTML error: %v", err)
	}
	if strings.Contains(result, "alert") || strings.Contains(result, "script") {
		t.Errorf("result = %q, want script content removed", result)
	}
	if !strings.Contains(result, "Content") {
		t.Errorf("result = %q, want to contain 'Content'", result)
	}
}

func TestCanvasHTML_StyleStripped(t *testing.T) {
	html := `<style>.red{color:red}</style><p>Visible</p>`
	result, err := CanvasHTML(html, 80)
	if err != nil {
		t.Fatalf("CanvasHTML error: %v", err)
	}
	if strings.Contains(result, ".red") {
		t.Errorf("result = %q, want style content removed", result)
	}
	if !strings.Contains(result, "Visible") {
		t.Errorf("result = %q, want to contain 'Visible'", result)
	}
}

func TestCanvasHTML_CodeBlock(t *testing.T) {
	html := `<pre><code>func main() {
    fmt.Println("hello")
}</code></pre>`
	result, err := CanvasHTML(html, 80)
	if err != nil {
		t.Fatalf("CanvasHTML error: %v", err)
	}
	if !strings.Contains(result, "func main()") {
		t.Errorf("result = %q, want code block content", result)
	}
}

func TestCanvasHTML_EmptyString(t *testing.T) {
	result, err := CanvasHTML("", 80)
	if err != nil {
		t.Fatalf("CanvasHTML error: %v", err)
	}
	if result != "" {
		t.Errorf("result = %q, want empty string", result)
	}
}

func TestCanvasHTML_WhitespaceOnly(t *testing.T) {
	result, err := CanvasHTML("   \n  \t  ", 80)
	if err != nil {
		t.Fatalf("CanvasHTML error: %v", err)
	}
	if result != "" {
		t.Errorf("result = %q, want empty string", result)
	}
}

func TestCanvasHTML_MixedContent(t *testing.T) {
	html := `
		<h2>Assignment Overview</h2>
		<p>Implement a <strong>binary search tree</strong> with the following methods:</p>
		<ul>
			<li>insert(key)</li>
			<li>search(key)</li>
			<li>delete(key)</li>
		</ul>
		<p>The complexity is <img class="equation_image" data-equation-content="O(\log n)">.</p>
		<h3>Resources</h3>
		<p>Watch the lecture: <iframe src="https://youtube.com/embed/abc"></iframe></p>
		<pre><code>class BST:
    def insert(self, key):
        pass</code></pre>
	`
	result, err := CanvasHTML(html, 80)
	if err != nil {
		t.Fatalf("CanvasHTML error: %v", err)
	}

	checks := []string{
		"Assignment Overview",
		"binary search tree",
		"insert(key)",
		"$O(\\log n)$",
		"[Video: https://youtube.com/embed/abc]",
		"class BST",
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("result missing %q", check)
		}
	}
}

func TestCanvasHTML_DefaultWidth(t *testing.T) {
	// Width <= 0 should not panic
	result, err := CanvasHTML("<p>Test</p>", 0)
	if err != nil {
		t.Fatalf("CanvasHTML error: %v", err)
	}
	if !strings.Contains(result, "Test") {
		t.Errorf("result = %q, want to contain 'Test'", result)
	}
}

func TestCanvasHTML_Links(t *testing.T) {
	html := `<p>See <a href="https://example.com">the docs</a> for details.</p>`
	result, err := CanvasHTML(html, 80)
	if err != nil {
		t.Fatalf("CanvasHTML error: %v", err)
	}
	if !strings.Contains(result, "the docs") {
		t.Errorf("result = %q, want link text", result)
	}
}
