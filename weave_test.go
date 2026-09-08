package loom_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom"
)

const fixture = "testdata/repo"

func load(t *testing.T) *loom.Config {
	t.Helper()
	c, err := loom.LoadConfig(filepath.Join(fixture, loom.ConfigName))
	if err != nil {
		t.Fatalf("读配置: %v", err)
	}
	return c
}

func weave(t *testing.T, c *loom.Config, tpl string) string {
	t.Helper()
	tm, err := loom.LoadTemplate(filepath.Join(fixture, "templates", tpl))
	if err != nil {
		t.Fatalf("%s 解析失败: %v", tpl, err)
	}
	out, err := loom.Weave(c, tm)
	if err != nil {
		t.Fatalf("%s 编织失败: %v", tpl, err)
	}
	return out
}

// 产物必须与金样逐字节相同 —— 编织是确定的，同样的源永远织出同一匹布。
func TestWeaveGolden(t *testing.T) {
	c := load(t)
	for _, cse := range []struct{ tpl, golden string }{
		{"doc.loom", "doc.md"},
		{"run.sh.loom", "run.sh"},
	} {
		got := weave(t, c, cse.tpl)
		want, err := os.ReadFile(filepath.Join(fixture, "golden", cse.golden))
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s 与金样不同\n--- 得到 ---\n%s\n--- 期望 ---\n%s", cse.tpl, got, want)
		}
	}
}

// 这门语言存在的理由：**抽掉纬线，经线原样还在**。
// 这条不是文档里的一句话，是可以机械验证的性质。
func TestWarpSurvivesStrip(t *testing.T) {
	c := load(t)
	got := weave(t, c, "doc.loom")
	stripped := stripMarks(got, "<!-- MINE:BEGIN -->", "<!-- MINE:END -->")

	up, err := os.ReadFile(filepath.Join(fixture, "upstream", "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	// frontmatter 是显式整块换掉的（frontmatter = mine），不在标记内，单独比正文
	if body(stripped) != body(string(up)) {
		t.Errorf("剥掉纬线之后正文与经线不同\n--- 剥完 ---\n%q\n--- 经线 ---\n%q",
			body(stripped), body(string(up)))
	}
}

func stripMarks(s, begin, end string) string {
	var out []string
	skip := false
	for _, l := range strings.Split(s, "\n") {
		switch {
		case l == begin:
			skip = true
		case l == end:
			skip = false
		case !skip:
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func body(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return s
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.TrimRight(strings.Join(lines[i+1:], "\n"), "\n")
		}
	}
	return s
}

// 错误必须响，而且要说清是哪一种 —— 静默穿错位置的产物看起来完全正常。
func TestErrorsAreLoud(t *testing.T) {
	c := load(t)
	cases := []struct {
		name, tpl, want string
	}{
		{"锚点找不到", `weave "doc.md" {
  type = "markdown"
  after "heading" "No Such Section" { insert = mine.heading["附录"] }
}`, "锚点找不到"},
		{"节点种类用错", `weave "doc.md" {
  type = "markdown"
  after "key" "Overview" { insert = mine.heading["附录"] }
}`, "这个类型没有"},
		{"未知层名", `weave "doc.md" {
  type = "markdown"
  append = nosuch.body
}`, "未知层名"},
		{"引用缺锚点", `weave "doc.md" {
  type = "markdown"
  append = mine.heading
}`, "需要一个锚点"},
		{"基底不存在", `weave "nope.md" {
  type = "markdown"
  append = mine.body
}`, "层里没有"},
	}
	for _, cse := range cases {
		t.Run(cse.name, func(t *testing.T) {
			tm, err := loom.ParseTemplate("t.loom", []byte(cse.tpl))
			if err == nil {
				_, err = loom.Weave(c, tm)
			}
			if err == nil {
				t.Fatal("本该报错，却编织成功了 —— 这正是这门语言要根除的失败形状")
			}
			if !strings.Contains(err.Error(), cse.want) {
				t.Errorf("报错内容对不上\n得到: %v\n期望含: %s", err, cse.want)
			}
		})
	}
}

// 锚点匹配到多处必须报错，不能取第一个。
func TestAmbiguousAnchorRefuses(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.hcl"), `
layer "up" {
  dir  = "up"
  role = "warp"
}
layer "me" {
  dir  = "me"
  role = "weft"
}
templates = "t"
`)
	mustWrite(t, filepath.Join(dir, "up", "a.md"), "## Same\n\nx\n\n## Same\n\ny\n")
	mustWrite(t, filepath.Join(dir, "me", "a.md"), "## Mine\n\nz\n")
	mustWrite(t, filepath.Join(dir, "t", "a.loom"), `
weave "a.md" {
  type = "markdown"
  from = "up"
  after "heading" "Same" { insert = me.heading["Mine"] }
}`)
	c, err := loom.LoadConfig(filepath.Join(dir, "loom.hcl"))
	if err != nil {
		t.Fatal(err)
	}
	tm, err := loom.LoadTemplate(filepath.Join(dir, "t", "a.loom"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loom.Weave(c, tm); err == nil || !strings.Contains(err.Error(), "匹配到 2 处") {
		t.Fatalf("重名锚点该拒绝，得到: %v", err)
	}
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.TrimLeft(s, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 语句顺序有意义：同一个锚点上的两条插入，写在前的就在前。
func TestStatementOrderIsSourceOrder(t *testing.T) {
	c := load(t)
	tm, err := loom.ParseTemplate("t.loom", []byte(`weave "doc.md" {
  type = "markdown"
  append = [mine.heading["调度层关系"], mine.heading["附录"]]
}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := loom.Weave(c, tm)
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(out, "## 调度层关系"), strings.Index(out, "## 附录")
	if i < 0 || j < 0 || i > j {
		t.Errorf("追加顺序没有跟着模板走：调度层关系@%d 附录@%d", i, j)
	}
}

// list 的元信息必须和真正编织时用的是同一份解析 —— 外层的门靠它，
// 而「每道门自己再解析一遍」正是这条命令要消灭的东西。
func TestDescribeMatchesWeave(t *testing.T) {
	c := load(t)
	i, err := loom.Describe(c, filepath.Join(fixture, "templates", "doc.loom"))
	if err != nil {
		t.Fatal(err)
	}
	if i.Target != "doc.md" || i.Type != "markdown" || i.From != "upstream" {
		t.Errorf("元信息不对: %+v", i)
	}
	if i.Path != "doc.md" {
		t.Errorf("基底路径默认该等于产物路径，得到 %q", i.Path)
	}
	if len(i.Anchors) != 1 || i.Anchors[0].Kind != "heading" || i.Anchors[0].Anchor != "Overview" {
		t.Errorf("锚点没报全: %+v", i.Anchors)
	}
	// frontmatter + after 的 insert + append 的 insert
	if len(i.Inserts) != 3 {
		t.Errorf("内容来源该有 3 条，得到 %d: %+v", len(i.Inserts), i.Inserts)
	}
}
