// File nesting: which explorer settings the extension writes in a Loom workspace, and the patterns it
// contributes for templates.
'use strict';

const fs = require('fs');
const path = require('path');
const { nestingUpdates } = require('../lib/nesting');

let pass = 0;
let fail = 0;
const ok = (cond, what, got) => {
  if (cond) pass++;
  else {
    fail++;
    console.log(`  ❌ ${what}`, got === undefined ? '' : JSON.stringify(got));
  }
};

const unset = { key: 'x', defaultValue: false };
const updates = (enabled, expand) => JSON.stringify(nestingUpdates(enabled, expand));

ok(updates(unset, { ...unset, defaultValue: true }) === '[["fileNesting.enabled",true],["fileNesting.expand",false]]',
  'nothing chosen: nesting on, folded', updates(unset, unset));
ok(updates({ ...unset, globalValue: false }, unset) === '[["fileNesting.expand",false]]',
  'nesting turned off in user settings stays off', updates({ ...unset, globalValue: false }, unset));
ok(updates({ ...unset, workspaceValue: true }, { ...unset, workspaceValue: true }) === '[]',
  'values already in the workspace are left alone (the second activation writes nothing)');
ok(updates(unset, { ...unset, workspaceFolderValue: true }) === '[["fileNesting.enabled",true]]',
  'a folder-level choice counts as chosen');

// The patterns cover both template names: SKILL.md.lm and SKILL.lm fold under SKILL.md.
const pkg = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'package.json'), 'utf8'));
const children = (pkg.contributes.configurationDefaults['explorer.fileNesting.patterns']['*'] || '').split(',').map((p) => p.trim());
ok(['${capture}.lm', '${basename}.lm'].every((p) => children.includes(p)),
  'nesting patterns for full and short template names', children);
ok(pkg.activationEvents.includes('workspaceContains:**/loom.lm'), 'the extension starts in a workspace with a loom.lm');

console.log(`loom nesting: ${pass} passed, ${fail} failed`);
process.exit(fail === 0 ? 0 : 1);
