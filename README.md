# Loom

**分叉了一个上游项目，还想继续跟着它走。** Loom 是为这件事做的一门小语言。

经线（warp）是上游那一层：逐字节保留，一根不断。
纬线（weft）是你自己那一层：一梭梭穿进经线之间。
产物是织成的那匹布 —— 而两股仍各是各的：**抽掉纬线，经线原样还在**。

```
loom weave  织一份
loom build  织全部
loom check  复核锚点在当前上游里还找不找得到
loom view   生成带锚点标注的派生视图
```

## 它解决什么

把上游内容和自己的改动揉在同一个文件里手工维护，上游发版时**漏了什么验不出来** ——
两份都改写过的文本之间没有可比锚点。这不是理论问题：一次上游同步漏了 10 处，
先后试了七种机械判据，依次报出 200 / 181 / 34 / 0（假绿）/ 13 / 25 处「缺失」，
每次抽样一读都证明是误报，真读才读出那 10 处。而读几千个内容单元的成本，每次发版都要重付。

分开之后，「上游有没有丢」退化成一次逐字节 diff。**那一整类 bug 结构上不可能发生**，
不是靠小心避免。

## 下梭按位置，不按行号

补丁能往任意文本的任意位置插，但上游改到你插入点旁边一行就打不上 —— build 停下要人重做。
Loom 的锚点认的是上游自己的**名字**：markdown 的标题、shell 的函数和横幅注释、
toml 的键、json 的路径。上游在旁边加一段，梭子照样落在对的地方，
**而且上游那处新改动也进了产物**。上游发版频繁时，这就是每次要不要人介入的差别。

找不到那根经线时织机当场停下，不猜、不取第一个、不打模糊补丁 ——
静默穿错位置的产物看起来完全正常，那是最贵的失败方式。

## loom.hcl

放在仓库根，说清哪一层是经线、哪一层是纬线。

```hcl
layer "upstream" {
  dir  = "src/upstream"
  role = "warp"          // 经线：上游镜像，逐字节，禁改
}

layer "mine" {
  dir  = "src/mine"
  role = "weft"          // 纬线：只写「我和上游不同的那部分」

  // 纬线进 markdown 产物时包上这对标记 ——
  // 「经线 100% 保留」靠剥掉它们再逐字节比来验证。
  // 其它类型不包：HTML 注释在 shell / toml / json 里不是注释，是垃圾或语法错误。
  mark "markdown" {
    begin = "<!-- MINE:BEGIN -->"
    end   = "<!-- MINE:END -->"
  }
}

templates = "src/templates"
anchored  = "src/anchored"     // loom view 的输出（派生视图，不提交）

// 可选：注册表类 json 的合并策略。节点 = 容器里的一条，身份由正则提取。
registry {
  group      = "hooks"
  id_pattern = "hooks/([A-Za-z0-9._-]+\\.(?:sh|js|py))"
}
```

**没有默认值是有意的。** 层名叫什么、哪一层是经线、标记长什么样 —— 每一条猜错都是
静默换了基底：产物仍然合法，只是上游那半没了。默认值只在含义唯一时才安全。

## 模板

一份 `.loom` = 一个 `weave` 块。语法是 HCL（Terraform 那一套），
所以报错带 `文件:行:列`，编辑器高亮现成。

```hcl
weave "skills/testing/SKILL.md" {
  type = "markdown"
  from = "upstream"          // 可省，默认经线

  frontmatter = mine         // 整块 frontmatter 取纬线那份
  bilingual   = ["description"]

  after "heading" "Overview" {
    insert = mine.heading["调度层关系"]
  }

  append = [
    mine.heading["客户 ≠ 用户"],
    mine.heading["常见借口"],
  ]
}
```

### weave 块头

| 键 | 必填 | 说明 |
|---|---|---|
| `type` | 是 | `markdown` / `toml` / `json` / `shell` / `text` |
| `from` | 否 | 以哪一层为底，默认经线 |
| `path` | 否 | 经线里的路径，默认 = 产物路径 |
| `covers` | 否 | 声明本产物取代了上游的哪些文件 |

`path` 是给**改过名的上游资产**用的。默认基底路径等于产物路径 ——
改名之后就指不到原件了，只剩「整份丢掉再自己写」，而那意味着上游此后的改动全部看不见。
写明 `path`，改名与扩展就能同时成立。

`from` 指到纬线层意味着「产物整份用我的，上游一个字进不了」。这是要付代价的选择，
应该在你自己的门里要求写明理由 —— **丢掉上游必须是一件写得出来的事**。

### 能锚到什么

