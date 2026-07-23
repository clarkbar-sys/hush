package web

import (
	"bytes"
	"os"
	"testing"

	"github.com/clarkbar-sys/hush/web/internal/webbuild"
)

// TestIndexHTMLInSync guards the split shell: index.html is generated from the
// per-app partials under src/ (see gen.go), and this asserts the committed file
// still equals a fresh assembly. If it fails, a partial was edited without
// regenerating — run `go generate ./web` and commit the regenerated index.html.
func TestIndexHTMLInSync(t *testing.T) {
	want, err := os.ReadFile("index.html")
	if err != nil {
		t.Fatalf("reading index.html: %v", err)
	}
	got, err := webbuild.Assemble("src")
	if err != nil {
		t.Fatalf("assembling from src/: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("index.html is out of sync with the web/src partials "+
			"(assembled %d bytes, committed %d bytes).\n"+
			"Run `go generate ./web` and commit the regenerated index.html.",
			len(got), len(want))
	}
}
