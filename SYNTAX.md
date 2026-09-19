# 语法草案

三个方案，参考 Go。语义完全相同，都遵守 DESIGN.md 里那条不变式：**写集合之外逐字节不变**。

每个方案都用同两个**真实**样本检验：

- `xsdd/docs/getting-started.lm` —— 16 条语句，逐节翻译，上游每节后面跟一节中文
- `xsdd/AGENTS.lm` —— 只有一句 `base.merge(self)`

---

## 共同基座：像 Go 的那部分

```go
func weave(base, self markdown) {
    base.Overview.after(self["Where this fits"])
    base.append(self["Appendix"])
}
```

| Go 的什么 | 这里对应什么 |
|---|---|
| `func` + 大括号 | 一个产物由一个函数织成 |
| 参数名在前、类型在后：`base, self markdown` | **类型就是文档种类**——md / js / json / shell |
| 方法调用 | 位置词是地址的最后一步 |
| 首字母大小写区分导出 | **大写来自文档**（作者写的标题），**小写属于语言**（`after` / `append`） |
| 没有异常 | 不猜：找不到、匹配到两个，都是编译错误 |
| gofmt | `lm fmt`，唯一的规范形式 |

名字里有空格或标点时用下标，和 Go 的 map 取值同形：

```go
base["Quick Start (Any Agent)"].after(self["Quick Start（任何 agent）"])
```

### 返回类型就是保证量

这是这门语言和 Go 最像、也最关键的一处：**签名直接说明了这个文件有多可信**。

```go
// 不返回 → 只有写 → 写集合可界定 → 逐字节可验证
func weave(base, self markdown) { ... }

// 返回一份文档 → 写的是根跨度 → 保证量为零 → 必须给理由
func weave(base, self markdown) markdown {
    return self
} // reason: "上游那份和我们的会跑两遍"
```

你最早草图里的 `return a.call(b.xxx, md.title)` 落在同一条规则下：**返回算出来的东西 = 根写入 = 要理由**。不是禁止计算，是**计算的代价写在签名上，编译器和人都看得见**。

---

## 方案 A：包（Go 的原生答案）

复用就是**导入一个包、调一个函数**。不发明 `rule` / `apply` / `use`。

```go
// xsdd/loom/skills.lm —— 一个包，自己不产出任何文件
package skills

/// 每个 skill 都套：描述双语拼接，正文接在上游之后。
func Frontmatter(base, self markdown) {
    base.frontmatter.description.start(self.frontmatter.description)
    base.append(self.body)
}
```

```go
// xsdd/skills/testing/SKILL.lm
import "loom/skills"

func weave(base, self markdown) {
    skills.Frontmatter(base, self)
    base.Overview.after(self["Where this fits"])
}
```

**getting-started 会变成：**

```go
func weave(base, self markdown) {
    base.How_Skills_Work.after(self["Skill 如何工作"])
    base["Quick Start (Any Agent)"].after(self["Quick Start（任何 agent）"])
    // …还有 14 行
}
```

**代价**：16 行还是 16 行；37 处重复变成 37 处 `import` + 37 处调用，**比现在更长**。Go 的答案在这里水土不服——Go 没有"把同一个函数自动套到几十个文件上"的需求。

---

## 方案 B：指令注释（Go 的 build tag 思路）

Go 用 `//go:build linux` 这种注释指令控制构建。同一招：

```go
// xsdd/loom/skills.lm
//loom:apply "skills/**"
package skills

func Frontmatter(base, self markdown) {
    base.frontmatter.description.start(self.frontmatter.description)
    base.append(self.body)
}
```

命中 `skills/**` 的产物自动套用，**被套的文件一个字都不用写**。

```go
// xsdd/skills/testing/SKILL.lm —— 只剩自己特有的那句
func weave(base, self markdown) {
    base.Overview.after(self["Where this fits"])
}
```

那 16 个"除了样板没有一句自己的话"的模板**整个文件消失**。

**代价**：作用在远处——读一个模板时，看不出它还被别的文件套了东西。缓解办法是构建报告必须逐个列出"这个产物套用了哪些包的哪些函数"，以及编辑器把套用的语句以灰色显示在文件里。

---

## 方案 C：编译器推断（把复杂留给编译器）

前两个方案都在想办法**让作者少写**。这个方案问的是另一个问题：**那 16 行，作者为什么要写？**

`getting-started` 的真实结构是：我们的文件和上游**逐节平行**，每一节都插在同名上游节之后。这个事实在我们的 `.md` 里**已经写着了**——章节顺序就是它。

```go
// xsdd/docs/getting-started.lm —— 全部
func weave(base, self markdown) {
    align(base, self)
}
```

`align` 的含义：我们文件里的节，按顺序对齐到上游的节序列，各自插在对应节之后。对不齐的地方——上游多一节、少一节、顺序变了——**编译器停下来报错并指出是哪一节**，不猜。

这不是新机制：anchor completion 今天已经在做同一件事的弱化版（按邻居推断单个节的位置，并把语句写回模板）。`align` 只是把"单个"变成"整列"。

**代价**：`align` 是一个**假设**——假设两边结构平行。假设不成立时必须响亮地失败，而不是织出一个看起来对的产物。这条是这门语言的底线，所以 `align` 的错误信息质量，决定它能不能存在。

---

## 三个方案的关系

它们不互斥：

| | 解决什么 | 真实数据上的效果 |
|---|---|---|
| A 包 | 复用的**载体** | 语法基座，但单靠它更啰嗦 |
| B 指令 | 把规则**套到一批文件** | 24 个只有样板的模板消失 |
| C 推断 | 让**本来就不该写的**不用写 | getting-started 从 16 行到 1 行 |

**A 是地基，B 和 C 是两个不同方向的省。** B 省的是"重复的话"，C 省的是"编译器本来就能看出来的话"。

---

## 还没解决的

**`getting-started` 可能是那个证伪例子。** 16 条语句表达的是「两个序列对齐」，而 DESIGN.md 第九节明确排除了控制流——没有循环，就没法说"对每一节都……"。

有两条出路，选哪条是真正的设计决策：

1. **`align` 这类内建算子**：把「对齐」做成语言的一个词。不引入循环，写集合仍然静态可知。代价是每多一种模式就多一个词。
2. **承认需要遍历**：引入受限的循环——只能遍历文档的部分，循环体只能是写。写集合仍然可界定（它是每次迭代写集合的并），不变式**不破**。

第 2 条比我在 DESIGN.md 里一刀切排除控制流更诚实：**破坏不变式的是「算出来的文本」，不是「重复的写」。** 一个只能遍历部分、只能产生写的循环，写集合照样静态可知。

这一条需要你定。
