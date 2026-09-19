'use strict';

// Editor glue only: resolving, completion and weaving live in lib/, where they are tested without
// VS Code.
const fs = require('fs');
const vscode = require('vscode');
const { definition } = require('./lib/definition');
const { completions } = require('./lib/completion');
const { findLm, productName, upstreamFile, weave } = require('./lib/preview');
const { hover } = require('./lib/hover');
const { commandFor, diagnose } = require('./lib/diagnostics');
const { filePatch, treePatch, treeRoot } = require('./lib/patch');
const { signature } = require('./lib/signature');

// The hover shown while Cmd/Ctrl is held previews the target range when it spans fewer than
// 8 lines; a longer range falls back to one line of context.
const PREVIEW_LINES = 7;

const KINDS = {
  section: vscode.CompletionItemKind.Reference,
  node: vscode.CompletionItemKind.Field,
  part: vscode.CompletionItemKind.Property,
  kind: vscode.CompletionItemKind.Function,
  method: vscode.CompletionItemKind.Method,
  type: vscode.CompletionItemKind.TypeParameter,
  object: vscode.CompletionItemKind.Variable,
  keyword: vscode.CompletionItemKind.Keyword,
  argument: vscode.CompletionItemKind.Property,
  file: vscode.CompletionItemKind.File,
  folder: vscode.CompletionItemKind.Folder,
};

// Diagnostics are re-run a moment after the last keystroke, as the preview is re-woven.
const LINT_DEBOUNCE = 400;

const SEVERITY = {
  error: vscode.DiagnosticSeverity.Error,
  warning: vscode.DiagnosticSeverity.Warning,
};

// Where a release can be had. Every view here is the compiler's own output, so without lm there is
// nothing any of them can show.
const RELEASES = 'https://github.com/axfor/loom/releases/latest';

// Said once, not once per keystroke: diagnostics re-run on every save, and one notification per run
// would be a wall of them. Reset when loom.path changes, so a fix is noticed.
let toldAboutLm = false;

// lmPath resolves the compiler and, the first time it is missing, says so where it will be seen.
// A document that reads "lm is not installed" is easy to miss behind the editor that asked for it,
// and a Loom repository is usually opened before lm has ever been installed.
function lmPath() {
  const lm = findLm(vscode.workspace.getConfiguration('loom').get('path'));
  if (lm || toldAboutLm) return lm;
  toldAboutLm = true;
  const download = 'Download the latest release';
  const configure = 'Set loom.path';
  vscode.window.showErrorMessage(
    'Loom: lm is not installed. Install the latest release — the preview, the patch and the errors underlined as you type are all woven by the compiler itself.',
    download, configure,
  ).then((choice) => {
    if (choice === download) vscode.env.openExternal(vscode.Uri.parse(RELEASES));
    else if (choice === configure) vscode.commands.executeCommand('workbench.action.openSettings', 'loom.path');
  });
  return null;
}

const PREVIEW = 'loom-preview';
const PATCH = 'loom-patch';

// Previews are read-only documents of their own scheme: the path names the product (so it gets that
// file's highlighting), the query holds the template. The content is woven by lm from the template
// as it is in the editor, and woven again as the template or any file changes.
class Previews {
  constructor() {
    this.changed = new vscode.EventEmitter();
    this.onDidChange = this.changed.event;
    this.open = new Map(); // preview uri → template path
    this.timers = new Map();
  }

  uriFor(template) {
    return vscode.Uri.from({ scheme: PREVIEW, path: `/Preview · ${productName(template)}`, query: template });
  }

  async provideTextDocumentContent(uri) {
    const template = uri.query;
    this.open.set(uri.toString(), template);
    const doc = vscode.workspace.textDocuments.find((d) => d.uri.scheme === 'file' && d.uri.fsPath === template);
    let text;
    try {
      text = doc ? doc.getText() : fs.readFileSync(template, 'utf8');
    } catch (err) {
      return `⛔ ${err.message}\n`;
    }
    const r = await weave(lmPath(), template, text);
    return r.error !== undefined ? `⛔ ${r.error}\n` : r.product;
  }

  // refresh weaves open previews again, a moment after the last change, for templates matching keep.
  refresh(keep = () => true) {
    for (const [key, template] of this.open) {
      if (!keep(template)) continue;
      clearTimeout(this.timers.get(key));
      this.timers.set(key, setTimeout(() => this.changed.fire(vscode.Uri.parse(key)), 300));
    }
  }
}

