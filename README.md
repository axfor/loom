# Loom

**You forked an upstream project and still want to follow it.** Loom is a small language for that.

The warp is upstream: kept byte for byte, never cut. The weft is your layer: threaded through the
warp. The product is the woven cloth, and the two stay separate: **pull the weft out and the warp is
still there.**

```
// templates/docs/setup.md.lm
base.Install.after("Install behind a proxy")
base."How it compares".drop(reason: "compares upstream with other projects; not ours")
base.append("Troubleshooting")
```

```
lm build    build the whole tree and print the build report
lm check    the same checks, writing nothing
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

A patch can insert anywhere, but when upstream changes the line next to your insertion point the
patch no longer applies, and someone has to redo it. Loom anchors on upstream's own **names**:
markdown headings, shell functions and banner comments, toml keys, json paths. Upstream adds a
paragraph nearby and your content still lands in the right place, **and the upstream change flows
into the product**. With a busy upstream, that is the difference between a human stepping in every
release or not.

When an anchor is not found, or matches more than once, the build stops. Loom does not guess, take
the first match, or apply a fuzzy patch: a product woven in the wrong place looks exactly like a
correct one, and that is the most expensive way to fail.

## What a build checks

- **Lost upstream content is a build error.** Every upstream section (markdown heading, shell
  function) must be in the product or be accounted for with `drop` / `replace` and a reason. A file
  of yours that silently replaces an upstream file is an error too.
- **Insert-only templates leave upstream untouched**: with your marks stripped, the body is compared
  with upstream byte for byte.
- **Sections nobody placed**: a section of yours that no template mentions is placed next to its
  neighbour, and the statement is written back into the template (anchor completion).
- **Variables**: `{{@url}}` in your sources takes its value from `lm.e`, or from the file passed with
  `-e`, so the same sources build per environment. An undefined variable is an error; there is no
  fallback between files.

Every build ends with a report: what was extended, overridden, dropped, added only by you, left out
from upstream, and what was lost (which must be 0).

## A repository

```
loom.lm              settings
lm.e                 variables (optional)
upstream/            the upstream project, untouched
mine/                your layer, laid out like the product
templates/           <product path>.lm for each woven file
```

```
// loom.lm
up        "upstream"
me        "mine"
templates "templates"
output    "dist"
mark      markdown "<!-- MINE:BEGIN -->" "<!-- MINE:END -->"
take      "references/**" "LICENSE"
```

A product file comes from a template if there is one, otherwise from your layer, otherwise from
upstream when `take` lists it.

## Templates

```
import ship "/.claude/commands/ship"

base.frontmatter.set(self.frontmatter)        // frontmatter from our file
base.frontmatter.join("description")         // description = ours + upstream's
base.Overview.after("Where this fits")       // our section after upstream's Overview
base."Example 2"."Phase 1".after("Notes")    // a section path when a name repeats
base.start(`> Generated — edit mine/ instead`)
base.prompt.as(markdown).append(ship.body)   // a toml key holding markdown
base.dbg.replace(self.dbg, reason: "upstream's dbg writes to stdout")
base.replace(self, reason: "the two scripts would run twice")
```

- `base` is the upstream file at the product's path, and the only thing a template changes.
  `self` is your file at the same path. Other files come in with `import`.
- A node is written as a string (`base."How it compares"`) or an identifier, where `_` matches a
  space (`base.How_it_compares`).
- Every method accepts `(...)` on one line or `{ }` with one argument per line.
- `replace` and `drop` need `reason:`; reasons appear in the build report.
- Statement order matters: two insertions at the same anchor keep the order they are written in.

The full reference is [docs/language.md](docs/language.md).

## Commands

```
lm build [-e vars] [-o dir] [-report file]   build the whole tree into the output directory
lm check [-e vars]                           every check of build; writes nothing
lm weave [-e vars] <template.lm>             weave one template to stdout
lm list [-tsv]                               template metadata for other tools
lm anchors                                   where each anchor resolves upstream right now
lm view                                      upstream files annotated with their anchors
lm migrate [-n]                              rewrite legacy HCL settings and templates
```

## Editor

`editors/vscode` is a VS Code extension: syntax highlighting for templates, settings, variables
files and `{{@name}}` placeholders, and go to definition from a template to the upstream or our
file and section it names. `make vs` runs its tests and packages it.

## Install

```
go install github.com/axfor/loom/cmd/lm@latest
```

## License

Apache-2.0.
