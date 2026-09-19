# Loom 语法

> **面向各种资源文件的结构化编程语言。一切资源可结构化、对象化引用。**

任何一种资源文件——markdown、shell、js、json、yaml、toml——编译器都把它解析成**一个对象**：有名字的部分构成的结构。语言就在这些对象上编程：引用、放置、变换。

完整规格。设计依据见 [DESIGN.md](DESIGN.md)，这里只讲怎么写。

---

## 0. 先看全貌

````go
// xsdd/skills/testing/SKILL.lm
// 产物是 skills/testing/SKILL.md

base.frontmatter.description.start(self.description)

base.Overview.after(self.job)

base.Verification.before:
    ## 自检清单

    跑 `lm check` 确认产物是最新的。

base."How it compares".drop(reason: "上游在和别的项目比，与我们无关")

---
Self:
    ```markdown
    # job

    它在 XSDD 的 build 阶段之后、review 之前。
    ```

    ```yaml
    description: 先写会失败的测试。
    ```
````

一个 `.lm` 产出一个产物，**内容和位置写在同一个文件里**。

- 顶层语句按顺序执行，**文件就是函数体**
- `---` 之后是**资源段**：`Self` 声明我们这一层由什么构成，`self` 是它的实例
- 没有 `package`；`fn`（§6.5）只用来在文件内分组，`import`（§6.6）只引内容

---

## 1. 一条总规则

> **点号取名字，方括号筛谓词。**

分隔符永远是点号。名字本身是标识符就直接写，拼不出来就加引号——但**始终是点号**。方括号只有一个用途：谓词。

```go
base.Overview                       标识符，直接写
base.frontmatter.description        键名也一样
base."How Skills Work"              有空格，加引号
base."1. Clone the repository"      数字开头带标点，加引号
base.sections[level == 2]           方括号 = 谓词，不是名字
```

点号后面还可以是**语言的词**——类别、位置、操作、轴，固定一张表。

### 点号是原样匹配，没有任何替换

这是和今天最大的区别。今天 `base.How_Skills_Work` 里的下划线**代表空格**，还配了一条「两个名字撞车时改用引号」的规则。

新规则里 **`.foo` 就是名字 `foo`，一个字符都不差**。没有下划线魔法，也就没有撞名规则。

### 和语言的词撞了怎么办

文档里真有一节叫 `sections`、或一个键叫 `after` 时：

```go
base.sections        语言的词：全部节
base."sections"      加引号 = 文档里那个叫 sections 的部分
```

**不加引号时先按语言的词查，查不到才当名字；加了引号就一定是名字。** 所以撞名永远有解，而且解法只有一个。

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
base = up."old/NAME.md"        // 上游改过路径
```

---

## 4. 选择

### 4.1 按名字：取一个

名字就是作者写的那串字——标题、函数名、键名。能当标识符写就用点号：

```go
base.Overview                       标题是 Overview
base.frontmatter.description        键是 description
base.boot                           shell 函数 boot
self.安装说明                        任何文字都行，标识符不限 ASCII
```

拼不出来的加引号——**仍然是点号**：

```go
base."Quick Start (Any Agent)"      有空格和括号
base."1. clone 仓库"                 数字开头
base."argument-hint"                有连字符
```

两种写法可以混着接：

```go
base."Example 2"."Phase 1"          两段都有空格
base."Example 2".Notes              章名要引号，节名恰好是标识符
base.servers.github.url             json / toml 的路径
base."mcp-servers".github.url       第一段有连字符
```

**点号是原样匹配**：`.Phase_1` 找的是名字 `Phase_1`，不是 `Phase 1`。名字里真有空格就只能加引号。

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
base.Overview.children      子部分
base.Overview.next          下一个同级
base.Overview.prev          上一个同级
base.Overview.parent        所属的上级
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
base.Overview.after      Overview 之后
base.Overview.before     之前
base.start                  文档开头
base.end                    文档结尾
```

### 5.2 写就是在地址上加括号

```go
base.Overview.after(self."Where this fits")
base.start(self."Read this first")
base.end(self.Appendix)
```

`base.append(x)` 是 `base.end(x)` 的别名，因为读起来更顺。

### 5.3 打开一层

值里面是另一种文档时，用 `as` 打开，之后照常寻址：

```go
base.prompt.as(markdown).Steps.after(self.我们的步骤)
```

---

## 6. 内容从哪来

### 6.1 引用

```go
base.Overview.after(self."Where this fits")     我们文件的一个部分
base.append(self.body)                              我们文件的正文
base.append(self.frontmatter)                       整个 frontmatter
base."X".after(a, b, c)                            多个，按序
```