// A patch is a read-only document holding a unified diff of upstream against the product. The
// button shows the template in front of you, as the preview and the review do; the whole tree is
// the same thing over every template, and the authority says which of the two a document is. The
// path ends in .diff so VS Code colours it.
//
// Both are built from the files on disk — unlike the preview, which follows the editor's text. lm
// weaves from a saved template here, so a patch of an unsaved buffer would describe a product that
// does not exist yet.
class Patches {
  constructor() {
    this.changed = new vscode.EventEmitter();
    this.onDidChange = this.changed.event;
    this.open = new Set();
    this.timer = null;
  }

  uriFor(template) {
    return vscode.Uri.from({ scheme: PATCH, authority: 'file', path: `/Patch · ${productName(template)}.diff`, query: template });
  }

  treeUriFor(from) {
    return vscode.Uri.from({ scheme: PATCH, authority: 'tree', path: '/Loom patch.diff', query: treeRoot(from) || from });
  }

  async provideTextDocumentContent(uri) {
    this.open.add(uri.toString());
    const lm = lmPath();
    const r = uri.authority === 'tree' ? await treePatch(lm, uri.query) : await filePatch(lm, uri.query);
    return r.error !== undefined ? `⛔ ${r.error}\n` : r.text;
  }

  // refresh rebuilds open patches a moment after the last change. Every file in the tree feeds the
  // patch, so there is nothing to match on — any of them changing is a reason to weave again.
  refresh() {
    if (this.open.size === 0) return;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => {
      for (const key of this.open) this.changed.fire(vscode.Uri.parse(key));
    }, 500);
  }
}

// Loom documents are the ones lm can diagnose: templates (loom), the settings file (loom-om) and
// variables (loom-env).
const DIAGNOSED = ['loom', 'loom-om', 'loom-env'];

function isLoom(doc) {
  return doc.uri.scheme === 'file' && DIAGNOSED.includes(doc.languageId);
}

// Linter keeps the squiggles on Loom documents up to date by running lm over them. Runs are
// debounced per document, and each carries a generation so a slow run cannot land on top of a
// newer one and show errors the author has already fixed.
class Linter {
  constructor() {
    this.diagnostics = vscode.languages.createDiagnosticCollection('loom');
    this.timers = new Map();
    this.runs = new Map();
  }

  // typed says the trigger was a keystroke. lm is piped a template's unsaved text, so that one is
  // diagnosed as it is written; loom.om and a .e file it reads from disk, and checking those while
  // the buffer is ahead of the file would squiggle text the author has already changed. They wait
  // for the save.
  schedule(doc, typed = false) {
    if (!isLoom(doc)) return;
    if (typed && !commandFor(doc.uri.fsPath).stdin) return;
    const key = doc.uri.toString();
    clearTimeout(this.timers.get(key));
    this.timers.set(key, setTimeout(() => this.lint(doc), LINT_DEBOUNCE));
  }

  // all re-lints every open Loom document: an upstream or our-layer file changed, and the product
  // it feeds is woven from it, so an error may have appeared or gone anywhere.
  all() {
    for (const doc of vscode.workspace.textDocuments) this.schedule(doc);
  }

  async lint(doc) {
    const key = doc.uri.toString();
    const generation = (this.runs.get(key) || 0) + 1;
    this.runs.set(key, generation);
    const lm = lmPath();
    const found = await diagnose(lm, doc.uri.fsPath, doc.getText());
    if (this.runs.get(key) !== generation) return;
    this.diagnostics.set(doc.uri, found.map((d) => {
      const line = Math.min(Math.max(d.line, 0), Math.max(doc.lineCount - 1, 0));
      const end = doc.lineAt(line).range.end;
      const start = new vscode.Position(line, Math.min(Math.max(d.col, 0), end.character));
      const diagnostic = new vscode.Diagnostic(new vscode.Range(start, end), d.message, SEVERITY[d.severity]);
      diagnostic.source = 'lm';
      return diagnostic;
    }));
  }

  forget(doc) {
    const key = doc.uri.toString();
    clearTimeout(this.timers.get(key));
    this.timers.delete(key);
    this.runs.delete(key);
    this.diagnostics.delete(doc.uri);
  }

