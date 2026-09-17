'use strict';

// File nesting for Loom workspaces: a template (X.lm, or the short name) folds under the file it
// builds, so the explorer shows one entry per file.
//
// The extension contributes the nesting patterns as defaults. Nesting itself is off in VS Code until
// someone turns it on, so in a workspace with a loom.lm the extension turns it on, folded — unless a
// value was already chosen at any level: a person's choice wins over ours.

// nestingUpdates takes inspect() of explorer.fileNesting.enabled and .expand and returns the
// [key, value] pairs to write to the workspace settings.
function nestingUpdates(enabled, expand) {
  const chosen = (i) => i && (i.globalValue !== undefined || i.workspaceValue !== undefined || i.workspaceFolderValue !== undefined);
  const out = [];
  if (!chosen(enabled)) out.push(['fileNesting.enabled', true]);
  if (!chosen(expand)) out.push(['fileNesting.expand', false]);
  return out;
}

module.exports = { nestingUpdates };
