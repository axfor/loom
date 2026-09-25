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
write('loom.om', 'base "up"\nself "me"\n');
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

// every keyword has a typed signature, a meaning and examples; methods explain each parameter's type
for (const [word, k] of Object.entries(KEYWORDS)) {
  ok(k.examples.length > 0 && k.signatures.length > 0 && k.what, `${word}: signature, meaning and examples are written`);
}
const after = at(4, 'after');
ok(after.markdown.includes('after(...content: Content)') && after.markdown.includes('**Parameters**') && after.markdown.includes('a section of our file'),
  'a method shows its typed signature and what each type accepts', after.markdown);
const start = at(3, 'append');
ok(start.markdown.includes('base.append(...content: Content)') && start.markdown.includes('base.<key>.append(value: Value)'),
  'a method with two receivers shows both signatures', start.markdown);

// The added syntax: every word the compiler reads as a word, not a name, explains itself.
{
  const src = [
    'if base.has.main {',                                           // 0
    '    base.main.wrap(self.main, self.main)',                    // 1
    '} else if base.functions[calls "curl" && !empty].any {',      // 2
    '    base.functions[calls "curl"].drop(reason: "x")',          // 3
    '} else {',                                                    // 4
    '    return err.format("no main: %s", ok)',                    // 5
    '}',                                                           // 6
    'fn twice(up, ours) {',                                        // 7
    '    up.after(ours)',                                          // 8
    '}',                                                           // 9
    'base.main.move(base.main.after)',                             // 10
    'return self // reason: ours',                                 // 11
    '---',                                                         // 12
    'Self:',                                                       // 13
    '    ```shell',                                                // 14
    '    main() { :; }',                                           // 15
    '    ```',                                                     // 16
  ].join('\n');
  const on = (line, word, n = 1) => {
    const l = src.split('\n')[line];
    let col = -1;
    for (let k = 0; k < n; k++) col = l.indexOf(word, col + 1);
    return hover(tpl, src, line, col);
  };
  for (const [line, word, n] of [[0, 'if'], [0, 'has'], [1, 'wrap'], [2, 'else'], [2, 'if'], [2, 'functions'], [2, 'calls'], [2, 'empty'], [2, 'any'],
    [3, 'drop'], [5, 'return'], [5, 'err'], [7, 'fn'], [10, 'move'], [10, 'after'], [11, 'return'], [13, 'Self']]) {
    const h = on(line, word, n);
    ok(h && h.markdown.includes('**Examples**'), `${word} on line ${line}: explained`, h && h.markdown.slice(0, 60));
  }
  // A place word hanging off a node is the place, and says so.
  ok(on(10, 'after').markdown.includes('base.<node>.after  ·'), 'a bare after reads as a place', on(10, 'after').markdown.slice(0, 120));
  ok(on(0, 'main') === null, 'a node name is not a keyword');
  const up = on(8, 'up');
  ok(up && up.markdown.includes('parameter of `fn twice`') && up.markdown.includes('Nothing calls'), 'a parameter nobody passes says so', up);
}
{
  // Words stay names where the compiler reads them as names: a quoted "children" is a section.
  const src = 'base."children".drop(reason: "x")\nbase.main.children.drop(reason: "x")';
  const l0 = src.split('\n')[0];
  ok(hover(tpl, src, 0, l0.indexOf('children')) === null, 'a quoted word is a name');
  const h = hover(tpl, src, 1, src.split('\n')[1].indexOf('children'));
  ok(h && h.markdown.includes('one level down'), 'a bare axis word is the axis', h);
}
{
  // Without a tree the words still read as words: the examples the extension ships are read, not built.
  const src = 'base.sections[level == 2].first.children.demote()';
  for (const w of ['sections', 'level', 'first', 'children', 'demote']) {
    const h = hover('/nowhere/x.lm', src, 0, src.indexOf(w));
    ok(h && h.markdown.includes('**Examples**'), `${w} is explained outside a tree`, h);
  }
}

// A parameter says what each call passes; a template field says what the build writes.
{
  const src = ['fn both(up, ours) {', '    up.after(ours)', '}', 'both(base.main, self.main)', 'both(base.main, `echo`)',
    'base.start.project(base.functions){', '    ```shell', '    # {name} at {level}', '    ```', '}'].join('\n');
  const l = src.split('\n');
  const h = hover(tpl, src, 1, l[1].indexOf('up'));
  ok(h && h.markdown.includes('line 4: `base.main`') && h.markdown.includes('line 5: `base.main`'), 'a parameter lists what each call passes', h && h.markdown);
  const o = hover(tpl, src, 1, l[1].indexOf('ours'));
  ok(o && o.markdown.includes('line 4: `self.main`') && o.markdown.includes('a literal'), 'content passed is listed too', o && o.markdown);
  const f = hover(tpl, src, 7, l[7].indexOf('{name}') + 2);
  ok(f && f.markdown.includes('{name}') && f.range.s === l[7].indexOf('{name}') && f.range.e === l[7].indexOf('{name}') + 6, 'a field explains itself', f);
  ok(hover(tpl, src, 7, l[7].indexOf(' at ') + 1) === null, 'the rest of a template is text');
}

fs.rmSync(root, { recursive: true, force: true });
console.log(`loom hover: ${pass} passed, ${fail} failed`);
process.exit(fail === 0 ? 0 : 1);
