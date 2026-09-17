// File nesting: the extension folds templates under the files they build through configuration
// defaults alone, so nothing is ever written into a workspace.
'use strict';

const fs = require('fs');
const path = require('path');

let pass = 0;
let fail = 0;
const ok = (cond, what, got) => {
  if (cond) pass++;
  else {
    fail++;
    console.log(`  ❌ ${what}`, got === undefined ? '' : JSON.stringify(got));
  }
};

const pkg = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'package.json'), 'utf8'));
const defaults = pkg.contributes.configurationDefaults;
ok(defaults['explorer.fileNesting.enabled'] === true && defaults['explorer.fileNesting.expand'] === false,
  'nesting is on and folded by default', defaults);
// SKILL.md.lm and SKILL.lm both fold under SKILL.md
const children = (defaults['explorer.fileNesting.patterns']['*'] || '').split(',').map((p) => p.trim());
ok(['${capture}.lm', '${basename}.lm'].every((p) => children.includes(p)), 'patterns for full and short template names', children);
const code = fs.readFileSync(path.join(__dirname, '..', 'extension.js'), 'utf8');
ok(!/\.update\(|ConfigurationTarget/.test(code), 'the extension writes no settings');

console.log(`loom nesting: ${pass} passed, ${fail} failed`);
process.exit(fail === 0 ? 0 : 1);
