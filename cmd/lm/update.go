package main

// lm update — replace this binary with the latest release.
//
// A release already publishes a binary per platform and a SHA256SUMS covering every file, so an
// update is: ask GitHub what the latest tag is, take the archive for this platform, check it
// against that file, and put the binary where this one is. The checksum is not optional — a self
// updater that installs whatever it was handed is a way to run someone else's code as you.
//
// Nothing here knows about weaving. It is in cmd/lm because it is a property of the command, not of
// the language.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const releasesAPI = "https://api.github.com/repos/axfor/loom/releases/latest"

// An archive is a few megabytes; a minute is long enough for a slow line and short enough that a
// hung connection does not look like a hung command.
const updateTimeout = time.Minute

// release is the part of GitHub's answer this needs.
type release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// updater is the whole operation, with everything it touches passed in so a test can point it at a
// local server and a temporary file instead of GitHub and /usr/local/bin.
type updater struct {
	api     string
	client  *http.Client
	goos    string
	goarch  string
	current string // the running version, as `lm version` prints it
	exe     string // the file to replace
	out     io.Writer
}

// update is `lm update`: the real one, against GitHub and this binary.
func update(check bool) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot tell where this lm is: %v", err)
	}
	// A symbolic link is the usual way a version manager puts a binary on PATH; the link is not what
	// should be overwritten.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	u := &updater{
		api:     releasesAPI,
		client:  &http.Client{Timeout: updateTimeout},
		goos:    runtime.GOOS,
		goarch:  runtime.GOARCH,
		current: versionString(),
		exe:     exe,
		out:     os.Stdout,
	}
	return u.run(check)
}

func (u *updater) run(check bool) error {
	r, err := u.latest()
	if err != nil {
		return err
	}
	if r.Tag == u.current {
		fmt.Fprintf(u.out, "lm %s is the latest release\n", u.current)
		return nil
	}
	if check {
		fmt.Fprintf(u.out, "lm %s is available; this is %s — run `lm update` to take it\n", r.Tag, u.current)
		return nil
	}

	name := archiveName(r.Tag, u.goos, u.goarch)
	archive, ok := assetURL(r, name)
	if !ok {
		return fmt.Errorf("release %s has no %s — this platform (%s/%s) is not in it", r.Tag, name, u.goos, u.goarch)
	}
	sums, ok := assetURL(r, "SHA256SUMS")
	if !ok {
		return fmt.Errorf("release %s publishes no SHA256SUMS, so %s cannot be checked; install it by hand instead", r.Tag, name)
	}

	fmt.Fprintf(u.out, "lm %s → %s\n", u.current, r.Tag)
	sumFile, err := u.get(sums)
	if err != nil {
		return fmt.Errorf("SHA256SUMS: %v", err)
	}
	want, ok := sumFor(string(sumFile), name)
	if !ok {
		return fmt.Errorf("SHA256SUMS does not cover %s", name)
	}
	body, err := u.get(archive)
	if err != nil {
		return fmt.Errorf("%s: %v", name, err)
	}
	got := sha256.Sum256(body)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("%s does not match its checksum — refusing to install it", name)
	}

	bin, err := binaryIn(body, name, u.goos)
	if err != nil {
		return err
	}
	if err := replace(u.exe, bin); err != nil {
		return err
	}
	fmt.Fprintf(u.out, "installed %s to %s\n", r.Tag, u.exe)
	return nil
}

// latest asks GitHub which release is current.
func (u *updater) latest() (*release, error) {
	body, err := u.get(u.api)
	if err != nil {
		return nil, fmt.Errorf("cannot reach GitHub: %v", err)
	}
	var r release
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("GitHub's answer is not what was expected: %v", err)
	}
	if r.Tag == "" {
		return nil, fmt.Errorf("GitHub named no latest release")
	}
	return &r, nil
}

func (u *updater) get(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	// GitHub refuses a request with no User-Agent.
	req.Header.Set("User-Agent", "lm/"+u.current)
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s said %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// archiveName is what script/release.sh calls the archive for a platform.
func archiveName(tag, goos, goarch string) string {
	if goos == "windows" {
		return fmt.Sprintf("lm_%s_%s_%s.zip", tag, goos, goarch)
	}
	return fmt.Sprintf("lm_%s_%s_%s.tar.gz", tag, goos, goarch)
}

func assetURL(r *release, name string) (string, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a.URL, true
		}
	}
	return "", false
}

// sumFor reads a `<hash>  <name>` line out of a SHA256SUMS file.
func sumFor(sums, name string) (string, bool) {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == name && len(f[0]) == 64 {
			return f[0], true
		}
	}
	return "", false
}

// binaryIn pulls the lm binary out of a release archive. The archive holds one directory named
// after itself, with the binary, the licence and the README in it.
func binaryIn(archive []byte, name, goos string) ([]byte, error) {
	want := "lm"
	if goos == "windows" {
		want = "lm.exe"
	}
	if strings.HasSuffix(name, ".zip") {
		return fromZip(archive, want)
	}
	return fromTarGz(archive, want)
}

func fromTarGz(archive []byte, want string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("the archive is not gzip: %v", err)
	}
	defer gz.Close()
	t := tar.NewReader(gz)
	for {
		h, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the archive is damaged: %v", err)
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == want {
			return io.ReadAll(t)
		}
	}
	return nil, fmt.Errorf("the archive holds no %s", want)
}

func fromZip(archive []byte, want string) ([]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("the archive is not a zip: %v", err)
	}
	for _, f := range z.File {
		if path.Base(f.Name) != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("the archive holds no %s", want)
}

// replace puts bin where exe is. The new file is written beside the old one and renamed over it, so
// the binary is never half-written: a rename within a directory either happened or did not.
func replace(exe string, bin []byte) error {
	dir := filepath.Dir(exe)
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(exe); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".lm-update-")
	if err != nil {
		return cannotWrite(dir, err)
	}
	defer os.Remove(tmp.Name()) // a no-op once the rename has taken it away
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	// Windows will not let a running executable be replaced, but it will let it be renamed away.
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return cannotWrite(dir, err)
		}
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		return cannotWrite(dir, err)
	}
	return nil
}

// cannotWrite turns a permission error into the thing to do about it: an lm in /usr/local/bin is
// not writable by the person running it, and "permission denied" alone does not say that.
func cannotWrite(dir string, err error) error {
	if os.IsPermission(err) {
		return fmt.Errorf("%s is not writable: run `sudo lm update`, or install lm somewhere of your own and set it on PATH", dir)
	}
	return err
}
