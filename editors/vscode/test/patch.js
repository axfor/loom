// Patch: the diff engine checked against git itself — every pair here is diffed both by lib/patch.js
// and by `git diff --no-index`, and the hunks must come out identical. Then the whole-tree patch,
// built with the real compiler over a tree that has a woven template and a merged one.
'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');
const { bar, script, split, treePatch, treeRoot, unified } = require('../lib/patch');

let pass = 0;
let fail = 0;
const ok = (cond, what, got) => {
  if (cond) pass++;
  else {
    fail++;
    console.log(`  ❌ ${what}`, got === undefined ? '' : JSON.stringify(got));
  }
};

// gitHunks runs the reference implementation and returns its patch from the first @@ onwards, which
// is everything but the file labels — those are ours to choose.
function gitHunks(dir, before, after) {
  const a = path.join(dir, 'a.txt');
  const b = path.join(dir, 'b.txt');
  fs.writeFileSync(a, before);
  fs.writeFileSync(b, after);
  let out = '';
  try {
    out = execFileSync('git', ['diff', '--no-index', '--no-color', '-U3', '--', a, b], { encoding: 'utf8' });
  } catch (err) {
    out = err.stdout || ''; // git exits 1 when the files differ, which is the normal case here
  }
  const at = out.indexOf('@@');
  return at < 0 ? '' : strip(out.slice(at));
}

// git appends the nearest preceding line that starts with a letter to a hunk header
// (`@@ -3,4 +3,4 @@ some line`). Plain `diff -u` does not, and neither do we: for markdown it picks
// a line of prose rather than the heading, so it buys nothing here. The comparison drops it.
function strip(patch) {
  return patch.replace(/^(@@ -\S+ \+\S+ @@).*$/gm, '$1');
}

function ourHunks(before, after) {
  const text = unified(before, after, 'A', 'B').text;
  const at = text.indexOf('@@');
  return at < 0 ? '' : text.slice(at);
}

function againstGit() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'loom-patch-git-'));
  const L = (...xs) => xs.join('\n') + '\n';

  const cases = {
    'a section inserted, as a template does': [
      L('## Overview', '', 'up', '', '## Process', 'p'),
      L('## Overview', '', 'up', '', '<!-- B -->', '## Ours', 'mine', '<!-- E -->', '## Process', 'p'),
    ],
    'lines removed': [L('a', 'b', 'c', 'd', 'e'), L('a', 'e')],
    'a line changed': [L('a', 'b', 'c'), L('a', 'B', 'c')],
    'two changes far apart are two hunks': [
      L(...Array.from({ length: 40 }, (_, i) => `line ${i}`)),
      L(...Array.from({ length: 40 }, (_, i) => (i === 2 ? 'CHANGED 2' : i === 30 ? 'CHANGED 30' : `line ${i}`))),
    ],
    'two changes close together are one hunk': [
      L(...Array.from({ length: 20 }, (_, i) => `line ${i}`)),
      L(...Array.from({ length: 20 }, (_, i) => (i === 5 ? 'X' : i === 8 ? 'Y' : `line ${i}`))),
    ],
    'appended at the end': [L('a', 'b'), L('a', 'b', 'c', 'd')],
    'prepended at the start': [L('c', 'd'), L('a', 'b', 'c', 'd')],
    'an empty file gains content': ['', L('a', 'b')],
    'a file is emptied': [L('a', 'b'), ''],
    'no newline at the end of the new file': [L('a', 'b'), 'a\nb'],
    'no newline at the end of the old file': ['a\nb', L('a', 'b')],
    'neither file ends with a newline': ['a\nb', 'a\nB'],
    'a blank line is inserted': [L('a', 'b'), L('a', '', 'b')],
    'everything replaced': [L('a', 'b', 'c'), L('x', 'y', 'z')],
    'repeated lines around the change': [L('x', 'x', 'x', 'a', 'x', 'x', 'x'), L('x', 'x', 'x', 'b', 'x', 'x', 'x')],
  };

  for (const [what, [before, after]] of Object.entries(cases)) {
    const mine = ourHunks(before, after);
    const theirs = gitHunks(dir, before, after);
    ok(mine === theirs, `same as git: ${what}`, mine === theirs ? undefined : { mine, theirs });
  }

  // Pseudo-random pairs, deterministic so a failure can be reproduced: the edit script has to agree
  // with git's on shapes nobody thought to write down.
  let seed = 12345;
  const rnd = () => ((seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff);
  let agreed = 0;
  for (let t = 0; t < 60; t++) {
    const n = 1 + Math.floor(rnd() * 25);
    const a = Array.from({ length: n }, (_, i) => `line ${i} ${Math.floor(rnd() * 3)}`);
    const b = a.filter(() => rnd() > 0.25);
    for (let i = 0; i < b.length; i++) if (rnd() > 0.85) b.splice(i, 0, `new ${Math.floor(rnd() * 100)}`);
    const before = a.join('\n') + '\n';
    const after = b.join('\n') + '\n';
    if (ourHunks(before, after) === gitHunks(dir, before, after)) agreed++;
    else console.log(`  ❌ random pair ${t} differs\n--- mine\n${ourHunks(before, after)}--- git\n${gitHunks(dir, before, after)}`);
  }
  ok(agreed === 60, `all 60 random pairs match git (${agreed}/60)`);

  fs.rmSync(dir, { recursive: true, force: true });
}

