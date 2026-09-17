package loom

// Migration: rewrite legacy-syntax (HCL) templates and settings into the new syntax.
//
// The criterion is a byte-identical product. A rewrite changes the notation, never the meaning:
// the same template read as legacy syntax and as new syntax must give the same intermediate result.
// So nothing is guessed here. Anything the new syntax can't express (legacy literals, content
// referenced from the upstream layer) is an error for a person to decide on; no statement is ever
// silently dropped.
//
// Comments move too. `//` comments in legacy templates mostly explain why something is woven the
// way it is, which is harder to rewrite than the statement itself. Each full-line comment goes back
// in front of the statement it preceded.

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Migrated is the migration result of one template.
type Migrated struct {
	From  string   // legacy template path
	To    string   // new template path (product path + .lm, under the templates directory)
	Text  string   // content in the new syntax
	Notes []string // things a person should look at (placeholder reasons, removed declarations)
}

type comment struct {
	line int
	text string
}

// legacyComments extracts full-line comments (// or #) with their line numbers.
func legacyComments(src []byte) []comment {
	var out []comment
	for i, l := range strings.Split(string(src), "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "//"):
			out = append(out, comment{i + 1, strings.TrimSpace(strings.TrimPrefix(t, "//"))})
		case strings.HasPrefix(t, "#"):
			out = append(out, comment{i + 1, strings.TrimSpace(strings.TrimPrefix(t, "#"))})
		}
	}
	return out
}

var coverageOK = regexp.MustCompile(`^coverage-ok:\s*(.+)$`)

