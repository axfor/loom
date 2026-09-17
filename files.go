package loom

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Ext is the template extension — the same suffix as the settings file, because one
// syntax should have exactly one suffix. The settings file is recognized by its name
// (loom.lm), templates by the directory they live in.
const Ext = ".lm"

// LoadTemplate reads one template file.
//
// A template does not state its product path — its location in the template directory is
// the product path (minus .lm), so this needs to know where the template directory is.
func LoadTemplate(c *Config, path string) (*Template, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
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
// a stable order keeps the product reproducible. A template's product path is its own path, so
// two templates can never weave the same product.
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
	return out, nil
}

func rel(c *Config, p string) string {
	if r, err := filepath.Rel(c.Root, p); err == nil {
		return r
	}
	return p
}
