// Signature help: the typed signature of the method whose arguments are being written, for the
// receiver it is called on, with the current argument marked.
'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const { signature } = require('../lib/signature');

let pass = 0;
let fail = 0;
const ok = (cond, what, got) => {
  if (cond) pass++;
  else {
    fail++;
    console.log(`  ❌ ${what}`, got === undefined ? '' : JSON.stringify(got));
  }
};

const root = fs.mkdtempSync(path.join(os.tmpdir(), 'loom-sig-'));
const write = (rel, text) => {
  fs.mkdirSync(path.dirname(path.join(root, rel)), { recursive: true });
  fs.writeFileSync(path.join(root, rel), text);
};
write('loom.om', 'base "up"\nself "me"\n');
write('up/a.md', '---\ndescription: Up.\n---\n\n## Overview\n');
write('me/a.md', '---\ndescription: Ours.\n---\n\n## Ours\n');
write('up/c.toml', 'description = "Up"\nprompt = """\n## Steps\n"""\n');
write('me/c.toml', 'description = "Ours"\n');
const md = path.join(root, 'me', 'a.lm');
const toml = path.join(root, 'me', 'c.lm');

const at = (file, src) => {
  const lines = src.split('\n');
  const line = lines.findIndex((l) => l.includes('|'));
  const character = lines[line].indexOf('|');
  lines[line] = lines[line].replace('|', '');
  return signature(file, lines.join('\n'), line, character);
};

let s = at(md, 'base.Overview.after(|)');
ok(s && s.label === 'base.<node>.after(...content: Content)' && s.active === 0 && s.params[0].doc.includes('a section of our file'), 'after on a node', s);
s = at(md, 'base.Overview.after("Ours", |)');
ok(s && s.active === 0, 'a list of content stays on its one parameter', s);
s = at(md, 'base.start(|)');
ok(s && s.label === 'base.start(...content: Content)', 'start on the file', s);
s = at(md, 'base.frontmatter.description.start(|)');
ok(s && s.label === 'base.<key>.start(value: Value)' && s.params[0].doc.includes('self.frontmatter.description'), 'start on a frontmatter key takes a value', s);
s = at(toml, 'base.description.append(|)');
ok(s && s.label === 'base.<key>.append(value: Value)', 'append on a toml key takes a value', s);
s = at(md, 'base.frontmatter.set(|)');
ok(s && s.label === 'base.frontmatter.set(frontmatter: Frontmatter)', 'set on the whole frontmatter', s);
s = at(md, 'base.Overview.replace("Ours", |)');
ok(s && s.label.includes('replace(content: Content, reason: Reason)') && s.active === 1, 'replace: the second argument is the reason', s);
s = at(md, 'base.replace(self, reason: |)');
ok(s && s.label === 'base.replace(file: File, reason: Reason)' && s.active === 1, 'a whole-file replace names the file and the reason', s);
s = at(md, 'base.Overview.drop(|)');
ok(s && s.params[0].label === 'reason: Reason' && s.params[0].doc.includes('build report'), 'drop takes a reason', s);
s = at(md, 'base.merge(|)');
ok(s && s.label === 'base.merge(file: self)', 'merge takes self', s);
s = at(toml, 'base.prompt.as(|)');
ok(s && s.label === 'base.<key>.as(type: Type)' && s.params[0].doc.includes('markdown'), 'as lists the types', s);
s = at(md, 'base.section(|)');
ok(s && s.label === 'base.section(heading: Name)', 'a node kind', s);
s = at(md, 'base.Overview.after{\n    "Ours"\n    |\n}');
ok(s && s.label.includes('after('), 'inside a block', s);
s = at(md, 'base.Overview.wrap(self.Ours, |)');
ok(s && s.label.startsWith('base.<node>.wrap(') && s.active === 1, 'wrap: the second argument goes after', s);
s = at(md, 'base.Overview.split(base.Overview.line("x"), |)');
ok(s && s.label.includes('split(at: Node, name: Name)') && s.active === 1, 'split: a second argument picks the signature that names the second half', s);
s = at(md, 'base.Overview.split(|)');
ok(s && s.label === 'base.<section>.split(at: Node)', 'split: one argument is where to cut', s);
s = at(md, 'base.Overview.unwrap(|)');
ok(s && s.params[0].label === 'reason: Reason', 'unwrap takes a reason', s);
s = at(md, 'base.start.project(|)');
ok(s && s.label.startsWith('base.<place>.project('), 'project on a place', s);
s = at(md, 'if base.has.Overview {\n    base.Overview.move(|)\n}');
ok(s && s.label.startsWith('base.<node>.move('), 'inside an if block', s);
ok(at(md, 'base.Overview.after("x")\n|') === null, 'outside a call: nothing');
ok(at(md, 'base.Overview.|') === null, 'after a dot: nothing');

fs.rmSync(root, { recursive: true, force: true });
console.log(`loom signature: ${pass} passed, ${fail} failed`);
process.exit(fail === 0 ? 0 : 1);