// MigrateTemplate rewrites one legacy-syntax template into object syntax.
func MigrateTemplate(c *Config, path string) (*Migrated, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !IsLegacySyntax(src) {
		return nil, fmt.Errorf("%s already uses the new syntax", path)
	}
	t, err := ParseTemplate(path, src)
	if err != nil {
		return nil, err
	}
	m := &Migrated{From: path, To: filepath.Join(c.Root, c.Templates, filepath.FromSlash(t.Target)+Ext)}

	isMe := func(name string) bool { return name == c.Weft || name == "me" }
	isUp := func(name string) bool { return name == c.Warp || name == "up" }
	for _, name := range []string{t.From} {
		if name != "" && !isMe(name) && !isUp(name) {
			return nil, fmt.Errorf("%s: layer `%s` is neither upstream nor our layer — object syntax only has base / self", path, name)
		}
	}
	if len(t.Covers) > 0 {
		return nil, fmt.Errorf("%s: object syntax has no covers", path)
	}

	// reuse files in `in` blocks become imports: collect them first and give each a unique object name
	imports := map[string]string{} // path in the layer -> object name
	var importOrder []string
	used := map[string]bool{"base": true, "self": true}
	var collect func(ss []Stmt)
	collect = func(ss []Stmt) {
		for _, st := range ss {
			if st.Op == "in" && st.Reuse != "" {
				if _, ok := imports[st.Reuse]; !ok {
					b := filepath.Base(st.Reuse)
					name := identFrom(strings.TrimSuffix(b, filepath.Ext(b)))
					for k := 2; used[name]; k++ {
						name = fmt.Sprintf("%s%d", strings.TrimRight(name, "0123456789"), k)
					}
					used[name] = true
					imports[st.Reuse] = name
					importOrder = append(importOrder, st.Reuse)
				}
			}
			collect(st.Kids)
		}
	}
	collect(t.Stmts)

	comments := legacyComments(src)
	var b strings.Builder
	emitComments := func(before int) {
		for len(comments) > 0 && comments[0].line < before {
			if comments[0].text == "" {
				b.WriteString("//\n")
			} else {
				b.WriteString("// " + comments[0].text + "\n")
			}
			comments = comments[1:]
		}
	}

	// The reason for `use me`: legacy templates put it in a `// coverage-ok:` comment
	reason := ""
	for i, cm := range comments {
		if mm := coverageOK.FindStringSubmatch(cm.text); mm != nil {
			reason = strings.TrimSpace(mm[1])
			comments = append(comments[:i:i], comments[i+1:]...)
			break
		}
	}

	firstLine := 1 << 30
	if len(t.Stmts) > 0 {
		firstLine = t.Stmts[0].Rng.Line
	}
	emitComments(firstLine)

	wroteHead := false
	if t.BasePath != t.Target {
		b.WriteString("import base " + quote(importSpec(c, "up", t.BasePath, t.Target)) + "\n")
		wroteHead = true
	}
	for _, rel := range importOrder {
		b.WriteString("import " + imports[rel] + " " + quote(importSpec(c, "me", rel, t.Target)) + "\n")
		wroteHead = true
	}
	if wroteHead {
		b.WriteString("\n")
	}
	if t.From != "" && isMe(t.From) {
		if reason == "" {
			reason = "(no reason found during migration; please add one)"
			m.Notes = append(m.Notes, "the whole-file replace reason is a placeholder; please fill it in")
		}
		b.WriteString("base.replace(self, reason: " + quote(reason) + ")\n")
	}

	// Joined keys (toml / json keys; in markdown, join acts on the frontmatter and doesn't affect set frontmatter)
	joined := map[string]bool{}
	if t.Type != "markdown" {
		for _, st := range t.Stmts {
			if st.Op == "bilingual" {
				for _, k := range st.Names {
					joined[k] = true
				}
			}
		}
	}

	var dropped []string
	var stmt func(st Stmt, typ, prefix, reuse string) error
	stmt = func(st Stmt, typ, prefix, reuse string) error {
		emitComments(st.Rng.Line)
		switch st.Op {
		case "after", "before", "replace":
			kind, anchor := st.Kind, st.Anchor
			if kind == "anchor" {
				d, ok := t.Anchors[anchor]
				if !ok {
					return fmt.Errorf("%s: anchor `%s` is not defined", st.Rng, anchor)
				}
				kind, anchor = d.Kind, d.Anchor
			}
			node, err := nodeText(kind, anchor, typ, st.Rng)
			if err != nil {
				return err
			}
			if st.Op == "replace" {
				srcs, err := srcsText(st.Srcs, typ, reuse, imports, isMe)
				if err != nil {
					return err
				}
				m.Notes = append(m.Notes, fmt.Sprintf("%s: the replace reason is a placeholder; please fill it in", st.Rng))
				b.WriteString(prefix + node + ".replace(" + srcs + ", reason: " + quote("(no reason found during migration; please add one)") + ")\n")
				return nil
			}
			srcs, err := srcsText(st.Srcs, typ, reuse, imports, isMe)
			if err != nil {
				return err
			}
			b.WriteString(prefix + node + "." + st.Op + "(" + srcs + ")\n")
		case "append", "prepend":
			srcs, err := srcsText(st.Srcs, typ, reuse, imports, isMe)
			if err != nil {
				return err
			}
			method := "append"
			if st.Op == "prepend" {
				method = "start"
			}
			b.WriteString(prefix + method + "(" + srcs + ")\n")
		case "frontmatter":
			if !isMe(st.Layer) {
				return fmt.Errorf("%s: frontmatter can only be replaced with our layer's", st.Rng)
			}
			b.WriteString(prefix + "frontmatter.set(self.frontmatter)\n")
		case "setgroup":
			for _, kv := range st.Kids {
				if joined[kv.SetKey] && kv.SetRef.Kind == kv.SetKey && kv.SetRef.File == "" {
					// join itself takes our value, appends upstream's and writes the whole key back, so setting the same key first does nothing
					continue
				}
				if !isMe(kv.SetRef.Layer) {
					return fmt.Errorf("%s: a set value can only come from our layer", kv.Rng)
				}
				key, err := nodeText(defaultKind[typ], kv.SetKey, typ, kv.Rng)
				if err != nil {
					return err
				}
				b.WriteString(prefix + key + ".set(self." + nameText(kv.SetRef.Kind) + ")\n")
			}
		case "bilingual":
			var qs []string
			for _, k := range st.Names {
				qs = append(qs, quote(k))
			}
			recv := prefix
			if typ == "markdown" {
				recv = prefix + "frontmatter."
			}
			b.WriteString(recv + "join(" + strings.Join(qs, ", ") + ")\n")
		case "patch":
			// Patches resolve relative to the template's directory; the template moves, so the path follows
			abs := filepath.Join(filepath.Dir(t.Path), st.Body)
			rel, err := filepath.Rel(filepath.Dir(m.To), abs)
			if err != nil {
				return err
			}
			b.WriteString(prefix + "patch(" + quote(filepath.ToSlash(rel)) + ")\n")
		case "in":
			key, err := nodeText(defaultKind[typ], st.Key, typ, st.Rng)
			if err != nil {
				return err
			}
			for _, k := range st.Kids {
				if err := stmt(k, st.As, prefix+key+".as("+st.As+").", st.Reuse); err != nil {
					return err
				}
			}
		case "inherit", "override", "new":
			for _, n := range st.Names {
				dropped = append(dropped, st.Op+" "+n)
			}
		case "anchor":
			// named anchors are expanded in place in the statement that uses them
		default:
			return fmt.Errorf("%s: migrate doesn't recognize statement `%s`", st.Rng, st.Op)
		}
		return nil
	}
	for _, st := range t.Stmts {
		if err := stmt(st, t.Type, "base.", ""); err != nil {
			return nil, err
		}
	}
	emitComments(1 << 30)
	if len(dropped) > 0 {
		sort.Strings(dropped)
		m.Notes = append(m.Notes, "removed function-level declarations (the build report works them out function by function): "+strings.Join(dropped, " / "))
	}
	m.Text = b.String()
	return m, nil
}

