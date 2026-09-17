# Loom for VS Code

Syntax highlighting, completion, a live preview and go to definition for the Loom language. For the syntax, see the [Loom README](../../README.md#reference).

## Highlighting

| File | What gets highlighted |
|---|---|
| `*.lm` (templates and the `loom.lm` settings file) | The objects `base` / `self` / imported names, nodes, methods, `as(type)`, node kinds, `reason:`, strings and backtick literals, `//` comments, settings; misspelled methods, argument names and words at the start of a line are marked red |
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

## Completion

Suggestions open after `.`, `"`, `/` and `(`, or with Ctrl-Space:

| Where | Offered |
|---|---|
| Start of a line | `base`, `import` (in `loom.lm`: the settings) |
| `base.` / `self.` / an imported name followed by `.` | The file's sections (keys, functions), then `frontmatter` / `body`, node kinds such as `section("...")`, and on `base` the methods that apply to the whole file |
| `base.Install.` | The methods that apply to a node: `after` / `before` / `replace` / `drop` (`set` / `join` on frontmatter, `set` / `as` on a key) |
| `base."Example 2".` | The subsections of that section, then its methods |
| `after(` / `after("` and the other content methods | Our sections, then `self` and imported names; `reason:` in `replace` |
| `drop(` | `reason:` |
| `join("` | Our frontmatter keys (or keys) |
| `section("` / `function("` / `marker("` / `key("` | That kind of node in the file being changed |
| `as(` | The types |
| `import "/` / `import "./` | Directories and files of our layer (`import base` lists upstream); the extension is left out when no other file has that name |

What a suggestion inserts is always something the compiler resolves to that node:

- A name with spaces or punctuation is inserted as a string; picking it inside a string replaces the whole string, quotes included.
- A name that appears more than once comes with the parent headings that tell it apart: `"Example 1"."Phase 1"` after a dot, `self."例 1"."Phase 1"` as an argument. A name no path can tell apart is not offered.
- Each suggestion shows where the node is (`## Install · mine/README.md:12`) and the first lines of it.
- Picking a method inserts its arguments (`drop(reason: "")`) and opens the next suggestions.

## Hover

Hover a keyword — `after`, `merge`, `drop`, `as`, `section`, `import`, `reason:` and the rest — for how it is written, what
it does, its arguments and examples; completion shows the same next to each suggestion. Keywords do not jump anywhere.
Hover `base`, `self` or an imported name to see which file it stands for.

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
| The key in `join("description")` | That key in our file |
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
- From the command line: `code --install-extension editors/vscode/loom-lang-0.6.1.vsix`

To skip packaging while developing, install the directory directly: run **Developer: Install Extension from Location...** from the Command Palette and choose `editors/vscode`.

Open the files in `examples/` to see it in action.

`.e` is also the extension of the Eiffel language; if you have an Eiffel extension installed, switch the language mode to **Loom Variables** from the status bar at the bottom right.

## Tests

```
npm test        # inside editors/vscode; make vs runs it first
```

- `test/tokenize.js`: tokenizes the samples with VS Code's own tokenizer (vscode-textmate) and checks the scope each token lands in
- `test/hover.js`: every keyword shows usage with examples, objects name their files, names show nothing
- `test/definition.js`: builds a real repository on disk, puts the cursor on names in templates, and checks which file and line each jump lands on, and that a string with spaces is one link
- `test/completion.js`: puts the cursor in templates over a real repository and checks what is offered and the range it replaces; every name it inserts must go to definition back to the node it was offered for
- `test/wordpattern.js`: runs VS Code's own word-finding algorithm over every position of every string and name in long template lines
- `test/preview.js`: builds `lm` from this repository and weaves a template from editor text that differs from the file on disk, including a compile error
- `test/nesting.js`: nesting is on and folded by default, with patterns for both template names, and the extension writes no settings

The logic lives in `lib/` and does not depend on VS Code: `loom.js` reads templates and finds nodes with the compiler's rules,
`definition.js`, `completion.js` and `preview.js` build on it. `extension.js` only wires them into the editor.
