# Loom 设计

> 把散落在各种类型和位置的**文档与数据**，组织成一张结构化、**可引用**的布。

分九层。每一层只用它下面的层：数据 → 命名 → 变换 → 保证 → 组织 → 语法 → 编译器 → 演进 → 现状。
新东西要加进来，先回答它属于哪一层；答不上来，说明它还没想清楚。

## 唯一判据

任何要加进这门语言的东西，只问一句：

> **它的写集合是什么？**

算得出 → 安全，可验证。
算不出 → 它写的是根跨度，保证量为零，必须写明理由并在构建报告里单列。

这一句贯穿全部九层。下面每一处「要 / 不要」的决定，都只是它的实例。

---

# L1 · 数据模型

## 1.1 两个概念

**文档** = （种类，文本）
**部分** = （名字，跨度）

名字**由作者写**——标题、函数名、键名。我们不发明名字，只捡起来。

## 1.2 递归

> **一个部分的内容，本身可以作为文档打开。**

json 的值里是 markdown，那段 markdown 有节；markdown 的代码块里是 shell，那段 shell 有函数。

所以「散落」不是「很多种文件堆在一起」，是**种类套着种类**。这张布有深度。数据和文档在这里没有区别。

## 1.3 一个种类要回答三个问题

1. 它把文本切成哪些**有名字的部分**？
2. 它的部分能被当作文档**打开**吗？（往下嵌）
3. 它能作为别人的部分**被打开**吗？（被嵌）

只答第 1 问的种类只做了三分之一——它只能当顶层文件，进不了这张有深度的布。

---

# L2 · 命名

## 2.1 地址：指一个部分

```
base                            一份文档
base.Overview                   它的一个部分
base."How it compares"          名字带标点时的字符串形式
base."A"."B"                    路径：在 A 的整章内定位 B
base.frontmatter.description    一个值
base.prompt.as(markdown).Steps  打开一层（L1.2 的递归在语法上的样子）
```

## 2.2 派生地址：部分的边界

部分的**边界**也是地址——它们是长度为零的跨度：

```
base.Overview 之前     base.Overview 之后     base 的开头     base 的结尾
```

这一步省掉了四个操作：**「在 X 之后插入」不是操作，是地址。**

## 2.3 选择器：指一组部分

```
base.sections                   所有节
base.sections[level == 2]       谓词
base.sections[name ~ "^Step "]  正则
base.Overview.children          轴：子节
base.Overview.next              轴：下一个同级
base.functions[calls "curl"]    内容谓词
```

选择是**只读**的：它不产生写，永远不会破坏任何保证。它只决定「对谁动手」。

## 2.4 名字是会变的（身份问题）

名字来自作者写的字。字改了，名字就变了。

所以**身份不是一份文档的属性，是两个版本之间的关系**。这条推论决定了它只能住在哪里：

| 方案 | 为什么不行 |
|---|---|
| 给上游加 id | 上游不可碰是前提 |
| 内容哈希 | 把「改标题就断」换成「改内容就断」 |
| 模糊匹配 | 猜错的产物和正确的长得一模一样 |
| **取上游新版的那一刻算** | ✅ 唯一同时握有新旧两版的时刻 |

那一刻能判断「内容没变而标题变了 = 重命名」，把它算出来、记下来、**报告出来**——引用跟随是被声明的，不是被猜的。今天 `lm sync` 合并的是文件内容；它同样可以合并**地址空间**。

---

# L3 · 变换

## 3.1 原语

```
write(跨度, 文本)
```

把跨度换成给定文本。**只有这一个。** 下面全部操作都定义成它的组合。

## 3.2 词汇表

| 操作 | 定义 |
|---|---|
| `after` `before` `start` `append` | 往边界空跨度写 |
| `wrap` | 往部分两端各写一次 |
| `replace` | 覆盖该部分的跨度 |
| `drop` | 往该部分的跨度写空 |
| `move` | drop 一次 + 在别处写**同样的字节** |
| `reorder` `swap` | 一组 move |
| `promote` `demote` | 只改标题的层级标记 |
| `split` `join` | 在部分内部增删一个边界标记 |
| `unwrap` | 去掉包裹层 |
| `merge` | 覆盖根跨度 |
| `project` | 由结构派生**一份新文档** |

