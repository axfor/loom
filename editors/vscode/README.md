# Loom for VS Code

Syntax highlighting and go to definition for the Loom language. For the syntax, see the [Loom README](../../README.md#reference).

## Highlighting

| File | What gets highlighted |
|---|---|
| `*.lm` (templates and the `loom.lm` settings file) | The objects `base` / `self` / imported names, nodes, methods, `as(type)`, node kinds, `reason:`, strings and backtick literals, `//` comments, settings; misspelled methods, argument names and words at the start of a line are marked red |
| `*.e` (variables files) | `name = value`, `#` comments at the start of a line; malformed lines are marked red |
| `{{@name}}` in sources | Highlighted in markdown / shell / json / toml / yaml / js / ts / python / go / html / css and more; `{{@@name}}` (a literal) is marked differently |

Also: `//` comment toggling, auto-closing brackets / quotes / backticks, and auto-indent after `{` and `(`.

## Go to definition

In a template, Cmd-click a name (Ctrl-click on Windows / Linux), or press F12:

| Cursor on | Jumps to |
|---|---|
| The path or `cmd` in `import cmd "/.claude/commands/ship"` | That file in our layer (the extension may be omitted; same rules as the compiler) |
| `base` / `self` / an imported name | The file it stands for: `base` is the upstream file at the same path (or wherever `import base "..."` points it), `self` is ours |
| `base.Install`, `base."How it compares"`, `base.Usage_Tips` | That section of the upstream file (`_` matches a space) |
| A method such as `.after` / `.drop` | The node it changes |
| `"Install XSDD"` inside a method | That section of our file |
| `self.frontmatter`, `cmd.body` | Where that part starts |
| The key in `join("description")` | That key in our file |
| The file name in `patch("x.diff")` | The patch file next to the template |
| `marker("...")`, `key("...")`, `function("...")` | The matching comment banner, key or function |

When a name is not found in the file, the jump lands at the top of the file. The text of a `reason:` and the words inside a literal do not jump.

## Packaging and installing

From the root of the Loom repository:

```
make vs
```

This runs the tests first and packages only if they pass, producing `editors/vscode/loom-lang-<version>.vsix`.

Install it into VS Code in either of these ways:

- In the Extensions view, open the `...` menu at the top right, choose **Install from VSIX...**, and pick the file above
- From the command line: `code --install-extension editors/vscode/loom-lang-0.2.0.vsix`

To skip packaging while developing, install the directory directly: run **Developer: Install Extension from Location...** from the Command Palette and choose `editors/vscode`.

Open the files in `examples/` to see it in action.

`.e` is also the extension of the Eiffel language; if you have an Eiffel extension installed, switch the language mode to **Loom Variables** from the status bar at the bottom right.

## Tests

```
npm test        # inside editors/vscode; make vs runs it first
```

- `test/tokenize.js`: tokenizes the samples with VS Code's own tokenizer (vscode-textmate) and checks the scope each token lands in
- `test/definition.js`: builds a real repository on disk, puts the cursor on names in templates, and checks which file and line each jump lands on

The go-to-definition logic lives in `lib/definition.js` and does not depend on VS Code; `extension.js` only wires it into the editor.
