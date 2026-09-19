# Loom for VS Code

Syntax highlighting, diagnostics, completion, a live preview, a patch against upstream and go to definition for the Loom language. For the syntax, see the [Loom README](../../README.md#reference).

## Highlighting

| File | What gets highlighted |
|---|---|
| `*.lm` (templates and the `loom.om` settings file) | The objects `base` / `self` / imported names, nodes, methods, `as(type)`, node kinds, `reason:`, strings and backtick literals, `//` comments, settings; misspelled methods, argument names and words at the start of a line are marked red |
| `*.e` (variables files) | `name = value`, `#` comments at the start of a line; malformed lines are marked red |
| `{{@name}}` in sources | Highlighted in markdown / shell / json / toml / yaml / js / ts / python / go / html / css and more; `{{@@name}}` (a literal) is marked differently |

Also: `//` comment toggling, auto-closing brackets / quotes / backticks, and auto-indent after `{` and `(`.

A quoted name counts as one word wherever the cursor is in it: the go-to-definition link, occurrence highlighting and
completion all take `"离线环境 fallback（无网络 / 内网部署）"` as a whole. (Double-click still selects a single word; that
follows the editor's word separators setting.)

## Templates next to their files

A template lives next to the file it builds, and the explorer folds it under that file — one entry per file:

```
▸ SKILL.md          SKILL.lm
▸ run.sh            run.lm
```

The extension turns this on by itself, through default settings — it writes no file into your workspace:
file nesting on and folded (`explorer.fileNesting.enabled` / `expand`), with patterns that fold `X.lm` and the
short name that leaves the extension out under `X`. Defaults apply in every workspace, so VS Code's own
nesting patterns (lock files under `package.json`, for one) show up elsewhere too; set
`"explorer.fileNesting.enabled": false` in your settings to turn nesting off. It needs VS Code 1.67 or later.

`.lm` and `.e` files show the Loom icon: warp threads (upstream) with the weft (ours) woven through.

## Preview

With a template open, click the preview button at the top right of the editor (or Cmd-K V, Ctrl-K V on Windows /
Linux). The product the template builds opens to the side — `SKILL.lm` shows the woven `SKILL.md`, with our
content between its marks — and follows as you type, saved or not. It is woven again when any file is saved or
changes on disk, since upstream and our files feed the product too. A compile error shows in the preview with its
`file:line:column`.

The review button next to it opens VS Code's diff editor instead: upstream's file on the left (where `import base` points,
when it does) and the woven product on the right, following your edits the same way — what the template changes about
upstream, the way a code review shows it.

The preview runs the compiler itself, `lm weave -stdin`, so what you see is what `lm build` writes. The `lm` used
is the `loom.path` setting, else `lm` on `PATH`, else Go's install directory (`~/go/bin/lm`).

Without lm there is nothing any of these views can show, so the first time it cannot be found the
extension says so in a notification — with the release page and the `loom.path` setting one click
away — rather than only inside the document that asked for it. It is said once, not once per
keystroke, and again if `loom.path` is changed and still wrong. `lm update` keeps an installed one
current.

## Patch

The third button shows the template in front of you as a unified diff — the `+` / `-` form
`git diff` prints — upstream on one side, the product it builds on the other:

```
doc.md · 5 insertions(+), 0 deletions(-)  +++++
upstream → ../dist · woven by lm, nothing written

diff --loom upstream/doc.md ../dist/doc.md
--- upstream/doc.md
+++ ../dist/doc.md
@@ -2,6 +2,11 @@
 
 Upstream overview text.
 
+<!-- MINE:BEGIN -->
+## Where this fits
+
+A section we added.
+<!-- MINE:END -->
 ## Process
```

Both sides are named by `loom.om`, relative to the tree root, the way git writes paths relative to
the repository: the left is the file in the `base` layer, which you can open, and the right is where
`output` says `lm build` writes the product — or the product's own path when no output is set.

Where Review puts the two files side by side, this is the same comparison as a patch: only the
changed lines with their context, compact enough to take in at a glance and plain enough to paste
into a review. **Loom · Show Patch Against Upstream: Whole Tree** in the command palette runs it
over every template of the tree at once, with a `git diff --stat` summary in front.

**A merge template says so, and that is where the view earns its place.** For `base.merge(self)` on
a script or a `.js` file, our file *is* the product: the patch is the edits we carry, the ones
`lm sync` re-applies to each new upstream with a three-way merge. They exist nowhere in the
repository as a file — the difference is the only place they live, and this is where you read it.
For a json registry the product is computed from both layers instead, so its diff is what the merge
produced.

A template that adds nothing to upstream says that, rather than opening an empty document, and so
does one whose product is only ours with no upstream file to compare against.

The patch is built from the files **on disk**, not from the unsaved buffer: lm weaves from the saved
template, so a patch of an unsaved edit would describe a product that does not exist yet. It is
rebuilt when any file is saved or changes on disk.

Templates are woven with `lm weave` — never `lm build`, which completes anchors and writes them back
into the templates. Opening a view must not edit your sources. A template that fails to weave is
reported with its error rather than quietly left out.

## Diagnostics

Errors are underlined where they are, as you type — the compiler's own, not a second opinion. Loom refuses to guess
when an anchor is missing or ambiguous, and this is where it says so before the build does.

| File | Checked with | When |
|---|---|---|
| a template | `lm weave -stdin` | as you type; the editor's text is what is checked, saved or not |
| `loom.om` | `lm check` | on open and on save — the settings are read from disk |
| `*.e` | `lm check -e <file>` | on open and on save; `-e` reads only that file, so the variables being edited are the ones checked |

Every diagnostic carries lm's own message, so a fix comes with it:

```
base.Overview.drop()
              ~~~~~~  drop changes upstream content and needs a reason: drop(reason: "...")

base.Overview.aftr("Ours")
              ~~~~  no method `aftr` — did you mean after?
```

Two details worth knowing:

- **Where a fault is not in the file you are editing** — our content uses `{{@name}}` that no variables file defines,
  say — lm names both the content file and the template that pulls it in. The squiggle goes on the template, and the
  content file's `path:line:column` stays in the message, so it still says where to look. An error about another file
  entirely (a broken `loom.om`) is reported at the top of the file you have open rather than not at all.
- **A `.lm` or `.e` file with no `loom.om` above it is left alone.** It is not part of a tree, so there is nothing to
  build and nothing to say — the extension also highlights Loom syntax in a repository that only documents it.

A variable a variables file defines but nothing expands is a warning, on the line that defines it.

Diagnostics re-run when any file is saved or changes on disk, since upstream and our files feed the weave. They need
the same `lm` as the preview (see below); without it, one diagnostic says how to install it.

## Completion

Suggestions open after `.`, `"`, `/` and `(`, or with Ctrl-Space:

| Where | Offered |
|---|---|
| Start of a line | `base`, `import` (in `loom.om`: the settings) |
| `base.` / `self.` / an imported name followed by `.` | The file's sections (keys, functions), then `frontmatter` / `body`, node kinds such as `section("...")`, and on `base` the methods that apply to the whole file |
| `base.Install.` | The methods that apply to a node: `after` / `before` / `replace` / `drop` |
| `base.frontmatter.` / `base.frontmatter.description.` | The frontmatter's keys and `set` / a key's `set` / `start` / `append` (on a toml key also `as`) |
| `base."Example 2".` | The subsections of that section, then its methods |
| `after(` / `after("` and the other content methods | Our sections, then `self` and imported names; `reason:` in `replace` |
| `drop(` | `reason:` |
| `base.frontmatter.description.start(` | Our keys, the same key first: `self.frontmatter.description` (in toml, `self.description`) |
| `section("` / `function("` / `marker("` / `key("` | That kind of node in the file being changed |
| `as(` | The types |
| `import "/` / `import "./` | Directories and files of our layer (`import base` lists upstream); the extension is left out when no other file has that name |

What a suggestion inserts is always something the compiler resolves to that node:

- A name with spaces or punctuation is inserted as a string; picking it inside a string replaces the whole string, quotes included.
- A name that appears more than once comes with the parent headings that tell it apart: `"Example 1"."Phase 1"` after a dot, `self."例 1"."Phase 1"` as an argument. A name no path can tell apart is not offered.
- Each suggestion shows where the node is (`## Install · mine/README.md:12`) and the first lines of it.
- Picking a method inserts its arguments (`drop(reason: "")`) and opens the next suggestions.

## Hover

Hover a keyword — `after`, `merge`, `drop`, `as`, `section`, `import`, `reason:` and the rest — for its typed signature
(`base.<key>.start(value: Value)`), what it does, what each parameter type accepts, and examples; completion shows the same
next to each suggestion. Keywords do not jump anywhere. Hover `base`, `self` or an imported name to see which file it stands for.

## Signature help

Typing `(` after a method (or a new line in its `{ }` block) shows the signature for what it is called on — `start` on the
file takes content, on a key it takes one value — with the argument being written marked, and what that parameter
accepts. `reason:` is marked as soon as it is typed.

## Go to definition

In a template, Cmd-click a name (Ctrl-click on Windows / Linux), or press F12:

| Cursor on | Jumps to |
|---|---|
| The path or `cmd` in `import cmd "/.claude/commands/ship"` | That file in our layer (the extension may be omitted; same rules as the compiler) |
| `base` / `self` / an imported name | The file it stands for: `base` is the upstream file at the same path (or wherever `import base "..."` points it), `self` is ours |
| `base.Install`, `base."How it compares"`, `base.Usage_Tips` | That section of the upstream file (`_` matches a space) |
| `"Phase 1"` in `base."Example 2"."Phase 1"` | That subsection under that parent |
| `"Install XSDD"` inside a method | That section of our file |
| `self.frontmatter`, `cmd.body` | Where that part starts |
| `description` in `base.frontmatter.description` / `self.frontmatter.description` | That frontmatter key upstream / in our file |
| The name in `marker("...")`, `key("...")`, `function("...")` | The matching comment banner, key or function |

Only objects and names lead to a file; keywords show their help on hover instead. Holding Cmd (Ctrl) over a name underlines the whole token, so a string with spaces is one link, and shows the first lines of
the target. The jump selects the name on the target line.

Names are found with the compiler's rules: headings from `##` down, none inside code fences, a banner by the start of its line
(`marker("# ── Test 3")`). When a name is not found, or matches more than one node and no path says which, the jump lands at
the top of the file rather than guessing. The text of a `reason:` and the words inside a literal do not jump.

## Packaging and installing

From the root of the Loom repository:

```
make vs
```

This runs the tests first and packages only if they pass, producing `editors/vscode/loom-lang-<version>.vsix`.

Install it into VS Code in either of these ways:

- In the Extensions view, open the `...` menu at the top right, choose **Install from VSIX...**, and pick the file above
- From the command line: `code --install-extension editors/vscode/loom-lang-<version>.vsix`

To skip packaging while developing, install the directory directly: run **Developer: Install Extension from Location...** from the Command Palette and choose `editors/vscode`.

Open the files in `examples/` to see it in action.

`.e` is also the extension of the Eiffel language; if you have an Eiffel extension installed, switch the language mode to **Loom Variables** from the status bar at the bottom right.

## Tests

```
npm test        # inside editors/vscode; make vs runs it first
```

- `test/tokenize.js`: tokenizes the samples with VS Code's own tokenizer (vscode-textmate) and checks the scope each token lands in
- `test/signature.js`: the signature picked for each receiver, and which argument is marked, in parentheses and blocks
- `test/hover.js`: every keyword shows usage with examples, objects name their files, names show nothing
- `test/definition.js`: builds a real repository on disk, puts the cursor on names in templates, and checks which file and line each jump lands on, and that a string with spaces is one link
- `test/completion.js`: puts the cursor in templates over a real repository and checks what is offered and the range it replaces; every name it inserts must go to definition back to the node it was offered for
- `test/wordpattern.js`: runs VS Code's own word-finding algorithm over every position of every string and name in long template lines
- `test/preview.js`: builds `lm` from this repository and weaves a template from editor text that differs from the file on disk, including a compile error
- `test/nesting.js`: nesting is on and folded by default, with patterns for both template names, and the extension writes no settings
- `test/patch.js`: diffs 76 file pairs with both the diff engine and `git diff --no-index`, and the hunks must come out identical — 16 written by hand (insertions, deletions, hunks that merge, a missing final newline) and 60 pseudo-random; then builds `lm` and checks both the one-template patch the button shows and the whole-tree one, over a woven template and a merged one
- `test/diagnostics.js`: parses lm's output on its own — positions, nested positions, duplicates, unused variables — then builds `lm` and checks the errors a deliberately broken tree really produces

The logic lives in `lib/` and does not depend on VS Code: `loom.js` reads templates and finds nodes with the compiler's rules,
`definition.js`, `completion.js`, `hover.js`, `signature.js`, `preview.js`, `diagnostics.js` and `patch.js` build on it, with keyword help in `docs.js`. `extension.js` only wires them into the editor.
