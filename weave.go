package loom

// 织：把经线那份当底，按语句把纬线的内容穿进去。
//
// 【错误必须响】锚点找不到、匹配到多处、节点种类用错 —— 一律带位置报错并让调用方非零退出。
// 静默穿错一节的产物看起来完全正常，那正是这门语言要根除的形状。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/axfor/loom/ast"
)

type edit struct {
	s, e int
	repl []string
	idx  int
}

// Weave 编织一份模板，返回产物内容。
func Weave(c *Config, t *Template) (string, error) {
	from := t.From
	if from == "" {
		from = c.Warp
	}

	// patch：基底是经线那份，编辑写成 unified diff。
	// -F0 关掉模糊匹配：默认的 fuzz 会在上下文对不上时把补丁打到近似位置**而不报错**，
	// 产物看起来正常、逻辑却错位 —— 那是这套机制最危险的失败方式。宁可打不上。
	for _, s := range t.Stmts {
		if s.Op != "patch" {
			continue
		}
		return applyPatch(c, t, s, from)
	}

	src, ok, err := c.read(from, t.BasePath)
	if err != nil {
		return "", fmt.Errorf("%s: %v", t.Path, err)
	}
	if !ok {
		return "", fmt.Errorf("%s: %s 层里没有 %s —— `from` 指的是产物以哪一份为底", t.Path, from, t.BasePath)
	}

	if t.Type == "json" {
		return mergeRegistry(c, t)
	}

	tree := ast.New(t.Type, src)
	if sh, isSh := tree.(*ast.Shell); isSh && len(sh.Unclosed) > 0 {
		return "", fmt.Errorf("%s: 这些函数有头无尾，解析不了：%s —— 不能当\"没有这个函数\"",
			t.Path, strings.Join(sh.Unclosed, ", "))
	}

	// set：标量键取另一层的值
	for _, s := range t.Stmts {
		if s.Op != "setgroup" {
			continue
		}
		for _, kv := range s.Kids {
			other, ok, err := c.read(kv.SetRef.Layer, t.Target)
			if err != nil {
				return "", fmt.Errorf("%s: %v", kv.Rng, err)
			}
			if !ok {
				return "", fmt.Errorf("%s: %s 层里没有 %s", kv.Rng, kv.SetRef.Layer, t.Target)
			}
			val, ok := ast.New(t.Type, other).BodyOf(kv.SetRef.Kind)
			if !ok {
				return "", fmt.Errorf("%s: %s 的 %s 里没有键 `%s`", kv.Rng, kv.SetRef.Layer, t.Target, kv.SetRef.Kind)
			}
			if !tree.SetBody(kv.SetKey, strings.Trim(val, `"`)) {
				return "", fmt.Errorf("%s: 基底里没有键 `%s`", kv.Rng, kv.SetKey)
			}
		}
	}

	// in <键> as <类型>：换一套嵌套 AST（toml 的 prompt 里是 markdown）
	for _, s := range t.Stmts {
		if s.Op != "in" {
			continue
		}
		body, ok := tree.BodyOf(s.Key)
		if !ok {
			return "", fmt.Errorf("%s: 基底里没有键 `%s`", s.Rng, s.Key)
		}
		inner := ast.New(s.As, body)
		if err := apply(c, t, s.Kids, inner, t.Target, &nestCtx{typ: t.Type, key: s.Key, reuse: s.Reuse}); err != nil {
			return "", err
		}
		tree.SetBody(s.Key, inner.Text())
	}

	if err := apply(c, t, t.Stmts, tree, t.Target, nil); err != nil {
		return "", err
	}

	// 【必须在 apply 之后】frontmatter = <层> 会把整块 frontmatter 换掉 ——
	// 放在它之前，双语值刚写进去就被整块覆盖掉，而产物看起来完全正常（只是另一种语言没了）。
	for _, s := range t.Stmts {
		if s.Op != "bilingual" {
			continue
		}
		for _, key := range s.Names {
			if err := applyBilingual(c, t, key, tree, s); err != nil {
				return "", err
			}
		}
	}

	out := tree.Text()
	// 文本文件以换行收尾。合成时在末尾追加内容会吃掉基底原有的那个换行。
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, nil
}

type nestCtx struct {
	typ, key, reuse string
}

