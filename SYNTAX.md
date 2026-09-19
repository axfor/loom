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

### 7.6 对齐

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
| `base.merge(self)` | `return self  // reason: ...` |
| `base.replace(self, reason: "r")` | `return self  // reason: r` |
| `base.frontmatter.description.start(x)` | `base.frontmatter["description"].start(x)` |
| `base.prompt.as(markdown).Steps` | `base["prompt"].as(markdown)["Steps"]` |
| 标识符形式 + 字符串形式 + 撞名规则 | 只有方括号 |
| 16 行逐节翻译 | `base.sections.align(self.sections)` |
