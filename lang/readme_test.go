package lang

// README.md teaches the language by example, and a reader copies the examples. An example the
// compiler refuses is a lie in the one place people learn from, so each template in it is parsed
// here, as the appendix of SYNTAX.md is.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestTheReadmeExamplesParse(t *testing.T) {
	src, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := readmeTemplates(string(src))
	if len(blocks) < 30 {
		t.Fatalf("only %d template examples found; the README has more, so this is not reading it right", len(blocks))
	}
	for _, b := range blocks {
		if _, _, _, err := parseObjects("README", []byte(b+"\n")); err != nil {
			t.Errorf("the README shows what the compiler refuses:\n%s\n  %v", b, err)
		}
	}
}

// statementStart is how a template's first line begins, as opposed to a report, a layout or a
// document shown beside it.
var statementStart = regexp.MustCompile(`^(base[.( ]|self\.|import |if |fn |ok = |return|// )`)

// readmeTemplates returns the README's template examples: the code blocks with no language tag
// whose first line is a statement, without the prose some of them align after the code, and
// without the ~~~ lines that point at an error being shown.
func readmeTemplates(s string) []string {
	var out []string
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); i++ {
		open := lines[i]
		if !strings.HasPrefix(open, "```") {
			continue
		}
		// A block closes at a line of exactly as many backticks: a fence inside a block written
		// with more is content. A tagged block is a document shown beside the language, not it.
		fence := open[:len(open)-len(strings.TrimLeft(open, "`"))]
		tagged := strings.TrimLeft(open, "`") != ""
		j := i + 1
		for j < len(lines) && lines[j] != fence {
			j++
		}
		body := lines[i+1 : min(j, len(lines))]
		i = j
		if tagged || len(body) == 0 || !statementStart.MatchString(strings.TrimSpace(body[0])) {
			continue
		}
		var kept []string
		for _, l := range body {
			if strings.HasPrefix(strings.TrimSpace(l), "~") {
				continue
			}
			kept = append(kept, withoutAlignedProse(l))
		}
		out = append(out, strings.Join(kept, "\n"))
	}
	return out
}

// withoutAlignedProse drops an explanation set in a column after the code: three or more spaces
// after some code, outside a string or a literal. Indentation at the start of a line is code.
func withoutAlignedProse(l string) string {
	quoted, run, code := false, 0, false
	for i, ch := range l {
		if ch == '"' || ch == '`' {
			quoted = !quoted
		}
		if ch == ' ' && !quoted {
			if run++; run >= 3 && code {
				return strings.TrimRight(l[:i-2], " ")
			}
			continue
		}
		run = 0
		code = true
	}
	return l
}