// apply 把位置语句作用到 ast 上。
//
// 【倒序应用，但同位置要再倒一次】从后往前改，前面的行号才不会被前面的插入顶掉。
// 而两条语句指向同一个锚点时，倒序会把它们的相对次序翻过来 ——
// 所以同位置按模板次序倒排，应用完之后正好还原成模板里的顺序。
func apply(c *Config, t *Template, stmts []Stmt, tree ast.Tree, rel string, nest *nestCtx) error {
	var edits []edit
	plain := t.Type == "text" || t.Type == "json"
	if nest != nil {
		plain = nest.typ == "text" || nest.typ == "json"
	}
	for _, s := range stmts {
		switch s.Op {
		case "after", "before", "replace":
			kind, anchor := s.Kind, s.Anchor
			if kind == "anchor" {
				d, ok := t.Anchors[anchor]
				if !ok {
					return fmt.Errorf("%s: 没有定义过锚点 `%s`（用 anchor 块先定义）", s.Rng, anchor)
				}
				kind, anchor = d.Kind, d.Anchor
			}
			if !ast.Has(tree.Kinds(), kind) {
				return fmt.Errorf("%s: 这个类型没有 `%s` 这种节点（有 %s）",
					s.Rng, kind, strings.Join(tree.Kinds(), "/"))
			}
			span, err := one(s, tree.Find(kind, anchor), kind, anchor)
			if err != nil {
				return err
			}
			body, err := payloads(c, t, s.Srcs, rel, nest)
			if err != nil {
				return err
			}
			marked := isMarked(c, t, s.Srcs, body)
			lines := tree.Lines()
			switch s.Op {
			case "after":
				// 【空行要算，不能硬加】前一节可能已经以空行收尾，硬加一个就成了两个空行；
				// 而后面紧跟标题时不补空行，markdown 会把它粘到上一段里。
				// 【带标记的插入不加外部空行】剥掉标记块后必须与经线逐字节相同，
				// 在标记**之外**加空行，剥完就多出空行。
				if marked || plain {
					edits = append(edits, edit{span[1], span[1], []string{body}, len(edits)})
				} else {
					var repl []string
					if !(span[1]-1 >= 0 && strings.TrimSpace(lines[span[1]-1]) == "") {
						repl = append(repl, "")
					}
					repl = append(repl, body)
					if !(span[1] < len(lines) && strings.TrimSpace(lines[span[1]]) == "") {
						repl = append(repl, "")
					}
					edits = append(edits, edit{span[1], span[1], repl, len(edits)})
				}
			case "before":
				if marked {
					edits = append(edits, edit{span[0], span[0], []string{body}, len(edits)})
				} else {
					var repl []string
					if !(span[0]-1 >= 0 && strings.TrimSpace(lines[span[0]-1]) == "") {
						repl = append(repl, "")
					}
					repl = append(repl, body, "")
					edits = append(edits, edit{span[0], span[0], repl, len(edits)})
				}
			default:
				edits = append(edits, edit{span[0], span[1], strings.Split(body, "\n"), len(edits)})
			}
		case "append", "prepend":
			// 列表里每一项各是一条编辑 —— 与写成多条 append 语句完全等价。
			for _, ref := range s.Srcs {
				body, err := payload(c, t, ref, rel, nest)
				if err != nil {
					return err
				}
				marked := refMarked(c, t, ref, body)
				n := len(tree.Lines())
				if s.Op == "append" {
					repl := []string{"", body}
					if marked {
						repl = []string{body}
					}
					edits = append(edits, edit{n, n, repl, len(edits)})
				} else {
					repl := []string{body, ""}
					if marked {
						repl = []string{body}
					}
					edits = append(edits, edit{0, 0, repl, len(edits)})
				}
			}
		case "frontmatter":
			other, ok, err := c.read(s.Layer, rel)
			if err != nil {
				return fmt.Errorf("%s: %v", s.Rng, err)
			}
			if !ok {
				return fmt.Errorf("%s: %s 层里没有 %s", s.Rng, s.Layer, rel)
			}
			fm, ok := ast.NewMarkdown(other).BodyOf("frontmatter")
			if !ok {
				return fmt.Errorf("%s: %s 的 %s 没有 frontmatter", s.Rng, s.Layer, rel)
			}
			hits := tree.Find("frontmatter", "")
			if len(hits) > 0 {
				edits = append(edits, edit{hits[0][0], hits[0][1], strings.Split(fm, "\n"), len(edits)})
			} else {
				edits = append(edits, edit{0, 0, append(strings.Split(fm, "\n"), ""), len(edits)})
			}
		}
	}
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].s != edits[j].s {
			return edits[i].s > edits[j].s
		}
		return edits[i].idx > edits[j].idx
	})
	for _, e := range edits {
		tree.Splice(e.s, e.e, e.repl)
	}
	return nil
}

