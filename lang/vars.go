package lang

// Variable injection: sources in our layer write {{@name}}, and the value comes from a variables file
// (lm.e by default, another one with `-e`).
//
//	# lm.e
//	url = https://github.com/axfor/XSDD
//
// Only the named file is read: with `-e inner.e` there is no fallback to lm.e. If the intranet file is
// missing a variable, a fallback would quietly build the public address into the intranet version —
// and the product would look perfectly normal. So a variable that is used but not defined is a build error.
//
// Only our content is substituted: the upstream layer is preserved byte for byte, with no substitution.
// The "byte-identical to upstream once marks are stripped" property must not break just because
// upstream happens to contain {{@x}}.

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// VarsName is the default variables file, kept next to loom.om.
const VarsName = "lm.e"

// Vars is every variable in one variables file.
type Vars struct {
	File   string // variables file path; empty = no variables file
	Values map[string]string
	used   map[string]bool
}

var (
	varLine     = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$`)
	placeholder = regexp.MustCompile(`\{\{@(@?)([A-Za-z_][A-Za-z0-9_]*)\}\}`)
)

// LoadVars reads a variables file: one `name = value` per line, with the value taken verbatim to the end of
// the line; `#` starts a comment only at the start of a line (values often contain URLs).
func LoadVars(path string) (*Vars, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	v := &Vars{File: path, Values: map[string]string{}, used: map[string]bool{}}
	first := map[string]int{}
	for i, l := range strings.Split(string(src), "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		m := varLine.FindStringSubmatch(t)
		if m == nil {
			return nil, fmt.Errorf("%s:%d: a variables file has one name = value per line (names are letters, digits and underscores, not starting with a digit)", path, i+1)
		}
		if n, dup := first[m[1]]; dup {
			return nil, fmt.Errorf("%s:%d: variable %s is defined twice (first on line %d)", path, i+1, m[1], n)
		}
		first[m[1]] = i + 1
		v.Values[m[1]] = m[2]
	}
	return v, nil
}

// NoVars means "no variables file": using any variable is a build error.
func NoVars() *Vars {
	return &Vars{Values: map[string]string{}, used: map[string]bool{}}
}

// Expand substitutes the variables in a piece of our content. file and line / col are for error
// messages: where the content came from, and the line and column of that file it starts at (1:1 for a whole file).
func (v *Vars) Expand(src []byte, file string, line, col int) ([]byte, error) {
	if !bytes.Contains(src, []byte("{{@")) || IsBinary(src) {
		return src, nil
	}
	var firstErr error
	out := placeholder.ReplaceAllFunc(src, func(m []byte) []byte {
		g := placeholder.FindSubmatch(m)
		if len(g[1]) > 0 {
			return []byte("{{@" + string(g[2]) + "}}") // {{@@name}}: the literal {{@name}}
		}
		name := string(g[2])
		val, ok := v.Values[name]
		if !ok {
			if firstErr == nil {
				at := posIn(src, bytes.Index(src, m), file, line, col)
				if v.File == "" {
					firstErr = fmt.Errorf("%s: variable {{@%s}} is used, but there is no variables file (default %s, or pass -e)", at, name, VarsName)
				} else {
					firstErr = fmt.Errorf("%s: variable {{@%s}} is not defined in %s", at, name, v.File)
				}
			}
			return m
		}
		v.used[name] = true
		return []byte(val)
	})
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// Used returns the names of variables that were used; Unused returns those defined but never used.
func (v *Vars) Used() []string   { return v.names(true) }
func (v *Vars) Unused() []string { return v.names(false) }

func (v *Vars) names(used bool) []string {
	var out []string
	for n := range v.Values {
		if v.used[n] == used {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// posIn converts a byte offset in src to file:line:col (src starts at line:col of file).
func posIn(src []byte, off int, file string, line, col int) string {
	before := src[:off]
	nl := bytes.Count(before, []byte("\n"))
	if nl == 0 {
		return fmt.Sprintf("%s:%d:%d", file, line, col+len([]rune(string(before))))
	}
	last := bytes.LastIndexByte(before, '\n')
	return fmt.Sprintf("%s:%d:%d", file, line+nl, 1+len([]rune(string(before[last+1:]))))
}

// isBinary reports whether b looks binary: a NUL in the first 8000 bytes means binary, so no substitution.
func IsBinary(b []byte) bool {
	n := len(b)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(b[:n], 0) >= 0
}