  dispose() {
    for (const t of this.timers.values()) clearTimeout(t);
    this.diagnostics.dispose();
  }
}

// patchTarget is the file a patch command was invoked for: the one the menu names, else the one in
// the editor. A tree has to be found above it, or there is nothing to compare with upstream.
// asItem turns what lib/completion.js offers into what VS Code shows. Templates and the settings
// file are different languages with different providers, and both offer the same shape.
function asItem(it) {
  const c = new vscode.CompletionItem(it.label, KINDS[it.kind]);
  c.detail = it.detail;
  if (it.markdown) c.documentation = new vscode.MarkdownString(it.markdown);
  else if (it.documentation) c.documentation = new vscode.MarkdownString().appendCodeblock(it.documentation, it.lang || '');
  if (it.insertText != null) c.insertText = it.snippet ? new vscode.SnippetString(it.insertText) : it.insertText;
  if (it.filterText) c.filterText = it.filterText;
  if (it.sortText) c.sortText = it.sortText;
  c.range = new vscode.Range(it.range.line, it.range.s, it.range.line, it.range.e);
  if (it.retrigger) c.command = { command: 'editor.action.triggerSuggest', title: 'Suggest' };
  return c;
}

function patchTarget(uri) {
  const editor = vscode.window.activeTextEditor;
  const target = uri instanceof vscode.Uri ? uri : editor && editor.document.uri;
  if (!target || target.scheme !== 'file') return null;
  if (!treeRoot(target.fsPath)) {
    vscode.window.showInformationMessage('No loom.om above this file — there is no tree to compare with upstream.');
    return null;
  }
  return target;
}

async function openPatch(uri) {
  const doc = await vscode.workspace.openTextDocument(uri);
  await vscode.window.showTextDocument(doc, { viewColumn: vscode.ViewColumn.Beside, preview: true, preserveFocus: true });
}

