package loom

// 模板的语法层：把一份 .lm（HCL）读成一串语句。
//
// 【为什么用 HCL 而不是自造语法】自造的每一条规则都得自己实现、自己报错、自己教。
// HCL 是 Terraform 那一套，认知成本已经付过了：块 + 属性 + 表达式，
// 报错带 文件:行:列，编辑器高亮现成。这门语言真正独有的东西是**怎么织**，
// 不是括号长什么样 —— 括号上省下的力气该花在织法上。
//
// 语句在模板里的**先后顺序是有意义的**（同一个锚点上的两条插入谁在前），
// 而 HCL 的属性在 Body 里是一张 map。所以每条语句都记住自己的字节偏移，
// 解析完按偏移排回模板顺序。

import (
	"fmt"
	"sort"

	"github.com/axfor/loom/ast"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Ref 是一条内容来源：某一层里的某个节点，或一段字面量。
type Ref struct {
	Layer   string // 层名
	Kind    string // heading / line / frontmatter / key / function / marker / body / all
	Anchor  string // body / all 没有锚点
	Literal string // 字面量内容（字符串或 heredoc）
	IsLit   bool
	Rng     hcl.Range
}

func (r Ref) String() string {
	if r.IsLit {
		return "<字面量>"
	}
	if r.Anchor == "" {
		return r.Layer + "." + r.Kind
	}
	return fmt.Sprintf("%s.%s[%q]", r.Layer, r.Kind, r.Anchor)
}

// Stmt 是一条语句。Op 决定读哪些字段。
type Stmt struct {
	Op     string // after/before/replace/append/prepend/frontmatter/set/bilingual/in/anchor/patch/inherit/override/new
	Kind   string // 位置：节点种类
	Anchor string // 位置：锚点
	Srcs   []Ref  // 要插什么

	As    string // in：里面按什么类型解析
	Reuse string // in：这一块的纬线内容改从哪个文件取
	Kids  []Stmt // in：块内语句

	Layer  string   // frontmatter：整块取哪一层
	SetKey string   // set：写哪个键
	SetRef Ref      // set：值取自哪里
	Key    string   // bilingual：哪个键
	Name   string   // anchor：锚点名
	Body   string   // patch：unified diff
	Names  []string // inherit/override/new：函数名

	Byte int // 模板里的字节偏移 —— 语句顺序靠它还原
	Rng  hcl.Range
}

// Template 是一份模板：织什么、按什么结构织、以哪一层为底、怎么织。
type Template struct {
	Path     string
	Target   string
	Type     string
	From     string // 经线层名
	BasePath string // 经线里的路径（改名的资产用；默认 = Target）
	Covers   []string
	Stmts    []Stmt
	Anchors  map[string]Stmt
}

var weaveSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{{Type: "weave", LabelNames: []string{"target"}}},
}

var bodyAttrs = []hcl.AttributeSchema{
	{Name: "type"}, {Name: "from"}, {Name: "path"}, {Name: "covers"},
	{Name: "frontmatter"}, {Name: "bilingual"}, {Name: "set"},
	{Name: "inherit"}, {Name: "override"}, {Name: "new"},
	{Name: "patch"}, {Name: "append"}, {Name: "prepend"},
	{Name: "as"}, {Name: "reuse"}, {Name: "insert"}, {Name: "with"},
}

var bodyBlocks = []hcl.BlockHeaderSchema{
	{Type: "after", LabelNames: []string{"kind", "anchor"}},
	{Type: "before", LabelNames: []string{"kind", "anchor"}},
	{Type: "replace", LabelNames: []string{"kind", "anchor"}},
	{Type: "in", LabelNames: []string{"key"}},
	{Type: "anchor", LabelNames: []string{"name"}},
}

var stmtSchema = &hcl.BodySchema{Attributes: bodyAttrs, Blocks: bodyBlocks}

