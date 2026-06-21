package tui

import (
	"regexp"
	"strings"
	"testing"
)

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

func TestMarkdownRendererForWidth_CachesByWidth(t *testing.T) {
	a := markdownRendererForWidth(40)
	b := markdownRendererForWidth(40)
	if a != b {
		t.Fatal("expected the same cached renderer for equal widths")
	}
	c := markdownRendererForWidth(60)
	if a == c {
		t.Fatal("expected a different renderer for a different width")
	}
}

func TestRenderMarkdownWidth_NoLeftMargin(t *testing.T) {
	// The default dark style indents every line by a 2-column document margin.
	// We remove it so lines begin at column 0 (the TextView supplies padding).
	out := renderMarkdownWidth("Hello world, this is a plain paragraph.", 80)
	for _, line := range strings.Split(out, "\n") {
		plain := stripANSI(line)
		if strings.TrimSpace(plain) == "" {
			continue
		}
		if strings.HasPrefix(plain, " ") {
			t.Fatalf("rendered line has unexpected leading margin: %q", plain)
		}
	}
}

func TestRenderMarkdownWidth_WrapsToWidth(t *testing.T) {
	long := "word " + strings.Repeat("alpha bravo charlie delta echo ", 6)

	narrow := renderMarkdownWidth(long, 24)
	for _, line := range strings.Split(narrow, "\n") {
		if w := len([]rune(stripANSI(line))); w > 24 {
			t.Fatalf("line exceeds wrap width 24: width=%d line=%q", w, stripANSI(line))
		}
	}

	// A narrower width should produce at least as many lines as a wide one.
	wide := renderMarkdownWidth(long, 200)
	if len(strings.Split(narrow, "\n")) <= len(strings.Split(wide, "\n")) {
		t.Fatalf("expected narrow render (%d lines) to wrap into more lines than wide render (%d lines)",
			len(strings.Split(narrow, "\n")), len(strings.Split(wide, "\n")))
	}
}
