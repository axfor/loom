package loom

// 锚点的三件工具：复核、列出、生成派生视图。
//
// 按名字锚定买到的是「经线旁边改了也照样落对位置」，代价是**看经线文件时看不见锚点**。
// 这三件工具把那个可见性还回来，而且比把锚点写进经线更准 ——
// 它们显示的是此刻真实解析到的位置，不是上次注入时的位置。

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/axfor/loom/ast"
)

type anchorUse struct {
	tpl, kind, anchor string
	named             string // 具名锚点的名字；内联的为空
	rng               string
}

// anchorUses 收集一份模板里所有**可寻址**的锚点使用。
//
// 【内联的也要收】`after "heading" "X"` 这种不进 anchor 块声明。
// 只查具名声明的话，这道门会报「0 个全部有效」—— 而那句话读起来和全绿一模一样。
// *一个门报"检查了 0 项"时，它说的是"我没在工作"，不是"没问题"。*
func anchorUses(t *Template) []anchorUse {
	var out []anchorUse
	var walk func([]Stmt)
	walk = func(ss []Stmt) {
		for _, s := range ss {
			switch s.Op {
			case "after", "before", "replace":
				if s.Kind == "anchor" {
					if d, ok := t.Anchors[s.Anchor]; ok {
						out = append(out, anchorUse{t.Path, d.Kind, d.Anchor, s.Anchor, d.Rng.String()})
					}
					continue
				}
				out = append(out, anchorUse{t.Path, s.Kind, s.Anchor, "", s.Rng.String()})
			case "in":
				// 嵌套块里的锚点指的是嵌套体，单独一套坐标，这里不查
			}
		}
	}
	walk(t.Stmts)
	return out
}

// CheckAnchors 逐个复核所有模板里的锚点在**当前经线**里还找不找得到、唯不唯一。
// 这就是「同步后补充不丢失」的机械形式：经线挪走了那块，这里当场点名，
// 而不是等到某天产物少了一段才发现。
func CheckAnchors(c *Config, w io.Writer) error {
	tpls, err := Templates(c)
	if err != nil {
		return err
	}
	var bad []string
	n := 0
	for _, p := range tpls {
		t, err := LoadTemplate(p)
		if err != nil {
			bad = append(bad, err.Error())
			continue
		}
		uses := anchorUses(t)
		if len(uses) == 0 {
			continue
		}
		from := t.From
		if from == "" {
			from = c.Warp
		}
		src, ok, err := c.read(from, t.BasePath)
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		if !ok {
			bad = append(bad, fmt.Sprintf("%s: 基底文件不存在（%s 层的 %s）", p, from, t.BasePath))
			continue
		}
		tree := ast.New(t.Type, src)
		for _, u := range uses {
			n++
			hits := tree.Find(u.kind, u.anchor)
			name := fmt.Sprintf("%s %q", u.kind, u.anchor)
			if u.named != "" {
				name = fmt.Sprintf("`%s`（%s %q）", u.named, u.kind, u.anchor)
			}
			switch {
			case len(hits) == 0:
				bad = append(bad, fmt.Sprintf("%s: 锚点 %s 在当前经线里找不到了 —— "+
					"经线多半改了那个标题/函数名，纬线那段会静默落不到位", u.rng, name))
			case len(hits) > 1:
				bad = append(bad, fmt.Sprintf("%s: 锚点 %s 在当前经线里匹配到 %d 处 —— "+
					"取第一个会把内容插到错的地方，而产物看起来完全正常", u.rng, name, len(hits)))
			}
		}
	}
	if len(bad) > 0 {
		fmt.Fprintf(w, "⛔ %d 个锚点在这次经线同步后失效：\n", len(bad))
		for _, b := range bad {
			fmt.Fprintf(w, "  · %s\n", b)
		}
		return fmt.Errorf("%d 个锚点失效", len(bad))
	}
	fmt.Fprintf(w, "✅ 锚点全部仍然有效（%d 个）\n", n)
	return nil
}