// ParseTemplate 读一份 .lm。
func ParseTemplate(path string, src []byte) (*Template, error) {
	f, diags := hclsyntax.ParseConfig(src, path, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	top, diags := f.Body.Content(weaveSchema)
	if diags.HasErrors() {
		return nil, diags
	}
	if len(top.Blocks) != 1 {
		return nil, fmt.Errorf("%s: 一份模板要有且只有一个 weave 块，收到 %d 个", path, len(top.Blocks))
	}
	b := top.Blocks[0]
	t := &Template{Path: path, Target: b.Labels[0], Anchors: map[string]Stmt{}}
	stmts, hdr, err := parseBody(b.Body, true)
	if err != nil {
		return nil, err
	}
	t.Type = hdr.typ
	t.From = hdr.from
	t.BasePath = hdr.path
	t.Covers = hdr.covers
	t.Stmts = stmts
	if t.Type == "" {
		return nil, fmt.Errorf("%s: weave 块缺 `type`（有 %v）", path, ast.Types)
	}
	if ast.Kinds(t.Type) == nil {
		return nil, fmt.Errorf("%s: 未知资源类型 `%s`（有 %v）", path, t.Type, ast.Types)
	}
	if t.BasePath == "" {
		t.BasePath = t.Target
	}
	for _, s := range stmts {
		if s.Op != "anchor" {
			continue
		}
		if _, dup := t.Anchors[s.Name]; dup {
			return nil, fmt.Errorf("%s: 锚点 `%s` 重复定义", s.Rng, s.Name)
		}
		t.Anchors[s.Name] = s
	}
	return t, nil
}

type header struct {
	typ, from, path string
	covers          []string
}

func parseBody(body hcl.Body, top bool) ([]Stmt, header, error) {
	var hdr header
	content, diags := body.Content(stmtSchema)
	if diags.HasErrors() {
		return nil, hdr, diags
	}
	var out []Stmt
	for name, a := range content.Attributes {
		switch name {
		case "type", "from", "path", "as", "reuse":
			if !top && (name == "type" || name == "from" || name == "path") {
				return nil, hdr, fmt.Errorf("%s: `%s` 只能写在 weave 块上", a.Range, name)
			}
			v, err := strAttr(a)
			if err != nil {
				return nil, hdr, err
			}
			switch name {
			case "type":
				hdr.typ = v
			case "from":
				hdr.from = v
			case "path":
				hdr.path = v
			}
			continue // as / reuse 由父块读
		case "covers":
			v, err := strListAttr(a)
			if err != nil {
				return nil, hdr, err
			}
			hdr.covers = v
			continue
		}
		s, err := attrStmt(name, a)
		if err != nil {
			return nil, hdr, err
		}
		out = append(out, s)
	}
	for _, blk := range content.Blocks {
		s, err := blockStmt(blk)
		if err != nil {
			return nil, hdr, err
		}
		out = append(out, s)
	}
	// 还原模板顺序 —— HCL 的属性是 map，顺序在语义里是有意义的
	sort.SliceStable(out, func(i, j int) bool { return out[i].Byte < out[j].Byte })
	return out, hdr, nil
}

func attrStmt(name string, a *hcl.Attribute) (Stmt, error) {
	s := Stmt{Op: name, Byte: a.Range.Start.Byte, Rng: a.Range}
	switch name {
	case "append", "prepend":
		refs, err := parseRefs(a.Expr)
		if err != nil {
			return s, err
		}
		s.Srcs = refs
	case "frontmatter":
		v, err := layerName(a.Expr)
		if err != nil {
			return s, err
		}
		s.Layer = v
	case "bilingual":
		keys, err := strListAttr(a)
		if err != nil {
			return s, err
		}
		if len(keys) == 0 {
			return s, fmt.Errorf("%s: bilingual 要给至少一个键", a.Range)
		}
		s.Names = keys
	case "inherit", "override", "new":
		names, err := strListAttr(a)
		if err != nil {
			return s, err
		}
		s.Names = names
	case "patch":
		// 【为什么补丁不写在模板里】HCL 的 heredoc 会做 ${…} 插值，而 diff 里
		// 天然带 ${VAR} —— 内联就得转义，转义之后模板里那段**不再是一份 diff**：
		// 编辑器不认、patch(1) 不认、复制出来直接用会失败。
		// 放进独立的 .diff 文件，它就还是一份 diff。
		v, err := strAttr(a)
		if err != nil {
			return s, err
		}
		s.Body = v
	case "set":
		obj, ok := a.Expr.(*hclsyntax.ObjectConsExpr)
		if !ok {
			return s, fmt.Errorf("%s: set 要写成 `set = { <键> = <层>.<键> }`", a.Range)
		}
		var kids []Stmt
		for _, it := range obj.Items {
			k, err := objKey(it.KeyExpr)
			if err != nil {
				return s, err
			}
			r, err := parseRef(it.ValueExpr)
			if err != nil {
				return s, err
			}
			kids = append(kids, Stmt{Op: "set", SetKey: k, SetRef: r,
				Byte: it.KeyExpr.Range().Start.Byte, Rng: it.KeyExpr.Range()})
		}
		s.Op = "setgroup"
		s.Kids = kids
	case "insert", "with":
		return s, fmt.Errorf("%s: `%s` 只能写在 after / before / replace 块里", a.Range, name)
	default:
		return s, fmt.Errorf("%s: 未知属性 `%s`", a.Range, name)
	}
	return s, nil
}

func blockStmt(b *hcl.Block) (Stmt, error) {
	s := Stmt{Op: b.Type, Byte: b.DefRange.Start.Byte, Rng: b.DefRange}
	switch b.Type {
	case "after", "before", "replace":
		// 位置块里只认一条 insert / with —— 别的语句该写在外层，
		// 嵌套一层位置块只会让"这条到底作用在谁身上"变得要猜。
		s.Kind, s.Anchor = b.Labels[0], b.Labels[1]
		want := "insert"
		if b.Type == "replace" {
			want = "with"
		}
		content, diags := b.Body.Content(&hcl.BodySchema{
			Attributes: []hcl.AttributeSchema{{Name: want, Required: true}},
		})
		if diags.HasErrors() {
			return s, diags
		}
		refs, err := parseRefs(content.Attributes[want].Expr)
		if err != nil {
			return s, err
		}
		s.Srcs = refs
	case "in":
		s.Key = b.Labels[0]
		content, diags := b.Body.Content(stmtSchema)
		if diags.HasErrors() {
			return s, diags
		}
		var err error
		if a, ok := content.Attributes["as"]; ok {
			if s.As, err = strAttr(a); err != nil {
				return s, err
			}
		}
		if a, ok := content.Attributes["reuse"]; ok {
			if s.Reuse, err = strAttr(a); err != nil {
				return s, err
			}
		}
		if s.As == "" {
			return s, fmt.Errorf("%s: in 块要写 `as = \"<类型>\"`", b.DefRange)
		}
		if ast.Kinds(s.As) == nil {
			return s, fmt.Errorf("%s: 未知嵌套类型 `%s`", b.DefRange, s.As)
		}
		kids, _, err := parseBody(b.Body, false)
		if err != nil {
			return s, err
		}
		s.Kids = kids
	case "anchor":
		s.Name = b.Labels[0]
		content, diags := b.Body.Content(stmtSchema)
		if diags.HasErrors() {
			return s, diags
		}
		if len(content.Attributes) != 1 {
			return s, fmt.Errorf("%s: anchor 块里要有且只有一条 `<节点种类> = \"<定位式>\"`", b.DefRange)
		}
		for k, a := range content.Attributes {
			v, err := strAttr(a)
			if err != nil {
				return s, err
			}
			s.Kind, s.Anchor = k, v
		}
	}
	return s, nil
}

// parseRefs 认单条引用，也认一个列表。
func parseRefs(e hcl.Expression) ([]Ref, error) {
	if tup, ok := e.(*hclsyntax.TupleConsExpr); ok {
		var out []Ref
		for _, it := range tup.Exprs {
			r, err := parseRef(it)
			if err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, nil
	}
	r, err := parseRef(e)
	if err != nil {
		return nil, err
	}
	return []Ref{r}, nil
}

// parseRef 认三种写法：
//
//	xsdd.body               整份正文
//	xsdd.heading["X"]       某一节
//	"字面量" / <<EOT … EOT   直接写在模板里的内容
func parseRef(e hcl.Expression) (Ref, error) {
	rng := e.Range()
	if tr, ok := e.(*hclsyntax.ScopeTraversalExpr); ok {
		t := tr.Traversal
		if len(t) < 2 || len(t) > 3 {
			return Ref{}, fmt.Errorf("%s: 引用要写成 `<层>.<节点种类>` 或 `<层>.<节点种类>[\"<锚点>\"]`", rng)
		}
		r := Ref{Layer: t.RootName(), Rng: rng}
		attr, ok := t[1].(hcl.TraverseAttr)
		if !ok {
			return Ref{}, fmt.Errorf("%s: `%s` 后面要跟节点种类", rng, r.Layer)
		}
		r.Kind = attr.Name
		if len(t) == 3 {
			idx, ok := t[2].(hcl.TraverseIndex)
			if !ok {
				return Ref{}, fmt.Errorf("%s: 锚点要写成 [\"…\"]", rng)
			}
			if idx.Key.Type() != cty.String {
				return Ref{}, fmt.Errorf("%s: 锚点要是字符串", rng)
			}
			r.Anchor = idx.Key.AsString()
		}
		return r, nil
	}
	v, diags := e.Value(nil)
	if diags.HasErrors() {
		return Ref{}, diags
	}
	if v.Type() != cty.String {
		return Ref{}, fmt.Errorf("%s: 这里要一段引用或一段字面量", rng)
	}
	return Ref{IsLit: true, Literal: v.AsString(), Rng: rng}, nil
}

func layerName(e hcl.Expression) (string, error) {
	if tr, ok := e.(*hclsyntax.ScopeTraversalExpr); ok && len(tr.Traversal) == 1 {
		return tr.Traversal.RootName(), nil
	}
	v, diags := e.Value(nil)
	if diags.HasErrors() {
		return "", diags
	}
	if v.Type() != cty.String {
		return "", fmt.Errorf("%s: 这里要一个层名", e.Range())
	}
	return v.AsString(), nil
}

func objKey(e hcl.Expression) (string, error) {
	if k, ok := e.(*hclsyntax.ObjectConsKeyExpr); ok {
		if tr, ok := k.Wrapped.(*hclsyntax.ScopeTraversalExpr); ok && len(tr.Traversal) == 1 {
			return tr.Traversal.RootName(), nil
		}
	}
	v, diags := e.Value(nil)
	if diags.HasErrors() {
		return "", diags
	}
	if v.Type() != cty.String {
		return "", fmt.Errorf("%s: 键要是标识符或字符串", e.Range())
	}
	return v.AsString(), nil
}

func strListAttr(a *hcl.Attribute) ([]string, error) {
	v, diags := a.Expr.Value(nil)
	if diags.HasErrors() {
		return nil, diags
	}
	if v.Type() == cty.String {
		return []string{v.AsString()}, nil
	}
	if !v.Type().IsTupleType() && !v.Type().IsListType() {
		return nil, fmt.Errorf("%s: 这里要一个字符串或字符串列表", a.Range)
	}
	var out []string
	for it := v.ElementIterator(); it.Next(); {
		_, ev := it.Element()
		if ev.Type() != cty.String {
			return nil, fmt.Errorf("%s: 列表里要全是字符串", a.Range)
		}
		out = append(out, ev.AsString())
	}
	return out, nil
}
