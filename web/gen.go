//go:build ignore

// Command gen assembles web/src/* into web/index.html — the single-page shell
// that embed.go bakes into hush-control. Run it after editing any partial:
//
//	go generate ./web
//
// It rewrites index.html in place; commit the regenerated file alongside your
// source change. The in-package TestIndexHTMLInSync fails CI if the two ever
// fall out of sync.
package main

import (
	"log"
	"os"

	"github.com/clarkbar-sys/hush/web/internal/webbuild"
)

func main() {
	out, err := webbuild.Assemble("src")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("index.html", out, 0o644); err != nil {
		log.Fatalf("writing index.html: %v", err)
	}
}
