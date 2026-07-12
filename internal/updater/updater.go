// Package updater lets hush-control see whether a newer release exists and,
// when asked, replace its own binary with it.
//
// The split of duties mirrors hush's privilege model. The check
// ([LatestRelease] / [Check]) is read-only and safe to run inside the
// long-lived, unprivileged control process to power the console's
// "update available" badge. The swap ([SelfUpdate]) rewrites the on-disk
// binary and so runs from the separate, root-owned oneshot invoked by
// hush-control-update.service — never from the sandboxed service itself.
//
// Downloads are verified against the SHA-256 digest GitHub returns for the
// release asset over the authenticated HTTPS API, so a tampered or truncated
// artifact is rejected before it is ever swapped in.
package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/clarkbar-sys/hush/internal/version"
)

// Repo is the GitHub "owner/name" hush releases are published under.
const Repo = "clarkbar-sys/hush"

// apiBase is the GitHub REST API root. It is a var (not a const) only so tests
// can point it at an httptest server; production never changes it.
var apiBase = "https://api.github.com"

// osExecutable resolves the running binary's path. It is a var so tests can
// point self-update at a throwaway file instead of the test binary itself.
var osExecutable = os.Executable

// maxDownload caps how many bytes SelfUpdate will pull for one asset, a guard
// against a runaway or hostile response. The control binary is ~10 MB, so
// 128 MB is comfortably clear while still bounded.
const maxDownload = 128 << 20

// Release describes the latest published release and the single asset that
// matches the running binary's OS/arch.
type Release struct {
	Tag       string // e.g. "v1.2.0"
	AssetURL  string // browser_download_url of the matching .tar.gz
	AssetName string // e.g. "hush-control_linux_arm64.tar.gz"
	Digest    string // lowercase hex SHA-256 of the asset (from the API)
}

// AssetName returns the release asset name for a binary on this platform, e.g.
// "hush-control_linux_arm64.tar.gz". It matches the naming the release
// workflow produces and install.sh consumes.
func AssetName(binary string) string {
	return fmt.Sprintf("%s_%s_%s.tar.gz", binary, runtime.GOOS, runtime.GOARCH)
}

// ghRelease is the slice of the GitHub release payload we consume.
type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"` // "sha256:<hex>"
	} `json:"assets"`
}

// LatestRelease fetches the latest published release and locates the asset for
// binary on the running platform. It returns an error if the release has no
// matching asset. The asset's digest may be empty for older releases; callers
// that will execute the download (SelfUpdate) must treat an empty digest as
// fatal.
func LatestRelease(ctx context.Context, client *http.Client, binary string) (Release, error) {
	url := apiBase + "/repos/" + Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github: unexpected status %s", resp.Status)
	}

	var gr ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&gr); err != nil {
		return Release{}, fmt.Errorf("decode release: %w", err)
	}
	if gr.TagName == "" {
		return Release{}, errors.New("github: release has no tag_name")
	}

	want := AssetName(binary)
	for _, a := range gr.Assets {
		if a.Name == want {
			return Release{
				Tag:       gr.TagName,
				AssetURL:  a.URL,
				AssetName: a.Name,
				Digest:    strings.ToLower(strings.TrimPrefix(a.Digest, "sha256:")),
			}, nil
		}
	}
	return Release{}, fmt.Errorf("release %s has no asset %q", gr.TagName, want)
}

// Check reports the running version, the latest released version, and whether
// the latter is newer. A "dev" (unstamped) build never reports an update
// available: we can't meaningfully compare it, and refusing avoids clobbering
// a locally built binary.
func Check(ctx context.Context, client *http.Client, binary string) (current, latest string, updateAvailable bool, err error) {
	current = version.Current()
	rel, err := LatestRelease(ctx, client, binary)
	if err != nil {
		return current, "", false, err
	}
	return current, rel.Tag, current != "dev" && Newer(rel.Tag, current), nil
}

// Result reports the outcome of a SelfUpdate call.
type Result struct {
	Updated bool   // true if the on-disk binary was replaced
	From    string // version before the update
	To      string // version after the update (== From when Updated is false)
}

