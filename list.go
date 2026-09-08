package loom

// loom list —— 把每份模板的元信息按 JSON 吐出来。
//
// 【为什么要有这条命令】用这门语言的项目往往有一串门要问模板的元信息：
// 这份织到哪儿？以哪一层为底？取代了上游的哪些文件？声明了哪些沿用/重写的函数？
// 每道门自己用 awk / 正则再解析一遍的话，**每道门都会解析得略有不同** ——
// 实测有一道门用 Python 的 glob 扫模板，而 glob 默认不进以点开头的目录，
// 于是 17 份模板对它从不生效，报出来的数字还是「57 份」，读起来像查全了。
// *默认行为里的盲区，比写错的判据更难发现。* 解析器只该有一个。

import (
	"encoding/json"
	"io"
	"path/filepath"
)

// Info 是一份模板对外可见的全部元信息。
type Info struct {
	Template string   `json:"template"` // 模板路径（相对仓库根）
	Target   string   `json:"target"`   // 产物路径
	Type     string   `json:"type"`
	From     string   `json:"from"`   // 基底层名
	Path     string   `json:"path"`   // 基底在那一层里的路径
	Covers   []string `json:"covers"` // 声明取代了经线的哪些文件
	Patch    string   `json:"patch"`  // 补丁文件（空 = 不是补丁式）
	Inherit  []string `json:"inherit"`
	Override []string `json:"override"`
	New      []string `json:"new"`
	Anchors  []Use    `json:"anchors"` // 落在经线上的定位点
	Inserts  []Src    `json:"inserts"` // 从各层取的内容
}

// Use 是一个定位点：在经线的哪种节点、哪个名字上。
type Use struct {
	Kind   string `json:"kind"`
	Anchor string `json:"anchor"`
	Named  string `json:"named,omitempty"` // 具名锚点的名字
	Where  string `json:"where"`           // 模板里的位置
}

// Src 是一条内容来源。
type Src struct {
	Layer  string `json:"layer"`
	Kind   string `json:"kind"`
	Anchor string `json:"anchor,omitempty"`
	Reuse  string `json:"reuse,omitempty"` // 改从哪个文件取
	Lit    bool   `json:"literal,omitempty"`
}

// Describe 读一份模板的元信息。
func Describe(c *Config, path string) (*Info, error) {
	t, err := LoadTemplate(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(c.Root, path)
	if err != nil {
		rel = path
	}
	from := t.From
	if from == "" {
		from = c.Warp
	}
	i := &Info{
		Template: rel, Target: t.Target, Type: t.Type, From: from, Path: t.BasePath,
		Covers: nz(t.Covers), Inherit: []string{}, Override: []string{}, New: []string{},
		Anchors: []Use{}, Inserts: []Src{},
	}
	var walk func(ss []Stmt, reuse string)
	walk = func(ss []Stmt, reuse string) {
		for _, s := range ss {
			switch s.Op {
			case "inherit":
				i.Inherit = append(i.Inherit, s.Names...)
			case "override":
				i.Override = append(i.Override, s.Names...)
			case "new":
				i.New = append(i.New, s.Names...)
			case "patch":
				i.Patch = s.Body
			case "after", "before", "replace":
				u := Use{Kind: s.Kind, Anchor: s.Anchor, Where: s.Rng.String()}
				if s.Kind == "anchor" {
					if d, ok := t.Anchors[s.Anchor]; ok {
						u = Use{Kind: d.Kind, Anchor: d.Anchor, Named: s.Anchor, Where: d.Rng.String()}
					}
				}
				i.Anchors = append(i.Anchors, u)
				i.Inserts = append(i.Inserts, srcs(s.Srcs, reuse)...)
			case "append", "prepend":
				i.Inserts = append(i.Inserts, srcs(s.Srcs, reuse)...)
			case "frontmatter":
				i.Inserts = append(i.Inserts, Src{Layer: s.Layer, Kind: "frontmatter"})
			case "setgroup":
				for _, kv := range s.Kids {
					i.Inserts = append(i.Inserts, Src{Layer: kv.SetRef.Layer, Kind: kv.SetRef.Kind})
				}
			case "in":
				walk(s.Kids, s.Reuse)
			}
		}
	}
	walk(t.Stmts, "")
	return i, nil
}

func srcs(rs []Ref, reuse string) []Src {
	var out []Src
	for _, r := range rs {
		out = append(out, Src{Layer: r.Layer, Kind: r.Kind, Anchor: r.Anchor, Reuse: reuse, Lit: r.IsLit})
	}
	return out
}

func nz(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// List 把全部模板的元信息写成 JSON。
func List(c *Config, w io.Writer) error {
	tpls, err := Templates(c)
	if err != nil {
		return err
	}
	out := make([]*Info, 0, len(tpls))
	for _, p := range tpls {
		i, err := Describe(c, p)
		if err != nil {
			return err
		}
		out = append(out, i)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}
