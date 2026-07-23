// Package webbuild assembles the console's single-page index.html from the
// per-app source partials under web/src. index.html is a build artifact: the
// real content lives in small, app-scoped files (the launcher, the payphone
// app, the fleet console, and the global boot / lost-connection chrome), and
// this package concatenates them — in the order named by web/src/manifest —
// into the one file go:embed ships.
//
// Two callers share it. web/gen.go regenerates index.html (`go generate ./web`),
// and an in-package test asserts the committed index.html still matches a fresh
// assembly, so a partial edited without regenerating can't drift past CI.
package webbuild

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ManifestName is the ordered list of partials, relative to the source root.
const ManifestName = "manifest"

// Assemble concatenates every partial listed in srcDir/manifest, in order, and
// returns the assembled index.html. The output is a pure byte-for-byte
// concatenation — each partial is an exact slice of the original file including
// its trailing newline — so assembling and slicing round-trip losslessly.
func Assemble(srcDir string) ([]byte, error) {
	paths, err := Manifest(srcDir)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	for _, rel := range paths {
		b, err := os.ReadFile(filepath.Join(srcDir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("webbuild: reading partial %q: %w", rel, err)
		}
		out.Write(b)
	}
	return out.Bytes(), nil
}

// Manifest returns the ordered partial paths from srcDir/manifest, skipping
// blank lines and #-comments.
func Manifest(srcDir string) ([]string, error) {
	f, err := os.Open(filepath.Join(srcDir, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("webbuild: opening manifest: %w", err)
	}
	defer f.Close()

	var paths []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// The path is the first field; anything after it (an inline #comment)
		// is documentation, not part of the path.
		paths = append(paths, strings.Fields(line)[0])
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("webbuild: reading manifest: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("webbuild: %s lists no partials", ManifestName)
	}
	return paths, nil
}