// ListAnchors 把每个锚点当前解析到经线的哪一行列出来。
func ListAnchors(c *Config, w io.Writer) error {
	tpls, err := Templates(c)
	if err != nil {
		return err
	}
	n := 0
	for _, p := range tpls {
		t, err := LoadTemplate(p)
		if err != nil {
			return err
		}
		uses := anchorUses(t)
		if len(uses) == 0 {
			continue
		}
		from := t.From
		if from == "" {
			from = c.Warp
		}
		src, _, _ := c.read(from, t.BasePath)
		tree := ast.New(t.Type, src)
		fmt.Fprintln(w, t.Target)
		for _, u := range uses {
			hits := tree.Find(u.kind, u.anchor)
			where := "★找不到"
			if len(hits) == 1 {
				where = fmt.Sprintf("经线第 %d 行", hits[0][0]+1)
			} else if len(hits) > 1 {
				where = fmt.Sprintf("★匹配 %d 处", len(hits))
			}
			label := u.anchor
			if u.named != "" {
				label = u.named
			}
			fmt.Fprintf(w, "  %-40s %-9s %s\n", trunc(label, 40), u.kind, where)
			n++
		}
	}
	fmt.Fprintf(w, "共 %d 个锚点\n", n)
	return nil
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

var extType = map[string]string{".md": "markdown", ".toml": "toml", ".sh": "shell", ".json": "json"}
var commentOf = map[string][2]string{
	"markdown": {"<!-- ", " -->"},
	"toml":     {"# ", ""},
	"shell":    {"# ", ""},
	"text":     {"# ", ""},
}

// AnchoredView 生成派生视图 —— 经线那份 + 每个**可寻址锚点**的标注。
//
// 【它是什么，不是什么】它是派生视图，不是源也不是产物：不参与编织、不进产物、不提交。
// 经线那一层因此仍然逐字节等于上游，「抽掉纬线逐字节比」仍是一行 diff。
//
// 【为什么标全部而不只标用过的】写模板时真正想知道的是「这个文件我能锚到哪些点」。
// 只标用过的，等于只显示已经用过的；而没用过的那些才是你要找的。
func AnchoredView(c *Config, w io.Writer) error {
	if c.Anchored == "" {
		return fmt.Errorf("loom.hcl 里没有 anchored —— 没说视图往哪儿写")
	}
	tpls, err := Templates(c)
	if err != nil {
		return err
	}
	// 只给「真的在织」的文件出视图 —— 经线几百个文件里绝大多数没有模板，
	// 全出一遍只会把真正要看的那几十个淹掉。
	want := map[string]bool{}
	for _, p := range tpls {
		t, err := LoadTemplate(p)
		if err != nil {
			return err
		}
		want[t.BasePath] = true
	}
	outRoot := filepath.Join(c.Root, c.Anchored)
	if err := os.RemoveAll(outRoot); err != nil {
		return err
	}
	warpDir := filepath.Join(c.Root, c.Layers[c.Warp].Dir)
	files, marks := 0, 0
	err = filepath.WalkDir(warpDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(warpDir, p)
		if !want[rel] {
			return nil
		}
		typ, ok := extType[filepath.Ext(rel)]
		if !ok {
			typ = "text"
		}
		if typ == "json" {
			return nil // JSON 没有注释语法，标不进去
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		text := string(b)
		tree := ast.New(typ, text)
		at := map[int][]string{}
		for _, kind := range tree.Kinds() {
			if kind == "line" {
				continue // 每行都是 line 锚点，标出来只是噪声
			}
			for _, nd := range ast.Addressable(tree, kind) {
				at[nd.Line] = append(at[nd.Line], fmt.Sprintf("%s %q", kind, nd.Name))
			}
		}
		cm := commentOf[typ]
		lines := strings.Split(text, "\n")
		res := []string{
			fmt.Sprintf("%s这是**派生视图**，别改也别引用：源是 %s/%s，标注由 loom view 生成%s",
				cm[0], c.Layers[c.Warp].Dir, rel, cm[1]),
			fmt.Sprintf("%s下面每个 ⚓ 标的是可寻址锚点，模板里可直接写 after \"<种类>\" \"<名字>\"%s", cm[0], cm[1]),
			"",
		}
		for i, l := range lines {
			for _, m := range at[i] {
				res = append(res, fmt.Sprintf("%s⚓ %s%s", cm[0], m, cm[1]))
				marks++
			}
			res = append(res, l)
		}
		dst := filepath.Join(outRoot, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		files++
		return os.WriteFile(dst, []byte(strings.Join(res, "\n")), 0o644)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "✓ 派生视图：%d 个文件、%d 个可寻址锚点 → %s（不提交，随时可重生成）\n",
		files, marks, c.Anchored)
	return nil
}

var _ = sort.Strings
