// Diagnostics: the parsing of lm's output on its own, then the whole path with the real compiler —
// lm is built from this repository and run over a tree that is deliberately broken.
'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');
const { commandFor, diagnose, inTree, isSettings, isVars, lineOf, parse } = require('../lib/diagnostics');

let pass = 0;
let fail = 0;
const ok = (cond, what, got) => {
  if (cond) pass++;
  else {
    fail++;
    console.log(`  ❌ ${what}`, got === undefined ? '' : JSON.stringify(got));
  }
};

function parsing() {
  const file = '/w/me/doc.lm';

  // lm counts lines and columns from 1, the editor from 0.
  let d = parse(file, '', `⛔ ${file}:3:15: drop needs a reason\n`, null);
  ok(d.length === 1 && d[0].line === 2 && d[0].col === 14, 'a position is turned from 1-based to 0-based', d);
  ok(d[0].message === 'drop needs a reason' && d[0].severity === 'error', 'the ⛔ and the position are off the message', d);

  // `template:l:c: content:l:c: message` — the outer position is the file being edited.
  d = parse(file, '', `⛔ ${file}:2:21: /w/me/doc.md:14:7: variable {{@x}} is not defined\n`, null);
  ok(d.length === 1 && d[0].line === 1 && d[0].col === 20, 'a nested position squiggles the template', d);
  ok(d[0].message === '/w/me/doc.md:14:7: variable {{@x}} is not defined', 'the inner position stays in the message', d);

  // An error about another file still has to be seen: the tree does not build.
  d = parse(file, '', '⛔ /w/loom.lm:3:1: unknown setting `bogus`\n', null);
  ok(d.length === 1 && d[0].line === 0 && d[0].col === 0, 'another file’s error goes to the top of this one', d);
  ok(d[0].message.includes('/w/loom.lm:3:1:'), 'and keeps the path, so it says where to look', d);

  // The same fault is reported once against the content file and once against the template.
  d = parse(file, '', `⛔ /w/me/doc.md:1:1: bad\n/w/me/doc.md:1:1: bad\n`, null);
  ok(d.length === 1, 'the same message is not reported twice', d);

  d = parse(file, '', 'lm: something went wrong with no position\n', null);
  ok(d.length === 1 && d[0].line === 0 && d[0].message.startsWith('lm: something'), 'a message with no position is kept', d);

  d = parse(file, '', '', null);
  ok(d.length === 0, 'a clean run has no diagnostics', d);

  // The report names an unused variable but not where it is defined.
  const vars = '# comment\nurl = https://example.com\nunused = nobody\n';
  d = parse('/w/lm.e', 'variables            1         url\n  ⚠ variable unused is defined but never used\n', '', vars);
  ok(d.length === 1 && d[0].line === 2 && d[0].severity === 'warning', 'an unused variable warns on its own line', d);
  ok(lineOf(vars, 'missing') === 0, 'a name the file does not define falls back to the first line');
  ok(lineOf('a.b = 1\nc = 2\n', 'c') === 1, 'a name is matched literally, not as a pattern');

  // Only a variables file is scanned for them: the report is printed for a template run too.
  d = parse(file, '  ⚠ variable unused is defined but never used\n', '', null);
  ok(d.length === 0, 'unused variables are only reported in the file that defines them', d);

  ok(isSettings('/w/loom.lm') && !isSettings('/w/me/doc.lm'), 'loom.lm is the settings file');
  ok(isVars('/w/lm.e') && !isVars('/w/me/doc.lm'), '.e is a variables file');
  ok(commandFor('/w/me/doc.lm').args.join(' ') === 'weave -stdin /w/me/doc.lm', 'a template is woven from stdin', commandFor('/w/me/doc.lm'));
  ok(commandFor('/w/me/doc.lm').stdin === true, 'and is fed the editor’s text');
  ok(commandFor('/w/loom.lm').args.join(' ') === 'check', 'loom.lm is checked');
  ok(commandFor('/w/lm.e').args.join(' ') === 'check -e /w/lm.e', 'a variables file is checked as the only one');
  ok(commandFor('/w/loom.lm').stdin === false && commandFor('/w/lm.e').stdin === false, 'neither is piped: lm reads them from disk');
}

