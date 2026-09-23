package lang

// loom.om — how the loom understands this repository. The settings syntax is read by settings.go.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigName is the loom's settings file.
const ConfigName = "loom.om"

// Version is the language this compiler speaks. A tree may declare the version it was
// written for; one that asks for something newer is refused, with what to do about it.
//
// Additions do not raise it — a tree written for 1.0 still builds, because everything 1.0
// could say means the same thing now. It goes up when something already written stops
// meaning what it meant.
const Version = "2.0"

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
	Root        string
	Layers      map[string]*Layer
	Order       []string
	Warp        string
	Weft        string
	Templates   string
	Loom        string // the language version this tree declares; empty = unstated
	Frontmatter string // what to do with a frontmatter key of ours upstream also has: set / start / append
	Keys        string // the same, for a top-level key of a file that holds values (toml, yaml)
	Body        string // what to do with our body when no section of it can follow an upstream one
	Output      string // output directory (default output of lm build)
	Anchored    string

	// How the whole tree builds (lm build): which upstream files go into the product as they are, which
	// directories are mirrored whole, and where the product manifest is written
	Take     []string
	Mirrors  [][2]string
	Manifest string

	// Vars holds the variables for this build; nil = no substitution (for callers that weave a single
	// file and don't care about variables)
	Vars *Vars
}

// FindConfig searches upward from start for loom.om.
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

func (c *Config) layerNames() string { return strings.Join(c.Order, " / ") }

// read reads one file from a layer. A missing file returns (\"\", false); **an unknown layer name is an error**.
//
// Why an unknown layer name must be an error: it used to be "not the warp, so treat it as the weft" —
// so once a trailing comment got swallowed into the value of from, the base silently became the weft
// copy, and the warp vanished entirely while the product looked perfectly valid.
// layer looks up a layer by name: `base` (upstream, the warp) or `self` (our layer, the weft).
func (c *Config) Layer(name string) (*Layer, bool) {
	l, ok := c.Layers[name]
	return l, ok
}

func (c *Config) Read(layer, rel string) (string, bool, error) {
	l, ok := c.Layer(layer)
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
func (c *Config) MarksFor(layer, typ string) (Marks, bool) {
	l, ok := c.Layer(layer)
	if !ok {
		return Marks{}, false
	}
	m, ok := l.Marks[typ]
	return m, ok
}
