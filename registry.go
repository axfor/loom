package loom

// A json product is both layers' file together: upstream's registrations and ours.
//
// The objects are merged key by key. Where both layers have the same list, an element of ours takes
// the place of the upstream element that **calls the same scripts**, and our other elements are
// added. Nothing has to be configured for that: a registration is recognised by the handler it names.
//
// Why identity and not a plain union: upstream and our layer often register the same handler with
// different wording (upstream wraps it in a fallback, we call it directly). A union then registers it
// twice — the file stays valid json and the handler simply runs twice, which usually breaks it,
// quietly. And why not "ours replaces upstream's file": upstream registering a new handler of its own
// would then never arrive, with nothing to say so.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/axfor/loom/ast"
)

// script names a handler: a file a registration calls.
var script = regexp.MustCompile(`[A-Za-z0-9_.-]+\.(?:sh|bash|js|mjs|cjs|ts|py|rb|pl)\b`)

func mergeRegistry(c *Config, t *Template) (string, error) {
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

	merged := mergeValue(up, wf)

	// Why check: when the merge goes wrong the product is still valid json, just an entry short or
	// over — and a handler that is not registered is the same as one that does not work, while one
	// registered twice runs twice. Neither says anything on its own.
	if miss := missing(wf, merged); len(miss) > 0 {
		return "", fmt.Errorf("%s: our registrations are not all in the product (%s) — this is a bug in the merge, not something the template can fix",
			t.Path, strings.Join(miss, ", "))
	}
	if dup := duplicates(merged); len(dup) > 0 {
		return "", fmt.Errorf("%s: %s would register the same handler twice (%s)", t.Path, t.Target, strings.Join(dup, ", "))
	}
	return merged.Marshal() + "\n", nil
}

// mergeValue merges ours onto upstream's: objects key by key, lists by what each element calls,
// anything else is ours.
func mergeValue(up, ours *ast.Value) *ast.Value {
	switch {
	case up == nil:
		return ours
	case ours == nil:
		return up
	case up.Kind == ast.Object && ours.Kind == ast.Object:
		out := ast.NewObject()
		for _, k := range up.Keys {
			if o, ok := ours.Get(k); ok {
				out.Set(k, mergeValue(up.Props[k], o))
			} else {
				out.Set(k, up.Props[k])
			}
		}
		for _, k := range ours.Keys {
			if _, ok := up.Get(k); !ok {
				out.Set(k, ours.Props[k])
			}
		}
		return out
	case up.Kind == ast.Array && ours.Kind == ast.Array:
		taken := map[string]bool{}
		for _, e := range ours.Elems {
			taken[identity(e)] = true
		}
		out := ast.NewArray()
		for _, e := range up.Elems {
			if !taken[identity(e)] {
				out.Elems = append(out.Elems, e)
			}
		}
		out.Elems = append(out.Elems, ours.Elems...)
		return out
	}
	return ours
}

// identity is what makes two registrations the same one: the handlers they call. An element that
// calls none is only itself.
func identity(v *ast.Value) string {
	if h := handlers(v); len(h) > 0 {
		return strings.Join(h, " ")
	}
	return v.Marshal()
}

// handlers lists the scripts a value names, without repeats, in order.
func handlers(v *ast.Value) []string {
	set := map[string]bool{}
	var walk func(*ast.Value)
	walk = func(v *ast.Value) {
		if v == nil {
			return
		}
		switch v.Kind {
		case ast.String:
			for _, m := range script.FindAllString(v.Str, -1) {
				set[m[strings.LastIndexByte(m, '/')+1:]] = true
			}
		case ast.Object:
			for _, k := range v.Keys {
				walk(v.Props[k])
			}
		case ast.Array:
			for _, e := range v.Elems {
				walk(e)
			}
		}
	}
	walk(v)
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// missing lists the registrations of ours that the product does not hold, by the path they sit at.
func missing(ours, product *ast.Value) []string {
	var out []string
	var walk func(at string, o, p *ast.Value)
	walk = func(at string, o, p *ast.Value) {
		if o == nil {
			return
		}
		switch o.Kind {
		case ast.Object:
			for _, k := range o.Keys {
				pc, _ := p.Get(k)
				if pc == nil {
					out = append(out, at+"/"+k)
					continue
				}
				walk(at+"/"+k, o.Props[k], pc)
			}
		case ast.Array:
			have := map[string]bool{}
			if p != nil && p.Kind == ast.Array {
				for _, e := range p.Elems {
					have[identity(e)] = true
				}
			}
			for _, e := range o.Elems {
				if !have[identity(e)] {
					out = append(out, at+"/"+identity(e))
				}
			}
		}
	}
	walk("", ours, product)
	return out
}

// duplicates lists handlers registered more than once in the same list.
func duplicates(v *ast.Value) []string {
	var out []string
	var walk func(*ast.Value)
	walk = func(v *ast.Value) {
		if v == nil {
			return
		}
		switch v.Kind {
		case ast.Object:
			for _, k := range v.Keys {
				walk(v.Props[k])
			}
		case ast.Array:
			seen := map[string]bool{}
			for _, e := range v.Elems {
				id := identity(e)
				if len(handlers(e)) > 0 {
					if seen[id] {
						out = append(out, id)
					}
					seen[id] = true
				}
				walk(e)
			}
		}
	}
	walk(v)
	return out
}
