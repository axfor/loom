# Loom 语法

完整规格。设计依据见 [DESIGN.md](DESIGN.md)，这里只讲怎么写。

---

## 0. 先看全貌

```go
// xsdd/skills/testing/SKILL.lm
// 产物是 skills/testing/SKILL.md

base.frontmatter["description"].start(self.frontmatter["description"])

base["Overview"].after(self["Where this fits"])

base["Verification"].before:
    ## 自检清单

    跑 `lm check` 确认：

    ```sh
    lm check -o ../plugins/XSDD
    ```

base["How it compares"].drop(reason: "上游在和别的项目比，与我们无关")

base.append(self.body)
```

一个 `.lm` 文件产出一个产物。**文件本身就是函数体**——没有 `func`、没有 `package`、没有 `import`。

---

## 1. 一条总规则

> **方括号是选择，点号是语言。**

方括号里是**文档里的东西**：作者写的名字、谓词。
点号后面是**语言的词**：部件类别、位置、操作、轴。数量有限，可补全，可类型检查。

```
base["Overview"].after(...)
     ^^^^^^^^^^  ^^^^^
     文档的数据   语言的词
```

---

## 2. 词法

| 元素 | 写法 |
|---|---|
| 注释 | `//` 到行尾 |
| 字符串 | `"..."`，单行，转义只有 `\"` 和 `\\`；其余反斜杠原样保留（正则好写） |
| 块 | 冒号 + 换行 + 缩进（见 §6） |
| 语句结束 | 换行 |
| 占位符 | `{{@name}}`，`{{@@name}}` 表示字面的 `{{@name}}` |

没有数字字面量、没有布尔字面量——除了谓词里（§4.2），语言里没有可以计算的东西。

---

## 3. 两个隐式参数

每个 `.lm` 文件里有且只有两个根：

| 根 | 是什么 | 可写吗 |
|---|---|---|
| `base` | 上游层里同路径的那份文档 | **可以**——产物就是被织过的 base |
| `self` | 我们层里同路径的那份文档 | 不可以，它是内容来源 |

它们的**种类由产物的扩展名决定**，作者不写类型。`SKILL.lm` 产出 `SKILL.md`，于是两者都是 `markdown`。

`base` 指向别处时显式改：

```go
base = up["old/NAME.md"]        // 上游改过路径
```

---

## 4. 选择

### 4.1 按名字：取一个

```go
base["Overview"]
base["Quick Start (Any Agent)"]
self["1. clone 仓库"]
```

名字就是作者写的那串字——标题、函数名、键名。**没有第二种写法**：没有标识符形式，没有下划线代空格。

嵌套按层加方括号：

```go
base["Example 2"]["Phase 1"]        // 在 Example 2 那一章里找 Phase 1
base.frontmatter["description"]     // frontmatter 是语言的词，description 是数据
base["a"]["b"]                      // json / toml 的路径
```

### 4.2 按谓词：取一组

```go
base.sections[level == 2]
base.sections[name ~ "^Step "]
base.functions[calls "curl"]
base.keys[value == ""]
```

谓词里能用的东西是**固定的一小组**，不是通用表达式：

| 写法 | 含义 |
|---|---|
| `level == 2` | 标题层级 |
| `name == "X"` / `name ~ "正则"` | 名字 |
| `empty` | 内容为空 |
| `calls "X"` | shell 函数里调用了 X |
| `has["Y"]` | 含有名为 Y 的子部分 |

复合用 `&&` `||` `!`。**没有算术，没有函数调用**——谓词只能问文档结构，不能算东西。

### 4.3 部件类别

点号后面跟类别名，取该类别的全部：

| 种类 | 类别 |
|---|---|
| markdown | `.sections` `.lines` `.frontmatter` `.body` |
| shell | `.functions` `.markers` `.lines` |
| toml / json | `.keys` `.values` |
| text | `.lines` |

### 4.4 轴

在一个选择上继续走：

```go
base["Overview"].children      子部分
base["Overview"].next          下一个同级
base["Overview"].prev          上一个同级
base["Overview"].parent        所属的上级
base.sections.first            第一个
base.sections.last             最后一个
```

### 4.5 一组上的操作

对一个**组**调用操作 = 对组里每个元素各做一次：

```go
base.sections[name ~ "^Step "].demote()     每个 Step 节都降一级
```

---

## 5. 地址与位置

### 5.1 派生地址

部分的**边界**也是地址，它们的长度为零：

```go
base["Overview"].after      Overview 之后
base["Overview"].before     之前
base.start                  文档开头
base.end                    文档结尾
```

### 5.2 写就是在地址上加括号

```go
base["Overview"].after(self["Where this fits"])
base.start(self["Read this first"])
base.end(self["Appendix"])
```

