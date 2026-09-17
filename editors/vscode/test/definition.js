// Go to definition, checked against a real repository on disk: each case puts the cursor on
// a word in a template and names the file and line the jump must land on.
'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const { definition } = require('../lib/definition');

const root = fs.mkdtempSync(path.join(os.tmpdir(), 'loom-def-'));
const write = (rel, text) => {
  const p = path.join(root, rel);
  fs.mkdirSync(path.dirname(p), { recursive: true });
  fs.writeFileSync(p, text);
};

write('loom.lm', 'base      "upstream"\nself      "mine"\ntemplates "t"\n');
write('upstream/skills/testing/SKILL.md', [
  '---', 'name: testing', 'description: Upstream.', '---', '',
  '## Overview', '', 'text', '',
  '```', '## Not a heading', '```', '',
  '## How it compares', '', 'x', '',
  '## Usage Tips', '', 'y', '',
].join('\n'));
write('mine/skills/testing/SKILL.md', [
  '---', 'name: testing', 'description: Our description.', '---', '',
  '## Überblick', '', 'a', '',
  '## Customer ≠ User', '', 'b', '',
].join('\n'));
write('mine/shared/notes.md', '# Shared\n\n## Café Notes\n\nc\n');
write('upstream/old/guide.md', '# Guide\n\n## Old Overview\n\nd\n');
write('upstream/cmd.toml', 'description = "Up"\nprompt = """\n## Steps\n\ndo it\n"""\n');
write('mine/cmd.toml', 'description = "Mine"\nprompt = """\n## Naïve Approach\n\ne\n"""\n');
write('mine/.claude/commands/ship.md', '---\ndescription: ship\n---\n\n## Release\n\nf\n');
write('upstream/run.sh', '#!/bin/sh\n# ── main ──\nmain() {\n  echo\n}\n');
write('mine/run.sh', 'boot() {\n  echo boot\n}\n');

const doc = path.join(root, 't/skills/testing/SKILL.md.lm');
const docText = [
  'import notes "/shared/notes"',                                   // 0
  'import base "/old/guide"',                                       // 1
  '',                                                               // 2
  'base.Old_Overview.after("Überblick", notes.Café_Notes)',          // 3
  'base.frontmatter.set(self.frontmatter)',                         // 4
  'base.frontmatter.join("description")',                           // 5
  'base.Old_Overview.before{',                                      // 6
  '    "Customer ≠ User"',                                           // 7
  '    `',                                                          // 8
  '    ## Overview inside a literal',                                // 9
  '    `',                                                          // 10
  '}',                                                              // 11
  'base."Old Overview".drop(reason: "Überblick")',                   // 12
  'base.merge(self)',                                               // 13
  'base.replace(self, reason: "x")',                                // 14
].join('\n');

const plain = path.join(root, 't/skills/testing/other.md.lm');
write('upstream/skills/testing/other.md', '## Overview\n\n## How it compares\n\n## Usage Tips\n');
const plainText = [
  'base."How it compares".drop(reason: "x")', // 0
  'base.Usage_Tips.after("Überblick")',        // 1
  'base.Nope.after("x")',                      // 2
].join('\n');

const cmd = path.join(root, 't/cmd.toml.lm');
const cmdText = [
  'import ship "/.claude/commands/ship"',        // 0
  'base.description.set(self.description)',      // 1
  'base.join("description")',                     // 2
  'base.prompt.as(markdown).append(ship.body)',   // 3
  'base.prompt.as(markdown).after("Naïve Approach")', // 4
  'base.prompt.as(markdown).Steps.after(self.Naïve_Approach)', // 5
].join('\n');

const sh = path.join(root, 't/run.sh.lm');
const shText = [
  'base.main.before(self.boot)',        // 0
  'base.marker("# ── main").after("boot")',  // 1
].join('\n');

let pass = 0;
let fail = 0;

// at: cursor on the first character of the n-th occurrence of `word` on `line`.
function check(file, text, line, word, want, n = 1) {
  const src = text.split('\n')[line];
  let col = -1;
  for (let k = 0; k < n; k++) col = src.indexOf(word, col + 1);
  if (col < 0) throw new Error(`"${word}" is not on line ${line}: ${src}`);
  const got = definition(file, text, line, col);
  const show = (d) => (d ? `${path.relative(root, d.file)}:${d.line + 1}` : 'nothing');
  const wantShow = want ? `${want[0]}:${want[1] + 1}` : 'nothing';
  if (show(got) === wantShow) {
    pass++;
  } else {
    fail++;
    console.log(`  ❌ line ${line} "${word}" → want ${wantShow}, got ${show(got)}`);
  }
}