### 6.2 资源从哪来：三条路，同一种东西

我们这一层的内容，有三种给法。**它们产出的是同一种东西**——一个可寻址的文档（或一组），寻址方式完全一样。

| 给法 | 写成 | 种类怎么定 |
|---|---|---|
| 隐式 | `self` | 我们层里**同路径**那份文件，按扩展名 |
| 外置 | `import "/skills/testing/notes"` | 按扩展名 |
| 内联 | `Self:` 资源段 | 按围栏的语言标签 |

**编译器自动把资源编译成对象**，任何一条路都不用写类型。

### 6.3 `import`：任意资源文件

```go
import "/commands/ship"           名字取自文件名：ship
import b "/data/registry"         改个名

base.append(ship.body)
base.Overview.after(b.servers.github)
```

引什么都行——`.md` `.sh` `.js` `.json` `.yaml` `.toml`⋯⋯**扩展名就是种类**，编译器按种类解析成有名字的部分，之后和 `self` 一样寻址。

扩展名可以省，只要那个名字下恰好有一个文件；有多个就报错，要求写全。

`import` 只引**内容**，不引函数——函数是文件内的组织手段（§6.5）。

### 6.4 `Self:`：内联资源段

内容短、和位置语句一起读才说得清时，写在文件里：

```go
base.Overview.after(self.job)

---
Self:
    ```markdown
    # job

    它在 XSDD 的 build 阶段之后、review 之前。
    ```

    ```json
    { "ddd": 11 }
    ```
```

**`Self` 是类，`self` 是实例。** `Self:` 声明我们这一层由什么构成，语句里的 `self` 是它的实例。

**`Self:` 之后是缩进块**——和写内容的 `after:` 同一条规则：冒号 + 换行 + 缩进。语言里只有这一条块规则。缩进之内是若干带语言标签的围栏，标签就是种类。

#### `self` 是命名空间

一个资源段里可以放**多份不同种类**的文档。寻址时种类是一层：

```go
self.markdown.job        markdown 那份里的 job
self.json.ddd            json 那份里的 ddd
self.job                 种类省掉 —— 只有一份有 job 时，编译器自己找
```

**省掉种类时编译器在所有份里找**：找到一个就用，找到两个就报错。不猜。

#### 具名资源

```go
base.Overview.after(self.n1.job)

---
Self as n1:
    ```markdown
    # job
    ```
---
Self as n2:
    ```json
    { "ddd": 11 }
    ```
```

地址的完整形状：

```
self [.段名] [.种类] .部分
      ^^^^^^ ^^^^^^
      都可省，省了就推断；推断不出唯一解就报错
```

#### 围栏里再有围栏

内容里本来就有代码块时，外层围栏写更多反引号——和 markdown 自己的规则一样：

`````go
---
Self:
    ````markdown
    # 用法

    ```sh
    lm build
    ```
    ````
`````

### 6.5 一个文件里能不能三种都用

能，但 `self` 只能有一个来源：

| 组合 | 行不行 |
|---|---|
| `Self:` + `import` | ✅ `self` 来自资源段，`import` 的另有其名 |
| 只 `import` | ✅ `self` 仍指同路径文件（没有就是没有） |
| `Self:` + 同路径还有个 `SKILL.md` | ❌ `self` 两个来源，报错 |

**所以「一个产物一个文件」和「资源分散在多个文件」都成立**，取决于内容多长、要不要 markdown 编辑器和预览、要不要被 `lm sync` 三方合并。

### 6.6 函数：在文件内组织

一个 `.lm` 只产出一个产物，所以不需要跨文件复用；但一个文件里语句多起来时，要能分组：

```go
fn frontmatter() {
    base.frontmatter.description.start(self.frontmatter.description)
    base.append(self.body)
}

fn sections() {
    base.Overview.after(self.job)
    base.Verification.before(self.checklist)
}

frontmatter()
sections()
```

**顶层语句按出现顺序执行**（文件就是函数体，§0）。`fn` 定义的要被调用才执行，没有隐含的入口。

函数**没有返回值**。写操作不返回成功与否——这是这门语言唯一一条硬禁令，理由见 §11.1。

参数可以传地址：

```go
fn bilingual(up, ours) {
    up.after(ours)
}

bilingual(base.Overview, self.job)
```

### 6.7 块：写内容

冒号 + 换行 + 缩进。**缩进是定界符，所以内容里不需要任何转义**：

```go
base.Overview.after:
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
base.Troubleshooting.move(base.sections.last.after)
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
base."How it compares".drop(reason: "上游在和别的项目比，与我们无关")
```

