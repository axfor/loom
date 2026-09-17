package loom

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Ext is the template extension — the same suffix as the settings file, because one
// syntax should have exactly one suffix. The settings file is recognized by its name
// (loom.lm), templates by the directory they live in.
const Ext = ".lm"

// LoadTemplate reads one template file.
//
// In the object syntax a template does not state its product path — its location in the
// template directory is the product path (minus .lm), so this needs to know where the
// template directory is. The legacy syntax (HCL, starting with `weave "..." {`) is still
// read; the two coexist until migration is done.
func LoadTemplate(c *Config, path string) (*Template, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if IsLegacySyntax(src) {
		return ParseTemplate(path, src)
	}
	target, err := TargetOf(c, path)
	if err != nil {
		return nil, err
	}
	return ParseTemplateSyntax(path, target, src, c.resolveImport)
}

// resolveImport resolves an import path to a real file in the layer.
//
//   - starting with ./ or ../: relative to this file's directory (the product path);
//     starting with /: from the layer root
//   - the extension may be omitted: it looks for the same name with any extension and
//     accepts exactly one match; several matches are an error, never a guess
func (c *Config) resolveImport(layer, spec, fromDir string) (string, error) {
	l, ok := c.layer(layer)
	if !ok {
		return "", fmt.Errorf("no %s layer", layer)
	}
	var rel string
	switch {
	case strings.HasPrefix(spec, "/"):
		rel = filepath.ToSlash(filepath.Clean(strings.TrimPrefix(spec, "/")))
	case strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../"):
		rel = filepath.ToSlash(filepath.Clean(filepath.Join(fromDir, spec)))
	default:
		return "", fmt.Errorf("import paths must start with ./, ../ or /: %q", spec)
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("import path escapes the %s layer: %q", layer, spec)
	}
	root := filepath.Join(c.Root, l.Dir)
	if st, err := os.Stat(filepath.Join(root, rel)); err == nil && !st.IsDir() {
		return rel, nil
	}
	if filepath.Ext(rel) == "" {
		matches, _ := filepath.Glob(filepath.Join(root, rel) + ".*")
		var files []string
		for _, m := range matches {
			if st, err := os.Stat(m); err == nil && !st.IsDir() {
				r, _ := filepath.Rel(root, m)
				files = append(files, filepath.ToSlash(r))
			}
		}
		switch len(files) {
		case 1:
			return files[0], nil
		case 0:
		default:
			return "", fmt.Errorf("%q matches several files in the %s layer (%s) — add the extension", spec, layer, strings.Join(files, ", "))
		}
	}
	return "", fmt.Errorf("%s layer: %q not found", layer, spec)
}

var legacyHead = regexp.MustCompile(`^\s*weave\s+"`)

// IsLegacySyntax reports whether a template uses the legacy syntax (HCL): its first
// statement is `weave "..."`. The object syntax has no weave keyword, so the two cannot
// be confused.
func IsLegacySyntax(src []byte) bool {
	for _, line := range strings.Split(string(src), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#") {
			continue
		}
		return legacyHead.MatchString(t)
	}
	return false
}

// TargetOf derives the product path from a template path: its path relative to the
// template directory, minus .lm.
func TargetOf(c *Config, path string) (string, error) {
	root, err := filepath.Abs(filepath.Join(c.Root, c.Templates))
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is not in the template directory %s — the product path comes from the template's location in that directory", path, root)
	}
	if !strings.HasSuffix(rel, Ext) {
		return "", fmt.Errorf("%s: template names must end in %s", path, Ext)
	}
	return filepath.ToSlash(strings.TrimSuffix(rel, Ext)), nil
}

// Templates lists every template in the configured template directory, sorted by path —
// a stable order keeps the product reproducible.
//
// One product gets exactly one template: if two templates write the same target, which
// one wins depends on enumeration order, and "I changed the template but nothing happened"
// becomes a mystery with no traceable cause. Only the loom can see this (it is the only
// thing that reads every template), so it refuses here. Leaving it to downstream gates,
// each with its own regex, yields checks that structurally cannot fire — such as a gate
// keyed by product path, where duplicates were already merged away.
func Templates(c *Config) ([]string, error) {
	root := filepath.Join(c.Root, c.Templates)
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, Ext) {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	seen := map[string]string{}
	for _, p := range out {
		t, err := LoadTemplate(c, p)
		if err != nil {
			continue // parse errors are reported by the callers; this only checks for duplicates
		}
		if first, dup := seen[t.Target]; dup {
			return nil, fmt.Errorf("%s has two templates: %s and %s — a product gets exactly one; "+
				"which one wins depends on enumeration order", t.Target, rel(c, first), rel(c, p))
		}
		seen[t.Target] = p
	}
	return out, nil
}

func rel(c *Config, p string) string {
	if r, err := filepath.Rel(c.Root, p); err == nil {
		return r
	}
	return p
}