| type | 节点种类 | 锚点是什么 |
|---|---|---|
| `markdown` | `heading` / `line` / `frontmatter` | 标题文字 / 整行原文 / frontmatter 块 |
| `toml` | `key` | 顶层键名 |
| `json` | `path` | 点分对象路径 |
| `shell` | `function` / `marker` / `line` | 函数名 / 注释横幅（按前缀匹配，到下一条横幅为止）/ 整行 |
| `text` | `line` | 整行原文 |

`marker` 是为「看上去无名字可锚」的顶层流程加的。实测一个上游脚本里 14 处编辑只有 1 处
落在函数内，其余 13 处在顶层 —— 但顶层其实有天然的名字：`# ── Test 3: … ───` 这类横幅，
11/14 落在唯一的横幅段里。*「没有结构」往往只是「我的 AST 里没有那种节点」。*

### 引用：插什么

```hcl
mine.body                  // 正文（markdown 去掉 frontmatter）
mine.all                   // 整份原文
mine.heading["某一节"]      // 那一节
mine.function["run"]       // 那个函数
"直接写在这里的字面内容"      // 字符串就是字面量
```

### 语句：插到哪

```hcl
after   "heading" "Overview" { insert = mine.heading["A"] }
before  "function" "main"    { insert = mine.function["boot"] }
replace "function" "gate"    { with   = mine.function["gate"] }

append  = mine.heading["附录"]              // 追加到末尾
prepend = mine.all                          // 插到开头
append  = [ mine.heading["A"], mine.heading["B"] ]   // 列表 = 按序多条
```

**语句在模板里的先后顺序是有意义的** —— 同一个锚点上的两条插入，谁写在前谁就在前。
（HCL 的属性在解析后是一张 map，Loom 按源码字节偏移把顺序排回来。）

### 其它语句

```hcl
frontmatter = mine                        // 整块 frontmatter 取某一层
set       = { description = mine.description }   // 单个键取某一层的值
bilingual = ["description"]               // 该键 = 纬线的值 + 经线的值

in "prompt" {                             // 换一套嵌套 AST
  as     = "markdown"                     // toml 的 prompt 里其实是 markdown
  reuse  = "commands/ship.md"             // 这一块的纬线内容改从另一个文件取
  append = mine.body
}

anchor "cache-setup" {                    // 没有天然锚点时起个名字，单点维护
  line = "mkdir -p \"$CACHE\""
}

patch = "session-start.sh.diff"           // 补丁式合成，指向一份 .diff 文件

inherit  = ["extract_header"]             // 分叉资产的函数级声明（只校验，不合成）
override = ["dbg"]
new      = ["main"]
```

**`bilingual` 存在的理由**：像 `description` 这种被机器用来做路由的字段，
两种语言都要有，而另一种语言那半**编译时从经线取**，不是手抄进纬线 ——
手抄的第二份副本会在上游改了之后悄悄漂移，而没有任何东西会告诉你。

**`patch` 为什么指向文件而不写在模板里**：HCL 的 heredoc 会做 `${…}` 插值，
而 diff 里天然带 `${VAR}` —— 内联就得转义，转义之后模板里那段**不再是一份 diff**：
编辑器不认、`patch(1)` 不认、复制出来直接用会失败。补丁用 `-F0` 打，
**关掉模糊匹配**：默认的 fuzz 会在上下文对不上时把补丁打到近似位置而不报错。

**`inherit` / `override` / `new`** 用于没法拼接的可执行文件 —— 把纬线的函数接在上游脚本
后面，改的是行为而不只是内容，所以整份取纬线。这三条声明让「上游修了一个我们原样沿用的
函数」变成一件查得出来的事。*分叉的风险从来不是「改了」，是「上游后来修了而我们不知道」。*

## 报错

所有错误都带位置并让命令非零退出。语言不做「尽力而为」——
合不上就停下，因为悄悄合错的产物看起来和对的一模一样。

| 错误 | 意思 |
|---|---|
| 锚点找不到 | 上游多半改了那个标题/函数名 —— 这不是故障，是该看一眼的信号 |
| 锚点匹配到多处 | 名字不唯一。不取第一个，请写得更准 |
| 这个类型没有 `X` 这种节点 | 比如对 toml 用 `heading` |
| `X` 层里没有 `Y` | 基底或引用的文件不存在 |
| 未知层名 | 层名写错了。**不猜** —— 猜错就是静默换基底 |
| 这些函数有头无尾 | shell 解析不了，不能当「没有这个函数」 |

## 装

```
go install github.com/axfor/loom/cmd/loom@latest
```

## 许可

Apache-2.0。
