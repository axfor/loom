package loom

// loom.lm — how the loom understands this repository.
//
// Why the settings file is HCL too: templates are HCL, and a different format for settings would make
// readers learn two sets of rules. A language should have one way to write things — a point that keeps
// coming up in templates as well (bilingual once had one form for markdown and another for toml, so a
// template reader had to know the file format before knowing which one to write).

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// ConfigName is the loom's settings file.
//
// Why not loom.hcl: HCL is **syntax this language borrows**, not its identity —
// the file name should say "this is for the loom", not "this is written in some third-party format".
// If the syntax ever changes, a file named .hcl becomes a lie.
const ConfigName = "loom.lm"

// Marks is a pair of wrapping marks. Weft content is wrapped in them on its way into the product, so
// "100% of the warp preserved" can be checked mechanically: strip the marked blocks, and what remains
// must be byte-identical to the warp copy.
type Marks struct{ Begin, End string }

// Layer is one source layer. The warp is the base and is preserved byte for byte; the weft is the layer threaded into it.
type Layer struct {
	Name  string
	Dir   string
	Role  string           // "warp" | "weft" | ""
	Marks map[string]Marks // resource type -> marks; types without an entry are not wrapped
}

// Config is the loom settings for one repository.
//
// Why no defaults: a default is only safe when its meaning is unambiguous. What the layers are called,
// which layer is the warp, what the marks look like — guessing any of these wrong **silently swaps the
// base**: the product is still valid, only the warp half is gone.
type Config struct {
	Root      string
	Layers    map[string]*Layer
	Order     []string
	Warp      string
	Weft      string
	Templates string
	Output    string // output directory (default output of lm build)
	Anchored  string

	// registry: the merge strategy for json products — a node is one element of the container array, and
	// its identity is extracted from the element by a regexp. A naive union registers the same entry twice,
	// and a registry with duplicate registrations often just breaks.
	RegGroup string
	RegID    *regexp.Regexp

	// How the whole tree builds (lm build): which upstream files go into the product as they are, which
	// directories are mirrored whole, and where the product manifest is written
	Take     []string
	Mirrors  [][2]string
	Manifest string

	// Vars holds the variables for this build; nil = no substitution (for callers that weave a single
	// file and don't care about variables)
	Vars *Vars
}

// FindConfig searches upward from start for loom.lm.
func FindConfig(start string) (string, error) {
	d, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		p := filepath.Join(d, ConfigName)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("searched upward from %s and found no %s — the loom does not know which layer is the warp, and will not guess", start, ConfigName)
		}
		d = parent
	}
}

var cfgSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "templates"}, {Name: "anchored"},
	},
	Blocks: []hcl.BlockHeaderSchema{
		{Type: "layer", LabelNames: []string{"name"}},
		{Type: "registry"},
	},
}

var layerSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{{Name: "dir", Required: true}, {Name: "role"}},
	Blocks:     []hcl.BlockHeaderSchema{{Type: "mark", LabelNames: []string{"type"}}},
}

var markSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{{Name: "begin", Required: true}, {Name: "end", Required: true}},
}

var regSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{{Name: "group", Required: true}, {Name: "id_pattern", Required: true}},
}

