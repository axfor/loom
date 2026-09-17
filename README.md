# Loom

**You forked an upstream project and still want to follow it.** Loom is a small language for that.

The warp is upstream: kept byte for byte, never cut. The weft is your layer: threaded through the
warp. The product is the woven cloth, and the two stay separate: **pull the weft out and the warp is
still there.**

```
// mine/docs/setup.lm
base.Install.after("Install behind a proxy")
base."How it compares".drop(reason: "compares upstream with other projects; not ours")
base.append("Troubleshooting")
```

```
lm build    build the whole tree and print the build report
lm check    the same checks, writing nothing; -o dir also reports out-of-date output
lm sync     take a new upstream release, carrying our edits onto it
```

## What it solves

Keep upstream content and your changes mixed in one file, and when upstream releases **you cannot
tell what got lost**: two rewritten texts have nothing to compare against. This is not theoretical.
One upstream sync dropped 10 changes; seven mechanical checks were tried in a row and reported 200 /
181 / 34 / 0 (false green) / 13 / 25 "missing" items. Every sample read turned out to be a false
alarm, and only reading everything found the 10. That reading cost comes back with every release.

With the layers apart, "did upstream content get lost" becomes a byte comparison, and Loom makes it
at every build. That whole class of bug cannot happen, instead of being avoided by being careful.

## Insert by name, not by line

A diff can change anything, but when upstream changes the lines next to yours it no longer fits,
and someone has to redo it. Loom anchors on upstream's own **names**:
markdown headings, shell functions and banner comments, toml keys, json paths. Upstream adds a
paragraph nearby and your content still lands in the right place, **and the upstream change flows
into the product**. With a busy upstream, that is the difference between a human stepping in every
release or not.

When an anchor is not found, or matches more than once, the build stops. Loom does not guess, take
the first match, or fit anything approximately: a product woven in the wrong place looks exactly like a
correct one, and that is the most expensive way to fail.

## Reference

