package ast

import "testing"

// markdown 的节点是标题 —— 但代码围栏里的 # 不是标题。实测踩过。
func TestMarkdownIgnoresHeadingsInFences(t *testing.T) {
	m := NewMarkdown("## Real\n\n```\n## Fake\n```\n\n## Also Real\n")
	if got := len(m.Find("heading", "Fake")); got != 0 {
		t.Errorf("围栏里的 ## Fake 被当成标题了（%d 处）", got)
	}
	if got := len(m.Find("heading", "Real")); got != 1 {
		t.Errorf("## Real 该找到 1 处，得到 %d", got)
	}
}

// 一节到下一个标题为止，不管层级 —— 子节属于父节。
func TestMarkdownSectionSpan(t *testing.T) {
	m := NewMarkdown("## A\n\nbody\n\n### A1\n\nsub\n\n## B\n")
	hits := m.Find("heading", "A")
	if len(hits) != 1 {
		t.Fatalf("找到 %d 处", len(hits))
	}
	if hits[0][0] != 0 || hits[0][1] != 4 {
		t.Errorf("A 的区间该是 [0,4)，得到 %v", hits[0])
	}
}

func TestMarkdownFrontmatter(t *testing.T) {
	m := NewMarkdown("---\nname: x\n---\n\nbody\n")
	fm, ok := m.BodyOf("frontmatter")
	if !ok || fm != "---\nname: x\n---" {
		t.Errorf("frontmatter 取错了: %q", fm)
	}
	// 不 lstrip：frontmatter 之后那个空行是正文的一部分
	b, _ := m.BodyOf("body")
	if b != "\nbody\n" {
		t.Errorf("正文该保留起始空行，得到 %q", b)
	}
	if _, ok := NewMarkdown("no fm here\n").BodyOf("frontmatter"); ok {
		t.Error("没有 frontmatter 时不该报告有")
	}
}

// shell 的函数切法不带状态 —— 上一个函数没在列首闭合就一直往里吞的写法会**静默丢函数**。
func TestShellFunctionsAreIndependent(t *testing.T) {
	s := NewShell("a() {\n  x\n}\n\nb() {\n  y\n}\n")
	for _, n := range []string{"a", "b"} {
		if got := len(s.Find("function", n)); got != 1 {
			t.Errorf("函数 %s 该找到 1 处，得到 %d", n, got)
		}
	}
}

func TestShellUnclosedIsReported(t *testing.T) {
	s := NewShell("a() {\n  x\n")
	if len(s.Unclosed) != 1 || s.Unclosed[0] != "a" {
		t.Errorf("有头无尾的函数该被报出来，得到 %v", s.Unclosed)
	}
	if len(s.Find("function", "a")) != 0 {
		t.Error("解析不了的函数不能当成找到了")
	}
}

// 横幅锚点按**前缀**匹配 —— 横幅尾部常是一串装饰用的 ─，
// 要求写全等于逼人抄装饰线，抄错一个字符就找不到。
func TestShellMarkerPrefixMatch(t *testing.T) {
	s := NewShell("# ── Test 3: blocks ─────────\nx\n# ── Test 4 ───\ny\n")
	hits := s.Find("marker", "# ── Test 3")
	if len(hits) != 1 {
		t.Fatalf("横幅该按前缀找到 1 处，得到 %d", len(hits))
	}
	if hits[0][1] != 2 {
		t.Errorf("横幅段该到下一条横幅为止，得到 %v", hits[0])
	}
}

func TestTomlBlockAndScalar(t *testing.T) {
	tm := NewToml("description = \"x\"\nprompt = \"\"\"\nline1\nline2\n\"\"\"\n")
	if v, _ := tm.BodyOf("description"); v != `"x"` {
		t.Errorf("标量取错: %q", v)
	}
	if v, _ := tm.BodyOf("prompt"); v != "line1\nline2" {
		t.Errorf("三引号块取错: %q", v)
	}
	if !tm.SetBody("description", "y") {
		t.Fatal("写不进去")
	}
	if v, _ := tm.BodyOf("description"); v != `"y"` {
		t.Errorf("写回后取错: %q", v)
	}
}

// text 只做精确整行匹配，绝不模糊 —— 宁可让人写得更准。
func TestTextExactLineOnly(t *testing.T) {
	x := NewText("alpha\nbeta\nbetamax\n")
	if got := len(x.Find("line", "beta")); got != 1 {
		t.Errorf("beta 该只匹配整行的那一条，得到 %d", got)
	}
}

// JSON 必须保序 —— 键顺序是人排的、有含义的，重排一次就再也对不回去。
func TestJSONPreservesKeyOrder(t *testing.T) {
	src := `{"z":1,"a":{"y":"二","b":[1,2]},"m":null}`
	v, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"z\": 1,\n  \"a\": {\n    \"y\": \"二\",\n    \"b\": [\n      1,\n      2\n    ]\n  },\n  \"m\": null\n}"
	if got := v.Marshal(); got != want {
		t.Errorf("序列化不对\n得到:\n%s\n期望:\n%s", got, want)
	}
}

// 非 ASCII 不转成 \u —— 中文该在产物里读得出来。
func TestJSONKeepsUnicodeLiteral(t *testing.T) {
	v, _ := Parse(`{"k":"中文 中"}`)
	if got := v.Marshal(); got != "{\n  \"k\": \"中文 中\"\n}" {
		t.Errorf("得到 %q", got)
	}
}

func TestJSONRejectsGarbage(t *testing.T) {
	if _, err := Parse(`{"a":}`); err == nil {
		t.Error("坏 JSON 该报错")
	}
}
