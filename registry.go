package loom

// Registry merge: the product is an "event -> handler" table, and a node is **one registration entry**,
// whose identity is extracted from the entry by a regexp in the settings (usually the name of the script called).
//
// Warp entries go in as they are, a weft entry replaces the warp entry with the same identity, and
// weft-only entries are appended.
// Why not a naive union: the same handler would be registered twice — and a registry with duplicate
// registrations often just breaks, quietly: the file is still valid JSON, only the behavior doubles.

import (
	"fmt"

	"github.com/axfor/loom/ast"
)

func mergeRegistry(c *Config, t *Template) (string, error) {
	if c.RegGroup == "" || c.RegID == nil {
		return "", fmt.Errorf("%s: type = \"json\" needs a registry block in loom.lm"+
			" (group + id_pattern) — without knowing what the identity is, duplicates cannot be removed", t.Path)
	}
	from := t.From
	if from == "" {
		from = c.Warp
	}
	upSrc, _, err := c.read(from, t.BasePath)
	if err != nil {
		return "", err
	}
	wfSrc, _, err := c.read(c.Weft, t.Target)
	if err != nil {
		return "", err
	}
	up, err := ast.Parse(upSrc)
	if err != nil {
		return "", fmt.Errorf("%s: %v", t.BasePath, err)
	}
	wf, err := ast.Parse(wfSrc)
	if err != nil {
		return "", fmt.Errorf("%s: %v", t.Target, err)
	}

	// (event, identity) pairs the weft already registers
	taken := map[[2]string]bool{}
	forEachEntry(c, wf, func(ev, id string, _ *ast.Value) {
		taken[[2]string{ev, id}] = true
	})

	merged := ast.NewObject()
	group := ast.NewObject()
	merged.Set(c.RegGroup, group)
	kept := 0

	if ug, ok := up.Get(c.RegGroup); ok && ug.Kind == ast.Object {
		for _, ev := range ug.Keys {
			for _, g := range ug.Props[ev].Elems {
				hs, ok := g.Get("hooks")
				if !ok || hs.Kind != ast.Array {
					continue
				}
				kf := ast.NewArray()
				for _, h := range hs.Elems {
					if taken[[2]string{ev, entryID(c, h)}] {
						continue
					}
					kf.Elems = append(kf.Elems, h)
				}
				if len(kf.Elems) == 0 {
					continue
				}
				ng := g.Clone()
				ng.Set("hooks", kf)
				appendTo(group, ev, ng)
				kept += len(kf.Elems)
			}
		}
	}
	if wg, ok := wf.Get(c.RegGroup); ok && wg.Kind == ast.Object {
		for _, ev := range wg.Keys {
			for _, g := range wg.Props[ev].Elems {
				appendTo(group, ev, g)
			}
		}
	}

	// Why count: when the merge goes wrong the product is still valid JSON, just a few entries short or over —
	// and a gate that is not registered is the same as a gate that does not work, and neither raises an error.
	if got, want := countEntries(merged, c), countEntries(wf, c)+kept; got != want {
		return "", fmt.Errorf("%s: entry count after merge does not add up (ours %d + upstream kept %d != %d)",
			t.Path, countEntries(wf, c), kept, got)
	}
	return merged.Marshal() + "\n", nil
}

func appendTo(group *ast.Value, ev string, g *ast.Value) {
	arr, ok := group.Get(ev)
	if !ok {
		arr = ast.NewArray()
		group.Set(ev, arr)
	}
	arr.Elems = append(arr.Elems, g)
}

func entryID(c *Config, h *ast.Value) string {
	cmd, ok := h.Get("command")
	if !ok || cmd.Kind != ast.String {
		return ""
	}
	m := c.RegID.FindStringSubmatch(cmd.Str)
	if m == nil || len(m) < 2 {
		return ""
	}
	return m[1]
}

func forEachEntry(c *Config, root *ast.Value, fn func(ev, id string, h *ast.Value)) {
	g, ok := root.Get(c.RegGroup)
	if !ok || g.Kind != ast.Object {
		return
	}
	for _, ev := range g.Keys {
		for _, grp := range g.Props[ev].Elems {
			hs, ok := grp.Get("hooks")
			if !ok || hs.Kind != ast.Array {
				continue
			}
			for _, h := range hs.Elems {
				if id := entryID(c, h); id != "" {
					fn(ev, id, h)
				}
			}
		}
	}
}

func countEntries(root *ast.Value, c *Config) int {
	n := 0
	g, ok := root.Get(c.RegGroup)
	if !ok || g.Kind != ast.Object {
		return 0
	}
	for _, ev := range g.Keys {
		for _, grp := range g.Props[ev].Elems {
			if hs, ok := grp.Get("hooks"); ok && hs.Kind == ast.Array {
				n += len(hs.Elems)
			}
		}
	}
	return n
}