const U = 'upstream/skills/testing/SKILL.md';
const M = 'mine/skills/testing/SKILL.md';

// import paths and imported objects
check(doc, docText, 0, '/shared', ['mine/shared/notes.md', 0]);
check(doc, docText, 0, 'notes', ['mine/shared/notes.md', 0]);
check(doc, docText, 1, '/old', ['upstream/old/guide.md', 0]);
check(doc, docText, 3, 'notes', ['mine/shared/notes.md', 0]);
check(doc, docText, 3, 'Café_Notes', ['mine/shared/notes.md', 2]);

// base is redirected by `import base`; identifiers match spaces; methods jump to their node
check(doc, docText, 3, 'base', ['upstream/old/guide.md', 0]);
check(doc, docText, 3, 'Old_Overview', ['upstream/old/guide.md', 2]);
check(doc, docText, 3, 'after', null); // keywords do not jump
check(doc, docText, 12, 'Old Overview', ['upstream/old/guide.md', 2]);
check(doc, docText, 12, 'drop', null);

// content named by a string lives in our file
check(doc, docText, 3, 'Überblick', [M, 5]);
check(doc, docText, 7, 'Customer', [M, 9]);
check(doc, docText, 12, 'Überblick', null); // a reason is not a name
check(doc, docText, 9, 'Overview', null); // inside a literal

// frontmatter, join, merge, whole-file replace
check(doc, docText, 4, 'self', [M, 0]);
check(doc, docText, 4, 'frontmatter', [M, 0], 2);
check(doc, docText, 5, 'description', [M, 2]);
check(doc, docText, 13, 'merge', null);
check(doc, docText, 13, 'self', [M, 0]);
check(doc, docText, 14, 'self', [M, 0]);

// string-form nodes; an identifier with _ ; a name that does not exist lands on the file
check(plain, plainText, 0, 'How it compares', ['upstream/skills/testing/other.md', 2]);
check(plain, plainText, 1, 'Usage_Tips', ['upstream/skills/testing/other.md', 4]);
check(plain, plainText, 2, 'Nope', ['upstream/skills/testing/other.md', 0]);

// toml: keys, a key viewed as markdown, content from another file
check(cmd, cmdText, 0, 'ship', ['mine/.claude/commands/ship.md', 0]);
check(cmd, cmdText, 1, 'description', ['upstream/cmd.toml', 0]);
check(cmd, cmdText, 1, 'description', ['mine/cmd.toml', 0], 2);
check(cmd, cmdText, 2, 'description', ['mine/cmd.toml', 0]);
check(cmd, cmdText, 3, 'prompt', ['upstream/cmd.toml', 1]);
check(cmd, cmdText, 3, 'body', ['mine/.claude/commands/ship.md', 4]);
check(cmd, cmdText, 4, 'Naïve Approach', ['mine/cmd.toml', 2]);
check(cmd, cmdText, 5, 'Steps', ['upstream/cmd.toml', 2]);
check(cmd, cmdText, 5, 'Naïve_Approach', ['mine/cmd.toml', 2]);

// shell: functions and banner markers
check(sh, shText, 0, 'main', ['upstream/run.sh', 2]);
check(sh, shText, 0, 'boot', ['mine/run.sh', 0]);
check(sh, shText, 1, 'marker', null);
check(sh, shText, 1, '# ── main', ['upstream/run.sh', 1]);
check(sh, shText, 1, 'boot', ['mine/run.sh', 0]);

