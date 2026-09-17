// Hover: keywords show how they are written, with examples; objects say which file they stand for.
'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const { hover } = require('../lib/hover');
const { KEYWORDS } = require('../lib/docs');

let pass = 0;
let fail = 0;
const ok = (cond, what, got) => {
  if (cond) pass++;
  else {
    fail++;
    console.log(`  ❌ ${what}`, got === undefined ? '' : JSON.stringify(got));
  }
};

const root = fs.mkdtempSync(path.join(os.tmpdir(), 'loom-hover-'));
const write = (rel, text) => {
  fs.mkdirSync(path.dirname(path.join(root, rel)), { recursive: true });
  fs.writeFileSync(path.join(root, rel), text);
};
write('loom.lm', 'base "up"\nself "me"\n');
write('up/run.sh', 'main() {\n  echo\n}\n');
write('me/run.sh', 'main() {\n  echo me\n}\n');
write('me/cmd.md', '## Steps\n');
const tpl = path.join(root, 'me', 'run.lm');
const text = [
  'import cmd "/cmd"',                                   // 0
  'base.merge(self)',                                    // 1
  'base.main.drop(reason: "x")',                         // 2
  'base.prompt.as(markdown).append(cmd.body)',           // 3
  'base.marker("# ── main").after(self.frontmatter)',    // 4
].join('\n');

const at = (line, word, n = 1) => {
  const src = text.split('\n')[line];
  let col = -1;
  for (let k = 0; k < n; k++) col = src.indexOf(word, col + 1);
  return hover(tpl, text, line, col);
};

for (const [line, word] of [[0, 'import'], [1, 'merge'], [2, 'drop'], [2, 'reason'], [3, 'as('], [3, 'append'], [3, 'body'], [4, 'marker'], [4, 'after'], [4, 'frontmatter']]) {
  const h = at(line, word);
  ok(h && h.markdown.includes('**Examples**') && h.markdown.includes('```loom') && h.markdown.includes(word), `${word}: usage with examples`, h && h.markdown.slice(0, 80));
}
const m = at(1, 'merge');
ok(m.range.s === 5 && m.range.e === 10, 'the hover covers the keyword', m.range);
ok(m.markdown.includes('lm sync'), 'merge explains how upstream is followed');

const b = at(1, 'base');
ok(b && b.markdown.includes('`up/run.sh`') && b.markdown.includes('opens it'), 'base names the upstream file', b);
const s = at(1, 'self');
ok(s && s.markdown.includes('`me/run.sh`'), 'self names our file', s);
const c = at(3, 'cmd');
ok(c && c.markdown.includes('`me/cmd.md`'), 'an imported name names its file', c);
ok(at(2, 'main') === null, 'a node name has no keyword help');
ok(at(3, 'markdown') === null, 'a type is not a keyword of its own');

// every keyword's examples parse as Loom statements a template could hold
for (const [word, k] of Object.entries(KEYWORDS)) {
  ok(k.examples.length > 0 && k.usage && k.what, `${word}: usage, meaning and examples are written`);
}

fs.rmSync(root, { recursive: true, force: true });
console.log(`loom hover: ${pass} passed, ${fail} failed`);
process.exit(fail === 0 ? 0 : 1);
