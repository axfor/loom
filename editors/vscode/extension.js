'use strict';

// Editor glue only: the resolving lives in lib/definition.js, where it is tested without VS Code.
const vscode = require('vscode');
const { definition } = require('./lib/definition');

function activate(context) {
  context.subscriptions.push(
    vscode.languages.registerDefinitionProvider({ language: 'loom' }, {
      provideDefinition(document, position) {
        const hit = definition(document.uri.fsPath, document.getText(), position.line, position.character);
        if (!hit) return null;
        return new vscode.Location(vscode.Uri.file(hit.file), new vscode.Position(hit.line, 0));
      },
    }),
  );
}

function deactivate() {}

module.exports = { activate, deactivate };