function activate(context) {
  const previews = new Previews();
  const patches = new Patches();
  const linter = new Linter();
  const watcher = vscode.workspace.createFileSystemWatcher('**/*');
  // A file on disk changed: the product is woven from upstream and our files, so both the previews
  // and the errors they could raise are out of date.
  const onDisk = () => {
    previews.refresh();
    patches.refresh();
    linter.all();
  };
  context.subscriptions.push(
    vscode.workspace.registerTextDocumentContentProvider(PREVIEW, previews),
    vscode.workspace.registerTextDocumentContentProvider(PATCH, patches),
    vscode.commands.registerCommand('loom.showPreview', async (uri) => {
      const editor = vscode.window.activeTextEditor;
      const target = uri instanceof vscode.Uri ? uri : editor && editor.document.uri;
      if (!target || target.scheme !== 'file' || !target.fsPath.endsWith('.lm')) return;
      const doc = await vscode.workspace.openTextDocument(previews.uriFor(target.fsPath));
      await vscode.window.showTextDocument(doc, { viewColumn: vscode.ViewColumn.Beside, preview: true, preserveFocus: true });
    }),
    // review: upstream on the left, the product on the right, in VS Code's diff editor
    vscode.commands.registerCommand('loom.showReview', async (uri) => {
      const editor = vscode.window.activeTextEditor;
      const target = uri instanceof vscode.Uri ? uri : editor && editor.document.uri;
      if (!target || target.scheme !== 'file' || !target.fsPath.endsWith('.lm')) return;
      const open = vscode.workspace.textDocuments.find((d) => d.uri.fsPath === target.fsPath);
      const upstream = upstreamFile(target.fsPath, open ? open.getText() : fs.readFileSync(target.fsPath, 'utf8'));
      if (!upstream) {
        vscode.window.showInformationMessage(`${productName(target.fsPath)} has no upstream file to compare with; open the preview instead.`);
        return;
      }
      const name = productName(target.fsPath);
      await vscode.commands.executeCommand('vscode.diff', vscode.Uri.file(upstream), previews.uriFor(target.fsPath),
        `${name}: upstream ↔ product`, { viewColumn: vscode.ViewColumn.Beside, preview: true, preserveFocus: true });
    }),
    // patch: this template's upstream file against the product it builds, as a unified diff
    vscode.commands.registerCommand('loom.showPatch', async (uri) => {
      const target = patchTarget(uri);
      if (!target || !target.fsPath.endsWith('.lm')) return;
      await openPatch(patches.uriFor(target.fsPath));
    }),
    // the same over every template of the tree, in one document
    vscode.commands.registerCommand('loom.showTreePatch', async (uri) => {
      const target = patchTarget(uri);
      if (!target) return;
      await openPatch(patches.treeUriFor(target.fsPath));
    }),
    // the template as typed, unsaved; any saved file, since upstream and our files feed the product
    vscode.workspace.onDidChangeTextDocument((e) => {
      if (e.document.uri.scheme !== 'file') return;
      previews.refresh((template) => template === e.document.uri.fsPath);
      linter.schedule(e.document, true);
    }),
    vscode.workspace.onDidOpenTextDocument((d) => linter.schedule(d)),
    // A changed loom.path deserves a fresh look, and a fresh complaint if it is still wrong.
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (!e.affectsConfiguration('loom.path')) return;
      toldAboutLm = false;
      previews.refresh();
      patches.refresh();
      linter.all();
    }),
    vscode.workspace.onDidSaveTextDocument(onDisk),
    watcher.onDidChange(onDisk),
    watcher.onDidCreate(onDisk),
    watcher.onDidDelete(onDisk),
    watcher,
    vscode.workspace.onDidCloseTextDocument((d) => {
      if (d.uri.scheme === PREVIEW) previews.open.delete(d.uri.toString());
      if (d.uri.scheme === PATCH) patches.open.delete(d.uri.toString());
      linter.forget(d);
    }),
    linter,
  );
  linter.all(); // documents already open when the extension starts

  const selector = { language: 'loom' };
  // The settings file is its own language now, and the only thing it shares with a template is that
  // lm can complete and diagnose it: its nodes, methods and imports have no meaning there.
  const settings = { language: 'loom-om' };
  context.subscriptions.push(
    vscode.languages.registerDefinitionProvider(selector, {
      provideDefinition(document, position) {
        const hit = definition(document.uri.fsPath, document.getText(), position.line, position.character);
        if (!hit) return null;
        const end = Math.max(hit.line + 1, Math.min(hit.end, hit.line + PREVIEW_LINES));
        return [{
          // the whole token under the cursor is the link, so a string with spaces reads as one name
          originSelectionRange: new vscode.Range(hit.origin.line, hit.origin.s, hit.origin.line, hit.origin.e),
          targetUri: vscode.Uri.file(hit.file),
          targetRange: new vscode.Range(hit.line, 0, end, 0),
          targetSelectionRange: new vscode.Range(hit.line, hit.s, hit.line, hit.e),
        }];
      },
    }),
    vscode.languages.registerHoverProvider(selector, {
      provideHover(document, position) {
        const h = hover(document.uri.fsPath, document.getText(), position.line, position.character);
        if (!h) return null;
        return new vscode.Hover(new vscode.MarkdownString(h.markdown), new vscode.Range(h.range.line, h.range.s, h.range.line, h.range.e));
      },
    }),
    vscode.languages.registerSignatureHelpProvider(selector, {
      provideSignatureHelp(document, position) {
        const s = signature(document.uri.fsPath, document.getText(), position.line, position.character);
        if (!s) return null;
        const info = new vscode.SignatureInformation(s.label, new vscode.MarkdownString(s.doc));
        // a parameter is marked by its offsets in the label, so a type named twice is still the right one
        let from = 0;
        info.parameters = s.params.map((p) => {
          const at = s.label.indexOf(p.label, from);
          from = at + p.label.length;
          return new vscode.ParameterInformation([at, at + p.label.length], new vscode.MarkdownString(p.doc));
        });
        const help = new vscode.SignatureHelp();
        help.signatures = [info];
        help.activeSignature = 0;
        help.activeParameter = s.active;
        return help;
      },
    }, { triggerCharacters: ['(', '{'], retriggerCharacters: [',', '\n'] }),
    vscode.languages.registerCompletionItemProvider(selector, {
      provideCompletionItems(document, position) {
        return completions(document.uri.fsPath, document.getText(), position.line, position.character).map(asItem);
      },
    }, '.', '"', '/', '('),
    vscode.languages.registerCompletionItemProvider(settings, {
      provideCompletionItems(document, position) {
        return completions(document.uri.fsPath, document.getText(), position.line, position.character).map(asItem);
      },
    }, '"'),
  );
}

function deactivate() {}

module.exports = { activate, deactivate };