### 7.4 值

```go
base.frontmatter.description.set(self.frontmatter.description)
base.frontmatter.description.start(self.frontmatter.description)   我们的 + 上游的
base.frontmatter.description.end(self.frontmatter.description)     上游的 + 我们的
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

## 8. 结果与判断

### 8.1 默认硬停，接住才归你

写操作**可以**返回结果，但只有你显式接住时才返回：

```go
base.Overview.after(self.job)          没接 → 失败即停止构建，报锚点找不到
ok = base.Overview.after(self.job)     接住 → 构建不停，你负责
if !ok {
    return err.format("上游没有 Overview: %s", ok)
}
```

**这条默认值是整个设计的关键。** 失败要被吞掉，必须有人**显式写下接住它的那个变量**——不可能手滑吞掉。

接住了不用也是错误（和 Go 的未使用变量一样）：

```go
ok = base.Overview.after(self.job)     ⛔ ok 声明了没用到
```

否则「接住」就成了吞掉的同义词。

### 8.2 结果里有什么

`ok` 不是裸 bool，它带着为什么：

```go
if !ok {
    return err.format("%s", ok)        → "锚点 Overview 在 upstream/SKILL.md 里找不到"
}
```

| 问 | 得到 |
|---|---|
| `!ok` | 失败了吗 |
| `%s` 格式化 | 失败原因，带 `文件:行:列` |

### 8.3 结构判断：只读，随时可用

问文档**长什么样**不需要先写再看结果：

```go
if base.has.Overview {
    base.Overview.after(self.job)
}

