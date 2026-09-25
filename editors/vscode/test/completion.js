// Completion, checked against a real repository on disk: each case puts the cursor at `|` in a
// template and checks what is offered there, what an item inserts, and the range it replaces.
'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const { completions } = require('../lib/completion');
const { definition } = require('../lib/definition');

const root = fs.mkdtempSync(path.join(os.tmpdir(), 'loom-complete-'));
const write = (rel, text) => {
  const p = path.join(root, rel);
  fs.mkdirSync(path.dirname(p), { recursive: true });
  fs.writeFileSync(p, text);
};

write('loom.om', 'base "upstream"\nself "mine"\n');
write('upstream/skills/testing/SKILL.md', [
  '---', 'name: testing', 'description: Upstream.', '---', '',
  '## Overview', '', 'text', '',
  '## How it compares', '', 'x', '',
  '## Example 1', '', '### Phase 1', '', 'a', '',
  '## Example 2', '', '### Phase 1', '', 'b', '', '### Phase 2', '', 'c', '',
].join('\n'));
write('mine/skills/testing/SKILL.md', [
  '---', 'name: testing', 'description: Ours.', '---', '',
  '## 离线环境 fallback（无网络 / 内网部署）', '', 'a', '',
  '## 例 1', '', '### Phase 1', '', 'b', '',
  '## 例 2', '', '### Phase 1', '', 'c', '',
].join('\n'));
write('mine/shared/notes.md', '## Café Notes\n');
write('mine/shared/notes.md.lm', '');
write('mine/shared/two.md', '');
write('mine/shared/two.toml', '');
write('upstream/old/guide.md', '## Old Overview\n');
write('upstream/cmd.toml', 'description = "Up"\nprompt = """\n## Steps\n\ndo it\n"""\n');
write('mine/cmd.toml', 'description = "Mine"\nprompt = """\n## Naïve Approach\n\ne\n"""\n## Not in the prompt\n');
write('upstream/run.sh', '#!/bin/sh\n# ── main ──\nmain() {\n  echo\n}\n');
write('mine/run.sh', 'boot() {\n  echo boot\n}\n');

const md = path.join(root, 'mine/skills/testing/SKILL.md.lm');
const toml = path.join(root, 'mine/cmd.toml.lm');
const sh = path.join(root, 'mine/run.sh.lm');

let pass = 0;
let fail = 0;
const ok = (cond, what, got) => {
  if (cond) pass++;
  else {
    fail++;
    console.log(`  ❌ ${what}`);
    if (got !== undefined) console.log('     got:', JSON.stringify(got));
  }
};

// at: the items offered where `|` is in the template text.
function at(file, src) {
  const lines = src.split('\n');
  const line = lines.findIndex((l) => l.includes('|'));
  const character = lines[line].indexOf('|');
  lines[line] = lines[line].replace('|', '');
  return { items: completions(file, lines.join('\n'), line, character), text: lines.join('\n'), line, character };
}
const labels = (items) => items.map((i) => i.label);
const find = (items, label) => items.find((i) => i.label === label);

