package build

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/axfor/loom/lang"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// lm sync follows upstream.
//
// A merge template says our file is upstream plus our edits. When upstream moves on, those edits
// have to be carried onto the new upstream, and replacing the upstream layer is the one moment the
// old upstream is still at hand. So that is when it happens: a three-way merge of our file, the
// upstream it was based on and the new upstream. Nothing is stored to do it later — no patch files
// in the source tree.
//
// Where our edits and upstream's changes overlap, the merge leaves conflict markers in our file, as
// git does, and the build refuses the file until they are resolved.

// SyncReport is what a sync did.
type SyncReport struct {
	Added, Changed, Removed []string // upstream files, relative to the upstream layer
	Merged                  []string // our files that took upstream's changes
	Conflicts               []string // our files left with conflict markers
	Gone                    []string // our files whose upstream file no longer exists
}

// Sync replaces the upstream layer with the tree at from and merges upstream's changes into the
// files of merge templates. from is taken as it is: leave out what should not be upstream (.git and
// the like) before calling.
func Sync(c *lang.Config, from string) (*SyncReport, error) {
	if c.Warp == "" || c.Weft == "" {
		return nil, fmt.Errorf("sync needs both base and self in loom.lm")
	}
	src, err := filepath.Abs(from)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", from)
	}
	upRoot, err := filepath.Abs(filepath.Join(c.Root, c.Layers[c.Warp].Dir))
	if err != nil {
		return nil, err
	}
	if within(src, upRoot) || within(upRoot, src) {
		return nil, fmt.Errorf("%s and the upstream layer %s overlap — sync from a copy of the new upstream somewhere else", src, upRoot)
	}
	meRoot := filepath.Join(c.Root, c.Layers[c.Weft].Dir)

	// Read what the merges need before the old upstream is gone. A template that does not load stops
	// the sync: after it, the old upstream can't be had back.
	type merge struct {
		ours, base string
		old, mine  []byte
	}
	var merges []merge
	var unresolved []string
	tpls, err := lang.Templates(c)
	if err != nil {
		return nil, err
	}
	for _, p := range tpls {
		t, err := lang.LoadTemplate(c, p)
		if err != nil {
			return nil, fmt.Errorf("fix the templates before syncing, the old upstream is needed to merge: %v", err)
		}
		for _, s := range t.Stmts {
			if s.Op != "merge" {
				continue
			}
			m := merge{ours: filepath.Join(meRoot, filepath.FromSlash(t.Target)), base: filepath.FromSlash(t.BasePath)}
			m.old, _ = os.ReadFile(filepath.Join(upRoot, m.base))
			m.mine, _ = os.ReadFile(m.ours)
			if hasConflictMarkers(string(m.mine)) {
				unresolved = append(unresolved, lang.Rel(c, m.ours))
			}
			merges = append(merges, m)
		}
	}
	// A conflict from the last sync can only be resolved against the upstream it came from, and this
	// sync replaces it. Merging again would nest markers inside markers and lose that upstream.
	if len(unresolved) > 0 {
		return nil, fmt.Errorf("%s still has conflict markers from the last sync — resolve them before syncing again", strings.Join(unresolved, ", "))
	}

	// A staging directory with no files would empty the upstream layer, and the build would only fail
	// afterwards. A copy that produced nothing is a failed copy, not a new upstream.
	n := 0
	if err := filepath.WalkDir(src, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return err
	}); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("%s holds no files — refusing to empty the upstream layer", src)
	}

	r := &SyncReport{}
	if err := mirror(src, upRoot, r); err != nil {
		return r, err
	}

	for _, m := range merges {
		ours := lang.Rel(c, m.ours)
		now, err := os.ReadFile(filepath.Join(upRoot, m.base))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			r.Gone = append(r.Gone, ours)
			continue
		case err != nil:
			return r, err
		case m.old == nil || m.mine == nil || bytes.Equal(m.old, now):
			continue
		}
		merged, conflict := now, false
		if !bytes.Equal(m.mine, m.old) {
			if merged, conflict, err = merge3(m.mine, m.old, now); err != nil {
				return r, fmt.Errorf("%s: %v", ours, err)
			}
		}
		mode := fs.FileMode(0o644)
		if st, err := os.Stat(m.ours); err == nil {
			mode = st.Mode().Perm()
		}
		if err := os.WriteFile(m.ours, merged, mode); err != nil {
			return r, err
		}
		if conflict {
			r.Conflicts = append(r.Conflicts, ours)
		} else {
			r.Merged = append(r.Merged, ours)
		}
	}
	return r, nil
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool {
	r, err := filepath.Rel(dir, p)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// mirror makes dst hold exactly what src holds: files with their content and permissions, symbolic
// links as links. Only what differs is written, so untouched files keep their timestamps.
func mirror(src, dst string, r *SyncReport) error {
	seen := map[string]bool{}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rp, _ := filepath.Rel(src, p)
		seen[rp] = true
		to := filepath.Join(dst, rp)
		// Upstream may have turned a file into a directory, or the other way round: a path in the way is
		// removed, or MkdirAll fails on it and the sync stops halfway.
		if err := clearWay(dst, rp); err != nil {
			return err
		}
		old, oldErr := os.Lstat(to)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			if oldErr == nil && old.Mode()&fs.ModeSymlink != 0 {
				if cur, _ := os.Readlink(to); cur == link {
					return nil
				}
			}
			os.RemoveAll(to)
			note(r, oldErr, filepath.ToSlash(rp))
			return os.Symlink(link, to)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if oldErr == nil && old.Mode().IsRegular() && old.Mode().Perm() == info.Mode().Perm() {
			if cur, err := os.ReadFile(to); err == nil && bytes.Equal(cur, b) {
				return nil
			}
		}
		if oldErr == nil && !old.Mode().IsRegular() {
			os.RemoveAll(to)
		}
		note(r, oldErr, filepath.ToSlash(rp))
		if err := os.WriteFile(to, b, info.Mode().Perm()); err != nil {
			return err
		}
		return os.Chmod(to, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	var dirs []string
	err = filepath.WalkDir(dst, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rp, _ := filepath.Rel(dst, p)
		if d.IsDir() {
			if rp != "." {
				dirs = append(dirs, p)
			}
			return nil
		}
		if !seen[rp] {
			r.Removed = append(r.Removed, filepath.ToSlash(rp))
			return os.Remove(p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs))) // deepest first
	for _, d := range dirs {
		os.Remove(d) // only empty directories go
	}
	return nil
}

// clearWay removes anything at dst/rp that is not the kind of thing about to be written there: a file
// where a directory has to go, or a directory where a file has to go.
func clearWay(dst, rp string) error {
	parts := strings.Split(filepath.ToSlash(rp), "/")
	at := dst
	for i, part := range parts {
		at = filepath.Join(at, part)
		st, err := os.Lstat(at)
		if err != nil {
			return nil // nothing in the way from here on
		}
		wantDir := i < len(parts)-1
		if st.IsDir() != wantDir {
			return os.RemoveAll(at)
		}
	}
	return nil
}

func note(r *SyncReport, oldErr error, rp string) {
	if oldErr == nil {
		r.Changed = append(r.Changed, rp)
	} else {
		r.Added = append(r.Added, rp)
	}
}

// merge3 merges upstream's change (old → now) into ours with diff3, which leaves conflict markers
// where both sides changed the same lines.
func merge3(ours, old, now []byte) ([]byte, bool, error) {
	if _, err := exec.LookPath("diff3"); err != nil {
		return nil, false, fmt.Errorf("the diff3 command is required to merge")
	}
	d, err := os.MkdirTemp("", "loom")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(d)
	names := []string{filepath.Join(d, "ours"), filepath.Join(d, "old"), filepath.Join(d, "now")}
	for i, b := range [][]byte{ours, old, now} {
		if err := os.WriteFile(names[i], b, 0o644); err != nil {
			return nil, false, err
		}
	}
	cmd := exec.Command("diff3", "-m", "-L", "ours", "-L", "upstream before sync", "-L", "upstream after sync", names[0], names[1], names[2])
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out, false, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return out, true, nil
	}
	return nil, false, fmt.Errorf("diff3: %v %s", err, strings.TrimSpace(stderr.String()))
}
