package loom

// lm build: compile the whole source tree into the product.
//
// Every file in the product has exactly one source, decided in this order:
//
//  1. a template exists -> weave it
//  2. our layer has it -> copy it as is (with variables expanded)
//  3. the upstream layer has it and take lists it -> copy it as is
//
// Then each mirror copies a whole directory of the product to a second place. Any file in the
// output directory that is not in this table is deleted: what was deleted from the source must
// disappear from the product too, or "the product is a function of the source" no longer holds.
//
// Everything is computed first, then written. The output directory is not touched until every
// template is woven and every check passes. If one fails, no file is written, so there is never a
// half-old, half-new product. Errors are reported all at once (like go build), not one per run.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Output is one file in the product.
type Output struct {
	Rel  string
	From string // "template" / "self" / "base" / "mirror"
	Src  string // source file copied as is; empty for woven or variable-expanded files
	Data []byte // content to write; nil = copy Src as is
	Mode fs.FileMode
	Link string // non-empty = a symlink pointing here
}

// Plan is everything one build computed, not yet written to the output directory.
type Plan struct {
	Outputs []*Output // sorted by path
	Report  *Report
}

// maxErrors caps the errors one build reports: report them all, but don't flood the screen.
const maxErrors = 20

// BuildErrors holds every error from one build.
type BuildErrors []error