// ── after a dot ──
{
  const { items } = at(md, 'base.|');
  const l = labels(items);
  ok(l.slice(0, 3).join(',') === 'Overview,How it compares,Example 1', 'base. lists upstream sections first, in file order', l);
  ok(['frontmatter', 'section', 'line', 'start', 'append', 'replace', 'merge'].every((x) => l.includes(x)), 'base. offers parts, kinds and file methods', l);
  ok(!l.includes('body') && !l.includes('join') && !l.includes('after'), 'base. offers no body, no join, no node methods', l);
  ok(find(items, 'Overview').insertText === 'Overview', 'a plain name is inserted as an identifier');
  // An anchor into upstream is written as the compiler writes it — a space becomes an underscore —
  // so that lm sync, which rewrites this same token when upstream renames something, agrees.
  ok(find(items, 'How it compares').insertText === 'How_it_compares', 'a space in an upstream anchor becomes an underscore', find(items, 'How it compares').insertText);
  ok(find(items, 'replace').insertText === 'replace(self, reason: "$1")', 'replace on the whole file takes a file and a reason');
  // a repeated name comes with the parent that tells it apart, and "Phase 2" does not need one
  ok(find(items, 'Example 1 › Phase 1').insertText === '"Example 1"."Phase 1"', 'a repeated subsection comes with its parent path', l);
  ok(find(items, 'Phase 2').insertText === 'Phase_2', 'a unique subsection needs no path', l);
  ok(!l.includes('Phase 1'), 'a repeated name is never offered bare', l);
}
{
  const { items, line, character } = at(md, 'base.How_it|');
  const it = find(items, 'How it compares');
  ok(it && it.range.s === 5 && it.range.e === character && it.range.line === line, 'typing an identifier: the item replaces it', it && it.range);
  ok(it && it.filterText.startsWith('How_it_compares'), 'an identifier with _ still finds a name with spaces', it && it.filterText);
}
{
  // inside a string after the dot, the whole string (quotes included) is replaced
  const src = 'base."How it|"';
  const { items } = at(md, src);
  const it = find(items, 'How it compares');
  ok(it && it.range.s === 5 && it.range.e === src.length - 1 && it.insertText === '"How it compares"', 'inside a string after a dot: the whole string is replaced', it);
  ok(!labels(items).includes('after'), 'inside a string only names are offered', labels(items));
}
{
  const l = labels(at(md, 'base.Overview.|').items);
  ok(['after', 'before', 'replace', 'drop'].every((x) => l.includes(x)) && !l.includes('start') && !l.includes('set'), 'a section offers node methods', l);
  const l2 = labels(at(md, 'base."Example 2".|').items);
  // Axes first — they are the same four under every section — then this section's own, then methods.
  ok(l2.slice(0, 4).join(',') === 'children,next,prev,parent', 'below a section: the axes come first', l2);
  ok(l2[4] === 'Phase 1' && l2[5] === 'Phase 2' && l2.includes('after'), 'below a section: its subsections, then methods', l2);
  const l3 = labels(at(md, 'base.frontmatter.|').items);
  ok(l3.join(',') === 'name,description,set', 'frontmatter offers its keys and set', l3);
  const l3b = labels(at(md, 'base.frontmatter.description.|').items);
  ok(l3b.join(',') === 'set,start,append,end', 'a frontmatter key offers set, start, append and end — append by its other name', l3b);
  const l4 = labels(at(md, 'base.Overview.after("x").|').items);
  ok(l4.length === 0, 'nothing follows a method', l4);
}

// ── inside a call ──
{
  const { items } = at(md, 'base.Overview.after(|)');
  const l = labels(items);
  ok(l[0] === '离线环境 fallback（无网络 / 内网部署）' && find(items, l[0]).insertText === '"离线环境 fallback（无网络 / 内网部署）"', 'after( offers our sections as strings', l);
  ok(find(items, '例 1 › Phase 1') && find(items, '例 1 › Phase 1').insertText === 'self."例 1"."Phase 1"', 'a repeated section of ours is written as a path on self', l);
  ok(l.includes('self') && !l.includes('reason:'), 'after( offers self and no reason:', l);
}
{
  const src = 'base.Overview.after("离线|")';
  const { items } = at(md, src);
  const it = items[0];
  ok(it && it.range.s === 20 && it.range.e === src.length - 2 && it.filterText === '"离线环境 fallback（无网络 / 内网部署）"', 'inside a string argument: the whole string is replaced', it);
  ok(!labels(items).includes('self'), 'inside a string only names are offered', labels(items));
}
{
  const l = labels(at(md, 'base.Overview.drop(|)').items);
  ok(l.join(',') === 'reason:', 'drop( offers only reason:', l);
  const l2 = labels(at(md, 'base.Overview.replace("例 2", |)').items);
  ok(l2.includes('reason:') && l2.includes('self'), 'replace( offers content and reason:', l2);
  const l3 = labels(at(md, 'base.frontmatter.description.start(|)').items);
  ok(l3.join(',') === 'self.frontmatter.description,self.frontmatter.name', 'a key\'s start( offers our keys, the same key first', l3);
  const l4 = labels(at(md, 'base.merge(|)').items);
  ok(l4.join(',') === 'self', 'merge( offers self', l4);
  const l5 = labels(at(md, 'base.section("|")').items);
  ok(l5.includes('Overview') && !l5.some((x) => x.includes('Phase 1')), 'section(" offers upstream sections a single name can reach', l5);
  const l6 = labels(at(md, 'base.frontmatter.set(|)').items);
  ok(l6.join(',') === 'self.frontmatter', 'frontmatter can only be set to a frontmatter', l6);
  const l7 = labels(at(md, 'base.Overview.after{\n    "例 2"\n    |\n}').items);
  ok(l7.includes('例 2 › Phase 1'), 'inside a block: our sections', l7);
  const l8 = labels(at(md, 'base.Overview.after(self.|)').items);
  ok(l8.includes('body') && l8.includes('frontmatter') && !l8.includes('after'), 'self. in an argument: nodes and parts, no methods', l8);
}

