package main

// lm update, against a local server standing in for GitHub and a temporary file standing in for the
// installed binary. The one that matters most is checksums: an archive that does not match must not
// be installed, whatever else happens.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tarGz builds a release archive the way script/release.sh does: one directory named after the
// archive, holding the binary next to the licence.
func tarGz(t *testing.T, dir string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: dir + "/" + name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipOf(t *testing.T, dir string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(dir + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// fakeRelease serves what GitHub serves: the release JSON, the archive and SHA256SUMS. sums is what
// the checksum file claims, so a test can make it lie.
func fakeRelease(t *testing.T, tag, asset string, archive []byte, sums string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "GitHub requires a User-Agent", http.StatusForbidden)
			return
		}
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":"%s/archive"},{"name":"SHA256SUMS","browser_download_url":"%s/sums"}]}`,
			tag, asset, srv.URL, srv.URL)
	})
	mux.HandleFunc("/archive", func(w http.ResponseWriter, r *http.Request) { w.Write(archive) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, sums) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// installed writes a stand-in for the lm on disk and returns its path.
func installed(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "lm")
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func newUpdater(srv *httptest.Server, exe, current string, out *bytes.Buffer) *updater {
	return &updater{
		api: srv.URL + "/api", client: srv.Client(),
		goos: "linux", goarch: "amd64", current: current, exe: exe, out: out,
	}
}

func TestUpdateInstallsTheLatestRelease(t *testing.T) {
	archive := tarGz(t, "lm_v2.0.0_linux_amd64", map[string]string{"lm": "NEW BINARY", "LICENSE": "..."})
	asset := "lm_v2.0.0_linux_amd64.tar.gz"
	srv := fakeRelease(t, "v2.0.0", asset, archive, sum(archive)+"  "+asset+"\n")

	exe := installed(t, "OLD BINARY")
	var out bytes.Buffer
	if err := newUpdater(srv, exe, "v1.0.0", &out).run(false); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW BINARY" {
		t.Errorf("the binary was not replaced: %q", got)
	}
	fi, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("mode %v, want 0755 — an lm that is not executable is worse than an old one", fi.Mode().Perm())
	}
	if !strings.Contains(out.String(), "v1.0.0 → v2.0.0") {
		t.Errorf("says nothing about what it did: %q", out.String())
	}
	// Nothing of the download may be left behind in the install directory.
	ents, _ := os.ReadDir(filepath.Dir(exe))
	if len(ents) != 1 {
		t.Errorf("left %d files in the install directory, want 1", len(ents))
	}
}