func (e BuildErrors) Error() string {
	var b strings.Builder
	for i, err := range e {
		if i == maxErrors {
			fmt.Fprintf(&b, "\n... and %d more", len(e)-maxErrors)
			break
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(err.Error())
	}
	return b.String()
}

func isOSJunk(name string) bool { return name == ".DS_Store" }

// PlanBuild compiles the whole tree: it completes anchors, weaves every template, gathers the
// files to copy and checks for lost content, but writes no product.
// With writeAnchors true, completed anchors are written back to the templates (lm build); with
// false, a missing anchor is an error (lm check).
func PlanBuild(c *Config, writeAnchors bool) (*Plan, error) {
	var errs BuildErrors
	fail := func(err error) { errs = append(errs, err) }
	if c.Vars == nil {
		c.Vars = NoVars()
	}
	r := &Report{VarsFile: c.Vars.File}
	byRel := map[string]*Output{}
	add := func(o *Output) {
		byRel[o.Rel] = o
	}

	upRoot := filepath.Join(c.Root, c.Layers[c.Warp].Dir)
	meRoot := ""
	if c.Weft != "" {
		meRoot = filepath.Join(c.Root, c.Layers[c.Weft].Dir)
	}

	// 1. Templates
	bases := map[string]bool{} // upstream files used as template bases: they are woven into the product, so they are not "not taken"
	tpls, err := Templates(c)
	if err != nil {
		return nil, err
	}
	claimed := map[string]bool{} // product paths that have a template: if the template fails, our file there must not be reported again as shadowing upstream
	// Templates and the patches they apply are sources, never product files. They usually live next
	// to our files in the self layer, so the walk over that layer must skip them.
	sourceOnly := map[string]bool{}
	for _, p := range tpls {
		if target, err := TargetOf(c, p); err == nil {
			claimed[target] = true
		}
		if abs, err := filepath.Abs(p); err == nil {
			sourceOnly[abs] = true
		}
		t, err := LoadTemplate(c, p)
		if err != nil {
			fail(err)
			continue
		}
		for _, s := range t.Stmts {
			if s.Op == "patch" {
				if abs, err := filepath.Abs(filepath.Join(filepath.Dir(t.Path), s.Body)); err == nil {
					sourceOnly[abs] = true
				}
			}
		}
		claimed[t.Target] = true
		bases[t.BasePath] = true
		done, err := completeAnchors(c, t, writeAnchors, r)
		if err != nil {
			fail(err)
			if done == nil {
				continue
			}
		}
		t = done
		out, err := Weave(c, t)
		if err != nil {
			fail(err)
			continue
		}
		for _, err := range account(c, t, out, r) {
			fail(err)
		}
		mode := fs.FileMode(0o644)
		if meRoot != "" {
			if st, err := os.Stat(filepath.Join(meRoot, filepath.FromSlash(t.Target))); err == nil {
				mode = productMode(st.Mode())
			} else if st, err := os.Stat(filepath.Join(upRoot, filepath.FromSlash(t.BasePath))); err == nil {
				mode = productMode(st.Mode())
			}
		}
		add(&Output{Rel: t.Target, From: "template", Data: []byte(out), Mode: mode})
	}

	// 2. Our layer
	upHas := func(rel string) bool {
		_, err := os.Lstat(filepath.Join(upRoot, filepath.FromSlash(rel)))
		return err == nil
	}
	if meRoot != "" {
		err := walkLayer(meRoot, func(rel, abs string, info fs.FileInfo) error {
			if _, woven := byRel[rel]; woven || claimed[rel] {
				return nil
			}
			if a, err := filepath.Abs(abs); err == nil && sourceOnly[a] {
				return nil
			}
			if upHas(rel) && !sameEntry(abs, filepath.Join(upRoot, filepath.FromSlash(rel))) {
				fail(fmt.Errorf("%s: this file in our layer shadows the upstream file at the same path, but no template accounts for it — "+
					"to use ours whole, write template %s: base.replace(self, reason: \"...\"); otherwise weave it in section by section",
					filepath.Join(meRoot, filepath.FromSlash(rel)), filepath.Join(c.Templates, rel+Ext)))
				return nil
			}
			o, err := copyOutput(c, rel, abs, info, "self", true)
			if err != nil {
				fail(err)
				return nil
			}
			add(o)
			r.Added = append(r.Added, rel)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// 3. Upstream layer: only what take lists
	matched := make([]bool, len(c.Take))
	err = walkLayer(upRoot, func(rel, abs string, info fs.FileInfo) error {
		take := false
		for k, pat := range c.Take {
			if matchPattern(pat, rel) {
				matched[k] = true
				take = true
			}
		}
		if _, done := byRel[rel]; done {
			return nil
		}
		if !take {
			if !bases[rel] {
				r.Untaken = append(r.Untaken, rel)
			}
			return nil
		}
		o, err := copyOutput(c, rel, abs, info, "base", false)
		if err != nil {
			fail(err)
			return nil
		}
		add(o)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for k, ok := range matched {
		if !ok {
			r.Warnings = append(r.Warnings, fmt.Sprintf("take %q matches no file in upstream (renamed or deleted upstream?)", c.Take[k]))
		}
	}

	// 4. Mirrors
	for _, m := range c.Mirrors {
		var n int
		var rels []string
		for rel := range byRel {
			if strings.HasPrefix(rel, m[0]+"/") {
				rels = append(rels, rel)
			}
		}
		sort.Strings(rels)
		for _, rel := range rels {
			src := byRel[rel]
			to := m[1] + strings.TrimPrefix(rel, m[0])
			if prev, clash := byRel[to]; clash && prev.From != "mirror" {
				fail(fmt.Errorf("mirror %q → %q: %s is already produced by %s, and the mirror would overwrite it", m[0], m[1], to, fromName(prev.From)))
				continue
			}
			cp := *src
			cp.Rel, cp.From = to, "mirror"
			add(&cp)
			n++
		}
		if n == 0 {
			fail(fmt.Errorf("mirror %q → %q: the product has no files under %s/", m[0], m[1], m[0]))
		}
	}

	if c.Manifest != "" {
		if _, clash := byRel[c.Manifest]; clash {
			fail(fmt.Errorf("manifest %q has the same name as a file in the product", c.Manifest))
		}
	}
	r.VarsUsed, r.VarsUnused = c.Vars.Used(), c.Vars.Unused()
	if len(errs) > 0 {
		return nil, errs
	}

	plan := &Plan{Report: r}
	for _, o := range byRel {
		plan.Outputs = append(plan.Outputs, o)
	}
	sort.Slice(plan.Outputs, func(i, j int) bool { return plan.Outputs[i].Rel < plan.Outputs[j].Rel })
	r.Files = len(plan.Outputs)
	sort.Strings(r.Added)
	return plan, nil
}

// sameEntry reports whether two paths are the same thing: symlinks with the same target, or files
// with byte-identical content. If our layer holds an exact copy of upstream's file, nothing upstream is lost.
func sameEntry(a, b string) bool {
	la, errA := os.Lstat(a)
	lb, errB := os.Lstat(b)
	if errA != nil || errB != nil {
		return false
	}
	if la.Mode()&fs.ModeSymlink != 0 || lb.Mode()&fs.ModeSymlink != 0 {
		ta, ea := os.Readlink(a)
		tb, eb := os.Readlink(b)
		return ea == nil && eb == nil && ta == tb
	}
	return la.Mode().IsRegular() && lb.Mode().IsRegular() && la.Size() == lb.Size() && sameFile(a, b)
}

// productMode is 0755 when the source is executable, otherwise 0644. Only the executable bit
// carries meaning (it is also all git records); copying other bits would make the product depend
// on how a checkout happens to be set up, such as group-writable files on one machine.
func productMode(m fs.FileMode) fs.FileMode {
	if m.Perm()&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func fromName(from string) string {
	switch from {
	case "template":
		return "a template"
	case "self":
		return "our layer"
	case "base":
		return "the upstream layer"
	}
	return from
}

// walkLayer walks the files and symlinks in a layer (without entering symlinked directories); rel is /-separated.
func walkLayer(root string, fn func(rel, abs string, info fs.FileInfo) error) error {
	if _, err := os.Stat(root); err != nil {
		return fmt.Errorf("layer directory %s does not exist", root)
	}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || isOSJunk(d.Name()) {
			return nil
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		return fn(filepath.ToSlash(rel), p, info)
	})
}

// copyOutput is one file copied as is. Text files from our layer get variables expanded; binaries and upstream files are left untouched.
func copyOutput(c *Config, rel, abs string, info fs.FileInfo, from string, expand bool) (*Output, error) {
	o := &Output{Rel: rel, From: from, Src: abs, Mode: productMode(info.Mode())}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(abs)
		if err != nil {
			return nil, err
		}
		o.Src, o.Link = "", target
		return o, nil
	}
	if !expand {
		return o, nil
	}
	head, err := readHead(abs, 8000)
	if err != nil {
		return nil, err
	}
	if isBinary(head) {
		return o, nil
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	out, err := c.Vars.Expand(b, abs, 1, 1)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(out, b) {
		o.Src, o.Data = "", out
	}
	return o, nil
}

func readHead(p string, n int) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	k, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:k], nil
}

// WriteBuild writes a computed plan into the output directory: it writes only changed files and deletes files not in the plan.
func WriteBuild(c *Config, plan *Plan, out string) error {
	outAbs, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	if err := guardOutput(c, outAbs); err != nil {
		return err
	}
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return err
	}
	unlock, err := lockOutput(outAbs)
	if err != nil {
		return err
	}
	defer unlock()

	keep := map[string]bool{}
	written := 0
	for _, o := range plan.Outputs {
		keep[o.Rel] = true
		changed, err := writeOutput(outAbs, o)
		if err != nil {
			return err
		}
		if changed {
			written++
		}
	}
	if c.Manifest != "" {
		keep[c.Manifest] = true
		if _, err := writeOutput(outAbs, manifestOutput(c, plan)); err != nil {
			return err
		}
	}

	// Delete files not in the plan, then empty directories
	var stale []string
	err = filepath.WalkDir(outAbs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || isOSJunk(d.Name()) {
			return nil
		}
		rel, _ := filepath.Rel(outAbs, p)
		if !keep[filepath.ToSlash(rel)] {
			stale = append(stale, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, p := range stale {
		if err := os.Remove(p); err != nil {
			return err
		}
	}
	removeEmptyDirs(outAbs)
	plan.Report.Out, plan.Report.Written, plan.Report.Removed = out, written, len(stale)
	return nil
}

func manifestOutput(c *Config, plan *Plan) *Output {
	var b strings.Builder
	b.WriteString("# Generated by lm build: the path of every file in the output directory (except this one).\n")
	b.WriteString("# The product is a function of the source: hand edits are overwritten by the next lm build.\n")
	for _, o := range plan.Outputs {
		b.WriteString(o.Rel + "\n")
	}
	return &Output{Rel: c.Manifest, From: "manifest", Data: []byte(b.String()), Mode: 0o644}
}

// StaleOutputs compares an existing output directory with what the build would write, without
// writing anything: files missing, files the sources no longer produce, content that differs
// (stale, or edited by hand), a lost or extra executable bit, a link pointing elsewhere.
// A missing output directory is not stale: there is nothing to compare yet.
//
// Only the executable bit is compared: it is the only permission that carries meaning, and the
// only one git records, so a checkout on another machine must not look stale.
func StaleOutputs(c *Config, plan *Plan, out string) ([]string, error) {
	outAbs, err := filepath.Abs(out)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(outAbs); err != nil || !st.IsDir() {
		return nil, nil
	}
	want := append([]*Output{}, plan.Outputs...)
	if c.Manifest != "" {
		want = append(want, manifestOutput(c, plan))
	}
	sort.Slice(want, func(i, j int) bool { return want[i].Rel < want[j].Rel })
	expected := map[string]bool{}
	var problems []string
	for _, o := range want {
		expected[o.Rel] = true
		dst := filepath.Join(outAbs, filepath.FromSlash(o.Rel))
		cur, err := os.Lstat(dst)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: missing from the output directory", o.Rel))
			continue
		}
		isLink := cur.Mode()&fs.ModeSymlink != 0
		switch {
		case o.Link != "":
			if t, err := os.Readlink(dst); !isLink || err != nil || t != o.Link {
				problems = append(problems, fmt.Sprintf("%s: should be a link to %s", o.Rel, o.Link))
			}
			continue
		case isLink || !cur.Mode().IsRegular():
			problems = append(problems, fmt.Sprintf("%s: should be a regular file", o.Rel))
			continue
		}
		same := false
		if o.Data != nil {
			b, err := os.ReadFile(dst)
			same = err == nil && bytes.Equal(b, o.Data)
		} else if src, err := os.Stat(o.Src); err == nil && src.Size() == cur.Size() {
			same = sameFile(o.Src, dst)
		}
		if !same {
			problems = append(problems, fmt.Sprintf("%s: differs from what the sources build (stale, or edited by hand)", o.Rel))
			continue
		}
		if wantX, haveX := o.Mode&0o111 != 0, cur.Mode().Perm()&0o111 != 0; wantX != haveX {
			if wantX {
				problems = append(problems, fmt.Sprintf("%s: lost its executable bit", o.Rel))
			} else {
				problems = append(problems, fmt.Sprintf("%s: is executable, but its source is not", o.Rel))
			}
		}
	}
	err = filepath.WalkDir(outAbs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || isOSJunk(d.Name()) {
			return nil
		}
		rel, _ := filepath.Rel(outAbs, p)
		if rel = filepath.ToSlash(rel); !expected[rel] {
			problems = append(problems, fmt.Sprintf("%s: not produced by the sources (left over, or added by hand)", rel))
		}
		return nil
	})
	return problems, err
}

// guardOutput rejects an output directory that is the source tree, contains it, or lies inside a
// layer or the templates directory. A build deletes files in the output directory that are not in
// the plan, so one wrong path would delete source.
func guardOutput(c *Config, outAbs string) error {
	rootAbs, err := filepath.Abs(c.Root)
	if err != nil {
		return err
	}
	inside := func(child, parent string) bool {
		r, err := filepath.Rel(parent, child)
		return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
	}
	if inside(rootAbs, outAbs) {
		return fmt.Errorf("output directory %s contains the source tree %s — a build deletes files in the output directory that are not in the plan, which would delete source", outAbs, rootAbs)
	}
	dirs := []string{c.Templates}
	for _, l := range c.Layers {
		dirs = append(dirs, l.Dir)
	}
	for _, d := range dirs {
		abs := filepath.Join(rootAbs, d)
		if inside(outAbs, abs) {
			return fmt.Errorf("output directory %s is inside source directory %s — the product would be built again as source", outAbs, abs)
		}
	}
	return nil
}

// lockOutput lets only one build write an output directory at a time. Two builds writing the same
// tree concurrently delete each other's files and leave a product with random files missing. A build
// that breaks when run twice is a defect in the build, not something people should have to watch for.
func lockOutput(outAbs string) (func(), error) {
	sum := sha256.Sum256([]byte(outAbs))
	lock := filepath.Join(os.TempDir(), "lm-build-"+hex.EncodeToString(sum[:8])+".lock")
	if err := os.Mkdir(lock, 0o700); err != nil {
		st, serr := os.Stat(lock)
		// A lock left by a killed process must not block forever: take it over after 30 minutes
		if serr == nil && time.Since(st.ModTime()) > 30*time.Minute {
			os.RemoveAll(lock)
			err = os.Mkdir(lock, 0o700)
		}
		if err != nil {
			return nil, fmt.Errorf("another lm build is already writing %s (lock %s) — concurrent writes to the same output tree corrupt each other; wait for it to finish", outAbs, lock)
		}
	}
	return func() { os.RemoveAll(lock) }, nil
}

// writeOutput writes one file, leaving it alone if content, mode and link target are unchanged. It reports whether anything changed.
func writeOutput(outAbs string, o *Output) (bool, error) {
	dst := filepath.Join(outAbs, filepath.FromSlash(o.Rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	cur, curErr := os.Lstat(dst)
	if o.Link != "" {
		if curErr == nil && cur.Mode()&fs.ModeSymlink != 0 {
			if t, err := os.Readlink(dst); err == nil && t == o.Link {
				return false, nil
			}
		}
		if curErr == nil {
			if err := os.RemoveAll(dst); err != nil {
				return false, err
			}
		}
		return true, os.Symlink(o.Link, dst)
	}
	if curErr == nil && (cur.IsDir() || cur.Mode()&fs.ModeSymlink != 0) {
		if err := os.RemoveAll(dst); err != nil {
			return false, err
		}
		curErr = os.ErrNotExist
	}
	same := false
	if curErr == nil {
		if o.Data != nil {
			if int64(len(o.Data)) == cur.Size() {
				b, err := os.ReadFile(dst)
				same = err == nil && bytes.Equal(b, o.Data)
			}
		} else if src, err := os.Stat(o.Src); err == nil && src.Size() == cur.Size() {
			same = sameFile(o.Src, dst)
		}
	}
	if same {
		if cur.Mode().Perm() != o.Mode {
			return true, os.Chmod(dst, o.Mode)
		}
		return false, nil
	}
	tmp := dst + ".lm-tmp"
	if o.Data != nil {
		if err := os.WriteFile(tmp, o.Data, o.Mode); err != nil {
			return false, err
		}
	} else if err := copyFile(o.Src, tmp, o.Mode); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp, o.Mode); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, dst)
}

func sameFile(a, b string) bool {
	fa, err := os.Open(a)
	if err != nil {
		return false
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false
	}
	defer fb.Close()
	ba, bb := make([]byte, 64<<10), make([]byte, 64<<10)
	for {
		na, ea := io.ReadFull(fa, ba)
		nb, eb := io.ReadFull(fb, bb)
		if na != nb || !bytes.Equal(ba[:na], bb[:nb]) {
			return false
		}
		if ea != nil || eb != nil {
			return (ea == io.EOF || ea == io.ErrUnexpectedEOF) && (eb == io.EOF || eb == io.ErrUnexpectedEOF)
		}
	}
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func removeEmptyDirs(root string) {
	var dirs []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != root {
			dirs = append(dirs, p)
		}
		return nil
	})
	for k := len(dirs) - 1; k >= 0; k-- {
		os.Remove(dirs[k]) // fails on a non-empty directory, which is what we want
	}
}
