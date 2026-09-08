package loom

// loom.lm —— 织机怎么认这个仓库。
//
// 【为什么配置也是 HCL】模板是 HCL，配置再换一种格式，读的人就得记两套规则。
// 一门语言只该有一种写法 —— 这条在模板里也反复出现（bilingual 曾经 markdown 一种写法、
// toml 另一种写法，读模板的人得先知道文件是什么格式才知道该写哪一句）。

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

// ConfigName 是织机的设置文件。
//
// 【为什么不叫 loom.hcl】HCL 是这门语言**借来的语法**，不是它的身份 ——
// 文件名该说"这是给织机看的"，而不是"这是用某个第三方格式写的"。
// 语法哪天换了，叫 .hcl 的文件就成了一句谎话。
const ConfigName = "loom.lm"

// Marks 是一对包裹标记。纬线内容进产物时包上，于是「经线 100% 保留」可以机械验证：
// 剥掉标记块，剩下的必须与经线那份逐字节相同。
type Marks struct{ Begin, End string }

// Layer 是一层源。经线（warp）是底、逐字节保留；纬线（weft）是穿进去的那一层。
type Layer struct {
	Name  string
	Dir   string
	Role  string           // "warp" | "weft" | ""
	Marks map[string]Marks // 资源类型 → 标记；没配的类型不包标记
}

// Config 是一个仓库的织机设置。
//
// 【为什么不给默认值】默认值只在含义唯一时才安全。层名叫什么、哪一层是经线、
// 标记长什么样 —— 每一条猜错都是**静默换了基底**：产物仍然合法，只是经线那半没了。
type Config struct {
	Root      string
	Layers    map[string]*Layer
	Order     []string
	Warp      string
	Weft      string
	Templates string
	Anchored  string

	// registry：json 产物的合并策略 —— 节点 = 容器数组里的一条，身份由正则从条目里提取。
	// 朴素并集会让同一条注册两次，而重复注册的注册表往往直接失效。
	RegGroup string
	RegID    *regexp.Regexp
}

// FindConfig 从 start 起向上找 loom.lm。
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
			return "", fmt.Errorf("从 %s 一路向上都没找到 %s —— 织机不知道哪一层是经线，不猜", start, ConfigName)
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

// LoadConfig 读 loom.lm。
func LoadConfig(path string) (*Config, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
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
				return nil, fmt.Errorf("%s: 层 `%s` 声明了两次", b.DefRange, l.Name)
			}
			c.Layers[l.Name] = l
			c.Order = append(c.Order, l.Name)
			switch l.Role {
			case "warp":
				if c.Warp != "" {
					return nil, fmt.Errorf("%s: 经线只能有一层，`%s` 和 `%s` 都声明了 role = \"warp\"",
						b.DefRange, c.Warp, l.Name)
				}
				c.Warp = l.Name
			case "weft":
				if c.Weft != "" {
					return nil, fmt.Errorf("%s: 纬线只能有一层，`%s` 和 `%s` 都声明了 role = \"weft\"",
						b.DefRange, c.Weft, l.Name)
				}
				c.Weft = l.Name
			case "":
			default:
				return nil, fmt.Errorf("%s: role 只能是 \"warp\"（经线）或 \"weft\"（纬线），收到 `%s`", b.DefRange, l.Role)
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
				return nil, fmt.Errorf("%s: id_pattern 不是合法正则：%v", b.DefRange, err)
			}
		}
	}
	if c.Warp == "" {
		return nil, fmt.Errorf("%s: 没有哪一层声明 role = \"warp\" —— 织机不知道拿哪一层当经线", path)
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
		return "", fmt.Errorf("%s: 这里要一个字符串", a.Range)
	}
	return v.AsString(), nil
}

func (c *Config) layerNames() string { return strings.Join(c.Order, " / ") }

// read 取某一层里的一个文件。文件不存在返回 (\"\", false)；**层名不认识是错误**。
//
// 【为什么未知层名必须报错】此前是「不是经线就当纬线」—— 于是把行尾注释吃进 from 的值
// 之后，基底静默变成了纬线那份，经线整个消失而产物看起来完全合法。
func (c *Config) read(layer, rel string) (string, bool, error) {
	l, ok := c.Layers[layer]
	if !ok {
		return "", false, fmt.Errorf("未知层名 `%s`（只有 %s）", layer, c.layerNames())
	}
	b, err := os.ReadFile(filepath.Join(c.Root, l.Dir, rel))
	if err != nil {
		return "", false, nil
	}
	return string(b), true, nil
}

// marksFor 返回某层在某资源类型下的包裹标记。没配就是不包。
//
// 【为什么按类型配】HTML 注释在 shell / toml / json 里不是注释，是垃圾或语法错误。
func (c *Config) marksFor(layer, typ string) (Marks, bool) {
	l, ok := c.Layers[layer]
	if !ok {
		return Marks{}, false
	}
	m, ok := l.Marks[typ]
	return m, ok
}
