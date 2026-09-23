package lang

// SYNTAX.md ends with an appendix that uses every construct of the language once, as a checklist.
// A language that does not accept its own checklist has a gap whatever its other tests say, so the
// checklist is read out of the spec and run.
//
// This checks that each construct parses, which is what the appendix is a list of — whether each
// one then does the right thing is what build's tests are for.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestTheAppendixParses(t *testing.T) {
	src, err := os.ReadFile("../SYNTAX.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	i, j := strings.Index(s, "# 附录 A"), strings.Index(s, "## 构件清单核对")
	if i < 0 || j < 0 || j < i {
		t.Fatal("the appendix is not where this expects it; if it moved, this test must follow")
	}
	items := appendixItems(s[i:j])
	if len(items) < 50 {
		t.Fatalf("only %d constructs found; the appendix is a list of every one, so this is not reading it right", len(items))
	}

	var failed int
	for _, c := range items {
		one := c
		// The appendix lists addresses as well as statements; an address needs something done to
		// it before it is a statement.
		if !strings.ContainsAny(c, "({") {
			one = c + `.drop(reason: "x")`
		}
		toks, err := Lex("appendix", []byte(one+"\n"))
		if err != nil {
			failed++
			t.Errorf("does not lex: %s\n  %v", show(c), err)
			continue
		}
		p := &oparser{toks: toks, lines: strings.Split(one, "\n")}
		if _, err := p.node(); err != nil {
			failed++
			t.Errorf("does not parse: %s\n  %v", show(c), err)
		}
	}
	if failed == 0 {
		t.Logf("every one of the appendix's %d constructs parses", len(items))
	}
}

// appendixItems reads the constructs out of the appendix: the code in its fenced blocks, without
// the aligned prose that explains each line, and with a construct spanning several lines kept
// together.
func appendixItems(a string) []string {
	var out, buf []string
	depth := 0
	for _, blk := range regexp.MustCompile("(?s)```go\n(.*?)\n```").FindAllStringSubmatch(a, -1) {
		for _, raw := range strings.Split(blk[1], "\n") {
			line := strings.TrimRight(raw, " \t")
			if depth == 0 {
				if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "//") || isRule(line) {
					continue
				}
				line = withoutProse(line)
				if line == "" {
					continue
				}
			}
			buf = append(buf, line)
			depth += strings.Count(line, "{") + strings.Count(line, "(") -
				strings.Count(line, "}") - strings.Count(line, ")")
			if depth <= 0 {
				out = append(out, strings.Join(buf, "\n"))
				buf, depth = nil, 0
			}
		}
	}
	return out
}

// withoutProse drops the explanation aligned after a line of code. The appendix sets its prose in
// a column, so a run of spaces inside a line is the gap between the two — code never has one.
func withoutProse(l string) string {
	quoted, run := false, 0
	for i, ch := range l {
		if ch == '"' {
			quoted = !quoted
		}
		if ch == ' ' && !quoted {
			if run++; run >= 3 {
				return strings.TrimRight(l[:i-2], " ")
			}
			continue
		}
		run = 0
	}
	return l
}

func isRule(l string) bool {
	t := strings.TrimSpace(l)
	return t != "" && strings.Trim(t, "─═") == ""
}

func show(c string) string {
	c = strings.ReplaceAll(c, "\n", " ⏎ ")
	if len(c) > 70 {
		return c[:70] + "…"
	}
	return c
}