**这张表才是语言的形状**，`write` 只是它们共同的底。

## 3.3 投影

`project` 是唯一产出新文档、而不修改既有文档的操作：

```
project base.sections[level <= 2] -> markdown:
    - [{name}](#{anchor})
```

从上游结构派生一份目录。来源一个字节没动。

**「一切文档」在这里才真正成立**：json 的结构能投影成 markdown 的表，markdown 的标题树能投影成 yaml 的导航。跨种类不靠转换文本，靠**读一边的结构、写另一边的结构**。

---

# L4 · 保证

## 4.1 不变式

既然唯一的改动方式是换跨度：

> **写集合之外的每一个字节，与来源逐字节相同。**

这不需要检查，是构造的结果。验证只需一步：**把写过的跨度抠掉，剩下的必须等于来源。**

标记（`<!-- MINE:BEGIN -->`）不是注释风格问题——**它就是写集合在产物里的记录**，让「抠掉」事后仍然可做。

## 4.2 保证是量，而且各操作不同

可信度 = **产物中不在写集合内的比例**。而 L3 的词汇表里，有些操作的保证**比插入还强**：

| 操作 | 保证 | 怎么验 |
|---|---|---|
| `after` `before` `start` `append` `wrap` | 上游 **100%** | 剥掉标记，逐字节比 |
| `move` `reorder` `swap` | **字节多重集不变** | 两边字节多重集相等 |
| `promote` `demote` `split` `join` | **除结构标记外逐字节不变** | 剥掉标记后比 |
| `project` | 来源**未被修改** | 平凡 |
| `replace` `drop` `unwrap` | 除该部分外全部 | 逐部分核对 |
| `merge` | **零** | 无法验证 → 必须写理由 |

第二行是这套设计里最值钱的一条。只有 `write` 的语言说不出「把这节挪到那节之后」——只能 drop 一次、再写一次，两次之间的联系丢了，报告里看到的是「删了一节、加了一节」。有了 `move`，报告能说「移动了一节，字节未变」。**同一件事，可验证性完全不同。**

## 4.3 报告

每个产物在报告里带上它的保证量和依据的操作。**不可验证的东西不被禁止，它被标价。**

---

# L5 · 组织

## 5.1 函数

一个产物由一个函数织成。参数名在前、类型在后；**类型就是文档种类**。

```go
func weave(base, self markdown) {
    base.Overview.after(self["Where this fits"])
    base.append(self["Appendix"])
}
```

## 5.2 返回类型就是保证量

L4 和 L5 在这里接上，这是整套设计里最紧的一处：

```go
// 不返回 → 只有写 → 写集合可界定 → 逐字节可验证
func weave(base, self markdown) { ... }

// 返回一份文档 → 写了根跨度 → 保证量为零 → 必须给理由
func weave(base, self markdown) markdown {
    return self
} // reason: "上游那份和我们的会跑两遍"
```

返回算出来的东西（`return a.call(...)`）落在同一条规则下。**不是禁止计算，是计算的代价写在签名上**——编译器和人都看得见。L0 那条唯一判据，在这里变成了一个类型规则。

## 5.3 复用

复用就是**调用一个函数**。不发明 `rule` / `apply` / `use`。

**未决**：怎么把一个函数套到一批产物上，三个方案见 L5.4。

## 5.4 套用（未决）

| 方案 | 做法 | 真实数据上的效果 |
|---|---|---|
| **包** | import + 显式调用 | 37 处重复变成 37 import + 37 调用，**比现在长** |
| **指令** | `//loom:apply "skills/**"`，仿 Go build tag | 24 个只有样板的模板**整个消失**；代价是作用在远处 |
| **推断** | 编译器按结构对齐自己算 | `getting-started` 从 16 行到 1 行；代价是假设不成立时必须响亮失败 |

三者不互斥：包是载体，指令省「重复的话」，推断省「本来就不该写的话」。

## 5.5 融合是并集

因为所有地址对着**来源**解析、编辑收齐后统一应用，**不相交的写可交换**。所以套用的写和本文件的写，融合方式就是集合并：

| 情形 | 语义 |
|---|---|
| 两个写落在同一个空跨度 | 都生效，按序 |
| 两个写覆盖相交的非空跨度 | **冲突，报错**，指出双方出处 |