// importSpec writes an import path from the layer root, dropping the extension when the path without it still resolves only to this file.
func importSpec(c *Config, layer, rel, target string) string {
	spec := "/" + rel
	short := strings.TrimSuffix(spec, path.Ext(spec))
	if short != spec {
		if got, err := c.resolveImport(layer, short, path.Dir(target)); err == nil && got == rel {
			return short
		}
	}
	return spec
}

// identFrom derives an object name from a file name, replacing characters not allowed in names with _.
func identFrom(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	name := b.String()
	if !isIdent(name) {
		name = "src"
	}
	return name
}

// nodeText writes a node (without the leading object): the default kind as its name, other kinds as kind("name").
func nodeText(kind, anchor, typ string, at Pos) (string, error) {
	if kind == "frontmatter" && anchor == "" {
		return "frontmatter", nil
	}
	if kind == defaultKind[typ] {
		return nameText(anchor), nil
	}
	for w, e := range kindCalls[typ] {
		if e == kind {
			return w + "(" + quote(anchor) + ")", nil
		}
	}
	return "", fmt.Errorf("%s: %s has no `%s` node kind", at, typ, kind)
}

// nameText writes a node name: as an identifier when possible (spaces become _), otherwise as a string.
// Names that contain an underscore or collide with a method name, frontmatter or body are always
// strings, so a reader never has to wonder which one is meant.
func nameText(name string) string {
	if name == "" || strings.Contains(name, "_") || contains(editMethods, name) ||
		name == "as" || name == "frontmatter" || name == "body" {
		return quote(name)
	}
	id := strings.ReplaceAll(name, " ", "_")
	if strings.Contains(name, "  ") || strings.HasPrefix(name, " ") || strings.HasSuffix(name, " ") || !isIdent(id) {
		return quote(name)
	}
	return id
}

// srcsText writes what to insert from our layer: default-kind names from self become strings; everything else names its object.
func srcsText(refs []Ref, typ, reuse string, imports map[string]string, isMe func(string) bool) (string, error) {
	var parts []string
	for _, r := range refs {
		if r.IsLit {
			return "", fmt.Errorf("%s: rewrite legacy literal content as a backtick literal by hand", r.Rng)
		}
		if !isMe(r.Layer) {
			return "", fmt.Errorf("%s: inserted content can only come from our layer, but this references upstream", r.Rng)
		}
		obj := "self"
		if reuse != "" {
			obj = imports[reuse]
		}
		switch r.Kind {
		case "body":
			parts = append(parts, obj+".body")
		case "all":
			parts = append(parts, obj)
		default:
			var node string
			if r.Kind == defaultKind[typ] {
				if obj == "self" {
					parts = append(parts, quote(r.Anchor))
					continue
				}
				node = nameText(r.Anchor)
			} else {
				n, err := nodeText(r.Kind, r.Anchor, typ, r.Rng)
				if err != nil {
					return "", err
				}
				node = n
			}
			parts = append(parts, obj+"."+node)
		}
	}
	return strings.Join(parts, ", "), nil
}

// quote writes a string literal, escaping only what it must: `"` becomes \"; a backslash becomes \\
// only when followed by `"` or a backslash, or at the end. So \. in a regexp is written as is and
// reads like hand-written code.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch ch := s[i]; ch {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			if i+1 == len(s) || s[i+1] == '"' || s[i+1] == '\\' {
				b.WriteString(`\\`)
			} else {
				b.WriteByte('\\')
			}
		default:
			b.WriteByte(ch)
		}
	}
	b.WriteByte('"')
	return b.String()
}