func one(s Stmt, hits [][2]int, kind, anchor string) ([2]int, error) {
	if len(hits) == 0 {
		return [2]int{}, fmt.Errorf("%s: 锚点找不到：%s %q。"+
			"经线那份多半改了这个位置 —— 这不是故障，是该看一眼的信号。", s.Rng, kind, anchor)
	}
	if len(hits) > 1 {
		return [2]int{}, fmt.Errorf("%s: 锚点匹配到 %d 处：%s %q。"+
			"不做\"取第一个\"—— 那会静默插到错的地方。请写得更准。", s.Rng, len(hits), kind, anchor)
	}
	return hits[0], nil
}

func payloads(c *Config, t *Template, refs []Ref, rel string, nest *nestCtx) (string, error) {
	var parts []string
	for _, r := range refs {
		p, err := payload(c, t, r, rel, nest)
		if err != nil {
			return "", err
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, "\n"), nil
}

func isMarked(c *Config, t *Template, refs []Ref, body string) bool {
	if len(refs) == 0 {
		return false
	}
	return refMarked(c, t, refs[0], body)
}

func refMarked(c *Config, t *Template, r Ref, body string) bool {
	if r.IsLit {
		return false
	}
	m, ok := c.marksFor(r.Layer, t.Type)
	return ok && strings.HasPrefix(body, m.Begin)
}

// payload 是一条内容来源的求值。
func payload(c *Config, t *Template, r Ref, rel string, nest *nestCtx) (string, error) {
	if r.IsLit {
		return r.Literal, nil
	}
	srcRel := rel
	if nest != nil && nest.reuse != "" {
		// 【跨文件引用】两个入口是同一份文档的两种形态，共用同一份纬线扩展；
		// 经线那半各取各的。没有这个的话，两个入口就得各维护一份，而那两份迟早分头漂。
		srcRel = nest.reuse
	}
	src, ok, err := c.read(r.Layer, srcRel)
	if err != nil {
		return "", fmt.Errorf("%s: %v", r.Rng, err)
	}
	if !ok {
		return "", fmt.Errorf("%s: %s 层里没有 %s", r.Rng, r.Layer, srcRel)
	}
	useNest := nest
	if srcRel != rel {
		useNest = nil // 来源是另一个文件，它的结构由它自己的扩展名决定
	}
	if useNest != nil {
		// 【嵌套上下文里，源也要取嵌套体】否则会把三引号收尾行一起当成节内容带进产物。
		inner, ok := ast.New(useNest.typ, src).BodyOf(useNest.key)
		if !ok {
			return "", fmt.Errorf("%s: %s 的 %s 里没有键 `%s`", r.Rng, r.Layer, srcRel, useNest.key)
		}
		src = inner
	}
	if r.Kind == "body" || r.Kind == "all" {
		v := strings.TrimRight(src, "\n")
		if r.Kind == "body" {
			if b, ok := ast.NewMarkdown(src).BodyOf("body"); ok {
				v = strings.TrimRight(b, "\n")
			}
		}
		return mark(c, t, r.Layer, v), nil
	}
	if r.Anchor == "" {
		return "", fmt.Errorf("%s: `%s.%s` 需要一个锚点，写成 %s.%s[\"…\"]", r.Rng, r.Layer, r.Kind, r.Layer, r.Kind)
	}
	var a ast.Tree
	for _, tn := range ast.Types {
		if ast.Has(ast.Kinds(tn), r.Kind) && (useNest == nil || tn != t.Type) {
			a = ast.New(tn, src)
			break
		}
	}
	if a == nil {
		a = ast.NewMarkdown(src)
	}
	hits := a.Find(r.Kind, r.Anchor)
	span, err := one(Stmt{Rng: r.Rng}, hits, r.Kind, r.Anchor)
	if err != nil {
		return "", err
	}
	return mark(c, t, r.Layer, strings.TrimRight(strings.Join(a.Lines()[span[0]:span[1]], "\n"), "\n")), nil
}

// mark 给纬线内容包上标记 —— 「经线 100% 保留」靠剥掉标记再逐字节比来验证。
func mark(c *Config, t *Template, layer, v string) string {
	m, ok := c.marksFor(layer, t.Type)
	if !ok || strings.TrimSpace(v) == "" {
		return v
	}
	return m.Begin + "\n" + v + "\n" + m.End
}

func applyPatch(c *Config, t *Template, s Stmt, from string) (string, error) {
	src, ok, err := c.read(from, t.BasePath)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%s: %s 层里没有 %s", s.Rng, from, t.BasePath)
	}
	if _, err := exec.LookPath("patch"); err != nil {
		return "", fmt.Errorf("%s: 需要 patch 命令", s.Rng)
	}
	d, err := os.MkdirTemp("", "loom")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(d)
	w := filepath.Join(d, "w")
	if err := os.WriteFile(w, []byte(src), 0o644); err != nil {
		return "", err
	}
	diff, err := os.ReadFile(filepath.Join(filepath.Dir(t.Path), s.Body))
	if err != nil {
		return "", fmt.Errorf("%s: 读不到补丁文件 %s：%v", s.Rng, s.Body, err)
	}
	pf := filepath.Join(d, "p")
	if err := os.WriteFile(pf, []byte(strings.TrimRight(string(diff), "\n")+"\n"), 0o644); err != nil {
		return "", err
	}
	in, err := os.Open(pf)
	if err != nil {
		return "", err
	}
	defer in.Close()
	cmd := exec.Command("patch", "-s", "-p0", "-F0", "--no-backup-if-mismatch", w)
	cmd.Stdin = in
	out, _ := cmd.Output()
	if cmd.ProcessState == nil || !cmd.ProcessState.Success() {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return "", fmt.Errorf("%s: 补丁打不上 —— 经线多半改到了我们编辑过的地方。"+
			"先看经线这次改了什么，再重做补丁。patch: %s", s.Rng, msg)
	}
	b, err := os.ReadFile(w)
	return string(b), err
}

