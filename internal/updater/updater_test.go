package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/clarkbar-sys/hush/internal/version"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.2.1", "v1.2.0", true},
		{"v1.3.0", "v1.2.9", true},
		{"v2.0.0", "v1.9.9", true},
		{"v1.2.0", "v1.2.0", false},
		{"v1.2.0", "v1.2.1", false},
		{"1.2.1", "v1.2.0", true}, // leading v optional
		{"v1.2.0", "dev", false},  // unparseable -> never newer
		{"garbage", "v1.2.0", false},
		{"v1.2.0-rc1", "v1.1.0", true}, // pre-release suffix trimmed
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// makeArchive builds a gzipped tar holding a single file named binary with the
// given contents, mirroring the release workflow's artifact shape.
func makeArchive(t *testing.T, binary string, contents []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: binary, Mode: 0o755, Size: int64(len(contents))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// releaseServer stands in for GitHub: it serves the latest-release JSON and the
// asset download, with a digest the caller controls (to exercise verification).
func releaseServer(t *testing.T, tag string, archive []byte, digest string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var assetURL string
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"tag_name": tag,
			"assets": []map[string]string{{
				"name":                 AssetName("hush-control"),
				"browser_download_url": assetURL,
				"digest":               "sha256:" + digest,
			}},
		}
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/asset.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	srv := httptest.NewServer(mux)
	assetURL = srv.URL + "/asset.tar.gz"
	return srv
}

func TestLatestReleaseMatchesPlatformAsset(t *testing.T) {
	archive := makeArchive(t, "hush-control", []byte("binary"))
	srv := releaseServer(t, "v9.9.9", archive, "deadbeef")
	defer srv.Close()

	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	rel, err := LatestRelease(context.Background(), srv.Client(), "hush-control")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v9.9.9" {
		t.Errorf("tag = %q, want v9.9.9", rel.Tag)
	}
	want := fmt.Sprintf("hush-control_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	if rel.AssetName != want {
		t.Errorf("asset = %q, want %q", rel.AssetName, want)
	}
	if rel.Digest != "deadbeef" {
		t.Errorf("digest = %q, want deadbeef (sha256: prefix stripped)", rel.Digest)
	}
}

func TestSelfUpdateSwapsBinary(t *testing.T) {
	stampVersion(t, "v1.0.0")

	newContents := []byte("#!/new binary v2\n")
	archive := makeArchive(t, "hush-control", newContents)
	digest := sha256hex(archive)
	srv := releaseServer(t, "v2.0.0", archive, digest)
	defer srv.Close()

	withAPIBase(t, srv.URL)
	exe := fakeExecutable(t, []byte("old binary v1"))

	res, err := SelfUpdate(context.Background(), srv.Client(), "hush-control")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Updated || res.From != "v1.0.0" || res.To != "v2.0.0" {
		t.Fatalf("result = %+v, want Updated v1.0.0 -> v2.0.0", res)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newContents) {
		t.Errorf("binary not swapped: got %q", got)
	}
}

func TestSelfUpdateRejectsBadDigest(t *testing.T) {
	stampVersion(t, "v1.0.0")

	archive := makeArchive(t, "hush-control", []byte("tampered"))
	srv := releaseServer(t, "v2.0.0", archive, "0000000000000000000000000000000000000000000000000000000000000000")
	defer srv.Close()

	withAPIBase(t, srv.URL)
	exe := fakeExecutable(t, []byte("old binary v1"))

	if _, err := SelfUpdate(context.Background(), srv.Client(), "hush-control"); err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "old binary v1" {
		t.Errorf("binary was replaced despite bad digest: %q", got)
	}
}

func TestSelfUpdateNoopWhenCurrent(t *testing.T) {
	stampVersion(t, "v2.0.0")

	archive := makeArchive(t, "hush-control", []byte("same"))
	srv := releaseServer(t, "v2.0.0", archive, sha256hex(archive))
	defer srv.Close()

	withAPIBase(t, srv.URL)
	exe := fakeExecutable(t, []byte("current"))

	res, err := SelfUpdate(context.Background(), srv.Client(), "hush-control")
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated {
		t.Error("Updated = true, want false when already at latest")
	}
	if got, _ := os.ReadFile(exe); string(got) != "current" {
		t.Errorf("binary changed on no-op update: %q", got)
	}
}

func TestSelfUpdateRefusesDevBuild(t *testing.T) {
	stampVersion(t, "dev")
	if _, err := SelfUpdate(context.Background(), http.DefaultClient, "hush-control"); err == nil {
		t.Fatal("expected refusal to update a dev build, got nil")
	}
}

// --- helpers ---

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func stampVersion(t *testing.T, v string) {
	t.Helper()
	old := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = old })
}

func withAPIBase(t *testing.T, base string) {
	t.Helper()
	old := apiBase
	apiBase = base
	t.Cleanup(func() { apiBase = old })
}

// fakeExecutable writes a stand-in binary into a temp dir and points
// os.Executable at it via a test hook, returning its path.
func fakeExecutable(t *testing.T, contents []byte) string {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "hush-control")
	if err := os.WriteFile(exe, contents, 0o755); err != nil {
		t.Fatal(err)
	}
	old := osExecutable
	osExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { osExecutable = old })
	return exe
}