不需要优先级，只需要**重叠检测**——用的正是 L4 维护不变式的同一套机器。

拿 81 个真实模板验过：**重叠冲突 0 个**。所以冲突是罕见例外，值得报错，不值得为它设计优先级。

---

# L6 · 语法

## 6.1 参考 Go

| Go 的什么 | 这里对应什么 |
|---|---|
| `func` + 大括号 | 一个产物由一个函数织成 |
| `base, self markdown` | 类型就是文档种类 |
| 首字母大小写区分导出 | **大写来自文档**（作者写的标题），**小写属于语言**（`after` / `append`） |
| map 取值 `m["k"]` | 名字带空格标点时用下标 |
| 没有异常 | 不猜：找不到、匹配到两个，都是编译错误 |
| gofmt | `lm fmt`，唯一的规范形式 |

## 6.2 块：缩进定界

真实文档里全是 `` `code` `` 和代码块，任何"选个终止符"的字面量都会被它们终止。缩进是**唯一永远不需要转义**的定界符：

```go
base.Overview.after:
    ## Where this fits

    Run `lm build` to weave it.

    ```sh
    echo hello
    ```
```

公共缩进由编译器剥掉。作者只做了一件他本来就会做的事。

## 6.3 实际长什么样

全部取自 `agent-skills` 的真实文件。左边是今天，右边是这套设计。

### 一份普通的 skill

今天：

```
base.frontmatter.description.start(self.frontmatter.description)
base.Overview.after("Position in SDLC（XSDD 调度层关系）")
base.append(self.body)
```

设计：

```go
func weave(base, self markdown) {
    base.frontmatter.description.start(self.frontmatter.description)
    base.Overview.after(self["Position in SDLC（XSDD 调度层关系）"])
    base.append(self.body)
}
```

差别不大——**这是对的**。语言的下限不该因为加了上限而变难写。

### 内容直接写在里面（今天做不到）

