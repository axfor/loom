<p align="center">
  <img src="editors/vscode/icons/loom.png" width="96" height="96" alt="Loom">
</p>

# LOOM

> ### A structured programming language for resource files of every kind.
> **Every resource becomes structure. Every part of it is referenced as an object.**

Markdown, shell, JavaScript, JSON, YAML, TOML — each is parsed into an object of named parts, and
the language programs against those objects: reference them, place them, transform them. The names
are the ones their authors already wrote — a heading, a function, a key — so nothing has to be
annotated first.

**Today that language extends someone else's AI skills, and still takes their updates.**

A skill set is files: `SKILL.md` with its frontmatter and its sections, commands, hooks that
register a handler in a json file, shell scripts they call. You want a section of your own inside
their skill, your commands beside theirs, your handler on their hook. Then they release, and every
one of those edits is yours to redo.

Loom keeps the two apart. Upstream stays in its own directory, untouched, byte for byte. Yours
lives in a second directory laid out the same way. Beside each of your files sits a short template
saying where your content belongs in theirs, and `lm build` weaves them into the skill set you ship.

**Extending, not forking.** Your layer holds only what is yours — no copy of upstream to keep in
step, no context lines around your changes. Upstream's own names are the anchors: a markdown
heading, a shell function, a toml key, a json path. `base.Overview.after("Where this fits")` means
*after their overview*, and goes on meaning it when they rewrite the paragraph under it or move the
section down the file.

**Merging that does not need you.** An upstream release flows in: their fixes where you changed
nothing, their new sections where your anchors still hold. A json registry is merged by the handler
each entry calls, so your version of a hook replaces theirs and a hook they add arrives on its own.
A script you edited line by line is carried onto each new upstream by `lm sync`, with conflict
markers only where they touched the lines you touched.

**Maintainable because nothing can go missing quietly.** The two layers never merge into one text,
so "did upstream content get lost" is a byte comparison — and the build makes it every time, on
every file. Upstream content can only be dropped or replaced with a written reason, and every
reason shows up in the build report. When an anchor is gone or matches twice, the build stops
rather than guessing.

**Where this is going.** Extending a skill set you do not own is the first use of the fabric, not
the point of it. Once every resource is an object of named parts, a product can be assembled from
many sources rather than two layers, one document can reference part of another instead of copying
it, and "what depends on this section" becomes a question with an answer. The nearer aim along that
line is writing skills in the first place — one source, built for every platform you target, since
what differs between them is the layout and the wrapping, not the content. `mirror` is already the
small version of that, one build writing the same commands into the two places two tools expect to
find them.

The design that follows from this is worked out in [DESIGN.md](DESIGN.md), and the language it
implies in [SYNTAX.md](SYNTAX.md). Neither is built yet.

The name is the mechanism. The warp is upstream: kept whole, never cut. The weft is your layer,
threaded through it. The cloth is the product — **pull the weft out and the warp is still there.**

```
lm build    build the whole tree and print the build report
lm check    the same checks, writing nothing; -o dir also reports out-of-date output
lm sync     take a new upstream release, carrying our edits onto it
lm update   replace lm with the latest release
```

## One file, end to end

Upstream ships a document. You want a section of your own after its overview, an appendix at the
end, and your wording in front of its description — without editing upstream's file.

**`upstream/doc.md`** — upstream's, never touched:

```markdown
---
name: doc
description: The upstream description.
---

## Overview

Upstream overview text.

## Process

Upstream process text.
```

**`mine/doc.md`** — only what is yours. No copy of upstream, no context lines, no line numbers:

```markdown
---
name: doc
description: Our half of the description.
---

## Where this fits

A section we added.

## Appendix

A section appended at the end.
```

**`mine/doc.lm`** — the template beside it: one statement per insertion, naming the upstream place
each piece belongs to.

```
base.frontmatter.description.start(self.frontmatter.description)
base.Overview.after("Where this fits")
base.append("Appendix")
```

`lm build` weaves them:

```markdown
---
name: doc
description: Our half of the description. The upstream description.
---

## Overview

Upstream overview text.

<!-- MINE:BEGIN -->
## Where this fits

A section we added.
<!-- MINE:END -->
## Process

Upstream process text.

<!-- MINE:BEGIN -->
## Appendix

A section appended at the end.
<!-- MINE:END -->
```

Every upstream line is there, in order, unchanged; everything of yours sits inside the marks. Take
the marked blocks out and you are holding `upstream/doc.md` again — and that is checked, not hoped
for: when a template only inserts, the build compares the product's body with the marks stripped
against upstream, and refuses to write if a single byte differs.

## What it solves

Keep upstream content and your changes mixed in one file, and when upstream releases **you cannot
tell what got lost**: two rewritten texts have nothing to compare against. This is not theoretical.
One upstream sync dropped 10 changes; seven mechanical checks were tried in a row and reported 200 /
181 / 34 / 0 (false green) / 13 / 25 "missing" items. Every sample read turned out to be a false
alarm, and only reading everything found the 10. That reading cost comes back with every release.

With the layers apart, "did upstream content get lost" becomes a byte comparison, and Loom makes it
at every build. That whole class of bug cannot happen, instead of being avoided by being careful.