if base.sections[level == 2].any {
    base.start.project(base.sections[level == 2]):
        - [{name}](#{anchor})
}
```

能问的就是 §4.2 那组谓词，加上 `.any` / `.count`。**只读，不产生写，永远安全。**

两种写法都行，但含义不同：

| 写法 | 说的是 |
|---|---|
| `if base.has.Overview { … }` | **我知道它可能没有**，没有就跳过 |
| `ok = …; if !ok { … }` | **我要自己处理失败**，包括报我自己的错 |

前者更清楚，优先用。

### 8.4 `return`：三种

```go
return self                        产物就是我们的文件（§9）
return err.format("...")           构建失败，输出这条消息
return                             提前结束，已经写下的算数
```

`err.format` 里的格式化**只进消息，不进产物**——所以它不影响产物的可溯源性（DESIGN L0）。

### 8.5 允许判断之后，保证还在吗

在。验证是「把写过的跨度抠掉，剩下的等于来源」，而**写集合是构建时记录的**，不是事先算出来的。所以条件分支不影响上游完整性。

真正被条件影响的是**可预测性**——同一份模板对不同版本的上游可能写不同的东西。这不是错误，但报告里要说清楚：

```
SKILL.md   extended  +2   跳过 1 处（base.has.Overview 为假）
```

## 9. `return self`：整份都是我们的

```go
// xsdd/AGENTS.lm
return self   // reason: 上游那份和我们的会跑两遍
```

`return` 在顶层，因为文件就是函数体。**返回 = 写了根跨度 = 保证量为零**，所以和 `drop` 一样必须给理由。

这取代了今天的 `base.merge(self)` 和 `base.replace(self, reason:)`——它们是同一件事，不需要两个词。

`return` 之后不能再有语句：整份都换掉了，写别的地方没有意义。

---

## 10. 类型

每个地址都有类型：**（种类，类别）**。

```
base.Overview          markdown / section
self.boot                 shell / function
base.frontmatter."x"     markdown / fmkey
base.prompt.as(markdown)   markdown / document
```

写操作要求**内容的类型与落点相符**，不符在解析期就报错：

```go
base.Overview.after(self.boot)
                       ~~~~~~~~~ shell/function 写不进 markdown/section
```

`as(kind)` 是**唯一的显式转换**。刻意只有一个——转换多了，「这段到底是什么」就不再能静态回答。

---

## 11. 文法

```ebnf
file       = { comment | import | fn | stmt | call | return } [ resources ] ;

import     = "import" [ ident ] string ;
fn         = "fn" ident "(" [ ident { "," ident } ] ")" "{" { stmt | call } "}" ;
call       = ident "(" [ args ] ")" ;
resources  = "---" NEWLINE { resource } ;
resource   = "Self" [ "as" ident ] ":" NEWLINE indented-fences ;

stmt       = [ ident "=" ] target "(" [ args ] ")"   (* 接住结果才不硬停 *)
           | target ":" block
           | if
           | call ;
if         = "if" cond "{" { stmt } "}" [ "else" "{" { stmt } "}" ] ;
cond       = [ "!" ] ( ident | target ) ;            (* 结果，或只读的结构查询 *)
return     = "return" [ target | err ] [ "//" reason ] ;
err        = "err" "." "format" "(" string { "," arg } ")" ;

target     = root { selector | "." word } ;
root       = "base" | "self" | ident ;   (* import 进来的名字、函数参数 *)
selector   = "[" predicate "]" ;                (* 方括号只用于谓词 *)
word       = class | axis | place | op | "as" "(" kind ")" | name ;
name       = ident | string ;                   (* 名字，原样匹配；不加引号时语言的词优先 *)

class      = "sections" | "lines" | "functions" | "markers"
           | "keys" | "values" | "frontmatter" | "body" ;
axis       = "children" | "next" | "prev" | "parent" | "first" | "last" ;
place      = "after" | "before" | "start" | "end" | "append" ;
op         = "wrap" | "replace" | "drop" | "unwrap" | "set"
           | "move" | "swap" | "promote" | "demote" | "split" | "join"
           | "project" | "align" ;

predicate  = pterm { ( "&&" | "||" ) pterm } ;
pterm      = [ "!" ] ( "level" cmp int | "name" ( "==" | "~" ) string
           | "empty" | "calls" string | "has" "." string "" | "(" predicate ")" ) ;
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

## 12. 编译器拒绝什么

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

## 13. 编译器替你做什么

| 你不写 | 它算 |
|---|---|
| 类型 | 由扩展名和部件类别推出 |
| 路径 | 名字唯一时不需要 |
| 公共缩进 | 剥掉 |
| 未被安放的内容去哪 | 按它在你文件里的邻居推断，**并把语句写回** |
| 重复的语句 | 结构对齐能看出来的，不用写（见 `align`） |

---

## 14. 和今天的对照

| 今天 | 全新 |
|---|---|
| `base.How_Skills_Work.after("X")` | `base."How Skills Work".after(self."X")`（下划线不再代表空格） |
| `base."How it compares".drop(reason: "r")` | `base."How it compares".drop(reason: "r")` |
| `base.merge(self)`（文本 / shell） | `return self  // reason: ...` |
| `base.merge(self)`（json registry） | `base.merge(self)` —— 不变，它是另一件事 |
| `base.replace(self, reason: "r")` | `return self  // reason: r` |
| `base.frontmatter.description.start(x)` | 不变 |
| `base.prompt.as(markdown).Steps` | `base.prompt.as(markdown).Steps` |
| 标识符形式（下划线代空格）+ 撞名规则 | 点号原样匹配，拼不出时加引号 |
| 16 行逐节翻译 | `base.sections.align(self.sections)` |

---

# 附录 A · 语法全构件

一个文件，把文法（§10）里每一个构件都用一遍。**不是真实用例**，是查阅用的对照表。

```go
// ───────────────────────────────────────────────────────────
// 注释：// 到行尾。下面这行是文档注释的位置，目前没有单独形式。
// ───────────────────────────────────────────────────────────


// ── 选择：按名字 ─────────────────────────────────────────────

base.Overview                        一个节
base."Quick Start (Any Agent)"         名字里有标点，照写
base."Example 2"."Phase 1"            嵌套：在 Example 2 那一章里找
base.frontmatter.description         frontmatter 是语言的词，键名是数据
base.servers.github.url        json / toml 的路径，逐层点号


// ── 选择：按类别（取全部） ───────────────────────────────────

base.sections                           markdown：全部节
base.lines                              全部行
base.frontmatter                        整个 frontmatter
base.body                               正文（frontmatter 之后的全部）
base.functions                          shell：全部函数
base.markers                            shell：全部横幅注释
base.keys                               toml / json：全部键
base.values                             全部值


// ── 选择：按谓词 ─────────────────────────────────────────────

base.sections[level == 2]               层级
base.sections[level >= 3]               比较：== != < <= > >=
base.sections[name == "Setup"]          名字相等
base.sections[name ~ "^Step [0-9]+"]    名字匹配正则
base.sections[empty]                    内容为空
base.functions[calls "curl"]            shell 函数里调用了 curl
base.sections[has["Verification"]]      含有名为 Verification 的子部分

base.sections[level == 2 && !empty]                     与、非
base.sections[name ~ "^Step " || name == "Setup"]       或
base.sections[!(level == 1 || empty)]                   括号


// ── 选择：轴 ─────────────────────────────────────────────────

base.Overview.children               子部分
base.Overview.next                   下一个同级
base.Overview.prev                   上一个同级
base.Overview.parent                 所属的上级
base.sections.first                     第一个
base.sections.last                      最后一个
base.sections[level == 2].first.children   轴可以接着走


// ── 打开一层 ─────────────────────────────────────────────────

base.prompt.as(markdown)                    值里是 markdown
base.prompt.as(markdown).Steps           打开后照常寻址
base.script.as(shell).functions[calls "rm"] 打开后照常选择


// ── 派生地址（长度为零的跨度） ───────────────────────────────

base.Overview.after                  该节之后
base.Overview.before                 之前
base.start                              文档开头
base.end                                文档结尾


// ═══ 以上全是「指向哪」。以下是「做什么」。═══════════════════


// ── 放置：上游 100% 保留 ─────────────────────────────────────

base.Overview.after(self."Where this fits")         引用我们的一个节
base.Overview.before(self.前言)
base.start(self."Read this first")
base.end(self.Appendix)
base.append(self.Appendix)                           end 的别名
base.Overview.after(self.A, self.B, self.C) 多个，按序
base.Overview.wrap(self.开头, self.结尾)        两端各一次

base.append(self.body)                                  我们的正文
base.append(self.frontmatter)                           整个 frontmatter

base.Overview.after:                                 块：缩进定界
    ## 直接写在这里

    反引号 `lm build`、代码块都只是普通文本：

    ```sh
    lm build -o ../plugins/XSDD
    ```

    仓库是 {{@url}}。占位符照常展开，{{@@url}} 是字面的 {{@url}}。


// ── 结构变换：保证比放置还强 ─────────────────────────────────

base.Troubleshooting.move(base.sections.last.after)   移动：字节多重集不变
base.A.swap(base.B)                                互换
base.sections[name ~ "^Step "].demote()                  一组：每个都降一级
base.Overview.promote()                               升一级
base.Setup.split(base.Setup.children.first)        在某处拆开
base.Setup.join()                                     与下一个并起来


// ── 改写：降低保证量，必须给理由 ─────────────────────────────

base."How it compares".drop(reason: "上游在和别的项目比，与我们无关")
base.Install.replace(self.安装, reason: "上游的装法在内网不通")
base.Notes.unwrap(reason: "这层包裹在产物里没有意义")


// ── 值 ───────────────────────────────────────────────────────

base.frontmatter.description.set(self.frontmatter.description)    换成我们的
base.frontmatter.description.start(self.frontmatter.description)  我们的 + 上游的
base.frontmatter.description.end(self.frontmatter.description)    上游的 + 我们的


// ── 投影：产出新文档，来源不动 ───────────────────────────────

base.start.project(base.sections[level == 2]):
    - [{name}](#{anchor})

base.append.project(base.functions):
    ### {name}

    {body}


// ── 对齐：两个序列一一对应 ───────────────────────────────────

base.sections.align(self.sections)


// ── 按身份合并：json registry ────────────────────────────────

base.merge(self)


// ── 整份都是我们的：顶层 return，之后不能再有语句 ─────────────

return self   // reason: 两份 AGENTS 会被 agent 同时读到，必须只有一份
```

## 构件清单核对

| 文法条目 | 上面出现在 |
|---|---|
| `comment` | 全文 |
| `selector` 字符串 | 「按名字」 |
| `selector` 谓词 | 「按谓词」全部七种 + `&&` `\|\|` `!` 括号 |
| `class` | 「按类别」八个 |
| `axis` | 「轴」六个 |
| `as(kind)` | 「打开一层」 |
| `place` | 「派生地址」四个 + `append` |
| `op` 放置类 | `after` `before` `start` `end` `append` `wrap` |
| `op` 变换类 | `move` `swap` `promote` `demote` `split` `join` |
| `op` 改写类 | `replace` `drop` `unwrap` |
| `op` 值 | `set` `start` `end` |
| `op` 其他 | `project` `align` `merge` |
| `arg` 引用 | `self."X"` `self.body` `self.frontmatter` |
| `arg` 多个 | `after(a, b, c)` |
| `arg` `reason:` | 「改写」三条 |
| `block` | 「块」与两处 `project` |
| `{{@name}}` | 块里 |
| `return` | 末行 |

**文法里没有出现在这里的构件：零。**

# 附录 B · 一棵完整的树

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
base.frontmatter.description.start(self.frontmatter.description)

base.Overview.after(self."Where this fits")

base.Verification.before:
    ## 自检清单

    跑 `lm check` 确认产物是最新的。

base."How it compares".drop(reason: "上游在和别的项目比，与我们无关")
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

---