async function compiler() {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'loom-diag-')));
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
  write('up/SKILL.md', '## Overview\n\nup\n');
  write('me/SKILL.md', '## Ours\n\nme\n');
  const tpl = path.join(root, 'me', 'SKILL.lm');
  write('me/SKILL.lm', 'base.Overview.after("Ours")\n');

  let d = await diagnose(lm, tpl, 'base.Overview.after("Ours")\n');
  ok(d.length === 0, 'a template that weaves has no diagnostics', d);

  // The editor's text is what is checked, not the file on disk, which is still correct here.
  d = await diagnose(lm, tpl, 'base.Overview.after("Ours")\nbase.Nowhere.after("Ours")\n');
  ok(d.length === 1 && d[0].line === 1 && d[0].severity === 'error', 'an unsaved edit is diagnosed, on its own line', d);
  ok(d[0].message.includes('Nowhere'), 'with the compiler’s own message', d);

  d = await diagnose(lm, tpl, 'base.Overview.drop()\n');
  ok(d.length === 1 && d[0].line === 0 && d[0].message.includes('reason'), 'a missing reason is diagnosed', d);

  // A variable our content uses but no variables file defines: lm blames the content file and the
  // template that pulls it in, and the template is the one open here.
  write('me/SKILL.md', '## Ours\n\nme {{@nope}}\n');
  write('lm.e', 'url = https://example.com\n');
  d = await diagnose(lm, tpl, 'base.Overview.after("Ours")\n');
  ok(d.length >= 1 && d.some((x) => x.message.includes('{{@nope}}')), 'an undefined variable reaches the template', d);
  write('me/SKILL.md', '## Ours\n\nme\n');

  // loom.lm: a settings error, squiggled where it is written.
  const cfg = path.join(root, 'loom.lm');
  write('loom.lm', 'base "up"\nself "me"\nbogus "x"\n');
  d = await diagnose(lm, cfg, fs.readFileSync(cfg, 'utf8'));
  ok(d.length === 1 && d[0].line === 2 && d[0].message.includes('bogus'), 'an unknown setting is diagnosed on its line', d);
  write('loom.lm', 'base "up"\nself "me"\nmark markdown "<!-- B -->" "<!-- E -->"\n');

  // A variables file: the one variable nothing expands.
  const e = path.join(root, 'lm.e');
  const eText = 'url = https://example.com\nunused = nobody\n';
  write('lm.e', eText);
  d = await diagnose(lm, e, eText);
  ok(d.some((x) => x.severity === 'warning' && x.line === 1 && x.message.includes('unused')), 'an unused variable warns in the .e file', d);

  // A template with no loom.lm above it is not part of a tree: nothing to say, rather than one
  // "found no loom.lm" error on every stray .lm file a repository happens to hold.
  const stray = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'loom-stray-')));
  fs.writeFileSync(path.join(stray, 'sample.lm'), 'base.Overview.after("Ours")\n');
  ok(inTree(tpl) === true, 'a template under a loom.lm is in a tree');
  ok(inTree(path.join(stray, 'sample.lm')) === false, 'a stray template is not');
  ok(inTree(path.join(stray, 'loom.lm')) === true, 'a loom.lm is the root of one, wherever it is');
  d = await diagnose(lm, path.join(stray, 'sample.lm'), 'base.Overview.after("Ours")\n');
  ok(d.length === 0, 'a template outside any tree is left alone', d);
  d = await diagnose(null, path.join(stray, 'sample.lm'), 'base.Overview.after("Ours")\n');
  ok(d.length === 0, 'and is not told to install lm either', d);
  fs.rmSync(stray, { recursive: true, force: true });

  const none = await diagnose(null, tpl, 'base.append("x")\n');
  ok(none.length === 1 && none[0].message.includes('go install'), 'no lm says how to install it', none);

  const gone = await diagnose(path.join(root, 'bin', 'missing-lm'), tpl, 'base.append("x")\n');
  ok(gone.length === 1 && gone[0].message.includes('cannot run'), 'an lm that will not start is a diagnostic, not a crash', gone);

  fs.rmSync(root, { recursive: true, force: true });
}

async function main() {
  parsing();
  await compiler();
  console.log(`loom diagnostics: ${pass} passed, ${fail} failed`);
  process.exit(fail === 0 ? 0 : 1);
}

main();
