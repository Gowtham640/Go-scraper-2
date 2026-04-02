package scraper

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// Regression: hex-only decoding left <\/style> as literal text inside <style>, so no <table> nodes existed.
func TestExtractSanitizedHTML_DecodesJSEscapesStyleAndTable(t *testing.T) {
	raw := `pageSanitizer.sanitize('<style>body{}<\/style><table border="0" style="width:900px;"><tbody><tr><td>Registration Number:</td><td><strong>X</strong></td></tr></tbody></table>');`
	out, err := ExtractSanitizedHTML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "</style>") {
		t.Fatalf("expected decoded </style>, got %q", out)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Find(`table[style="width:900px;"]`).Length() != 1 {
		snippet := out
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		t.Fatalf("user info table missing after HTML parse, snippet=%q", snippet)
	}
}
