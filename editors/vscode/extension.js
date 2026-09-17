'use strict';

// Editor glue only: resolving lives in lib/definition.js and lib/completion.js, where it is
// tested without VS Code.
const vscode = require('vscode');
const { definition } = require('./lib/definition');
const { completions } = require('./lib/completion');

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

function activate(context) {
  const selector = { language: 'loom' };
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
    vscode.languages.registerCompletionItemProvider(selector, {
      provideCompletionItems(document, position) {
        return completions(document.uri.fsPath, document.getText(), position.line, position.character).map((it) => {
          const c = new vscode.CompletionItem(it.label, KINDS[it.kind]);
          c.detail = it.detail;
          if (it.documentation) c.documentation = new vscode.MarkdownString().appendCodeblock(it.documentation, it.lang || '');
          if (it.insertText != null) c.insertText = it.snippet ? new vscode.SnippetString(it.insertText) : it.insertText;
          if (it.filterText) c.filterText = it.filterText;
          if (it.sortText) c.sortText = it.sortText;
          c.range = new vscode.Range(it.range.line, it.range.s, it.range.line, it.range.e);
          if (it.retrigger) c.command = { command: 'editor.action.triggerSuggest', title: 'Suggest' };
          return c;
        });
      },
    }, '.', '"', '/', '('),
  );
}

function deactivate() {}

module.exports = { activate, deactivate };
