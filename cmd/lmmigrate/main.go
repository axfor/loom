// lmmigrate rewrites a tree's templates from Loom 1 spelling to Loom 2: base becomes up, a bare
// string that names a section of ours becomes self."…", and a whole-file replace becomes
// return self. It edits tokens the compiler found, never text it guessed at. Temporary: not
// part of lm.
package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/axfor/loom/lang"
)

type edit struct {
	at, end int
	text    string
}

func main() {
	c, err := lang.LoadConfig(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	tpls, err := lang.Templates(c)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dry := len(os.Args) > 2 && os.Args[2] == "-n"
	total := map[string]int{}
	for _, p := range tpls {
		src, err := os.ReadFile(p)
		if err != nil {
			panic(err)
		}
		t, err := lang.LoadTemplate(c, p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", p, err)
			os.Exit(1)
		}
		toks, err := lang.Lex(p, src)
		if err != nil {
			panic(err)
		}
		at := map[lang.Pos]int{}
		for i, tk := range toks {
			at[tk.Pos] = i
		}
		var edits []edit
		seen := map[int]bool{}
		// a bare string naming a section of ours: in Loom 2 it would be text
		var refs func([]lang.Ref)
		refs = func(rs []lang.Ref) {
			for _, r := range rs {
				if r.Layer != "self" || r.IsLit || r.File != "" {
					continue
				}
				i, ok := at[r.Rng]
				if !ok || toks[i].Kind != lang.KString || seen[toks[i].Off] {
					continue
				}
				seen[toks[i].Off] = true
				edits = append(edits, edit{toks[i].Off, toks[i].Off, "self."})
				total["bare string → self."]++
			}
		}
		var walk func([]lang.Stmt)
		walk = func(ss []lang.Stmt) {
			for _, s := range ss {
				refs(s.Srcs)
				refs([]lang.Ref{s.SetRef})
				walk(s.Kids)
				walk(s.Else)
			}
		}
		walk(t.Stmts)
		// base, where it names the upstream file: not a node called base (after a dot)
		for i, tk := range toks {
			if tk.Kind == lang.KIdent && tk.Text == "base" && (i == 0 || toks[i-1].Kind != lang.KDot) {
				edits = append(edits, edit{tk.Off, tk.End, "up"})
				total["base → up"]++
			}
		}
		sort.Slice(edits, func(i, j int) bool { return edits[i].at > edits[j].at })
		out := append([]byte(nil), src...)
		for _, e := range edits {
			out = append(out[:e.at], append([]byte(e.text), out[e.end:]...)...)
		}
		// up.replace(self, reason: "…") on a line of its own is what return self says in one line
		reRep := regexp.MustCompile(`(?m)^up\.replace\(self, reason: "((?:[^"\\]|\\.)*)"\)[ \t]*$`)
		s := reRep.ReplaceAllStringFunc(string(out), func(m string) string {
			total["replace(self) → return self"]++
			r := reRep.FindStringSubmatch(m)[1]
			r = strings.ReplaceAll(strings.ReplaceAll(r, `\"`, `"`), `\\`, `\`)
			return "return self // reason: " + r
		})
		// comments describe the syntax too
		reCom := regexp.MustCompile(`(?m)^([ \t]*//.*)\bbase\.`)
		for reCom.MatchString(s) {
			s = reCom.ReplaceAllString(s, "${1}up.")
			total["comment base. → up."]++
		}
		if s != string(src) {
			total["files changed"]++
			if !dry {
				if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
					panic(err)
				}
			}
		}
	}
	for k, v := range total {
		fmt.Printf("%-30s %d\n", k, v)
	}
}