// LoadConfig reads loom.lm.
// loadLegacyConfig reads the legacy syntax (HCL). For the new syntax see settings.go.
func loadLegacyConfig(path string, src []byte) (*Config, error) {
	var err error
	f, diags := hclsyntax.ParseConfig(src, path, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	content, diags := f.Body.Content(cfgSchema)
	if diags.HasErrors() {
		return nil, diags
	}
	c := &Config{Root: filepath.Dir(path), Layers: map[string]*Layer{}}
	if a, ok := content.Attributes["templates"]; ok {
		if c.Templates, err = strAttr(a); err != nil {
			return nil, err
		}
	}
	if a, ok := content.Attributes["anchored"]; ok {
		if c.Anchored, err = strAttr(a); err != nil {
			return nil, err
		}
	}
	for _, b := range content.Blocks {
		switch b.Type {
		case "layer":
			l, err := loadLayer(b)
			if err != nil {
				return nil, err
			}
			if _, dup := c.Layers[l.Name]; dup {
				return nil, fmt.Errorf("%s: layer `%s` is declared twice", b.DefRange, l.Name)
			}
			c.Layers[l.Name] = l
			c.Order = append(c.Order, l.Name)
			switch l.Role {
			case "warp":
				if c.Warp != "" {
					return nil, fmt.Errorf("%s: there can be only one warp layer, but both `%s` and `%s` declare role = \"warp\"",
						b.DefRange, c.Warp, l.Name)
				}
				c.Warp = l.Name
			case "weft":
				if c.Weft != "" {
					return nil, fmt.Errorf("%s: there can be only one weft layer, but both `%s` and `%s` declare role = \"weft\"",
						b.DefRange, c.Weft, l.Name)
				}
				c.Weft = l.Name
			case "":
			default:
				return nil, fmt.Errorf("%s: role must be \"warp\" or \"weft\", got `%s`", b.DefRange, l.Role)
			}
		case "registry":
			rc, diags := b.Body.Content(regSchema)
			if diags.HasErrors() {
				return nil, diags
			}
			if c.RegGroup, err = strAttr(rc.Attributes["group"]); err != nil {
				return nil, err
			}
			pat, err := strAttr(rc.Attributes["id_pattern"])
			if err != nil {
				return nil, err
			}
			if c.RegID, err = regexp.Compile(pat); err != nil {
				return nil, fmt.Errorf("%s: id_pattern is not a valid regexp: %v", b.DefRange, err)
			}
		}
	}
	if c.Warp == "" {
		return nil, fmt.Errorf("%s: no layer declares role = \"warp\" — the loom does not know which layer to use as the warp", path)
	}
	if c.Templates == "" {
		c.Templates = "templates"
	}
	return c, nil
}

func loadLayer(b *hcl.Block) (*Layer, error) {
	lc, diags := b.Body.Content(layerSchema)
	if diags.HasErrors() {
		return nil, diags
	}
	l := &Layer{Name: b.Labels[0], Marks: map[string]Marks{}}
	var err error
	if l.Dir, err = strAttr(lc.Attributes["dir"]); err != nil {
		return nil, err
	}
	if a, ok := lc.Attributes["role"]; ok {
		if l.Role, err = strAttr(a); err != nil {
			return nil, err
		}
	}
	for _, mb := range lc.Blocks {
		mc, diags := mb.Body.Content(markSchema)
		if diags.HasErrors() {
			return nil, diags
		}
		begin, err := strAttr(mc.Attributes["begin"])
		if err != nil {
			return nil, err
		}
		end, err := strAttr(mc.Attributes["end"])
		if err != nil {
			return nil, err
		}
		l.Marks[mb.Labels[0]] = Marks{Begin: begin, End: end}
	}
	return l, nil
}

func strAttr(a *hcl.Attribute) (string, error) {
	if a == nil {
		return "", nil
	}
	v, diags := a.Expr.Value(nil)
	if diags.HasErrors() {
		return "", diags
	}
	if v.Type() != cty.String {
		return "", fmt.Errorf("%s: a string is required here", a.Range)
	}
	return v.AsString(), nil
}

func (c *Config) layerNames() string { return strings.Join(c.Order, " / ") }

// read reads one file from a layer. A missing file returns (\"\", false); **an unknown layer name is an error**.
//
// Why an unknown layer name must be an error: it used to be "not the warp, so treat it as the weft" —
// so once a trailing comment got swallowed into the value of from, the base silently became the weft
// copy, and the warp vanished entirely while the product looked perfectly valid.
// layer looks up a layer by name. `up` / `me` are fixed layer names in the language, meaning the warp
// layer and the weft layer — they still resolve when the settings give the layers other names (legacy
// settings call them upstream / xsdd).
func (c *Config) layer(name string) (*Layer, bool) {
	if l, ok := c.Layers[name]; ok {
		return l, true
	}
	switch name {
	case "up":
		l, ok := c.Layers[c.Warp]
		return l, ok
	case "me":
		l, ok := c.Layers[c.Weft]
		return l, ok
	}
	return nil, false
}

func (c *Config) read(layer, rel string) (string, bool, error) {
	l, ok := c.layer(layer)
	if !ok {
		return "", false, fmt.Errorf("unknown layer name `%s` (known layers: %s)", layer, c.layerNames())
	}
	p := filepath.Join(c.Root, l.Dir, rel)
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false, nil
	}
	if c.Vars != nil && l.Role == "weft" {
		if b, err = c.Vars.Expand(b, p, 1, 1); err != nil {
			return "", false, err
		}
	}
	return string(b), true, nil
}

// marksFor returns a layer's wrapping marks for a resource type. No entry means no wrapping.
//
// Why per type: an HTML comment is not a comment in shell / toml / json — it is garbage or a syntax error.
func (c *Config) marksFor(layer, typ string) (Marks, bool) {
	l, ok := c.layer(layer)
	if !ok {
		return Marks{}, false
	}
	m, ok := l.Marks[typ]
	return m, ok
}
