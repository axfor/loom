package loom

// loom.lm in object syntax: it shares the lexer with templates (lexer.go), one setting per line.
//
//	up        "upstream"
//	me        "xsdd"
//	templates "templates"
//	output    "../plugins/XSDD"
//	mark      markdown "<!-- XSDD:BEGIN -->" "<!-- XSDD:END -->"
//	registry  "hooks" "hooks/([A-Za-z0-9._-]+\.(?:sh|js|py))"
//	take      "references/**" "skills/**" "LICENSE"
//	mirror    ".gemini/commands" "commands"
//	manifest  ".build-manifest"
//
// The layer names are fixed as up / me — those are the words templates use, so the
// templates need no change from one project to the next.
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
	"regexp"
	"strings"
)

var settingKeywords = []string{"up", "me", "templates", "output", "mark", "registry", "take", "mirror", "manifest"}

// LoadConfig reads loom.lm.
func LoadConfig(path string) (*Config, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseSettings(path, src)
}

func parseSettings(path string, src []byte) (*Config, error) {
	toks, err := lexLoom(path, src)
	if err != nil {
		return nil, err
	}
	// One setting per line: split the tokens into lines at newlines
	var lines [][]tok
	var cur []tok
	for _, t := range toks {
		switch t.kind {
		case kNewline, kEOF:
			if len(cur) > 0 {
				lines = append(lines, cur)
				cur = nil
			}
		case kIdent, kString:
			cur = append(cur, t)
		default:
			return nil, fmt.Errorf("%s: the settings file takes one setting per line, with only names and quoted values; unexpected %s", t.pos, t)
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
		if first.kind != kIdent {
			return nil, fmt.Errorf("%s: a line must start with a setting name, got %s", first.pos, first)
		}
		kw := first.text
		if !contains(settingKeywords, kw) {
			return nil, unknownIn(kw, settingKeywords, "setting", first.pos)
		}
		as := ln[1:]
		if kw != "mark" && kw != "take" && kw != "mirror" {
			if p, dup := seen[kw]; dup {
				return nil, fmt.Errorf("%s: `%s` can only be set once (first set at %s)", first.pos, kw, p)
			}
			seen[kw] = first.pos
		}
		switch kw {
		case "up", "me", "templates", "output", "manifest":
			if len(as) != 1 || as[0].kind != kString {
				return nil, fmt.Errorf("%s: `%s` takes one quoted directory: %s \"...\"", first.pos, kw, kw)
			}
			v := as[0].text
			switch kw {
			case "up":
				c.Layers["up"] = &Layer{Name: "up", Dir: v, Role: "warp", Marks: map[string]Marks{}}
				c.Order = append(c.Order, "up")
				c.Warp = "up"
			case "me":
				c.Layers["me"] = &Layer{Name: "me", Dir: v, Role: "weft", Marks: map[string]Marks{}}
				c.Order = append(c.Order, "me")
				c.Weft = "me"
			case "templates":
				c.Templates = v
			case "output":
				c.Output = v
			case "manifest":
				c.Manifest = v
			}
		case "mark":
			if len(as) != 3 || as[0].kind != kIdent || as[1].kind != kString || as[2].kind != kString {
				return nil, fmt.Errorf("%s: `mark` takes a type, a begin marker and an end marker: mark markdown \"<!-- BEGIN -->\" \"<!-- END -->\"", first.pos)
			}
			if _, ok := defaultKind[as[0].text]; !ok {
				return nil, unknownIn(as[0].text, typeWords(), "type", as[0].pos)
			}
			for _, m := range marks {
				if m.typ == as[0].text {
					return nil, fmt.Errorf("%s: marks for %s declared twice (first at %s)", first.pos, m.typ, m.pos)
				}
			}
			marks = append(marks, markDecl{as[0].text, as[1].text, as[2].text, first.pos})
		case "take":
			if len(as) == 0 {
				return nil, fmt.Errorf("%s: `take` lists the upstream paths to carry into the product; * and ** are allowed: take \"references/**\" \"LICENSE\"", first.pos)
			}
			for _, a := range as {
				if a.kind != kString {
					return nil, fmt.Errorf("%s: paths must be quoted", a.pos)
				}
				if err := checkPattern(a.text); err != nil {
					return nil, fmt.Errorf("%s: %v", a.pos, err)
				}
				c.Take = append(c.Take, a.text)
			}
		case "mirror":
			if len(as) != 2 || as[0].kind != kString || as[1].kind != kString {
				return nil, fmt.Errorf("%s: `mirror` takes a source directory in the product and a mirror directory: mirror \".gemini/commands\" \"commands\"", first.pos)
			}
			c.Mirrors = append(c.Mirrors, [2]string{cleanRel(as[0].text), cleanRel(as[1].text)})
		case "registry":
			if len(as) != 2 || as[0].kind != kString || as[1].kind != kString {
				return nil, fmt.Errorf("%s: `registry` takes a group name and an identity regex: registry \"hooks\" \"hooks/(...)\"", first.pos)
			}
			c.RegGroup = as[0].text
			if c.RegID, err = regexp.Compile(as[1].text); err != nil {
				return nil, fmt.Errorf("%s: invalid identity regex: %v", as[1].pos, err)
			}
		}
	}
	if c.Warp == "" {
		return nil, fmt.Errorf("%s: upstream location not set — add a line up \"<upstream dir>\"", path)
	}
	if len(marks) > 0 {
		me, ok := c.Layers["me"]
		if !ok {
			return nil, fmt.Errorf("%s: marks wrap our layer's content, but me \"<our dir>\" is not set", marks[0].pos)
		}
		for _, m := range marks {
			me.Marks[m.typ] = Marks{Begin: m.begin, End: m.end}
		}
	}
	if c.Templates == "" {
		c.Templates = "templates"
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
func matchPattern(pattern, rel string) bool {
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
