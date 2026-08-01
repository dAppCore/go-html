// SPDX-Licence-Identifier: EUPL-1.2

package markdown_test

import (
	"bytes"
	"strings"
	"testing"

	core "dappco.re/go"
	coreio "dappco.re/go/io"

	"dappco.re/go/render/display/tui/markdown"
)

// render builds a Renderer with opts and renders md, failing the test on any
// error from either step — the shared plumbing every option test below runs
// through.
func render(t *testing.T, md string, opts ...markdown.Option) string {
	t.Helper()
	r, err := markdown.New(opts...)
	if err != nil {
		t.Fatalf("New(...): %v", err)
	}
	out, err := r.Render(md)
	if err != nil {
		t.Fatalf("Render(%q): %v", md, err)
	}
	return out
}

// ptr returns a pointer to v. ansi.StylePrimitive's fields are all pointers,
// so a zero-value field reads as "unset" rather than as false/"".
//
//go:fix inline
func ptr[T any](v T) *T { return new(v) }

// TestStyleConfig proves StyleConfig, StyleBlock and StylePrimitive are
// genuine aliases of the glamour/ansi structs, not shadow types: a composite
// literal built from outside the package nests three levels deep and
// round-trips its exported fields unchanged.
func TestStyleConfig(t *testing.T) {
	cfg := markdown.StyleConfig{
		H1: markdown.StyleBlock{
			StylePrimitive: markdown.StylePrimitive{
				Color: new("#D97757"),
				Bold:  new(true),
			},
		},
	}

	if got := cfg.H1.Color; got == nil || *got != "#D97757" {
		t.Fatalf("StyleConfig.H1.Color = %v, want %q", got, "#D97757")
	}
	if got := cfg.H1.Bold; got == nil || !*got {
		t.Fatalf("StyleConfig.H1.Bold = %v, want true", got)
	}
}

// TestWithStyles proves a custom StyleConfig actually reaches the renderer:
// colouring H1 changes the rendered heading versus the package's unstyled
// default.
func TestWithStyles(t *testing.T) {
	const md = "# Heading"

	cfg := markdown.StyleConfig{
		H1: markdown.StyleBlock{
			StylePrimitive: markdown.StylePrimitive{
				Color: new("#D97757"),
				Bold:  new(true),
			},
		},
	}

	styled := render(t, md, markdown.WithStyles(cfg))
	plain := render(t, md)

	if styled == plain {
		t.Fatalf("Render(%q) with WithStyles(custom H1 colour) = %q, want it to differ from the unstyled default", md, styled)
	}
}

// TestWithStylePath proves the documented fallback: a stylePath that isn't a
// standard style name is read as a JSON file instead of erroring.
func TestWithStylePath(t *testing.T) {
	const md = "# Heading"

	dir := t.TempDir()
	path := core.Path(dir, "custom-style.json")
	if err := coreio.Local.Write(path, `{"h1":{"color":"#D97757","bold":true}}`); err != nil {
		t.Fatalf("Write(%q): %v", path, err)
	}

	styled := render(t, md, markdown.WithStylePath(path))
	plain := render(t, md)

	if styled == plain {
		t.Fatalf("Render(%q) with WithStylePath(%q) = %q, want it to differ from the unstyled default (file fallback didn't load)", md, path, styled)
	}
}

// TestWithStylesFromJSONBytes proves a style parses straight from an
// in-memory JSON document, with no file involved.
func TestWithStylesFromJSONBytes(t *testing.T) {
	const md = "# Heading"

	styled := render(t, md, markdown.WithStylesFromJSONBytes([]byte(`{"h1":{"color":"#D97757","bold":true}}`)))
	plain := render(t, md)

	if styled == plain {
		t.Fatalf("Render(%q) with WithStylesFromJSONBytes(...) = %q, want it to differ from the unstyled default", md, styled)
	}
}

// TestWithStylesFromJSONFile proves a style loads from a JSON file on disk —
// unlike WithStylePath, it never tries a standard-style-name lookup first.
func TestWithStylesFromJSONFile(t *testing.T) {
	const md = "# Heading"

	dir := t.TempDir()
	path := core.Path(dir, "style.json")
	if err := coreio.Local.Write(path, `{"h1":{"color":"#5A8F7B","bold":true}}`); err != nil {
		t.Fatalf("Write(%q): %v", path, err)
	}

	styled := render(t, md, markdown.WithStylesFromJSONFile(path))
	plain := render(t, md)

	if styled == plain {
		t.Fatalf("Render(%q) with WithStylesFromJSONFile(%q) = %q, want it to differ from the unstyled default", md, path, styled)
	}
}

// TestWithEnvironmentConfig proves the option reads GLAMOUR_STYLE at
// render-build time: switching the environment variable between two
// standard styles changes the output.
func TestWithEnvironmentConfig(t *testing.T) {
	const md = "# Heading"

	t.Setenv("GLAMOUR_STYLE", "light")
	light := render(t, md, markdown.WithEnvironmentConfig())

	t.Setenv("GLAMOUR_STYLE", "dark")
	dark := render(t, md, markdown.WithEnvironmentConfig())

	if light == dark {
		t.Fatalf("WithEnvironmentConfig() rendered %q for both GLAMOUR_STYLE=light and GLAMOUR_STYLE=dark, want different output", light)
	}
}

