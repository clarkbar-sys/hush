package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleFileServesContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(p, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handleFile(rec, httptest.NewRequest(http.MethodGet, "/file?path="+p, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "hello world" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Errorf("missing Accept-Ranges: %v", rec.Header())
	}
}

func TestHandleFileRangeRequest(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(p, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/file?path="+p, nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	handleFile(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if rec.Body.String() != "2345" {
		t.Fatalf("range body = %q, want 2345", rec.Body.String())
	}
}

func TestHandleFileDownloadDisposition(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handleFile(rec, httptest.NewRequest(http.MethodGet, "/file?path="+p+"&download=1", nil))
	if cd := rec.Header().Get("Content-Disposition"); cd == "" {
		t.Errorf("want attachment disposition, got none")
	}
}

func TestHandleFileErrors(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		path string
		want int
	}{
		{"directory", dir, http.StatusBadRequest},
		{"missing", filepath.Join(dir, "nope"), http.StatusNotFound},
		{"relative", "not/absolute", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handleFile(rec, httptest.NewRequest(http.MethodGet, "/file?path="+c.path, nil))
			if rec.Code != c.want {
				t.Errorf("status = %d, want %d", rec.Code, c.want)
			}
		})
	}
}