`base.append(x)` 是 `base.end(x)` 的别名，因为读起来更顺。

### 5.3 打开一层

值里面是另一种文档时，用 `as` 打开，之后照常寻址：

```go
base["prompt"].as(markdown)["Steps"].after(self["我们的步骤"])
```

---

## 6. 内容从哪来

### 6.1 引用

```go
base["Overview"].after(self["Where this fits"])     我们文件的一个部分
base.append(self.body)                              我们文件的正文
base.append(self.frontmatter)                       整个 frontmatter
base["X"].after(a, b, c)                            多个，按序
```

### 6.2 块

冒号 + 换行 + 缩进。**缩进是定界符，所以内容里不需要任何转义**：

```go
base["Overview"].after:
    ## Where this fits

    跑 `lm build`，或者：

    ```sh
    lm build -o ../plugins/XSDD
    ```

    反引号、代码块、缩进，全都只是普通文本。
```

公共缩进由编译器剥掉。块在第一个缩进不足的行处结束。

---

## 7. 操作

### 7.1 放置（上游 100% 保留）

| 操作 | 写在哪 |
|---|---|
| `地址.after(内容...)` | 该部分之后 |
| `地址.before(内容...)` | 之前 |
| `base.start(内容...)` | 文档开头 |
| `base.end(内容...)` / `base.append(...)` | 文档结尾 |
| `地址.wrap(前, 后)` | 该部分两端各一次 |

### 7.2 结构变换（保证比放置还强）

| 操作 | 语义 | 保证 |
|---|---|---|
| `地址.move(目标地址)` | 挪到别处 | **字节多重集不变** |
| `地址.swap(另一地址)` | 互换 | **字节多重集不变** |
| `地址.promote()` / `.demote()` | 标题升 / 降一级 | 除层级标记外逐字节不变 |
| `地址.split(在哪)` / `.join()` | 拆 / 并 | 除边界标记外逐字节不变 |

```go
base["Troubleshooting"].move(base.sections.last.after)
base.sections[name ~ "^Step "].demote()
```

### 7.3 改写（需要理由）

会降低保证量的操作，**必须带 `reason:`**，理由会出现在构建报告里：

| 操作 | 语义 |
|---|---|
| `地址.replace(内容, reason: "...")` | 换掉该部分 |
| `地址.drop(reason: "...")` | 去掉该部分 |
| `地址.unwrap(reason: "...")` | 去掉包裹层 |

```go
base["How it compares"].drop(reason: "上游在和别的项目比，与我们无关")
```

### 7.4 值

```go
base.frontmatter["description"].set(self.frontmatter["description"])
base.frontmatter["description"].start(self.frontmatter["description"])   我们的 + 上游的
base.frontmatter["description"].end(self.frontmatter["description"])     上游的 + 我们的
```

### 7.5 投影（产出新文档，来源不动）

```go
base.start.project(base.sections[level == 2]):
    - [{name}](#{anchor})
```

块是模板，`{name}` `{anchor}` `{body}` `{level}` 取自被选中的每个部分。
跨种类就在这里发生：json 的结构能投影成 markdown 的表。

### 7.6 按身份合并（registry）

```go
base.merge(self)
```

json 这类**注册表**用它：两边的条目按**身份**合并——我们的条目顶替掉上游那条「调用同一个处理器」的，我们独有的加进去，上游独有的留着。

这和 `return self`（§8）是**两件不同的事**：`return self` 说「整份都是我们的」，`merge` 说「两份按条目拼起来，谁都不整份胜出」。一个 registry 用 `return self` 会把上游自己新增的钩子全丢掉。

保证：产物里我们每个条目都在，且**没有任何处理器被注册两次**——后者是这个操作存在的理由，重复注册会让钩子跑两遍，而文件仍是合法 json。

### 7.7 对齐

```go
base.sections.align(self.sections)
```

两个序列按顺序一一对应，各自插在对应部分之后。**对不齐就报错**——多一节、少一节、顺序变了，编译器指出是哪一节，不猜。

---

## 8. `return`：整份都是我们的

```go
// xsdd/AGENTS.lm
return self   // reason: 上游那份和我们的会跑两遍
```

`return` 在顶层，因为文件就是函数体。**返回 = 写了根跨度 = 保证量为零**，所以和 `drop` 一样必须给理由。

这取代了今天的 `base.merge(self)` 和 `base.replace(self, reason:)`——它们是同一件事，不需要两个词。

`return` 之后不能再有语句：整份都换掉了，写别的地方没有意义。

---

## 9. 类型

每个地址都有类型：**（种类，类别）**。

```
base["Overview"]          markdown / section
self.boot                 shell / function
base.frontmatter["x"]     markdown / fmkey
base["prompt"].as(markdown)   markdown / document
```