// ── statements, comments, literals ──
{
  const l = labels(at(md, 'base.Overview.after("x")\n|').items);
  ok(l.join(',') === 'base,import,if,if / else if,fn,return,Self', 'a new line offers the objects and the words that start a statement', l);
  const l2 = labels(at(md, 'base.Overview.after{\n    "x"\n}\n|').items);
  ok(l2.join(',') === 'base,import,if,if / else if,fn,return,Self', 'after a closed block: a new statement', l2);
  const l3 = labels(at(md, 'base.Overview.after(\n|').items);
  ok(l3.join(',') === 'base,import,if,if / else if,fn,return,Self', '( does not continue on the next line', l3);
  const l4 = labels(at(md, '// base.|').items);
  ok(l4.length === 0, 'nothing is offered in a comment', l4);
  const l5 = labels(at(md, 'base.start(`\nbase.|\n`)').items);
  ok(l5.length === 0, 'nothing is offered inside a literal', l5);
}

// ── import paths ──
{
  ok(labels(at(md, 'import "|"').items).join(',') === '/,./,../', 'an empty path offers where to start');
  const l = labels(at(md, 'import "/|"').items);
  ok(l.includes('shared/') && l.includes('skills/') && !l.some((x) => x.startsWith('old')), 'a / path lists our layer root', l);
  const { items } = at(md, 'import "/shared/n|"');
  const names = labels(items);
  ok(names.includes('notes') && names.includes('two.md') && names.includes('two.toml') && !names.some((x) => x.endsWith('.lm')), 'files: extension left out only when unique; no templates', names);
  ok(items[0].range.s === 16, 'a path item replaces only the last part', items[0].range);
  const l2 = labels(at(md, 'import base "/old/|"').items);
  ok(l2.join(',') === 'guide', 'import base lists the upstream layer', l2);
  const l3 = labels(at(md, 'import "./|"').items);
  ok(l3.join(',') === 'SKILL', './ is relative to the product directory (templates beside it are not offered)', l3);
}

// ── views, toml, shell, settings ──
{
  const l = labels(at(toml, 'base.prompt.as(markdown).|').items);
  ok(l[0] === 'Steps' && l.includes('append') && !l.includes('replace') && !l.includes('merge'), 'a view offers its sections and view methods', l);
  const l2 = labels(at(toml, 'base.prompt.as(markdown).after("|")').items);
  ok(l2.join(',') === 'Naïve Approach', 'in a view, strings name sections of the same key in our file', l2);
  const l3 = labels(at(toml, 'base.|').items);
  ok(l3[0] === 'description' && l3[1] === 'prompt' && !l3.includes('join') && l3.includes('key'), 'toml: keys and key(...)', l3);
  const l3c = labels(at(toml, 'base.description.start(|)').items);
  ok(l3c[0] === 'self.description', 'toml: a key\'s start( offers our keys, the same key first', l3c);
  const l4 = labels(at(toml, 'base.prompt.as(|)').items);
  ok(l4.includes('markdown') && l4.includes('yaml') && l4.length === 6, 'as( offers the types', l4);
  const l5 = labels(at(sh, 'base.|').items);
  ok(l5[0] === 'main' && ['function', 'marker', 'line'].every((x) => l5.includes(x)), 'shell: functions and node kinds', l5);
  const l6 = labels(at(sh, 'base.marker("|")').items);
  ok(l6.join(',') === '# ── main ──', 'marker(" offers the banner lines', l6);
  const l7 = labels(at(path.join(root, 'loom.om'), 'base "upstream"\nte|').items);
  ok(l7.includes('templates') && l7.includes('manifest'), 'loom.om offers the settings', l7);
}

