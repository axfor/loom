package build

// lm uses — which templates name a given piece of upstream.
//
// The build already works this out: every template names upstream nodes, and resolving them is
// most of what a weave does. It was thrown away afterwards, so the tree could answer "where does
// this anchor point" and never "what points here" — and the second question is the one asked
// before touching anything: what breaks if this section moves, who depends on this file.
//
// Nothing is computed that the build did not already compute. This inverts it.

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/axfor/loom/lang"
)

// Uses prints, for each upstream node some template names, the templates that name it.
// With a pattern, only upstream paths containing it are listed.
func Uses(c *lang.Config, w io.Writer, pattern string) error {
	tpls, err := lang.Templates(c)
	if err != nil {
		return err
	}

	// upstream file → node → the places that name it
	type ref struct{ node, where string }
	byFile := map[string][]ref{}
	for _, p := range tpls {
		i, err := Describe(c, p)
		if err != nil {
			return err
		}
		if i.Path == "" || (pattern != "" && !strings.Contains(i.Path, pattern)) {
			continue
		}
		for _, a := range i.Anchors {
			node := a.Anchor
			if a.Kind != "heading" {
				node = a.Kind + " " + a.Anchor
			}
			byFile[i.Path] = append(byFile[i.Path], ref{node, lang.Rel(c, a.Where)})
		}
		// A merge or a whole-file replace names no node, but it certainly depends on the file.
		if len(i.Anchors) == 0 {
			byFile[i.Path] = append(byFile[i.Path], ref{"(the whole file)", i.Template})
		}
	}

	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)

	if len(files) == 0 {
		if pattern != "" {
			fmt.Fprintf(w, "nothing names anything in an upstream path containing %q\n", pattern)
			return nil
		}
		fmt.Fprintln(w, "no template names anything upstream")
		return nil
	}

	for _, f := range files {
		refs := byFile[f]
		// Group the places by the node they name, so a node named from three templates is one line.
		at := map[string][]string{}
		var order []string
		for _, r := range refs {
			if _, seen := at[r.node]; !seen {
				order = append(order, r.node)
			}
			at[r.node] = append(at[r.node], r.where)
		}
		sort.Strings(order)
		fmt.Fprintf(w, "%s\n", f)
		width := 0
		for _, n := range order {
			if len(n) > width {
				width = len(n)
			}
		}
		for _, n := range order {
			fmt.Fprintf(w, "  %-*s  ← %s\n", width, n, strings.Join(at[n], ", "))
		}
	}
	return nil
}
