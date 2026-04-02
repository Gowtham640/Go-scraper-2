package scraper

import (
	"strconv"
	"strings"
	"testing"
)

// Regression: portal sanitize() payload uses JS escapes and may include literal newlines; strconv.Unquote fails.
func TestUnescapeJavaScriptString_DecodesLikePortal(t *testing.T) {
	js := "\n\\x3Ctable class=\\x22course_tbl\\x22>\\x3C/tr>"
	out, err := unescapeJavaScriptString(js)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<table class="course_tbl">`) {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestStrconvUnquoteFailsOnLiteralNewline(t *testing.T) {
	s := "a\nb"
	_, err := strconv.Unquote(`"` + s + `"`)
	if err == nil {
		t.Fatal("expected strconv.Unquote to reject raw newline inside quoted literal")
	}
}