// Every name completion inserts must resolve back to the node it was offered for:
// written into a template, go to definition on it lands on the line the item names.
{
  const lineOf = (it) => Number(it.detail.split(':').pop()) - 1;
  const lands = (file, text, where, it, want) => {
    const src = text.split('\n')[0];
    const d = definition(file, text, 0, src.lastIndexOf(where));
    ok(d && d.line === want, `inserting ${it.insertText} resolves back to line ${want + 1}`, d && d.line + 1);
  };
  for (const it of at(md, 'base.|').items.filter((i) => i.kind === 'section')) {
    const text = `base.${it.insertText}.drop(reason: "x")`;
    lands(md, text, it.insertText.split('.').pop().replace(/"/g, ''), it, lineOf(it));
  }
  for (const it of at(md, 'base."Example 2".|').items.filter((i) => i.kind === 'section')) {
    const text = `base."Example 2".${it.insertText}.drop(reason: "x")`;
    lands(md, text, it.insertText.split('.').pop().replace(/"/g, ''), it, lineOf(it));
  }
  for (const it of at(md, 'base.Overview.after(|)').items.filter((i) => i.kind === 'section')) {
    const text = `base.Overview.after(${it.insertText})`;
    lands(md, text, it.insertText.split('.').pop().slice(1), it, lineOf(it));
  }
}

// ── below a key, the keys nested under it ──
// The compiler addresses a nested key by the dotted path of its parents, so base.jobs.build is one
// name. The editor stopped at the first segment and called the rest bad, which made go-to-definition
// on it do nothing and left completion with no children to offer.
{
  write('upstream/w.yaml', 'name: n\njobs:\n  build:\n    runs-on: x\n  test:\n    runs-on: y\n');
  write('mine/w.yaml', 'name: o\n');
  const yml = path.join(root, 'mine/w.yaml.lm');
  const l = labels(at(yml, 'base.jobs.|').items);
  ok(l.includes('build') && l.includes('test'), 'below a key, its own children are offered', l);
  ok(!l.includes('name'), 'and nothing from the top level', l);
  ok(!l.some((x) => x.includes('.')), 'each child is offered by its own segment, not the whole path', l);
  const deeper = labels(at(yml, 'base.jobs.build.|').items);
  ok(deeper.includes('runs-on'), 'and it keeps going down', deeper);
}

// ── the two sides are written differently, as the compiler writes them ──
// An anchor into upstream goes through NameText (a space becomes an underscore), because lm sync
// rewrites that token when upstream renames something. Content of ours is always quoted, because
// that is what anchor completion appends next to it. Matching both keeps a template from being
// rewritten the moment the build touches it.
{
  ok(find(at(md, 'base.|').items, 'How it compares').insertText === 'How_it_compares', 'an upstream anchor uses the underscore form');
  const self = at(md, 'base.Overview.after(self.|)').items;
  const it = find(self, '离线环境 fallback（无网络 / 内网部署）');
  ok(it && it.insertText.startsWith('"'), 'content of ours is quoted', it && it.insertText);
}

// ── after a fenced literal ──
// A fence is one token, backticks and all. Counting to the next single backtick instead — which
// is what the lexer did — puts everything after a line of prose containing one ` out of step,
// and the whole rest of the file goes dead in the editor while the compiler builds it happily.
{
  const src = [
    'base.Overview.after(```markdown',
    'Use the ` character to mark code in markdown.',
    '```)',
    'base.|',
  ].join('\n');
  const l = labels(at(md, src).items);
  ok(l.includes('Overview') && l.includes('How it compares'), 'an odd backtick inside a fence does not blind the rest of the file', l);
}
{
  // A longer fence holds a shorter one: the inner ``` is content, not the closer.
  const src = [
    'base.Overview.after(````markdown',
    'A ` in prose, then a block:',
    '```sh',
    'echo hi',
    '```',
    '````)',
    'base.|',
  ].join('\n');
  const l = labels(at(md, src).items);
  ok(l.includes('Overview'), 'a nested fence is content, not the end of the outer one', l);
}
{
  // Half-written is the normal state of a template being edited: the open fence runs to the end
  // of the file, so what is inside it is content, not half a statement to resolve.
  const { items } = at(md, ['base.Overview.after(```markdown', 'base.How it| compares'].join('\n'));
  ok(labels(items).length === 0, 'inside an unterminated fence nothing is offered', labels(items));
}

// ── inside a class call: a group takes predicates, not a name ──
{
  const { items } = at(md, 'base.sections(|)');
  const l = labels(items);
  ok(l.includes('match:') && l.includes('empty') && l.includes('level:'), 'a markdown group offers all three predicates', l);
  ok(!l.includes('Overview'), 'a group takes predicates, so no names are offered', l);
  ok(find(items, 'level:').insertText === 'level: ${1:2}', 'level: inserts a depth to fill in');
  ok(find(items, 'match:').insertText === 'match: "$1"', 'match: inserts a regular expression to fill in');
}
{
  // Only a heading has a level, so only a heading group offers it.
  const l = labels(at(toml, 'base.keys(|)').items);
  ok(l.includes('match:') && l.includes('empty'), 'a toml group offers the predicates it has', l);
  ok(!l.includes('level:'), 'a key has no level, so level: is not offered', l);
  const lines = labels(at(md, 'base.lines(|)').items);
  ok(!lines.includes('level:'), 'a line has no level either', lines);
}

// ── the added syntax ──
{
  // Each receiver offers what the compiler accepts on it (objparse.go's edit), no more.
  const sec = labels(at(md, 'base.Overview.|').items);
  ok(['move', 'wrap', 'swap', 'promote', 'demote', 'unwrap', 'split', 'join'].every((x) => sec.includes(x)), 'a section offers the structural methods', sec);
  const fn = labels(at(sh, 'base.main.|').items);
  ok(['move', 'wrap', 'swap'].every((x) => fn.includes(x)) && !['promote', 'unwrap', 'split', 'join'].some((x) => fn.includes(x)),
    'a shell function takes no heading operations', fn);
  const file = labels(at(md, 'base.|').items);
  ok(file.includes('end') && file.includes('sections') && file.includes('lines') && file.includes('has'), 'the file offers end, its groups, and has', file);
  const shFile = labels(at(sh, 'base.|').items);
  ok(shFile.includes('functions') && shFile.includes('markers') && !shFile.includes('sections'), 'a group word follows the type', shFile);
}
{
  // A group: first / last, the if questions, and what means the same done to each of them.
  const g = labels(at(md, 'base.sections[level == 2].|').items);
  ok(['first', 'last', 'any', 'count', 'drop', 'promote', 'demote', 'unwrap'].every((x) => g.includes(x)), 'a group offers first, last, any, count and the group methods', g);
  ok(!g.includes('after') && !g.includes('Overview'), 'a group takes no single-node method and no name', g);
  const bare = labels(at(md, 'base.sections.|').items);
  ok(bare.includes('first') && bare.includes('drop'), 'a bare group word is every node of the kind', bare);
  const keys = labels(at(toml, 'base.keys[empty].|').items);
  ok(keys.includes('drop') && !keys.includes('promote'), 'a group of keys has no heading level to change', keys);
  const one = labels(at(md, 'base.sections[level == 2].first.|').items);
  ok(one.includes('after') && one.includes('children') && !one.includes('first'), 'first picks one node, which takes node methods and axes', one);
  const kids = labels(at(md, 'base.Overview.children.|').items);
  ok(kids.includes('first') && kids.includes('drop') && !kids.includes('after'), 'children lands on a group again', kids);
}
{
  const pl = labels(at(md, 'base.start.|').items);
  ok(pl.join(',') === 'project', 'a place takes only project', pl);
  const pl2 = labels(at(md, 'base.Overview.after.|').items);
  ok(pl2.join(',') === 'project', 'a place next to a node takes only project', pl2);
  const has = labels(at(md, 'if base.has.|').items);
  ok(has.includes('Overview') && !has.includes('after') && !has.includes('sections'), 'has asks about a name, so names and nothing else', has);
}
{
  // Inside a predicate: the fields it can ask about, and a snippet for how each is written.
  const { items } = at(md, 'base.sections[|]');
  const l = labels(items);
  ok(['level', 'name', 'value', 'empty', 'calls', 'has'].every((x) => l.includes(x)), 'inside [ the predicate fields are offered', l);
  ok(find(items, 'level').insertText === 'level == ${1:2}', 'level inserts a comparison to fill in', find(items, 'level').insertText);
  const and = labels(at(md, 'base.sections[level == 2 && |]').items);
  ok(and.includes('name'), 'after && the next field is offered', and);
  const typing = labels(at(md, 'base.sections[lev|]').items);
  ok(typing.includes('level'), 'typing a field completes it', typing);
}
{
  // Statements of the added syntax are read line by line, so what follows them still completes.
  const inIf = labels(at(md, 'if base.has.Overview {\n    base.Overview.|\n}').items);
  ok(inIf.includes('after'), 'inside an if block, statements complete as anywhere else', inIf);
  const cond = labels(at(md, 'if base.has.Overview {\n}\nbase.|').items);
  ok(cond.includes('Overview'), 'an if block ends, and the file goes on', cond);
  const caught = labels(at(md, 'ok = base.Overview.|').items);
  ok(caught.includes('after'), 'a caught result still completes its statement', caught);
  const st = labels(at(md, '|').items);
  ok(['if', 'fn', 'return', 'Self'].every((x) => st.includes(x)), 'a new line offers the statement keywords', st);
  const els = labels(at(md, 'if base.has.Overview {\n} |').items);
  ok(els.join(',') === 'else,else if', 'after an if block closes, else is offered', els);
  const typed = labels(at(md, 'if base.has.Overview {\n} el|').items);
  ok(typed.includes('else'), 'typing else after the block completes it', typed);
  const it = find(at(md, '|').items, 'if');
  ok(it.markdown && it.markdown.includes('**Examples**'), 'a keyword comes with its help', it.markdown);
}

// ── self written in the template: resource sections ──
{
  const tail = ['', '---', 'Self:', '    ```markdown', '    ## job', '    ## tip', '    ```', '', '    ```json', '    { "ddd": 11 }', '    ```',
    'Self as notes:', '    ```markdown', '    ## tip', '    ```'].join('\n');
  const top = at(md, 'base.Overview.after(self.|)' + tail).items;
  const l = labels(top);
  ok(l.includes('notes') && l.includes('markdown') && l.includes('json') && l.includes('job'), 'self. offers the section names, the kinds, and the parts', l);
  ok(!l.includes('tip') && find(top, 'notes › tip') && find(top, 'notes › tip').insertText === 'notes."tip"',
    'a part two documents share is offered with its section, and never bare', l);
  ok(!l.includes('sections') && !l.includes('has'), 'content is one node at a time: no groups on self', l);
  const kind = labels(at(md, 'base.Overview.after(self.json.|)' + tail).items);
  ok(kind.includes('ddd') && !kind.includes('job'), 'a kind narrows to that document', kind);
  const sec = labels(at(md, 'base.Overview.after(self.notes.|)' + tail).items);
  ok(sec.includes('tip') && !sec.includes('job') && !sec.includes('notes'), 'a section narrows to its documents', sec);
  const args = at(md, 'base.Overview.after(|)' + tail).items;
  ok(find(args, 'job') && find(args, 'notes › tip') && find(args, 'notes › tip').insertText === 'self.notes."tip"' && labels(args).includes('self'),
    'a content argument offers the parts of Self and self itself', labels(args));
}

// ── a tree that declares Loom 2 ──
{
  // Names are taken as written and a bare string is text, so completion writes what that means:
  // a name with a space is quoted, and our sections are named on self.
  const v2 = path.join(root, 'v2');
  write('v2/loom.om', 'loom "2.0"\nbase "up"\nself "me"\n');
  write('v2/up/a.md', '## How it compares\n\nx\n\n## Plain\n\ny\n');
  write('v2/me/a.md', '## Our Part\n\no\n');
  const f = path.join(v2, 'me/a.md.lm');
  const items = at(f, 'base.|').items;
  ok(find(items, 'How it compares').insertText === '"How it compares"', 'Loom 2: a name with a space is quoted, not underscored', find(items, 'How it compares').insertText);
  ok(find(items, 'Plain').insertText === 'Plain', 'Loom 2: an identifier is written as it is', find(items, 'Plain').insertText);
  const args = at(f, 'base.Plain.after(|)').items;
  ok(find(args, 'Our Part') && find(args, 'Our Part').insertText === 'self."Our Part"', 'Loom 2: our section is named on self, since a bare string is text', find(args, 'Our Part'));
  ok(labels(at(f, 'base.Plain.after("Our|")').items).length === 0, 'Loom 2: inside a string there is nothing to complete');
  const src = 'base.How_it_compares.after(self.x)\nbase."How it compares".after("Our Part")';
  const under = definition(f, src, 0, 6);
  ok(under && under.line === 0 && under.s === 0 && under.e === 0, 'Loom 2: How_it_compares is not "How it compares" — the file, not the heading', under);
  const quoted = definition(f, src, 1, 7);
  ok(quoted && quoted.line === 0 && quoted.e > 0, 'Loom 2: the quoted name is the heading', quoted);
  ok(definition(f, src, 1, src.split('\n')[1].indexOf('Our Part')) === null, 'Loom 2: a bare string is text and leads nowhere');
}

fs.rmSync(root, { recursive: true, force: true });
console.log(`loom completion: ${pass} passed, ${fail} failed`);
process.exit(fail === 0 ? 0 : 1);