写操作要求**内容的类型与落点相符**，不符在解析期就报错：

```go
base["Overview"].after(self.boot)
                       ~~~~~~~~~ shell/function 写不进 markdown/section
```

`as(kind)` 是**唯一的显式转换**。刻意只有一个——转换多了，「这段到底是什么」就不再能静态回答。

---

## 10. 文法

```ebnf
file       = { comment | stmt | return } ;

stmt       = target "(" [ args ] ")"
           | target ":" block ;
return     = "return" target [ "//" reason ] ;

target     = root { selector | "." word } ;
root       = "base" | "self" ;
selector   = "[" ( string | predicate ) "]" ;
word       = class | axis | place | op | "as" "(" kind ")" ;

class      = "sections" | "lines" | "functions" | "markers"
           | "keys" | "values" | "frontmatter" | "body" ;
axis       = "children" | "next" | "prev" | "parent" | "first" | "last" ;
place      = "after" | "before" | "start" | "end" | "append" ;
op         = "wrap" | "replace" | "drop" | "unwrap" | "set"
           | "move" | "swap" | "promote" | "demote" | "split" | "join"
           | "project" | "align" ;

predicate  = pterm { ( "&&" | "||" ) pterm } ;
pterm      = [ "!" ] ( "level" cmp int | "name" ( "==" | "~" ) string
           | "empty" | "calls" string | "has" "[" string "]" | "(" predicate ")" ) ;
cmp        = "==" | "!=" | "<" | "<=" | ">" | ">=" ;

args       = arg { "," arg } ;
arg        = target | string | "reason" ":" string ;
block      = NEWLINE indented-lines ;

comment    = "//" ... NEWLINE ;
string     = '"' ... '"' ;
kind       = "markdown" | "shell" | "toml" | "json" | "text" ;
```

**没有 `func`、没有 `import`、没有变量、没有赋值、没有算术、没有控制流。** 整个文法只有两种语句：在一个地址上写，或者整份返回。

---

## 11. 编译器拒绝什么

| 情形 | 报什么 |
|---|---|
| 名字一个都没匹配到 | 指出名字和它查找的范围 |
| 名字匹配到两个 | 列出两处，要求加路径 |
| 类型不符 | 指出内容的类型和落点要求的类型 |
| 两个写跨度相交 | 指出双方出处 |
| `align` 对不齐 | 指出是哪一节开始错位 |
| 上游内容不见了且无人认领 | 指出丢了哪一节 |
| `drop` / `replace` / `unwrap` / `return` 没写理由 | 指出哪一句 |
| `return` 之后还有语句 | 指出多余的语句 |

一条都不猜。**织错位置的产物和正确的长得一模一样**，这是不猜的唯一理由，也是足够的理由。

---

## 12. 编译器替你做什么

| 你不写 | 它算 |
|---|---|
| 类型 | 由扩展名和部件类别推出 |
| 路径 | 名字唯一时不需要 |
| 公共缩进 | 剥掉 |
| 未被安放的内容去哪 | 按它在你文件里的邻居推断，**并把语句写回** |
| 重复的语句 | 结构对齐能看出来的，不用写（见 `align`） |

---

## 13. 和今天的对照

| 今天 | 全新 |
|---|---|
| `base.How_Skills_Work.after("X")` | `base["How Skills Work"].after(self["X"])` |
| `base."How it compares".drop(reason: "r")` | `base["How it compares"].drop(reason: "r")` |
| `base.merge(self)`（文本 / shell） | `return self  // reason: ...` |
| `base.merge(self)`（json registry） | `base.merge(self)` —— 不变，它是另一件事 |
| `base.replace(self, reason: "r")` | `return self  // reason: r` |
| `base.frontmatter.description.start(x)` | `base.frontmatter["description"].start(x)` |
| `base.prompt.as(markdown).Steps` | `base["prompt"].as(markdown)["Steps"]` |
| 标识符形式 + 字符串形式 + 撞名规则 | 只有方括号 |
| 16 行逐节翻译 | `base.sections.align(self.sections)` |

---

# 附录 · 一棵完整的树

四个产物，四种情况：编织、对齐、整份是我们的、按身份合并。

```
loom.om
agent-skills/                  上游（base）—— 一个字节都不碰
  AGENTS.md
  skills/testing/SKILL.md
  docs/getting-started.md
  hooks/hooks.json
xsdd/                          我们（self）
  AGENTS.md    AGENTS.lm
  skills/testing/SKILL.md    skills/testing/SKILL.lm
  docs/getting-started.md    docs/getting-started.lm
  hooks/hooks.json           hooks/hooks.lm
```

## loom.om