## When an anchor no longer fits

A diff can change anything, but when upstream changes the lines next to yours it no longer applies,
and someone has to redo it. Anchoring on upstream's own names — markdown headings, shell functions
and banner comments, toml keys, json paths — is what removes that person from the loop: with a busy
upstream, it is the difference between a human stepping in every release and none.

It does not always hold. Upstream renames the heading you anchored on, or splits it in two, and
there is no honest guess to make.

When an anchor is not found, or matches more than once, the build stops. Loom does not guess, take
the first match, or fit anything approximately: a product woven in the wrong place looks exactly like a
correct one, and that is the most expensive way to fail.

## Files with nothing to anchor to

Some files have no names to weave by — a shell script where you changed a few lines inside a
function, or a json registry both layers add entries to. There the template is `base.merge(self)`:
your file is the product, and `lm sync` carries each new upstream release onto it with a three-way
merge, leaving conflict markers only where upstream changed the lines you changed. The build still
checks that no upstream function went missing without a stated reason. See
[Following upstream](#following-upstream) and [Registries](#registries).

## Reference

- [Layout](#layout)
- [Settings: loom.om](#settings-loomom)
- [Templates](#templates)
- [Objects](#objects)
- [Nodes](#nodes)
- [Methods](#methods)
- [Registries](#registries)
- [What lm build does](#what-lm-build-does)
- [Variables](#variables)
- [Anchor completion](#anchor-completion)
- [The build report](#the-build-report)
- [Install](#install)
- [Commands](#commands)
- [Editor](#editor)

---

## Layout

```
loom.om                           settings
lm.e                              variables (optional)
upstream/                         base: the upstream project, untouched
mine/                             self: our layer, laid out like the product
  skills/testing/SKILL.md           our content for that product file
  skills/testing/SKILL.lm           its template: how upstream and ours are woven
  hooks/run.sh                      our version of an upstream script
  hooks/run.lm                      its template: base.merge(self)
```

Both trees share the product's layout, so "what is this product file made of" needs no lookup
table: the same path in upstream and in our layer, and a template beside our file. Templates are
sources only; the build never copies them into the product. To keep templates in a tree of their
own, set `templates "dir"`.

A template is named after the file it builds, with the extension left out: `SKILL.lm` builds
`SKILL.md`. The build finds the file in the layers — one named exactly so (`LICENSE.lm` builds
`LICENSE`), or else the one file with that name and an extension, templates not counted.
When several could be meant (`notes.md` and `notes.yml`), the short name is an error and the
template takes the full name, `notes.md.lm`; a name no layer has a file for is the product path as
written. Two templates for the same product are an error.

In VS Code the extension folds `X.lm` under the file it builds, so the explorer shows one entry per
file (see [editors/vscode](editors/vscode/README.md)).

A product file comes from exactly one place, in this order:

1. a template exists → woven
2. our layer has the file → copied (variables expanded)
3. upstream has the file and `take` lists it → copied

---

## Settings: loom.om

One setting per line. `//` starts a comment.

```
loom      "1.0"

// what to do with a key of ours that upstream also has
frontmatter start   // a markdown file's frontmatter
keys        start   // a toml or yaml file's top-level keys
body        append  // where our body goes when no section of it can follow an upstream one

base      "upstream"
self      "mine"
output    "../dist"

// our content in markdown products is wrapped in these marks
mark      markdown "<!-- MINE:BEGIN -->" "<!-- MINE:END -->"

// upstream files that go into the product as they are
take      "references/**" "skills/**" "LICENSE"

// copy one product directory to another path
mirror    ".gemini/commands" "commands"

// write the list of product files here
manifest  ".build-manifest"
```

| Setting | Meaning |
|---|---|
| `frontmatter` | what the build should do with a frontmatter key of ours that upstream also has: `set`, `start` or `append`. Say it once and the build writes the statement into each template that needs it, the way it completes anchors. Leave it out and an unaccounted key is an error, as before — whether ours replaces upstream's value or goes before it changes what the product says, and nothing decides that for you |
| `body` | where our body goes when *none* of its sections can follow an upstream one — our headings share nothing with upstream's, most often because they are a translation. Anchor completion says so and stops today; with `start` or `append` the tree has answered once, and `base.append(self.body)` is written into the template. Where even one section of ours has a neighbour, the neighbour rule still decides: this answers having no neighbour, it does not switch that rule off |
| `keys` | the same, one level out: the top-level keys of a toml or yaml file. A key a statement already speaks for is left alone, including one opened as a view; a key upstream does not have is added to the file rather than written into upstream's, so the setting does not cover it |
| `loom` | the language version this tree is written for; an lm that speaks an older one refuses it and says to update. Leaving it out is allowed and means "whatever this lm speaks" |
| `base` | upstream directory (required); in a template, `base` is the file at the product's path in it |
| `self` | our layer's directory; in a template, `self` is the file at the product's path in it |
| `templates` | template directory; without it, templates live next to our files in `self` |
| `output` | default output directory of `lm build` |
| `mark` | begin and end marks for one file type; may appear once per type |
| `take` | upstream paths that go into the product; `*`, `?`, `[...]` match within a path segment, `**` matches any number of directories; may repeat |
| `mirror` | after building, copy every product file under the first directory to the second; may repeat |
| `manifest` | write a list of all product files to this path in the output directory |

Upstream files are not taken by default. An upstream repository usually carries things that only
serve its own development (evaluation fixtures, CI settings). What was not taken is listed in the
build report, so a file newly added upstream never goes missing silently.

Marks matter: they are what makes "upstream is still there" checkable. Configure them only for types
where the mark is a comment; an HTML comment in a shell script is not.

---

## Templates

A template is a list of statements, one per line.

```
// mine/skills/testing/SKILL.lm
import ship "/.claude/commands/ship"

base.frontmatter.description.start(self.frontmatter.description)
base.Overview.after("Where this skill sits", "Customers are not users")
base."How it compares".drop(reason: "compares upstream with other projects; does not apply here")
base.append(ship.body)
```

Read it as: *upstream's `Overview` section, insert after it our sections "Where this skill sits" and
"Customers are not users".* The thing being changed comes first, the operation is a method on it,
and the content is the argument.

Lexical rules:

| Element | Form |
|---|---|
| comment | `//` to the end of the line |
| string | `"..."` on one line; the only escapes are `\"` and `\\`, any other backslash is kept (regexes stay readable) |
| literal | `` `...` `` may span lines, no escapes; leading and trailing blank lines and the common indentation are removed |
| identifier | letters (any script), digits, `_`; does not start with a digit |
| statement end | a newline |

---

## Objects

| Object | What it is | Layer | Can be changed |
|---|---|---|---|
| `base` | the upstream file at the product's path | upstream | yes — the product *is* the woven base |
| `self` | our layer's file at the product's path | ours | no, it is a content source |
| an imported name | another file of our layer | ours | no, it is a content source |

Only `base` can be changed: a template produces exactly one file. Calling a method on `self`
would read as if another file were changed while nothing happens, so it is an error.

### import

```
import "../commands/ship"             // name taken from the file: ship
import cmd "/.claude/commands/ship"   // named cmd
import base "/old/name"               // base is a renamed upstream file
```

| Rule | |
|---|---|
| path | `./` and `../` are relative to the product's directory; `/` starts at the layer root |
| extension | may be left out when exactly one file matches (templates do not count); several matches is an error |
| layer | `base` resolves in upstream, every other name in our layer |
| name | without a name, the file name without extension; it must be a valid identifier |
| duplicates | importing a name twice is an error; `self` cannot be redirected |
| type | taken from the extension: `.md` markdown, `.toml`, `.yaml` / `.yml`, `.json`, `.sh` shell, anything else text |

---

## Nodes

A node is a part of a file. A bare name means the default node kind of the file type:

| Type | Default node | Other nodes |
|---|---|---|
| markdown | section: a heading down to the next heading | `section("...")`, `line("...")`, `frontmatter`, a frontmatter key `frontmatter.description` (quoted when it has a `-`: `frontmatter."argument-hint"`), `body` (everything after the frontmatter) |
| shell | function | `function("...")`, `marker("...")` (a banner comment `# ── Name ──` down to the next banner, matched by prefix), `line("...")` |
| toml | key | `key("...")` |
| yaml | key (nested by path) | `key("...")`; a key covers whatever is indented under it |
| json | key (nested by path) | `key("a.b")` |
| text | line | `line("...")` |

### What each type is checked for

Not every guarantee can mean the same thing for every kind of file, and the table says which ones
do. Each row was checked by running the build, not read off the code.

| Type | Upstream content can't be lost | Ours can't go missing | Insert-only stays byte-identical | Anchor completion | `move` verified |
|---|---|---|---|---|---|
| markdown | by section | by section, and by frontmatter key | the body — the frontmatter is changed on purpose | yes | yes |
| shell | by function | by function | the whole file | yes | yes |
| toml | by key | by key | the whole file, unless a value is written | — | yes |
| yaml | by key | by key | the whole file, unless a value is written | — | yes |
| json | by construction: a registry merge is upstream's entries plus ours | — | — (a registry is merged whole) | — | — |
| text | **no** — see below | **no** | the whole file | — | — (a move needs a plain node, and every line is a call) |

Anchor completion stops where order stops meaning anything: a section follows the section before
it, and so does a function, but a toml key's neighbour says nothing about where a new key belongs.

**text is the one gap, and it is deliberate.** Its nodes are lines, and a line *is* its content, so
"this node is still here" — the question asked of the other types, indifferent to what changed
inside it — becomes "this line is unchanged". That is a far stricter promise, not the same one
extended, so it is not made without asking. TODO.md carries the decision and what it would cost.

A nested key is reached by chaining names, `base.jobs.test`, or by quoting the whole path,
`base."jobs.test"`, for a segment a dot cannot spell. A trailing part of a path is enough while it
is unambiguous: `base.steps` finds `jobs.build.steps` as long as nothing else ends in `steps`.

### String form and identifier form

```
base."How it compares"     // string form: always works
base.How_it_compares       // identifier form: _ matches a space or an underscore
```

- If an identifier matches more than one name (both `A B` and `A_B` exist), it is an error: use the
  string form.
- A name with punctuation can only be written in string form.
- After a dot, a name followed by `(` or `{` is a method; anything else is a node. A section
  literally called `append` is `base."append"`.

### Sections and subsections

A markdown section runs from its heading to the **next heading of any level**. Subsections are nodes
of their own:

```markdown
## Setup              ← base.Setup is these two lines
intro

### Option 1          ← base.Option_1
...
```

So `base.Setup.after(...)` inserts after the intro, before `### Option 1`.

### Section paths

The same subsection name often appears under several sections. A path says which one:

```
base."Example 2"."Phase 1".after(...)
self."Example 2"."Phase 1"
```

Every name but the last is looked up inside the previous section's whole chapter (down to the next
heading of the same or a higher level); the last name is an ordinary section. Each step must match
exactly one heading.

---

## Methods

| Method | On | Meaning | Arguments |
|---|---|---|---|
| `after` | node | insert after the node | content, one or more |
| `before` | node | insert before the node | content, one or more |
| `start` | file / view | insert at the start | content, one or more |
| `start` | key value | the value becomes ours followed by upstream's: `base.frontmatter.description.start(self.frontmatter.description)` | one value |
| `append` | file / view | insert at the end | content, one or more |
| `append` | key value | the value becomes upstream's followed by ours | one value |
| `replace` | node | replace the node with content | one content, `reason:` |
| `replace` | `base` | whole-file replace: `base.replace(self, reason: "...")` | a file object, `reason:` |
| `drop` | node | leave the node out of the product on purpose | `reason:` |
| `move` | node | put the node somewhere else in the same file | `after:` or `before:`, naming a node of upstream |
| `promote` / `demote` | markdown section | take the heading up or down one level | none |
| `wrap` | any node | insert before and after in one statement | none |
| `swap` | any node | two nodes trade places | none |
| `unwrap` | markdown section | the heading goes, what was under it comes up a level | yes |
| `join` | markdown section | this section and the next run together | yes |
| `split` | markdown section | a heading of ours cuts it in two | none |
| `set` | `frontmatter` | take our whole frontmatter: `base.frontmatter.set(self.frontmatter)` | `self.frontmatter` |
| `set` | key value | the value becomes ours: `base.frontmatter."argument-hint".set(self.frontmatter."argument-hint")`, `base.description.set(self.description)`; adds a frontmatter key upstream does not have | one value |
| `merge` | `base` | the product is upstream's file and ours together. For a script ours is the product and `lm sync` carries the edits onto each new upstream ([Following upstream](#following-upstream)); for a json registry the entries are merged by identity, ours winning where both register the same handler ([Registries](#registries)) | `self` |
| `as` | toml / yaml / json key | view the key's value as another type; methods follow | a type name |

### Arguments

| Written | Means |
|---|---|
| `"Install XSDD"` | `self."Install XSDD"`: a node of our file, same type as where it goes |
| `self.boot`, `cmd."Name"`, `self."A"."B"` | a node of a named object |
| `cmd.body`, `self.frontmatter` | a part of a file |
| `self.frontmatter.description`, `self.description` (toml / json) | a key's value, for `set` / `start` / `append` on a key |
| `self`, `cmd` | a whole file |
| `` `...` `` | literal content |
| `reason: "..."` | named argument: why upstream content is changed |

`(...)` on one line and `{ }` over several lines mean exactly the same:

```
base.Install.after("Install XSDD", "Proxy settings")

base.Install.after{
    "Install XSDD"
    "Proxy settings"
}
```

In the block form each argument is on its own line, without commas. An empty block is an error.

### Literals

A single backtick ends at the next one, which is enough for a line and not enough for a document:
real markdown is full of inline `code` and fenced blocks. **Three or more backticks open a fence**,
closed by as many again at the start of a line, and a tag says what the content is:

````
base."Where this fits".before(```markdown
## Where this fits

Run `lm build` to weave it:

```sh
lm build -o ../dist
```
```)
````

Nothing inside needs escaping. A fence inside the content is held by opening with more backticks
than it uses — the same rule markdown itself has. The common indentation is removed, and the tag
gives the editor the language to highlight the content as, so writing content inside a template
reads the way writing it in its own file does.

```
base.start(`> Generated from mine/README.md — do not edit`)

base.start{
    "Read this first"
    `
    > Generated from mine/README.md — do not edit
    `
}
```

A literal is our content: in markdown it is wrapped in marks like any other of our content.

### Selecting a group

The plural form of a node kind takes a predicate instead of a name, and the operation is done to
every node it finds:

```
base.sections(match: "^Step ").demote()
base.sections(empty).drop(reason: "upstream left the shells of sections it never wrote")
base.sections(level: 3).demote()
base.functions(match: "^_").drop(reason: "private helpers we replace wholesale")
```

| Predicate | Matches |
|---|---|
| `match: "..."` | the node's name against a regular expression |
| `level: N` | markdown headings at depth N, 1 to 6 |
| `empty` | nodes with nothing under them |

Several predicates narrow each other: `sections(level: 2, match: "^Step ")` is the level-two
headings called Step, not the union of the two.

Only a markdown heading has a level, so `level:` on a line, a function or a key is refused where it
is written rather than left to match nothing.

A predicate that matches nothing is an error, not a statement that quietly did nothing.

A group takes only the operations that mean the same thing done to each of it — `drop`, `promote`,
`demote`. Anything needing a target or a source of its own (`after`, `move`) names one node.

**The guarantee is not weakened by there being several.** Each node is checked on its own and the
report lists each one, so a predicate is a way of writing less, never of knowing less.

### Projecting upstream's shape

`project` derives content from the structure of a file rather than from its words: every node a
predicate finds, put through a template once.

```
base.start(base.sections(match: "^Step ").project(`- {name}`))
base.append(base.functions(match: "^test_").project(`### {name}

{body}`))
```

| Field | Is |
|---|---|
| `{name}` | the node's name — the one its author wrote |
| `{level}` | a markdown heading's level; 0 for other nodes |
| `{body}` | what the node holds, trimmed |

This is the only content source that reads upstream, and it is allowed to because it copies
nothing upstream says — only the names it gave things. What it writes was not there before, so it
is ours: it goes inside our marks, and upstream is still proved whole.

**There is no field for a link target.** An anchor is a convention of whichever renderer reads the
product — GitHub, Obsidian and mkdocs each slugify a heading differently — and that is not a
property of the document. Guessing one would be this language making up a rule it cannot check. If
a link is wanted, write the target the renderer expects.

### Reasons

`replace` and `drop` change upstream content, so they need `reason:`. Other methods may not have one.
Reasons show up in the build report.

`move` needs none, and that is the point of it. It changes where a node is, never what it is, so
nothing is lost and nothing is ours: the node's own bytes are still in the product. The build checks
exactly that — not that the file still parses, but that the moved node is byte for byte what
upstream wrote — and the report lists the move with where it went.

```
base.Troubleshooting.move(after: base.B)
base."Quick Start".move(before: base.Install)
```

`promote` and `demote` are the same kind of thing for heading level — when you wrap upstream's
sections under one of your own, everything below has to shift a level:

```
base.Setup.demote()
```

Only the `#` markers change. The build strips the markers from both sides and the section must be
identical otherwise, so a code fence with a `#` in it, or anything else inside, cannot quietly move.

These are the operations that promise *more* than an insertion does. An insertion promises upstream
is untouched around it; these promise the node itself came through unchanged — byte for byte for a
move, everything-but-the-level for a re-levelling — which is why each is checked rather than
assumed, and why neither needs a reason. The report lists them under `restructured`.

Two statements that write over the same lines are a conflict, not a question of precedence: the
build stops and names both, because whichever lost would leave no trace.

### Views

A key whose value is really a document of another type — a toml `"""` block, a yaml block scalar,
a json string:

```
import cmd "/.claude/commands/ship"
base.prompt.as(markdown).append(cmd.body)
base.prompt.as(markdown).Steps.after("Our step")
```

The value is parsed as that type and woven section by section, then put back where it came from,
re-indented under its key. A backtick or a fenced code block inside it is just text — it is content,
not a literal in the template.

Inside a view, a bare string names a section of the same key's value in our file.

This is also why markdown frontmatter needs no special case: it is yaml, so
`base.frontmatter.description` and a yaml key are the same thing reached two ways.

### Combinations

- A **whole-file replace** can only be combined with `drop`. Nothing else could take effect.
- A **merge** can only be combined with `drop`. Our file is the product.
- In those two cases `drop` is a declaration: *this upstream section is intentionally not in the
  product*. The build verifies the section really is absent.

**Splitting a section** has no operation of its own, because inserting a heading is already one.
A section runs to the next heading, so putting a heading in the middle of it makes two:

```
base.Setup."Step A".before(`## Setup, part two`)
```

Everything upstream wrote is still there, in the same order, so the build still proves the file
unchanged byte for byte.

### Asking the document a question

A template can ask before it writes. The question reads the document and writes nothing, so it is
always safe:

```
if base.has.Overview {
    base.Overview.after(self.notes)
}

if base.sections[level == 2].any {
    base.start(base.sections[level == 2].project(`- {name}`))
}
```

Questions chain, and the first that holds decides:

```
if base.has.Overview {
    base.Overview.after(self.notes)
} else if base.has.Summary {
    base.Summary.after(self.notes)
} else {
    base.start(self.notes)
}
```

A write can also be caught instead of stopping the build. Catching is the only way a failure gets
through, so it has to be written down — and a caught result that nothing reads is an error, or
catching would be a synonym for swallowing:

```
ok = base.Overview.after(self.notes)
if !ok {
    return err.format("upstream dropped Overview: %s", ok)
}
```

The result carries why, with the file and position. `err.format` only formats the message: nothing
it produces can reach the product.

The report says which way each question went, because that is the one thing a reader cannot see
from the template alone:

```
skipped              1 places  a question decided against it
  me/SKILL.md.lm                               SKILL.md skipped (base.has.Nope did not hold)
```

### Predicates

A group can be picked with a predicate in brackets, and the terms compose with `&&`, `||` and `!`:

| Asks | Written |
|---|---|
| how deep a heading sits | `level == 2`, also `!=` `<` `<=` `>` `>=` |
| what it is called | `name == "Setup"`, `name ~ "^Step "` |
| whether it holds anything | `empty` |
| what a shell function calls | `calls "curl"` |
| what a section contains | `has."Usage"` |

Those last two ask what they say rather than searching for the word. `calls "curl"` wants `curl` at
the start of a statement — after a separator, inside a substitution, or following `if` / `then` /
`do` — so `echo curly` and a commented-out `curl` are not calls to it. `has."Usage"` wants a part
of that name inside this one, not the word somewhere in its text. A yaml key holds every key below
it, so every ancestor of a path answers to it.

```
base.sections[level == 3 && name ~ "^Step "].demote()
base.sections[empty].drop(reason: "upstream left the shells of sections it never wrote")
```

`.first` and `.last` take one out of a group, and `has."Usage"` may also be written `has["Usage"]`.

A bare class name is every node of that kind:

```
base.sections.demote()
```

An empty call, `base.sections()`, stays an error — that reads as a call someone meant to fill in,
and guessing what they meant is the one thing this language will not do.

### Axes

From a node to one the document relates to it:

```
base.Setup.children.demote()     the sections one level down
base.Setup.next.promote()        the next section at this level
base.Setup.prev                  the one before
base."Step A".parent             the section this one sits in
```

Only markdown nests, so `children` and `parent` are its alone.

Axes chain, and each step is checked against what it lands on — `children` gives a group, so
`first` may follow it; `next` wants one node, so it may not:

```
base.sections[level == 2].first.children.demote()
```

### Writing at a place

`after`, `before`, `start`, `end` and `append` name a span of length zero, and `project` writes
derived content there:

```
base.start.project(base.sections[level == 2]){
    ```markdown
    - {name}
    ```
}
```

`base.start(base.sections[...].project(`- {name}`))` says the same thing on one line.

### Grouping statements in a file

`fn` groups statements; it is inlined where it is called, so there is no call at build time and no
recursion:

```
fn bilingual(up, ours) {
    up.after(ours)
}

bilingual(base.Overview, self.overview)
bilingual(base.Usage, self.usage)
```

### Our content, written in the template

Content short enough to read beside the statement that places it can live in the template, after a
line of dashes. It is addressed exactly as a file of ours would be:

````
base.Overview.after(self.job)

---
Self:
    ```markdown
    ## job

    Where it sits in the build.
    ```
````

The fence's language tag is the kind. A section can hold one document of each kind, and a template
can have several sections, named:

````
base.Overview.after(self.docs.job)
base.description.start(self.cfg.toml.description)

---
Self as docs:
    ```markdown
    ## job

    …
    ```
---
Self as cfg:
    ```toml
    description = "ours"
    ```
````

The address is `self[.section][.kind].part`, and both middle parts may be left out: the one
document that holds the name is the one meant. None is an error that lists what there is, and two
is an error that asks which — nothing is guessed.

### Returning our file

```
return self // reason: upstream's and ours would both run
```

That is what a whole-file replace has always been, said in one line. It gives up every guarantee
about upstream, so it needs a reason, and nothing may follow it.

### Following upstream

Some files can't be woven by name: a script where we changed a few lines inside functions, say. For
those, our file is upstream plus our edits, and the template says so:

```
// mine/hooks/run.lm
base.merge(self)
```

Edit `mine/hooks/run.sh` as you would any file; the build uses it as the product and still checks
that no upstream function went missing without a `drop`. There is no patch file to write or keep.

When upstream releases, take it with `lm sync`:

```
lm sync ../upstream-checkout      the new release; leave out .git and anything else that is not upstream
```

Sync replaces the upstream layer with that tree — files added, changed and removed, links kept as
links — and, while the old upstream is still at hand, merges upstream's change into each merge
template's file (a three-way merge: our file, the upstream it was based on, the new upstream).
Upstream's fixes where we changed nothing flow in. Where upstream changed the lines we changed, our
file gets conflict markers, as in git, and the build refuses it until they are resolved. A file
upstream removed is listed too. Sync exits non-zero while any of that needs a person.

### When upstream renames something

An anchor names a node by the words its author wrote, so a rename upstream breaks every anchor
pointing at it — and the sync that brings the rename in is the one moment both versions are in
hand. So that is where it is worked out:

```
followed       1 files   upstream renamed it; the anchor was rewritten — review with your commit
  mine/doc.lm: Overview → Introduction
```

A rename is recorded **only** when a name vanished and exactly one new name holds byte-identical
content. A name that vanished with nothing matching it was deleted. One that could have become any
of several is reported and left alone:

```
ambiguous      1 files   a name vanished and could be several things — left for you
  doc.md § Overview could be A or B
```

It applies to every kind — a shell function and a yaml key are named by their authors too — and it
edits the name where it is written and nothing else, so what you review is one word per rename.

Resolve a conflict before the next sync: the markers can only be resolved against the upstream they
came from, so sync refuses to replace it while any are left.

Take upstream through `lm sync`: a copy made some other way leaves no old upstream to merge from, and
our edits would be kept but upstream's change to those files would not come in.

### Registries

A json file is usually a registry: a table saying which handler runs on which event, like a hooks
file. A json product is built from both layers, and its template says exactly that:

```
// mine/hooks/hooks.lm
base.merge(self)
```

The objects are merged key by key. In a list, an entry of ours takes the place of the upstream entry
that **calls the same scripts**, and our other entries are added. So our version of a handler wins,
and a handler upstream adds of its own arrives on its own. Nothing is configured: a registration is
recognised by the handler it names (`hooks/run.sh` in the command it runs).

What that is for is the thing that must not happen: registering the same handler twice. Upstream and
our layer word it differently (upstream wraps the script in a fallback, we call it directly), so a
plain union keeps both — the file stays valid json and the handler simply runs twice, which usually
breaks it, quietly. The build fails when an entry of ours is missing from the product, and when the
same handler ends up registered twice.

A registry is merged whole, so its template is that one statement: weaving statements have no part in
it, and the build refuses them rather than ignore them.

---

## What lm build does

1. Loads every template. Every error found is reported, not just the first (at most 20 are shown).
2. Completes anchors (below) and writes them back into templates.
3. Weaves each template and checks it:
   - **lost content**: for every kind of node the build can name — markdown sections, shell
     functions, toml and yaml keys — every upstream node must still be in the product (marks
     stripped) or be accounted for by a `drop` / `replace` with a reason.
     Otherwise the build fails. A whole-file replace with a reason accounts for the whole file.
   - **dropping a section with subsections** requires dropping (or otherwise accounting for) each
     subsection, since a section stops at the next heading.
   - **insert-only templates**, of any kind: with our marks stripped, the product must be
     byte-identical to upstream. For markdown that means the body, since `set` / `start` / `append`
     change the frontmatter on purpose; for a toml or yaml file a template that writes a value is
     not insert-only either, and is not held to this.
   - **our keys**: every key of our file must be in the product — a markdown file's frontmatter
     taken with `set` or put next to upstream's with `start` / `append`, a toml or yaml file's
     top-level keys the same — or the build fails. A frontmatter value over several lines (a list,
     a nested map) is not joined on one line: it can only be taken whole, with
     `base.frontmatter.set(self.frontmatter)`, and the build says so when it has to be. A key
     upstream does not have is added to the file with `base.append(self.<key>)`.
4. Copies our layer's files. A file of ours at the same path as an upstream file, with no template,
   is an error — it would silently replace upstream with no reason. An identical copy is fine.
5. Copies upstream files listed in `take`, applies `mirror`, writes `manifest`.
6. Writes the output directory: only files that changed are written, permissions follow the source,
   symbolic links are kept as links, files that are no longer produced are deleted.

Nothing is written unless every step succeeded. Two builds of the same output directory at the same
time are refused (a lock older than 30 minutes is taken over). An output directory that contains the
source tree, or lies inside a layer or the template directory, is refused: stale files are deleted
there, and that would be source code.

---

## Variables

Our sources may contain placeholders:

```markdown
Repository: {{@url}}
```

Values come from a variables file, one `name = value` per line. The value runs to the end of the
line, without quotes. `#` is a comment only at the start of a line, because values are often URLs.

```
# lm.e
url = https://github.com/axfor/XSDD

# inner.e
url = https://git.inner.example/xsdd
```

```
lm build               uses lm.e next to loom.om, if it exists
lm build -e inner.e    uses inner.e only
```

- `-e` reads **only** that file and never falls back to `lm.e`. A variable missing from `inner.e`
  would otherwise quietly build the public URL into the internal version. A variable that is used
  but not defined is an error, reported at `file:line:col`.
- Defined but unused variables are a warning.
- `{{@@name}}` produces the literal text `{{@name}}`.
- Only our layer is expanded: our files, and literals in templates. Upstream text is never changed.
- Binary files (a NUL byte in the first 8000 bytes) are not expanded.

---

## Anchor completion

Our file has a section that no template statement mentions. It would not be in the product, and
nothing would say so. `lm build` places it next to its neighbour in our file and writes the change
back into the template.

It works for **markdown sections and shell functions** — the two kinds the build already accounts
for by name, and so the two whose place it can work out. Elsewhere order carries no meaning (a toml
key's neighbour says nothing about where a new key belongs), and inferring one would be inventing
an order upstream never had; a key of ours that the product does not have is reported instead.

- after the nearest preceding section that the template does place, in the same statement
  (`base.Install.after("Install XSDD")` becomes `base.Install.after("Install XSDD", "Proxy settings")`);
- or, if it comes first in our file, before the nearest following one.

If the name is not unique in our file, a section path is written (`self."Example 2"."Phase 1"`).
When a completed call gets a section path, or would pass 100 characters on one line, the whole call is
written as a block with one argument per line:

```
base."Example 2".after{
    "Example 2 notes"
    self."Example 2 notes"."Phase 1"
}
```

If there is no neighbour to follow, or the neighbour is placed by `replace`, the build fails: Loom does
not guess. `lm check` reports missing anchors as errors and writes nothing. Where *nothing* of ours
can follow anything of upstream's — our headings are a translation, say — the `body` setting answers
that once for the tree.

---

## The build report

```
lm build → ../dist  (674 files · 3 written · 0 removed · variables from lm.e)

extended            66 files   upstream kept whole, ours inserted
  skills/testing/SKILL.md                      +3: frontmatter / description (bilingual) / Where this skill sits
  ... and 65 more
overridden          13 files   ours replaces part or all of upstream
  hooks/session-start.sh                       whole file · the two scripts would run twice
  hooks/sdd-cache-post.sh                      merge · functions: kept 2 · changed 1 (dbg) · added 1 (main)
dropped             15 places  left out on purpose
  README.md § How it compares                  compares upstream with other projects; does not apply here
added              547 files   only in our layer
not taken          100 files   in upstream, not listed in take
  evals/...
lost                 0 places  ← must be 0, otherwise the build fails
guaranteed          98% of upstream  proved unchanged; 4.1 kB is inside what we wrote
anchors completed   17 places  written back to templates; review them with your commit
  docs/cursor-setup.md.lm                      "Option 1: project skills" placed after "Setup" (line 10)
variables            1         url
```

The last line is the one the language exists for. **Trust is a quantity, not a category**: every
byte of upstream outside what we wrote is compared and proved unchanged, so the share that is inside
what we wrote is exactly the share nobody verified. An insert-only tree reads 100%. A `drop` or a
`replace` takes its node out of the count, and the file says so on its own line — `0% of upstream
kept · whole file`. Nothing is forbidden; it is priced.

`lm check` prints the same report with `lm check` as its first line and nothing written.

`lm build -report report.md` also writes the complete report as markdown.

`lm check` runs the same checks without writing. With `-o dir` it then compares that output directory
with what the build would write: files missing, files the sources no longer produce, content that
differs (stale, or edited by hand), a lost or extra executable bit, a link pointing elsewhere. It
fails when anything differs, so "the product is up to date with its sources" is one command.

---

## Install

A release carries `lm` for each platform and the VS Code extension:

```
curl -fsSLO https://github.com/axfor/loom/releases/latest/download/lm_<version>_<os>_<arch>.tar.gz
tar -xzf lm_*.tar.gz && sudo mv lm_*/lm /usr/local/bin/
```

`os` is darwin, linux or windows and `arch` is arm64 or amd64 (windows ships amd64 as a `.zip`).
`SHA256SUMS` in the same release covers every file. With Go at hand,
`go install github.com/axfor/loom/cmd/lm@latest` does the same thing from source.

Once installed, `lm update` keeps it current: it asks GitHub for the latest release, takes the
archive for this platform, **checks it against that release's `SHA256SUMS`** and puts the binary
where the running one is. An archive that does not match is refused rather than installed — a self
updater that runs whatever it was handed is a way to run someone else's code as you. `lm update
-check` only says what is available. Where lm sits in a directory you do not own (`/usr/local/bin`),
it says so and what to do about it.

The extension is the `loom-lang-<version>.vsix` of the same release: in VS Code, Extensions →
`…` → Install from VSIX.

Building a release is one command, and it publishes nothing:

```
make release V=v1.1.0     lm for five platforms + the extension + SHA256SUMS, into dist/
git tag v1.1.0 && git push origin v1.1.0    publishes it (the workflow builds and attaches dist/)
```

---

## Commands

```
lm build [-e vars] [-o dir] [-report file]   build the whole tree
lm check [-e vars] [-o dir]                  every check of build; with -o, also output files that differ; writes nothing
lm sync <dir>                                take a new upstream release; merge it into merge templates' files
lm weave [-e vars] [-stdin] <template.lm>    weave one template to stdout; -stdin reads the template text from stdin
lm list [-tsv]                               template metadata as JSON (or six TSV columns) for other tools
lm anchors                                   where each anchor currently resolves upstream
lm uses [path-fragment]                      the other direction: what names each piece of upstream
lm view                                      write upstream files annotated with their anchors
lm update [-check]                           replace this lm with the latest release; -check only says what is available
lm version                                   the version, platform and Go version
```

`lm` finds `loom.om` by walking up from the current directory.

`lm list` resolves identifier forms and section paths to real names, so a tool reading it can compare
anchors with upstream headings directly.

`lm uses` turns that round. Every other view answers "where does this anchor point"; this one
answers **"what points here"** — which is the question asked before touching anything:

```
docs/getting-started.md
  How Skills Work          ← mine/docs/getting-started.lm:3:22
  Quick Start (Any Agent)  ← mine/docs/getting-started.lm:4:32
skills/testing/SKILL.md
  Overview                 ← mine/skills/testing/SKILL.lm:5:15, mine/skills/other.lm:2:20
```

What breaks if this section moves, what depends on this file, which upstream nodes nothing names at
all. Nothing is computed that the build did not already compute — resolving anchors is most of what
a weave does, and the answer used to be thrown away.

---

## Editor

`editors/vscode` is a VS Code extension: syntax highlighting for templates, settings, variables
files and `{{@name}}` placeholders; the compiler's own errors underlined as you type; completion of
nodes, methods, our sections and import paths; a live preview of the product a template builds; a
unified diff of that product against upstream, with the edits a `base.merge(self)` file carries
marked as such; and go to definition from a template to the upstream or our file and section it
names. `make vs` runs its tests and packages it.

## License

Apache-2.0.