func TestUpdateRefusesAnArchiveThatDoesNotMatchItsChecksum(t *testing.T) {
	archive := tarGz(t, "lm_v2.0.0_linux_amd64", map[string]string{"lm": "TAMPERED"})
	asset := "lm_v2.0.0_linux_amd64.tar.gz"
	// The checksum file names a different hash: whatever it is, it is not this archive.
	srv := fakeRelease(t, "v2.0.0", asset, archive, strings.Repeat("a", 64)+"  "+asset+"\n")

	exe := installed(t, "OLD BINARY")
	var out bytes.Buffer
	err := newUpdater(srv, exe, "v1.0.0", &out).run(false)
	if err == nil {
		t.Fatal("installed an archive that did not match its checksum")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("error does not say why: %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD BINARY" {
		t.Errorf("the binary was touched anyway: %q", got)
	}
}

func TestUpdateRefusesWhenTheReleaseHasNoChecksums(t *testing.T) {
	archive := tarGz(t, "lm_v2.0.0_linux_amd64", map[string]string{"lm": "NEW"})
	asset := "lm_v2.0.0_linux_amd64.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v2.0.0","assets":[{"name":%q,"browser_download_url":"x"}]}`, asset)
	}))
	t.Cleanup(srv.Close)
	_ = archive

	exe := installed(t, "OLD")
	var out bytes.Buffer
	err := newUpdater(srv, exe, "v1.0.0", &out).run(false)
	// The exact refusal, not just the word: without it the check could be skipped and the download
	// could fail for some other reason, and a loose assertion would call that a pass.
	if err == nil || !strings.Contains(err.Error(), "publishes no SHA256SUMS") {
		t.Fatalf("want a refusal saying the release publishes no SHA256SUMS, got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("started downloading before finding out it could not check anything: %q", out.String())
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD" {
		t.Errorf("the binary was replaced unchecked: %q", got)
	}
}

func TestUpdateOnTheLatestDoesNothing(t *testing.T) {
	asset := "lm_v1.0.0_linux_amd64.tar.gz"
	srv := fakeRelease(t, "v1.0.0", asset, nil, "")
	exe := installed(t, "SAME")
	var out bytes.Buffer
	if err := newUpdater(srv, exe, "v1.0.0", &out).run(false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "is the latest release") {
		t.Errorf("says nothing: %q", out.String())
	}
	if got, _ := os.ReadFile(exe); string(got) != "SAME" {
		t.Errorf("rewrote the binary for nothing: %q", got)
	}
}

func TestUpdateCheckOnlyReports(t *testing.T) {
	archive := tarGz(t, "lm_v2.0.0_linux_amd64", map[string]string{"lm": "NEW"})
	asset := "lm_v2.0.0_linux_amd64.tar.gz"
	srv := fakeRelease(t, "v2.0.0", asset, archive, sum(archive)+"  "+asset+"\n")
	exe := installed(t, "OLD")
	var out bytes.Buffer
	if err := newUpdater(srv, exe, "v1.0.0", &out).run(true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "v2.0.0 is available") {
		t.Errorf("does not say what is available: %q", out.String())
	}
	if got, _ := os.ReadFile(exe); string(got) != "OLD" {
		t.Errorf("-check installed something: %q", got)
	}
}

func TestUpdateSaysWhenThePlatformIsNotInTheRelease(t *testing.T) {
	asset := "lm_v2.0.0_darwin_arm64.tar.gz" // not the linux/amd64 the updater is asking for
	srv := fakeRelease(t, "v2.0.0", asset, nil, "")
	var out bytes.Buffer
	err := newUpdater(srv, installed(t, "OLD"), "v1.0.0", &out).run(false)
	if err == nil || !strings.Contains(err.Error(), "linux/amd64") {
		t.Fatalf("want an error naming the platform, got %v", err)
	}
}

func TestArchiveNamePerPlatform(t *testing.T) {
	for _, c := range []struct{ goos, goarch, want string }{
		{"darwin", "arm64", "lm_v1.2.3_darwin_arm64.tar.gz"},
		{"linux", "amd64", "lm_v1.2.3_linux_amd64.tar.gz"},
		{"windows", "amd64", "lm_v1.2.3_windows_amd64.zip"},
	} {
		if got := archiveName("v1.2.3", c.goos, c.goarch); got != c.want {
			t.Errorf("%s/%s: %s, want %s", c.goos, c.goarch, got, c.want)
		}
	}
}

func TestBinaryInBothArchiveKinds(t *testing.T) {
	tgz := tarGz(t, "lm_v1_linux_amd64", map[string]string{"lm": "ELF", "README.md": "no"})
	got, err := binaryIn(tgz, "lm_v1_linux_amd64.tar.gz", "linux")
	if err != nil || string(got) != "ELF" {
		t.Errorf("tar.gz: %q %v", got, err)
	}
	z := zipOf(t, "lm_v1_windows_amd64", map[string]string{"lm.exe": "PE", "README.md": "no"})
	got, err = binaryIn(z, "lm_v1_windows_amd64.zip", "windows")
	if err != nil || string(got) != "PE" {
		t.Errorf("zip: %q %v", got, err)
	}
	empty := tarGz(t, "lm_v1_linux_amd64", map[string]string{"README.md": "only this"})
	if _, err := binaryIn(empty, "lm_v1_linux_amd64.tar.gz", "linux"); err == nil {
		t.Error("an archive with no binary in it should be an error")
	}
}

func TestSumForReadsOneLine(t *testing.T) {
	sums := "aaa  short\n" + strings.Repeat("b", 64) + "  lm_v1_linux_amd64.tar.gz\n" + strings.Repeat("c", 64) + "  SHA256SUMS\n"
	got, ok := sumFor(sums, "lm_v1_linux_amd64.tar.gz")
	if !ok || got != strings.Repeat("b", 64) {
		t.Errorf("got %q %v", got, ok)
	}
	if _, ok := sumFor(sums, "short"); ok {
		t.Error("a line whose hash is not 64 characters is not a checksum")
	}
	if _, ok := sumFor(sums, "absent"); ok {
		t.Error("found a name the file does not cover")
	}
}