var (
	cfgLayerRe = regexp.MustCompile(`^layer\s+"([^"]+)"`)
	cfgMarkRe  = regexp.MustCompile(`^mark\s+"([^"]+)"`)
	cfgAttrRe  = regexp.MustCompile(`^(templates|anchored)\s*=`)
	cfgRegRe   = regexp.MustCompile(`^registry\b`)
)

// MigrateConfig rewrites a legacy-syntax (HCL) loom.lm into the new syntax.
//
// Comments follow the item they describe: a comment above `layer "upstream"` goes above `up`, and
// one inside a mark block goes above `mark`. Comments at the top of the file stay at the top.
func MigrateConfig(path string) (string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !isLegacyConfig(src) {
		return "", fmt.Errorf("%s already uses the new syntax", path)
	}
	c, err := loadLegacyConfig(path, src)
	if err != nil {
		return "", err
	}
	for _, name := range c.Order {
		if name != c.Warp && name != c.Weft {
			return "", fmt.Errorf("%s: layer `%s` is neither upstream nor our layer — the new syntax only has the up / me layers", path, name)
		}
	}

	notes := map[string][]string{}
	var header []string
	var buf []string
	var stack []string
	seenItem := false
	flush := func(key string) {
		if !seenItem {
			header, buf = buf, nil
			seenItem = true
		}
		notes[key] = append(notes[key], buf...)
		buf = nil
	}
	for _, raw := range strings.Split(string(src), "\n") {
		l := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(l, "//") || strings.HasPrefix(l, "#"):
			t := strings.TrimSpace(strings.TrimLeft(l, "/#"))
			buf = append(buf, t)
		case l == "":
			if len(buf) > 0 && buf[len(buf)-1] != "" {
				buf = append(buf, "")
			}
		case cfgLayerRe.MatchString(l):
			key := "layer:" + cfgLayerRe.FindStringSubmatch(l)[1]
			flush(key)
			stack = append(stack, key)
		case cfgMarkRe.MatchString(l):
			key := "mark:" + cfgMarkRe.FindStringSubmatch(l)[1]
			flush(key)
			stack = append(stack, key)
		case cfgRegRe.MatchString(l):
			flush("registry")
			stack = append(stack, "registry")
		case cfgAttrRe.MatchString(l):
			flush(cfgAttrRe.FindStringSubmatch(l)[1])
		case strings.HasPrefix(l, "}"):
			if len(stack) > 0 {
				flush(stack[len(stack)-1])
				stack = stack[:len(stack)-1]
			}
		default:
			if len(stack) > 0 {
				flush(stack[len(stack)-1])
			}
		}
	}

	var b strings.Builder
	writeNotes := func(lines []string) {
		for len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		for _, n := range lines {
			if n == "" {
				b.WriteString("//\n")
			} else {
				b.WriteString("// " + n + "\n")
			}
		}
	}
	writeNotes(header)
	if len(header) > 0 {
		b.WriteString("\n")
	}
	up := c.Layers[c.Warp]
	writeNotes(notes["layer:"+c.Warp])
	b.WriteString("up        " + quote(up.Dir) + "\n")
	if c.Weft != "" {
		me := c.Layers[c.Weft]
		writeNotes(notes["layer:"+c.Weft])
		b.WriteString("me        " + quote(me.Dir) + "\n")
	}
	b.WriteString("\n")
	writeNotes(append(notes["templates"], notes["anchored"]...))
	b.WriteString("templates " + quote(c.Templates) + "\n")
	if c.Weft != "" {
		me := c.Layers[c.Weft]
		var types []string
		for typ := range me.Marks {
			types = append(types, typ)
		}
		sort.Strings(types)
		for _, typ := range types {
			b.WriteString("\n")
			writeNotes(notes["mark:"+typ])
			mk := me.Marks[typ]
			b.WriteString("mark      " + typ + " " + quote(mk.Begin) + " " + quote(mk.End) + "\n")
		}
	}
	if c.RegGroup != "" {
		b.WriteString("\n")
		writeNotes(notes["registry"])
		b.WriteString("registry  " + quote(c.RegGroup) + " " + quote(c.RegID.String()) + "\n")
	}
	return b.String(), nil
}