```
base      "agent-skills"
self      "xsdd"
output    "../plugins/XSDD"

mark      markdown "<!-- XSDD:BEGIN -->" "<!-- XSDD:END -->"
take      "references/**" "LICENSE"
```

---

## 例一 · 编织一份 skill

**`agent-skills/skills/testing/SKILL.md`**（上游）

```markdown
---
name: testing
description: Write tests that fail first.
---

## Overview

Tests come before the code they check.

## How it compares

Unlike other frameworks, this one …

## Verification

Run the suite twice.
```

**`xsdd/skills/testing/SKILL.md`**（我们）

```markdown
---
description: 先写会失败的测试。
---

## Where this fits

它在 XSDD 的 build 阶段之后、review 之前。
```

**`xsdd/skills/testing/SKILL.lm`**

```go
base.frontmatter["description"].start(self.frontmatter["description"])

base["Overview"].after(self["Where this fits"])

base["Verification"].before:
    ## 自检清单

    跑 `lm check` 确认产物是最新的。

base["How it compares"].drop(reason: "上游在和别的项目比，与我们无关")
```

**产物 `../plugins/XSDD/skills/testing/SKILL.md`**

```markdown
---
name: testing
description: 先写会失败的测试。 Write tests that fail first.
---

## Overview

Tests come before the code they check.

<!-- XSDD:BEGIN -->
## Where this fits

它在 XSDD 的 build 阶段之后、review 之前。
<!-- XSDD:END -->
<!-- XSDD:BEGIN -->
## 自检清单

跑 `lm check` 确认产物是最新的。
<!-- XSDD:END -->
## Verification

Run the suite twice.
```

构建报告：

```
skills/testing/SKILL.md   extended   +3   保证 100%（去掉 drop 的那节）
  dropped § How it compares   上游在和别的项目比，与我们无关
```

`## How it compares` 整节不在产物里——**因为写了理由**。没写理由的话构建会停。

---

## 例二 · 逐节对齐

上游 4 节，我们 4 节中文，一一对应。

**`xsdd/docs/getting-started.lm`**

```go
base.sections.align(self.sections)
```

一行。对不齐——上游多一节、顺序变了——编译器指出是哪一节开始错位，不猜。

今天这个文件是 16 行，每行一个 `base.X.after("译名")`。

---

## 例三 · 整份都是我们的

`AGENTS.md` 是上游那份加上我们的编辑，改动散在字里行间，没有可锚的名字。

**`xsdd/AGENTS.lm`**

```go
return self   // reason: 两份 AGENTS 会被 agent 同时读到，必须只有一份
```

产物就是 `xsdd/AGENTS.md` 原样。

保证量为零——**报告会明说这一份没人验过**：

```
AGENTS.md   returned   保证 0%   两份 AGENTS 会被 agent 同时读到，必须只有一份
```

上游发新版时，`lm sync` 把上游的改动三方合并进 `xsdd/AGENTS.md`，冲突处留标记。

---

## 例四 · 按身份合并

**`agent-skills/hooks/hooks.json`**（上游）

```json
{
  "SessionStart": [
    { "command": "sh hooks/session-start.sh || true" }
  ]
}
```

**`xsdd/hooks/hooks.json`**（我们）

```json
{
  "SessionStart": [
    { "command": "sh hooks/session-start.sh" }
  ],
  "PostToolUse": [
    { "command": "sh hooks/cache.sh" }
  ]
}
```

**`xsdd/hooks/hooks.lm`**

```go
base.merge(self)
```

**产物**

```json
{
  "SessionStart": [
    { "command": "sh hooks/session-start.sh" }
  ],
  "PostToolUse": [
    { "command": "sh hooks/cache.sh" }
  ]
}
```

两边都注册了 `hooks/session-start.sh`，**写法不同**（上游带 `|| true`）。按身份合并认的是**它调用哪个脚本**，所以我们那条顶替掉上游那条——而不是两条都留下。

这正是这个操作存在的理由：两条都留，文件仍是合法 json，钩子**跑两遍**，而没人会发现。

用 `return self` 会怎样：上游之后新增的钩子永远进不来。所以 §7.6 和 §8 是两件事。

---

## 四例对照

| 产物 | 写法 | 保证 | 上游新增的东西会自动进来吗 |
|---|---|---|---|
| `SKILL.md` | `after` / `before` / `drop` | 100%（除 drop 那节） | ✅ |
| `docs/getting-started.md` | `align` | 100% | ✅ 对不齐会报错 |
| `AGENTS.md` | `return self` | **0%** | ⚠️ 靠 `lm sync` 三方合并 |
| `hooks/hooks.json` | `merge` | 条目级 | ✅ |

**保证量从上到下递减，而每一行都在构建报告里写着。** 这就是 L4「保证是量，不是档」在一棵真实的树上长什么样。
