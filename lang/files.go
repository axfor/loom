package lang

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
// (loom.om), templates by the directory they live in.
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
	return LoadTemplateSource(c, path, src)
}

// LoadTemplateSource reads a template from src instead of the file at path — an editor buffer not
// saved yet. The path still decides which product it builds and where its imports start.
func LoadTemplateSource(c *Config, path string, src []byte) (*Template, error) {
	target, err := TargetOf(c, path)
	if err != nil {
		return nil, err
	}
	t, err := ParseTemplateSyntax(path, target, src, c.resolveImport)
	if err != nil {
		return nil, err
	}
	// self has one source. A resource section says our content is written here; a file of ours at
	// the same path says it is written there. With both, every `self.x` would have two places to
	// look, and which one won would depend on nothing the author wrote.
	if len(t.Resources) > 0 && c.Weft != "" {
		if _, ok, _ := c.Read(c.Weft, target); ok {
			return nil, fmt.Errorf("%s: this template writes our content in a resource section, and %s also has %s — self would have two sources. Keep one: delete the section, or delete the file and the section's content goes on living here",
				path, c.Weft, target)
		}
	}
	return t, nil
}

// resolveImport resolves an import path to a real file in the layer.
//
//   - starting with ./ or ../: relative to this file's directory (the product path);
//     starting with /: from the layer root
//   - the extension may be omitted: it looks for the same name with any extension and
//     accepts exactly one match; several matches are an error, never a guess. Templates do not
//     count: one sits next to the file it builds, and is never content.
func (c *Config) resolveImport(layer, spec, fromDir string) (string, error) {
	l, ok := c.Layer(layer)
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
		files := withExtension(root, rel)
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

// realPath resolves symbolic links in p, or returns p when it can't.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// withExtension lists the files named rel plus one or more extensions (rel.md, rel.min.js) in a
// layer root, templates left out, as slash paths relative to the root. The name is compared without
// regard to case: on macOS and Windows GUIDE.md and guide.sh would want templates that are one file.
func withExtension(root, rel string) []string {
	dir, base := filepath.Split(filepath.Join(root, filepath.FromSlash(rel)))
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(strings.ToLower(n), strings.ToLower(base)+".") || strings.HasSuffix(n, Ext) {
			continue
		}
		r, _ := filepath.Rel(root, filepath.Join(dir, n))
		out = append(out, filepath.ToSlash(r))
	}
	return out
}

// TargetOf derives the product path from a template path: its path relative to the template
// directory, minus .lm. The product file's extension may be left out — SKILL.lm builds SKILL.md —
// and is then found in the layers:
//   - a file named exactly as written is the product (SKILL.md.lm, LICENSE.lm)
//   - otherwise the files named <name>.<ext> in any layer, templates left out: one name
//     is the product; several are an error that asks for the full name, never a guess
//   - no such file: the name as written, a product no layer has a file for
func TargetOf(c *Config, path string) (string, error) {
	root, err := filepath.Abs(filepath.Join(c.Root, c.Templates))
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// The same directory can be reached through a symbolic link (on macOS /var is /private/var), so
	// both sides are compared as real paths. A template that does not exist yet keeps its own path.
	root, abs = realPath(root), filepath.Join(realPath(filepath.Dir(abs)), filepath.Base(abs))
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is not in the template directory %s — the product path comes from the template's location in that directory", path, root)
	}
	if !strings.HasSuffix(rel, Ext) {
		return "", fmt.Errorf("%s: template names must end in %s", path, Ext)
	}
	name := filepath.ToSlash(strings.TrimSuffix(rel, Ext))
	var roots []string
	for _, n := range c.Order {
		roots = append(roots, filepath.Join(c.Root, c.Layers[n].Dir))
	}
	for _, root := range roots {
		if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err == nil && !st.IsDir() {
			return name, nil
		}
	}
	seen := map[string]bool{}
	var found []string
	for _, root := range roots {
		for _, f := range withExtension(root, name) {
			if !seen[f] {
				seen[f] = true
				found = append(found, f)
			}
		}
	}
	sort.Strings(found)
	switch {
	case len(found) == 0:
		return name, nil
	case len(found) == 1 && strings.HasPrefix(filepath.Base(found[0]), filepath.Base(name)+"."):
		return found[0], nil
	}
	var full []string
	for _, f := range found {
		full = append(full, filepath.Base(f)+Ext)
	}
	return "", fmt.Errorf("%s: %s could mean %s — name the template after the one it builds: %s",
		path, filepath.Base(name)+Ext, baseNames(found), strings.Join(full, " or "))
}

func baseNames(paths []string) string {
	var out []string
	for _, p := range paths {
		out = append(out, filepath.Base(p))
	}
	return strings.Join(out, ", ")
}

// TemplateName is the template path to suggest for a product: the short name when it would build
// exactly that product, the full name otherwise.
func TemplateName(c *Config, target string) string {
	full := filepath.Join(c.Root, c.Templates, filepath.FromSlash(target)+Ext)
	if ext := filepath.Ext(target); ext != "" {
		short := filepath.Join(c.Root, c.Templates, filepath.FromSlash(strings.TrimSuffix(target, ext))+Ext)
		if got, err := TargetOf(c, short); err == nil && got == target {
			return Rel(c, short)
		}
	}
	return Rel(c, full)
}

// Templates lists every template in the configured template directory, sorted by path —
// a stable order keeps the product reproducible.
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

func Rel(c *Config, p string) string {
	if r, err := filepath.Rel(c.Root, p); err == nil {
		return r
	}
	return p
}
