package lang

// loom.lm: it shares the lexer with templates (lexer.go), one setting per line.
//
//	base      "upstream"
//	self      "xsdd"
//	templates "templates"
//	output    "../plugins/XSDD"
//	mark      markdown "<!-- XSDD:BEGIN -->" "<!-- XSDD:END -->"
//	take      "references/**" "skills/**" "LICENSE"
//	mirror    ".gemini/commands" "commands"
//	manifest  ".build-manifest"
//
// The layers are called base and self, the same words a template uses for the upstream file
// and our file: `base "upstream"` is where every `base` comes from.
//
// Upstream files are not all taken by default: an upstream repo often holds things that
// only serve its own development (eval fixtures, CI config), and carrying them into the
// product is a burden. So the upstream layer contributes only the paths listed in take;
// the rest are listed in the build report — when upstream adds a file and nobody has
// decided whether to take it, the report shows it instead of it silently going missing.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// registry is listed to be refused with what to do instead, not to be taken as an unknown word
var settingKeywords = []string{"base", "self", "templates", "output", "mark", "registry", "take", "mirror", "manifest"}

// LoadConfig reads loom.lm.
func LoadConfig(path string) (*Config, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseSettings(path, src)
}

func parseSettings(path string, src []byte) (*Config, error) {
	toks, err := Lex(path, src)
	if err != nil {
		return nil, err
	}
	// One setting per line: split the tokens into lines at newlines
	var lines [][]Tok
	var cur []Tok
	for _, t := range toks {
		switch t.Kind {
		case KNewline, KEOF:
			if len(cur) > 0 {
				lines = append(lines, cur)
				cur = nil
			}
		case KIdent, KString:
			cur = append(cur, t)
		default:
			return nil, fmt.Errorf("%s: the settings file takes one setting per line, with only names and quoted values; unexpected %s", t.Pos, t)
		}
	}
	c := &Config{Root: filepath.Dir(path), Layers: map[string]*Layer{}, Anchored: "anchored"}
	seen := map[string]Pos{}
	type markDecl struct {
		typ        string
		begin, end string
		pos        Pos
	}
	var marks []markDecl
	for _, ln := range lines {
		first := ln[0]
		if first.Kind != KIdent {
			return nil, fmt.Errorf("%s: a line must start with a setting name, got %s", first.Pos, first)
		}
		kw := first.Text
		if !contains(settingKeywords, kw) {
			return nil, unknownIn(kw, settingKeywords, "setting", first.Pos)
		}
		as := ln[1:]
		if kw != "mark" && kw != "take" && kw != "mirror" {
			if p, dup := seen[kw]; dup {
				return nil, fmt.Errorf("%s: `%s` can only be set once (first set at %s)", first.Pos, kw, p)
			}
			seen[kw] = first.Pos
		}
		switch kw {
		case "base", "self", "templates", "output", "manifest":
			if len(as) != 1 || as[0].Kind != KString {
				return nil, fmt.Errorf("%s: `%s` takes one quoted directory: %s \"...\"", first.Pos, kw, kw)
			}
			v := as[0].Text
			switch kw {
			case "base":
				c.Layers["base"] = &Layer{Name: "base", Dir: v, Role: "warp", Marks: map[string]Marks{}}
				c.Order = append(c.Order, "base")
				c.Warp = "base"
			case "self":
				c.Layers["self"] = &Layer{Name: "self", Dir: v, Role: "weft", Marks: map[string]Marks{}}
				c.Order = append(c.Order, "self")
				c.Weft = "self"
			case "templates":
				c.Templates = v
			case "output":
				c.Output = v
			case "manifest":
				c.Manifest = v
			}
		case "mark":
			if len(as) != 3 || as[0].Kind != KIdent || as[1].Kind != KString || as[2].Kind != KString {
				return nil, fmt.Errorf("%s: `mark` takes a type, a begin marker and an end marker: mark markdown \"<!-- BEGIN -->\" \"<!-- END -->\"", first.Pos)
			}
			if _, ok := defaultKind[as[0].Text]; !ok {
				return nil, unknownIn(as[0].Text, typeWords(), "type", as[0].Pos)
			}
			for _, m := range marks {
				if m.typ == as[0].Text {
					return nil, fmt.Errorf("%s: marks for %s declared twice (first at %s)", first.Pos, m.typ, m.pos)
				}
			}
			marks = append(marks, markDecl{as[0].Text, as[1].Text, as[2].Text, first.Pos})
		case "take":
			if len(as) == 0 {
				return nil, fmt.Errorf("%s: `take` lists the upstream paths to carry into the product; * and ** are allowed: take \"references/**\" \"LICENSE\"", first.Pos)
			}
			for _, a := range as {
				if a.Kind != KString {
					return nil, fmt.Errorf("%s: paths must be quoted", a.Pos)
				}
				if err := checkPattern(a.Text); err != nil {
					return nil, fmt.Errorf("%s: %v", a.Pos, err)
				}
				c.Take = append(c.Take, a.Text)
			}
		case "mirror":
			if len(as) != 2 || as[0].Kind != KString || as[1].Kind != KString {
				return nil, fmt.Errorf("%s: `mirror` takes a source directory in the product and a mirror directory: mirror \".gemini/commands\" \"commands\"", first.Pos)
			}
			c.Mirrors = append(c.Mirrors, [2]string{cleanRel(as[0].Text), cleanRel(as[1].Text)})
		case "registry":
			// There is nothing left to configure: a json product is both layers' registrations together,
			// and an element of ours takes the place of the upstream one calling the same scripts.
			return nil, fmt.Errorf("%s: `registry` is not a setting any more — a json product is built from both layers, ours replacing the upstream registration that calls the same scripts; write base.merge(self) in its template and delete this line", first.Pos)
		}
	}
	if c.Warp == "" {
		return nil, fmt.Errorf("%s: upstream location not set — add a line base \"<upstream dir>\"", path)
	}
	if len(marks) > 0 {
		me, ok := c.Layers["self"]
		if !ok {
			return nil, fmt.Errorf("%s: marks wrap our layer's content, but self \"<our dir>\" is not set", marks[0].pos)
		}
		for _, m := range marks {
			me.Marks[m.typ] = Marks{Begin: m.begin, End: m.end}
		}
	}
	if c.Templates == "" {
		// Without a templates setting, templates live next to our files: skills/x/SKILL.md.lm beside
		// skills/x/SKILL.md, so a file and the template that weaves it are found in one place.
		c.Templates = "templates"
		if self, ok := c.Layers["self"]; ok {
			c.Templates = self.Dir
		}
	}
	return c, nil
}

// checkPattern validates a take path: a path relative to the layer root, where a segment
// may use * ? [..] and a whole ** segment matches any number of directories.
func checkPattern(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "../") || p == ".." {
		return fmt.Errorf("take paths start at the layer root, not with / or ../: %q", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "**" {
			continue
		}
		if _, err := filepath.Match(seg, ""); err != nil {
			return fmt.Errorf("invalid take path: %q", p)
		}
	}
	return nil
}

// matchPattern reports whether a layer-relative path matches one take path.
func MatchPattern(pattern, rel string) bool {
	return matchSegs(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func matchSegs(ps, ss []string) bool {
	for len(ps) > 0 {
		if ps[0] == "**" {
			for k := 0; k <= len(ss); k++ {
				if matchSegs(ps[1:], ss[k:]) {
					return true
				}
			}
			return false
		}
		if len(ss) == 0 {
			return false
		}
		if ok, _ := filepath.Match(ps[0], ss[0]); !ok {
			return false
		}
		ps, ss = ps[1:], ss[1:]
	}
	return len(ss) == 0
}

func cleanRel(p string) string {
	return strings.Trim(filepath.ToSlash(filepath.Clean(p)), "/")
}