// SelfUpdate replaces the running binary with the latest release when a newer
// one exists. It downloads the matching asset, verifies its SHA-256 against the
// digest from the GitHub API, extracts the binary, and atomically swaps it into
// place. It does NOT restart the service — the caller (running as root) does
// that once Updated is true.
//
// It refuses to run for a "dev" build, fails closed on a missing digest, and
// leaves the existing binary untouched on any error.
func SelfUpdate(ctx context.Context, client *http.Client, binary string) (Result, error) {
	current := version.Current()
	res := Result{From: current, To: current}
	if current == "dev" {
		return res, errors.New("refusing to self-update a dev build (no stamped version to compare)")
	}

	rel, err := LatestRelease(ctx, client, binary)
	if err != nil {
		return res, err
	}
	if !Newer(rel.Tag, current) {
		return res, nil // already current
	}
	if rel.Digest == "" {
		return res, fmt.Errorf("release %s asset %s has no digest; refusing unverified update", rel.Tag, rel.AssetName)
	}

	exe, err := osExecutable()
	if err != nil {
		return res, err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return res, err
	}

	newBin, err := downloadBinary(ctx, client, rel, binary)
	if err != nil {
		return res, err
	}
	if err := replaceBinary(exe, newBin); err != nil {
		return res, err
	}
	res.Updated = true
	res.To = rel.Tag
	return res, nil
}

// downloadBinary fetches the release asset, verifies its digest, and returns
// the extracted binary bytes.
func downloadBinary(ctx context.Context, client *http.Client, rel Release, binary string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.AssetURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: unexpected status %s", rel.AssetName, resp.Status)
	}

	sum := sha256.New()
	body, err := io.ReadAll(io.TeeReader(io.LimitReader(resp.Body, maxDownload), sum))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", rel.AssetName, err)
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != rel.Digest {
		return nil, fmt.Errorf("checksum mismatch for %s: got %s, want %s", rel.AssetName, got, rel.Digest)
	}
	return extractBinary(body, binary)
}

// extractBinary pulls the single named file out of a gzipped tar archive (the
// shape the release workflow produces: one binary at the archive root).
func extractBinary(targz []byte, binary string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(targz))
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("untar: %w", err)
		}
		if filepath.Base(hdr.Name) == binary {
			b, err := io.ReadAll(io.LimitReader(tr, maxDownload))
			if err != nil {
				return nil, fmt.Errorf("read %s from archive: %w", binary, err)
			}
			return b, nil
		}
	}
	return nil, fmt.Errorf("archive has no %q entry", binary)
}

// replaceBinary atomically swaps the file at exe for the given bytes by writing
// a sibling temp file and renaming it over exe. Same-directory rename is atomic
// on Linux, and replacing a running binary's file is safe: the live process
// keeps the old inode while new execs pick up the new one.
func replaceBinary(exe string, newBin []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".hush-update-*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w (need write access to the binary's directory)", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(newBin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpName, exe); err != nil {
		return fmt.Errorf("replace %s: %w", exe, err)
	}
	return nil
}

// Newer reports whether release version a is strictly newer than b. Both are
// parsed as "vMAJOR.MINOR.PATCH" (a leading "v" is optional). Any unparseable
// input yields false, so a malformed tag never triggers an update.
func Newer(a, b string) bool {
	av, aok := parseSemver(a)
	bv, bok := parseSemver(b)
	if !aok || !bok {
		return false
	}
	for i := range av {
		if av[i] != bv[i] {
			return av[i] > bv[i]
		}
	}
	return false
}

// parseSemver parses "v1.2.0" (or "1.2.0") into [major, minor, patch]. A
// pre-release or build suffix on the patch (e.g. "1.2.0-rc1") is trimmed.
func parseSemver(s string) ([3]int, bool) {
	var out [3]int
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return out, false
	}
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		if i == 2 {
			p, _, _ = strings.Cut(p, "-")
			p, _, _ = strings.Cut(p, "+")
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