- [Layout](#layout)
- [Settings: loom.lm](#settings-loomlm)
- [Templates](#templates)
- [Objects](#objects)
- [Nodes](#nodes)
- [Methods](#methods)
- [What lm build does](#what-lm-build-does)
- [Variables](#variables)
- [Anchor completion](#anchor-completion)
- [The build report](#the-build-report)
- [Commands](#commands)
- [Editor](#editor)

---

## Layout

```
loom.lm                           settings
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

## Settings: loom.lm

One setting per line. `//` starts a comment.

```
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

// json registries: merge entries by an identity pulled out with a regex
registry  "hooks" "hooks/([A-Za-z0-9._-]+\.(?:sh|js|py))"
```

| Setting | Meaning |
|---|---|
| `base` | upstream directory (required); in a template, `base` is the file at the product's path in it |
| `self` | our layer's directory; in a template, `self` is the file at the product's path in it |
| `templates` | template directory; without it, templates live next to our files in `self` |
| `output` | default output directory of `lm build` |
| `mark` | begin and end marks for one file type; may appear once per type |
| `take` | upstream paths that go into the product; `*`, `?`, `[...]` match within a path segment, `**` matches any number of directories; may repeat |
| `mirror` | after building, copy every product file under the first directory to the second; may repeat |
| `manifest` | write a list of all product files to this path in the output directory |
| `registry` | merge rule for json registries: group name and identity regex |

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
| type | taken from the extension: `.md` markdown, `.toml`, `.json`, `.sh` shell, anything else text |

---

## Nodes

A node is a part of a file. A bare name means the default node kind of the file type:

| Type | Default node | Other nodes |
|---|---|---|
| markdown | section: a heading down to the next heading | `section("...")`, `line("...")`, `frontmatter`, a frontmatter key `frontmatter.description` (quoted when it has a `-`: `frontmatter."argument-hint"`), `body` (everything after the frontmatter) |
| shell | function | `function("...")`, `marker("...")` (a banner comment `# ── Name ──` down to the next banner, matched by prefix), `line("...")` |
| toml | key | `key("...")` |
| json | key (dotted path) | `key("a.b")` |
| text | line | `line("...")` |

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
| `set` | `frontmatter` | take our whole frontmatter: `base.frontmatter.set(self.frontmatter)` | `self.frontmatter` |
| `set` | key value | the value becomes ours: `base.frontmatter."argument-hint".set(self.frontmatter."argument-hint")`, `base.description.set(self.description)`; adds a frontmatter key upstream does not have | one value |
| `merge` | `base` | our file is upstream plus our edits: it is the product, and `lm sync` carries the edits onto each new upstream ([Following upstream](#following-upstream)) | `self` |
| `as` | toml / json key | view the key's value as another type; methods follow | a type name |

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

### Reasons

`replace` and `drop` change upstream content, so they need `reason:`. Other methods may not have one.
Reasons show up in the build report.

### Views

A toml key whose value is really markdown:

```
import cmd "/.claude/commands/ship"
base.prompt.as(markdown).append(cmd.body)
base.prompt.as(markdown).Steps.after("Our step")
```

Inside a view, a bare string names a section of the same key's value in our file.

### Combinations

- A **whole-file replace** can only be combined with `drop`. Nothing else could take effect.
- A **merge** can only be combined with `drop`. Our file is the product.
- In those two cases `drop` is a declaration: *this upstream section is intentionally not in the
  product*. The build verifies the section really is absent.

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

Take upstream through `lm sync`: a copy made some other way leaves no old upstream to merge from, and
our edits would be kept but upstream's change to those files would not come in.

---

## What lm build does

1. Loads every template. Every error found is reported, not just the first (at most 20 are shown).
2. Completes anchors (below) and writes them back into templates.
3. Weaves each template and checks it:
   - **lost content**: for markdown sections and shell functions, every upstream node must still be
     in the product (marks stripped) or be accounted for by a `drop` / `replace` with a reason.
     Otherwise the build fails. A whole-file replace with a reason accounts for the whole file.
   - **dropping a section with subsections** requires dropping (or otherwise accounting for) each
     subsection, since a section stops at the next heading.
   - **insert-only markdown templates**: with our marks stripped, the body must be byte-identical to
     upstream.
   - **our frontmatter**: every key of our file's frontmatter must be in the product — taken with
     `set`, or put next to upstream's value with `start` / `append` — or the build fails.
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
lm build               uses lm.e next to loom.lm, if it exists
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
back into the template:

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
not guess. `lm check` reports missing anchors as errors and writes nothing.

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
anchors completed   17 places  written back to templates; review them with your commit
  docs/cursor-setup.md.lm                      "Option 1: project skills" placed after "Setup" (line 10)
variables            1         url
```

`lm check` prints the same report with `lm check` as its first line and nothing written.

`lm build -report report.md` also writes the complete report as markdown.

`lm check` runs the same checks without writing. With `-o dir` it then compares that output directory
with what the build would write: files missing, files the sources no longer produce, content that
differs (stale, or edited by hand), a lost or extra executable bit, a link pointing elsewhere. It
fails when anything differs, so "the product is up to date with its sources" is one command.

---

## Commands

```
lm build [-e vars] [-o dir] [-report file]   build the whole tree
lm check [-e vars] [-o dir]                  every check of build; with -o, also output files that differ; writes nothing
lm sync <dir>                                take a new upstream release; merge it into merge templates' files
lm weave [-e vars] [-stdin] <template.lm>    weave one template to stdout; -stdin reads the template text from stdin
lm list [-tsv]                               template metadata as JSON (or six TSV columns) for other tools
lm anchors                                   where each anchor currently resolves upstream
lm view                                      write upstream files annotated with their anchors
```

`lm` finds `loom.lm` by walking up from the current directory.

`lm list` resolves identifier forms and section paths to real names, so a tool reading it can compare
anchors with upstream headings directly.

---

## Editor

`editors/vscode` is a VS Code extension: syntax highlighting for templates, settings, variables
files and `{{@name}}` placeholders; completion of nodes, methods, our sections and import paths;
a live preview of the product a template builds; and go to definition from a template to the upstream or our file and section it names. `make vs`
runs its tests and packages it.

## Install

```
go install github.com/axfor/loom/cmd/lm@latest
```

## License

Apache-2.0.