function units() {
  ok(unified('same\n', 'same\n', 'A', 'B').text === '', 'identical files produce no patch');
  const r = unified('a\n', 'a\nb\n', 'A', 'B');
  ok(r.added === 1 && r.removed === 0, 'insertions and deletions are counted', r);
  ok(r.text.startsWith('diff --loom A B\n--- A\n+++ B\n'), 'the headers name both sides', r.text);

  ok(split('').lines.length === 0 && split('').noEol === false, 'an empty file has no lines');
  ok(split('a\n').lines.length === 1 && split('a\n').noEol === false, 'a trailing newline adds no phantom line');
  ok(split('a').noEol === true, 'a missing final newline is noticed');

  const s = script(['a', 'b', 'c'], ['a', 'x', 'c']);
  ok(s.map((e) => e.op).join('') === ' -+ ' || s.map((e) => e.op).join('') === ' +- ',
    'a changed line is one out and one in', s.map((e) => e.op + e.text));

  ok(bar(3, 0, 3, 40) === '+++', 'the histogram is all + when nothing was removed');
  ok(bar(0, 2, 2, 40) === '--', 'and all - when nothing was added');
  ok(bar(1, 1, 200, 40) === '+-' || bar(1, 1, 200, 40).length <= 40, 'a wide file is scaled to fit');
}

async function tree() {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'loom-patch-')));
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

  write('loom.lm', 'base "up"\nself "me"\nmark markdown "<!-- B -->" "<!-- E -->"\n');
  write('up/doc.md', '## Overview\n\nup\n\n## Process\n\np\n');
  write('me/doc.md', '## Ours\n\nmine\n');
  write('me/doc.lm', 'base.Overview.after("Ours")\n');
  // A .js file carried with base.merge(self): the product is our file, so the diff is the edits
  // lm sync re-applies to each new upstream — a patch that exists nowhere as a file.
  write('up/run.js', 'function boot() {\n  up();\n}\n');
  write('me/run.js', 'function boot() {\n  up();\n  ours();\n}\n');
  write('me/run.lm', 'base.merge(self)\n');

  ok(treeRoot(path.join(root, 'me', 'doc.lm')) === root, 'the tree root is the loom.lm directory');
  ok(treeRoot(path.join(os.tmpdir(), 'nowhere.lm')) === null, 'a file in no tree has no root');

  const r = await treePatch(lm, path.join(root, 'me', 'doc.lm'));
  ok(!r.error, 'the tree patch is produced', r.error);
  ok(r.files.length === 2, 'both templates appear', r.files && r.files.map((f) => f.target));
  ok(/2 files changed, 6 insertions\(\+\), 0 deletions\(-\)/.test(r.text), 'the summary counts every file', r.text.split('\n')[0]);
  ok(r.text.includes('diff --loom upstream/doc.md product/doc.md'), 'each file has a header naming both sides');
  ok(r.text.includes('+<!-- B -->') && r.text.includes('+## Ours'), 'our inserted content is on + lines');
  ok(r.text.includes('  up()') && !r.text.includes('-  up()'), 'upstream lines it kept are context, not deletions');

  const merged = r.files.find((f) => f.target === 'run.js');
  ok(merged && merged.merge === true, 'a merge template is marked as one', merged);
  ok(/run\.js.*\(merge\)/.test(r.text), 'and the summary says so', r.text.split('\n').find((l) => l.includes('run.js')));
  ok(r.text.includes('lm sync carries onto each new upstream'), 'with a line saying what a merge diff means');
  ok(r.text.includes('+  ours();'), 'the merge diff is the edits we carry');

  // A template that does not weave must not silently drop out of the patch.
  write('me/doc.lm', 'base.Nowhere.after("Ours")\n');
  const broken = await treePatch(lm, path.join(root, 'me', 'doc.lm'));
  ok(broken.text.includes('⛔') && broken.text.includes('Nowhere'), 'a template that fails to weave is reported', broken.text.split('\n').slice(0, 8));
  write('me/doc.lm', 'base.Overview.after("Ours")\n');

  // Nothing of ours: a patch that says so beats an empty document.
  const bare = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'loom-bare-')));
  fs.writeFileSync(path.join(bare, 'loom.lm'), 'base "up"\nself "me"\n');
  fs.mkdirSync(path.join(bare, 'up'));
  fs.mkdirSync(path.join(bare, 'me'));
  const empty = await treePatch(lm, path.join(bare, 'me'));
  ok(empty.text && empty.text.includes('No template changes upstream.'), 'a tree with no templates says so', empty);
  fs.rmSync(bare, { recursive: true, force: true });

  const stray = await treePatch(lm, path.join(os.tmpdir(), 'no-loom-here', 'x.lm'));
  ok(stray.error && stray.error.includes('no loom.lm'), 'a file outside any tree is an error, not a crash', stray);

  const none = await treePatch(null, path.join(root, 'me', 'doc.lm'));
  ok(none.error && none.error.includes('go install'), 'no lm says how to install it', none);

  fs.rmSync(root, { recursive: true, force: true });
}

async function main() {
  units();
  againstGit();
  await tree();
  console.log(`loom patch: ${pass} passed, ${fail} failed`);
  process.exit(fail === 0 ? 0 : 1);
}

main();
