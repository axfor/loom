// Preview, checked with the real compiler: lm is built from this repository, and a template is woven
// from editor text that differs from the file on disk.
'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');
const { findLm, productName, upstreamFile, weave } = require('../lib/preview');

let pass = 0;
let fail = 0;
const ok = (cond, what, got) => {
  if (cond) pass++;
  else {
    fail++;
    console.log(`  ❌ ${what}`, got === undefined ? '' : JSON.stringify(got));
  }
};

async function main() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'loom-preview-'));
  const write = (rel, text) => {
    fs.mkdirSync(path.dirname(path.join(root, rel)), { recursive: true });
    fs.writeFileSync(path.join(root, rel), text);
  };
  const lm = path.join(root, 'bin', 'lm');
  try {
    execFileSync('go', ['build', '-o', lm, './cmd/lm'], { cwd: path.join(__dirname, '..', '..', '..'), stdio: 'pipe' });
  } catch (err) {
    console.log(`  ❌ cannot build lm with go: ${err.message}`);
    process.exit(1);
  }

  write('loom.om', 'base "up"\nself "me"\nmark markdown "<!-- B -->" "<!-- E -->"\n');
  write('up/SKILL.md', '## Overview\n\nup\n');
  write('me/SKILL.md', '## Ours\n\nme\n\n## Also ours\n\nmore\n');
  const tpl = path.join(root, 'me', 'SKILL.lm');
  write('me/SKILL.lm', 'base.Overview.after("Ours")\n');

  ok(productName(tpl) === 'SKILL.md', 'the preview is named after the product', productName(tpl));
  ok(upstreamFile(tpl, 'base.Overview.after("Ours")\n') === path.join(root, 'up', 'SKILL.md'), 'review compares with the upstream file');
  write('up/old/NAME.md', '## Overview\n');
  ok(upstreamFile(tpl, 'import base "/old/NAME"\n') === path.join(root, 'up', 'old', 'NAME.md'), 'review follows import base');
  write('me/ours.md', '## Only ours\n');
  ok(upstreamFile(path.join(root, 'me', 'ours.lm'), 'base.append("Only ours")\n') === null, 'no upstream file: nothing to compare');

  // the editor's text wins over the file on disk
  const unsaved = 'base.Overview.after("Ours", "Also ours")\n';
  const r = await weave(lm, tpl, unsaved);
  ok(r.product && r.product.includes('## Ours') && r.product.includes('## Also ours') && r.product.includes('<!-- B -->'),
    'weaves the unsaved text', r);

  const bad = await weave(lm, tpl, 'base.Nowhere.after("Ours")\n');
  ok(bad.error && bad.error.includes('SKILL.lm:1:') && bad.error.includes('Nowhere'), 'a compile error comes back with its position', bad);

  const none = await weave(null, tpl, unsaved);
  ok(none.error && none.error.includes('go install'), 'no lm says how to install it', none);

  // An lm that exits before reading its stdin — one too old for -stdin, or a repository with no
  // loom.om — breaks the pipe while the template is still being written. That must come back as an
  // error, not an uncaught EPIPE that leaves the preview waiting for ever.
  write('bin/gone', '#!/bin/sh\nexit 1\n');
  fs.chmodSync(path.join(root, 'bin', 'gone'), 0o755);
  const gone = await weave(path.join(root, 'bin', 'gone'), tpl, 'base.append("x")\n'.repeat(200000));
  ok(gone.error && gone.error.includes('exited with 1'), 'an lm that never reads stdin is an error', gone);

  ok(findLm('/opt/lm', {}) === '/opt/lm', 'a configured path wins');
  ok(findLm('', { PATH: path.dirname(lm) }) === lm, 'lm on PATH is found');
  ok(findLm('', { PATH: '', GOBIN: path.dirname(lm) }) === lm, "Go's install directory is the fallback");

  fs.rmSync(root, { recursive: true, force: true });
  console.log(`loom preview: ${pass} passed, ${fail} failed`);
  process.exit(fail === 0 ? 0 : 1);
}

main();