// applyBilingual：该键的值 = 纬线的值 + 经线的值。
//
// 【为什么要统一成一条指令】格式差异是实现细节，不该泄漏到语言里 ——
// markdown 一种写法、toml 另一种写法的话，读模板的人得先知道文件是什么格式
// 才知道该写哪一句。现在两边都写 `bilingual = ["description"]`，
// 由这里按树的类型各自落地。
//
// 【为什么经线那半编译时取，而不是手抄进纬线】手抄的第二份副本会在经线改了
// 之后悄悄漂移，而没有任何东西会告诉我们。
func applyBilingual(c *Config, t *Template, key string, tree ast.Tree, s Stmt) error {
	if c.Weft == "" {
		return fmt.Errorf("%s: bilingual 需要有一层声明 role = \"weft\"", s.Rng)
	}
	xs, ok, err := c.read(c.Weft, t.Target)
	if err != nil {
		return fmt.Errorf("%s: %v", s.Rng, err)
	}
	if !ok {
		return fmt.Errorf("%s: %s 层里没有 %s", s.Rng, c.Weft, t.Target)
	}
	val, ok := keyValue(t.Type, xs, key)
	if !ok {
		return fmt.Errorf("%s: %s 的那份里没有键 `%s`", s.Rng, c.Weft, key)
	}
	val = strings.Trim(strings.TrimSpace(val), `"`)

	from := t.From
	if from == "" {
		from = c.Warp
	}
	if up, ok, _ := c.read(from, t.BasePath); ok {
		if uv, ok := keyValue(t.Type, up, key); ok {
			uv = strings.Trim(strings.TrimSpace(uv), `"`)
			// 幂等：可以反复编译，已经拼过就不再拼
			if uv != "" && !strings.Contains(val, uv) {
				val = strings.TrimRight(val, " \t") + " " + uv
			}
		}
	}

	if t.Type == "markdown" {
		hits := tree.Find("frontmatter", "")
		if len(hits) == 0 {
			return fmt.Errorf("%s: 基底没有 frontmatter", s.Rng)
		}
		fm, _ := tree.BodyOf("frontmatter")
		// markdown 没有按键写回的接口 —— frontmatter 是一个行区间，按区间换
		lines := strings.Split(fm, "\n")
		done := false
		for i, l := range lines {
			if strings.HasPrefix(l, key+":") {
				lines[i] = key + ": " + val
				done = true
				break
			}
		}
		if !done {
			return fmt.Errorf("%s: 基底的 frontmatter 里没有键 `%s`", s.Rng, key)
		}
		tree.Splice(hits[0][0], hits[0][1], lines)
		return nil
	}
	if !tree.SetBody(key, val) {
		return fmt.Errorf("%s: 基底里没有键 `%s`", s.Rng, key)
	}
	return nil
}

// keyValue 从一份源里取一个键：markdown 看 frontmatter，其它类型看它自己的键。
func keyValue(typ, src, key string) (string, bool) {
	if typ != "markdown" {
		return ast.New(typ, src).BodyOf(key)
	}
	fm, ok := ast.NewMarkdown(src).BodyOf("frontmatter")
	if !ok {
		return "", false
	}
	for _, l := range strings.Split(fm, "\n") {
		if strings.HasPrefix(l, key+":") {
			return strings.TrimPrefix(l, key+":"), true
		}
	}
	return "", false
}