// a string with spaces is one link: wherever the cursor is inside it, the origin is the whole
// string, and the jump selects the name on the target line
{
  write('upstream/guide.md', '## Install\n\nx\n\n## Example 1\n\n### Phase 1\n\na\n\n## Example 2\n\n### Phase 1\n\nb\n');
  write('mine/guide.md', '## 离线环境 fallback（无网络 / 内网部署）\n\nc\n\n## 例 2\n\n### Phase 1\n\nd\n');
  const g = path.join(root, 't/guide.md.lm');
  const gText = [
    'base.append("离线环境 fallback（无网络 / 内网部署）")',   // 0
    'base."Example 2"."Phase 1".after(self."例 2"."Phase 1")', // 1
    'base."Phase 1".after("x")',                             // 2
  ].join('\n');
  const str = '"离线环境 fallback（无网络 / 内网部署）"';
  const at = gText.split('\n')[0].indexOf(str);
  for (const word of ['离线', 'fallback', ' / ', '部署）']) {
    const d = definition(g, gText, 0, gText.split('\n')[0].indexOf(word));
    const ok = d && d.file === path.join(root, 'mine/guide.md') && d.line === 0 &&
      d.origin.s === at && d.origin.e === at + str.length && d.s === 3 && d.e === 3 + str.length - 2;
    if (ok) pass++;
    else {
      fail++;
      console.log(`  ❌ cursor on "${word}" inside a string with spaces: got`, d);
    }
  }
  // section paths pick the subsection under the named parent, in upstream and in ours
  check(g, gText, 1, 'Phase 1', ['upstream/guide.md', 12]);
  check(g, gText, 1, 'after', null); // a keyword
  check(g, gText, 1, 'Phase 1', ['mine/guide.md', 6], 2);
  // a name that matches several sections is not guessed: the jump lands on the file
  check(g, gText, 2, 'Phase 1', ['upstream/guide.md', 0]);
}

// headings inside a fenced code block are not headings
{
  const { findNode } = require('../lib/loom');
  const text = fs.readFileSync(path.join(root, U), 'utf8');
  if (findNode(text, { kind: 'heading', name: 'Not a heading', ident: false }) === null) pass++;
  else {
    fail++;
    console.log('  ❌ a heading inside a code fence was matched');
  }
}

// templates beside our files (no templates setting): the template path still maps to its product
{
  const r2 = fs.mkdtempSync(path.join(os.tmpdir(), 'loom-def2-'));
  fs.writeFileSync(path.join(r2, 'loom.lm'), 'base "up"\nself "me"\n');
  fs.mkdirSync(path.join(r2, 'up'), { recursive: true });
  fs.mkdirSync(path.join(r2, 'me'), { recursive: true });
  fs.writeFileSync(path.join(r2, 'up', 'a.md'), '# A\n\n## Install\n');
  fs.writeFileSync(path.join(r2, 'me', 'a.md'), '## Ours\n');
  const text = 'base.Install.after("Ours")';
  const d1 = definition(path.join(r2, 'me', 'a.md.lm'), text, 0, text.indexOf('Install'));
  const d2 = definition(path.join(r2, 'me', 'a.md.lm'), text, 0, text.indexOf('Ours'));
  if (d1 && d1.file === path.join(r2, 'up', 'a.md') && d1.line === 2 && d2 && d2.file === path.join(r2, 'me', 'a.md') && d2.line === 0) pass++;
  else {
    fail++;
    console.log('  ❌ template beside our file: got', d1, d2);
  }
  // a template may leave out the extension: a.lm builds a.md; with a.md and a.yml it could mean either
  fs.writeFileSync(path.join(r2, 'me', 'b.md'), '## Mine\n');
  const d4 = definition(path.join(r2, 'me', 'a.lm'), text, 0, text.indexOf('Install'));
  const d5 = definition(path.join(r2, 'me', 'b.lm'), 'base.append("Mine")', 0, 14);
  fs.writeFileSync(path.join(r2, 'me', 'b.yml'), 'x: 1\n');
  const d6 = definition(path.join(r2, 'me', 'b.lm'), 'base.append("Mine")', 0, 14);
  if (d4 && d4.file === path.join(r2, 'up', 'a.md') && d4.line === 2 && d5 && d5.file === path.join(r2, 'me', 'b.md') && d6 === null) pass++;
  else {
    fail++;
    console.log('  ❌ short template names: got', d4, d5, d6);
  }
  // the template next to a.md does not make an import without the extension ambiguous
  fs.writeFileSync(path.join(r2, 'me', 'a.md.lm'), text);
  const imp = 'import "/a"';
  const d3 = definition(path.join(r2, 'me', 'b.md.lm'), imp, 0, imp.indexOf('/a'));
  if (d3 && d3.file === path.join(r2, 'me', 'a.md')) pass++;
  else {
    fail++;
    console.log('  ❌ import without extension next to a template: got', d3);
  }
  fs.rmSync(r2, { recursive: true, force: true });
}

fs.rmSync(root, { recursive: true, force: true });
console.log(`loom definition: ${pass} passed, ${fail} failed`);
process.exit(fail === 0 ? 0 : 1);