反引号字面量会被行内 `` ` `` 终止，所以今天这段内容只能放在另一个 `.md` 文件里：

```go
func weave(base, self markdown) {
    base.Overview.after:
        ## Where this fits

        跑 `lm build` 就能织进去：

        ```sh
        lm build -o ../plugins/XSDD
        ```
}
```

缩进定界，反引号和代码块都只是普通文本。

### `xsdd/AGENTS.lm`：合并

今天是一整个文件，正文只有一句 `base.merge(self)`。设计里它是一个签名：

```go
func weave(base, self markdown) markdown {
    return self
} // reason: 上游那份和我们的会跑两遍
```

**返回类型就说明了这个文件的保证量为零**（L5.2），不需要额外的关键字来标记它特殊。

### 词汇表变富之后

这一段今天**完全写不出来**——没有选择器，也没有 `move` / `demote` / `project`：

```go
func weave(base, self markdown) {
    // 上游没有目录，我们从它自己的二级标题投影一份，放在开头
    base.start.project(base.sections[level == 2]):
        - [{name}](#{anchor})

    // 上游把 Troubleshooting 放在中间，产物里要它在最后
    base.Troubleshooting.move(base.sections.last.after)

    // 因为上面多加了一层，所有 Step 节降一级
    base.sections[name ~ "^Step "].demote()
}
```

三条语句，三种保证强度，报告里各自列出来：

| 语句 | 保证 |
|---|---|
| `project` | 来源未被修改 |
| `move` | 字节多重集不变 |
| `demote` | 除层级标记外逐字节不变 |

**没有一条是「替换了一段文本」。** 这就是「结构化」和「文本处理」的区别。

### 包与套用

```go
//loom:apply "skills/**"
package skills

/// 每个 skill：描述双语拼接，正文接在上游之后。
func Frontmatter(base, self markdown) {
    base.frontmatter.description.start(self.frontmatter.description)
    base.append(self.body)
}
```

命中 `skills/**` 的产物自动套用。那 24 个「除了样板没有一句自己的话」的模板**整个文件消失**。

### 还写不出来的：`xsdd/docs/getting-started.lm`

它今天是 16 条同形状的语句——逐节翻译，上游每节后面跟一节中文：

```
base.How_Skills_Work.after("Skill 如何工作")
base."Quick Start (Any Agent)".after("Quick Start（任何 agent）")
... 还有 14 行
```

它说的是**「两个序列对齐」**，而 L3 的词汇表里没有这个词。两种候选写法：

```go
// 候选一：内建算子
base.sections.align(self.sections)

// 候选二：受限遍历（只能走部分、只能产生写）
for sec := range base.sections {
    sec.after(self[sec.name])
}
```

候选一不引入遍历，但每多一种模式就多一个词。候选二更诚实——写集合仍是每次迭代的并集，静态可知，不变式不破（L6.5）。

**这是这份设计目前唯一确知的缺口。**

## 6.6 全新语法：三个方案

不是微调今天的写法，是重画。共同前提：**名字只有一种写法**。

今天有两种——标识符形式 `base.How_Skills_Work`（下划线代空格）和字符串形式 `base."How it compares"`，外加一条「标识符撞名时报错、改用字符串」的规则。Go 里没有这种东西：动态名字用索引取。**全新设计应该整条删掉它**，只留索引。

三个方案的差别在**「部分」怎么被取到**。同一个真实文件（`xsdd/docs/getting-started.lm`，逐节翻译）。

### 甲 · 索引取部分，位置是方法

```go
package docs

func GettingStarted(up, mine markdown) {
    up["How Skills Work"].after(mine["Skill 如何工作"])
    up["Quick Start (Any Agent)"].after(mine["Quick Start（任何 agent）"])
    up["1. Clone the repository"].after(mine["1. clone 仓库"])
    // …还有 13 行
}
```

索引结果是**一个部分**，部分上挂位置方法。最贴近 Go 的 map + 方法。
16 行还是 16 行。

### 乙 · 文档是接收者，锚点是参数

```go
func GettingStarted(up, mine markdown) {
    up.after("How Skills Work", mine["Skill 如何工作"])
    up.after("Quick Start (Any Agent)", mine["Quick Start（任何 agent）"])
    // …
}
```

所有操作都是**文档的方法**，锚点退化成普通参数。好处：`up` 的方法集是固定的、可补全、可类型检查；不需要「索引出来的东西是什么类型」这一层。
坏处：读起来「在谁之后」被埋进参数里，没有甲直观。

### 丙 · 选择器是唯一入口，单个是特例

```go
func GettingStarted(up, mine markdown) {
    up.sections().align(mine.sections())
}
```

取一个部分是「取一组」的退化情况：

```go
up.sections("Overview").after(mine.sections("Where this fits"))
up.sections(level(2)).demote()
```

**16 行变 1 行**——因为它承认了这个文件真正在说的是「两个序列对齐」（L3 的缺口）。
坏处：最简单的事也要经过选择器，下限被抬高了。

---

### 三者的取舍

| | 名字怎么取 | 最简单的一句 | 16 行的那个文件 | 最像 |
|---|---|---|---|---|
| 甲 | 索引 → 部分 | `up["X"].after(y)` | 16 行 | Go 的 map |
| 乙 | 参数 | `up.after("X", y)` | 16 行 | Go 的方法集 |
| 丙 | 选择器 | `up.sections("X").after(y)` | **1 行** | jQuery / XPath |

**甲和乙是同一个语言的两种写法**，差别只在锚点放哪。**丙是另一个语言**——它把「一组」当成基本情况，于是词汇表（L3）和它天然贴合，但简单的事变啰嗦。

一个折中是**甲 + 丙**：索引取一个，`.sections(...)` 取一组，两者都在。代价是有两条取部分的路。

## 6.7 未决：五个语法决定

其余语法都是这五条的后果。决定一条就在「状态」里记一笔，不要只留在对话里。

| # | 决定 | 推荐 | 状态 |
|---|---|---|---|
| 1 | 要不要 `func` 包一层 | 要 | ⬜ 未定 |
| 2 | 位置词是方法还是动词前置 | 方法 | ⬜ 未定 |
| 3 | 引用自己的内容用裸字符串还是显式 | 显式为准，裸串为糖 | ⬜ 未定 |
| 4 | merge 用返回类型还是关键字 | 返回类型 | ⬜ 未定 |
| 5 | 套用用指令注释还是编译器推断 | 不确定 | ⬜ 未定 |

---

### 1. 要不要 `func` 包一层

视觉上差别最大，「像不像一门语言」主要来自这里。

```go
// A 今天：语句直接躺在文件顶层
base.Overview.after("Where this fits")

// B：包在函数里
func weave(base, self markdown) {
    base.Overview.after(self["Where this fits"])
}
```

**推荐 B。** 不只是好看：`base` / `self` 从「魔法全局」变成**有类型的参数**，于是 merge 能用返回类型表达（决定 4）、复用能用调用表达（L5.3）。**没有 B，后面几条都没地方挂。**

代价：最简单的文件从 1 行变 3 行。

### 2. 位置词是方法，还是动词前置

```go
base.Overview.after(x)        // A 方法链（今天）
after base.Overview: x        // B 动词前置
```

**推荐 A。** 位置词是**地址的最后一步**（L2.2），方法链如实反映了这一点。动词前置会让它看起来像一个独立操作，而系统里它不是。

### 3. 引用我们自己的内容

```go
base.Overview.after("Where this fits")        // A 裸字符串 = 我们的同名节（今天）
base.Overview.after(self["Where this fits"])  // B 显式
```

**推荐 B 为准、A 为简写。** 有了 `func(base, self ...)`，`self` 本就在作用域里，裸字符串只是省掉它的糖；但两个参数都在场时，显式那版没有歧义。

### 4. merge 用返回类型还是关键字

```go
func weave(base, self markdown) markdown {
    return self
} // reason: 上游那份和我们的会跑两遍

base.merge(self)   // 今天
```

**推荐返回类型。** 它把「这个文件的保证量为零」变成**类型系统的事实**，而不是一个要另外记住的特例。`return a.call(...)` 这类计算也自动落进同一条规则（L5.2）。

### 5. 套用到一批文件

```go
//loom:apply "skills/**"     // A 指令注释，仿 Go build tag
```

**最不确定的一条。** A 的问题是**作用在远处**：读一个模板时，看不出它还被别处套了东西。缓解只能靠构建报告逐个列出、以及编辑器把套来的语句灰显。

**B 替代：编译器推断**——根本不写套用，按结构自己算。更符合 L7.1，但假设不成立时必须响亮失败。

## 6.8 文法

```ebnf
file       = { comment | doc | import | func | directive } ;

func       = "func" ident "(" params ")" [ kind ] "{" { stmt } "}" ;
params     = { ident { "," ident } kind } ;
directive  = "//loom:" ident { string } ;

stmt       = target ( "(" [ args ] ")" | ":" block ) | call | "return" target ;
call       = qname "(" [ args ] ")" ;
target     = root { "." step } [ selector ] [ "." op ] ;
root       = "base" | "self" | ident ;
step       = ident | string | "as" "(" kind ")" ;
selector   = "[" predicate "]" | "." axis ;
axis       = "children" | "next" | "prev" | "parent" ;
op         = "after" | "before" | "start" | "append" | "wrap" | "replace"
           | "drop" | "move" | "reorder" | "swap" | "promote" | "demote"
           | "split" | "join" | "unwrap" | "merge" | "project" ;

args       = arg { "," arg } ;
arg        = string | target | ident | "reason" ":" string ;
block      = NEWLINE indented-lines ;

doc        = "///" ... NEWLINE ;
comment    = "//" ... NEWLINE ;
kind       = "markdown" | "shell" | "toml" | "json" | "text" | ... ;
```

**没有表达式、没有赋值、没有算术。** `return` 只能返回一份文档，不能返回算出来的文本——那条由 L5.2 的类型规则挡住。

## 6.9 刻意不要的

| 不要 | 为什么（全部来自 L0） |
|---|---|
| 表达式、字符串拼接 | 算出来的文本没有写集合 |
| 任意控制流（`while`、任意条件） | 迭代次数不由文档决定，写集合算不出 |
| 变量与赋值 | 同上；`{{@name}}` 是**替换**不是变量，不参与寻址 |
| 继承 / 覆盖链 | L5.5 已是并集、重叠是错误——优先级会把**可检测的冲突**变成**沉默的行为** |
| 一文件多产物 | 「这个产物由什么组成」能被回答的前提 |

**遍历部分不在此列。** 对文档的部分逐个产生写，写集合是每次迭代的并集，迭代空间来自源文档——读一遍源就知道。破坏不变式的是**算出来的文本**，不是**重复的写**。要不要有，取决于 L3 的词汇表能否覆盖真实用例。

---

# L7 · 编译器契约

## 7.1 原则

> **凡是编译器能算出来的，作者就不该写；凡是编译器不该猜的，作者必须写。**

两句都要。第一句给开发体验，第二句是底线。这门语言已有最纯粹的例子：**anchor completion**——你在 `.md` 里加一节，构建算出它该去哪、并把语句写回模板。

## 7.2 补什么

| 作者不写 | 编译器算 | 今天 |
|---|---|---|
| 文档种类 | 扩展名 | ✅ |
| 部分的类别 | 种类的默认类别 | ✅ |
| 路径 | 名字唯一时不需要 | ✅ |
| 未安放内容的去处 | 按邻居推断，**写回模板** | ✅ |
| 公共缩进 | 剥掉 | ✅ |
| 参数类型 | 由用法推断 | ✍️ |
| 套用命中谁 | glob / 结构对齐 | ✍️ |
| 写集合、重叠、保证量 | L4 那套机器 | 部分 |

## 7.3 拒绝什么

| 情形 | 为什么不猜 |
|---|---|
| 名字匹配到两个 / 一个都没有 | 织错位置的产物和正确的长得一模一样 |
| 两个写跨度相交 | 结果取决于顺序，而顺序不该有意义 |
| 上游内容不见了且无人认领 | 整门语言存在的理由 |
| 降低保证量却没写理由 | L4.3：不禁止，但要标价 |
| 推断找不到依据 | 没有依据时，推断就是猜 |

---

# L8 · 演进

```
// loom.om
loom "1.2"
```

一棵要被引用很多年的树，必须能说出自己按哪一版语言写的。`registry` 的移除、`loom.lm` → `loom.om` 的改名，两次破坏性变更都只能靠错误信息补救。

---

# L9 · 现状与分期

## 9.1 这套系统今天实现了多少

| 层 | 今天 | 缺口 |
|---|---|---|
| L1 文档 / 部分 | ✅ | 五种类型，`ast.New` 是一条 switch |
| L1 递归（`as`） | ⚠️ | 只在 toml/json 的值上 |
| L2 地址 / 派生地址 | ✅ | 根固定为 base/self/import |
| L2 选择器 | ❌ | 只能按名字指一个 |
| L2 身份 | ❌ | sync 合内容，不合地址空间 |
| L3 原语 | ✅ | —— |
| L3 词汇表 | ⚠️ | 只有前四行，没有 move / project 等 |
| L4 不变式 | ✅ | 每次构建断言 |
| L4 保证是量 | ❌ | 分档，报告不给量 |
| L5 函数 / 复用 | ❌ | 291 条语句里 68 条是四行样板 |
| L6 语法 | ⚠️ | 无块、无选择器、无 func |
| L7 推断 | ✅ anchor completion | 其余待做 |
| L8 版本 | ❌ | —— |

## 9.2 分期

每期独立证明或证伪一件事：

| 期 | 做什么 | 证明什么 | 属于 |
|---|---|---|---|
| 1 | 再加一种类型，**两个方向都能嵌** | L1.2 那条递归是不是真的 | L1 |
| 2 | 选择器 + `move` / `project` | 词汇表变富时保证能不能变强 | L2 L3 L4 |
| 3 | `func` + 套用 | 抽象能否不动不变式 | L5 |
| 4 | sync 时算重命名 | 身份能否被声明而不是被猜 | L2.4 |

第 1 期排前面不是因为类型重要，而是**它验证 L1.2**——上面八层全压在那条递归规则上。如果一种新类型没法两个方向都嵌，「一张布」就是假的，后面全得重想。

---

# 会推翻这套系统的问题

它压在一个判断上：**人们想对文档做的事，都能说成「选中一些部分，对它们做结构变换」**。

> **有没有一个你真想做、却没法这么说的改动？**

已知的一个候选：`getting-started.lm` 的 16 行是「两个序列对齐」。L3 的词汇表里没有它——除非 `project` 或一个 `align` 算子能表达，否则它就是第一个反例。