// TestWithBaseURL proves relative links are rewritten against baseURL —
// absent the option, the resolved absolute URL never appears in the output.
func TestWithBaseURL(t *testing.T) {
	const md = "[docs](/relative)"
	const want = "https://example.com/relative"

	based := render(t, md, markdown.WithBaseURL("https://example.com"))
	if !strings.Contains(based, want) {
		t.Fatalf("Render(%q) with WithBaseURL(%q) = %q, want it to contain %q", md, "https://example.com", based, want)
	}

	plain := render(t, md)
	if strings.Contains(plain, want) {
		t.Fatalf("Render(%q) without WithBaseURL unexpectedly contains %q", md, want)
	}
}

// TestWithChromaFormatter proves the formatter name reaches chroma: the same
// themed code block renders different escape sequences for a 16-colour
// versus a 256-colour terminal formatter. The "dark" standard style carries
// a Chroma palette on its CodeBlock, so highlighting is already engaged —
// WithChromaFormatter only changes which encoding it renders through.
func TestWithChromaFormatter(t *testing.T) {
	const md = "```go\nx := 1\n```\n"

	term256 := render(t, md, markdown.WithStandardStyle("dark"), markdown.WithChromaFormatter("terminal256"))
	term16 := render(t, md, markdown.WithStandardStyle("dark"), markdown.WithChromaFormatter("terminal16"))

	if term256 == term16 {
		t.Fatal(`Render() of a code block with WithChromaFormatter("terminal256") vs "terminal16" produced identical output, want different ANSI encodings`)
	}
}

// TestWithInlineTableLinks proves the option moves a table cell's link href
// from a footnote list below the table to inline within the cell.
func TestWithInlineTableLinks(t *testing.T) {
	const md = "| Name | Link |\n| --- | --- |\n| A | [Example](https://example.com/) |\n"
	const footnoteMarker = "[1]:"

	footer := render(t, md, markdown.WithInlineTableLinks(false))
	if !strings.Contains(footer, footnoteMarker) {
		t.Fatalf("Render() with WithInlineTableLinks(false) = %q, want it to contain the footnote marker %q", footer, footnoteMarker)
	}

	inline := render(t, md, markdown.WithInlineTableLinks(true))
	if strings.Contains(inline, footnoteMarker) {
		t.Fatalf("Render() with WithInlineTableLinks(true) = %q, want no footnote marker %q (the link renders inline instead)", inline, footnoteMarker)
	}
}

// TestWithOptions proves a folded bundle applies identically to passing the
// same options directly, and that the bundle isn't a no-op.
func TestWithOptions(t *testing.T) {
	const md = "line one\nline two"

	direct := render(t, md, markdown.WithWordWrap(20), markdown.WithPreservedNewLines())
	folded := render(t, md, markdown.WithOptions(markdown.WithWordWrap(20), markdown.WithPreservedNewLines()))
	plain := render(t, md)

	if folded != direct {
		t.Fatalf("Render() via WithOptions(...) = %q, want the same as passing the options directly = %q", folded, direct)
	}
	if folded == plain {
		t.Fatal("Render() via WithOptions(...) equals the bare default, want the folded options to take effect")
	}
}

// TestRender proves the one-shot Render builds a styled document straight
// from a stylePath, with no Renderer ever crossing the caller's code.
func TestRender(t *testing.T) {
	dark, err := markdown.Render("# Heading", "dark")
	if err != nil {
		t.Fatalf(`Render(%q, "dark"): %v`, "# Heading", err)
	}
	light, err := markdown.Render("# Heading", "light")
	if err != nil {
		t.Fatalf(`Render(%q, "light"): %v`, "# Heading", err)
	}

	if dark == light {
		t.Fatalf("Render(..., %q) = Render(..., %q), want different styling between standard styles", "dark", "light")
	}
}

// TestRenderBytes proves the []byte-in-[]byte-out sibling of Render behaves
// the same way: two different standard styles render two different results.
func TestRenderBytes(t *testing.T) {
	dark, err := markdown.RenderBytes([]byte("# Heading"), "dark")
	if err != nil {
		t.Fatalf(`RenderBytes(%q, "dark"): %v`, "# Heading", err)
	}
	light, err := markdown.RenderBytes([]byte("# Heading"), "light")
	if err != nil {
		t.Fatalf(`RenderBytes(%q, "light"): %v`, "# Heading", err)
	}

	if bytes.Equal(dark, light) {
		t.Fatalf("RenderBytes(..., %q) = RenderBytes(..., %q), want different styling between standard styles", "dark", "light")
	}
}

// TestRenderWithEnvironmentConfig proves the one-shot form reads
// GLAMOUR_STYLE the same way WithEnvironmentConfig does.
func TestRenderWithEnvironmentConfig(t *testing.T) {
	t.Setenv("GLAMOUR_STYLE", "light")
	light, err := markdown.RenderWithEnvironmentConfig("# Heading")
	if err != nil {
		t.Fatalf("RenderWithEnvironmentConfig(%q): %v", "# Heading", err)
	}

	t.Setenv("GLAMOUR_STYLE", "dark")
	dark, err := markdown.RenderWithEnvironmentConfig("# Heading")
	if err != nil {
		t.Fatalf("RenderWithEnvironmentConfig(%q): %v", "# Heading", err)
	}

	if light == dark {
		t.Fatalf("RenderWithEnvironmentConfig() rendered %q for both GLAMOUR_STYLE=light and GLAMOUR_STYLE=dark, want different output", light)
	}
}
