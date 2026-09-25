'use strict';

// Completion for Loom templates and loom.om: what can be written at the cursor?
//
//   base.|  self.|  cmd.|         the nodes of that file, its parts, node kinds, and the methods that apply
//   base."Example 2".|            the subsections of that section, and the methods on it
//   .after(|  .after("|           our sections (a repeated name comes with its parent path), self, imports
//   .drop(|  .replace(x, |        reason:
//   .description.start(|          our value for the key: self.frontmatter.description / self.description
//   .merge(|                      self
//   .section("|  .function("|     that kind of node in the file
//   .as(|                         the types
//   import "/|                    directories and files of the layer
//   start of a line               base, import (in loom.om: the settings)
//
// Items carry the range they replace. A string is replaced as a whole, quotes included, so a
// name with spaces is never inserted by halves. Like definition.js, no `vscode` import here.

const fs = require('fs');
const path = require('path');
const loom = require('./loom');
const { markdownFor } = require('./docs');

const METHOD_DOCS = {
  after: 'insert after the node',
  before: 'insert before the node',
  start: 'insert at the start; on a key: ours before upstream\'s value',
  append: 'insert at the end; on a key: ours after upstream\'s value',
  replace: 'replace with content; needs reason:',
  drop: 'leave out of the product on purpose; needs reason:',
  set: 'take ours: the whole frontmatter, or a key\'s value',
  merge: 'our file is upstream plus our edits; lm sync merges each new upstream into it',
  as: "view the key's value as another type",
  move: 'put the node somewhere else; its own bytes do not change',
  promote: 'take the heading up one level',
  demote: 'take the heading down one level',
  wrap: 'insert before and after in one statement',
  swap: 'trade places with another node',
  unwrap: 'take the heading out and lift what was under it; needs reason:',
  split: 'cut the section in two at a node inside it',
  join: 'run this section and the next together; needs reason:',
  project: "derive content from upstream's shape, not its words",
  end: 'insert at the end; on a key: ours after upstream\'s value',
};

const SNIPPETS = {
  after: 'after($1)',
  before: 'before($1)',
  start: 'start($1)',
  append: 'append($1)',
  replace: 'replace($1, reason: "$2")',
  drop: 'drop(reason: "$1")',
  set: 'set($1)',
  merge: 'merge(self)',
  as: 'as($1)',
  move: 'move(after: base.$1)',
  promote: 'promote()',
  demote: 'demote()',
  wrap: 'wrap($1, $2)',
  swap: 'swap(base.$1)',
  unwrap: 'unwrap(reason: "$1")',
  split: 'split($1, "$2")',
  join: 'join(reason: "$1")',
  project: 'project($1){\n\t```markdown\n\t$0\n\t```\n}',
  end: 'end($1)',
};

const SETTINGS = [
  ['loom', 'loom "$1"', 'the language version this tree is written against'],
  ['base', 'base "$1"', 'the upstream layer directory'],
  ['self', 'self "$1"', 'our layer directory'],
  ['templates', 'templates "$1"', 'where templates live (default: next to our files)'],
  ['output', 'output "$1"', 'the product directory'],
  ['mark', 'mark ${1|markdown,shell,toml,yaml,json,text|} "$2" "$3"', 'the begin / end marks around our content'],
  ['frontmatter', 'frontmatter ${1|set,start,append|}', 'what to do with a frontmatter key of ours that upstream also has'],
  ['keys', 'keys ${1|set,start,append|}', 'the same, for a top-level key of a toml or yaml file'],
  ['body', 'body ${1|start,append|}', 'where our body goes when none of it can follow an upstream section'],
  ['registry', 'registry "$1" "$2"', 'a group of files registered by a regex'],
  ['take', 'take "$1"', 'upstream files copied into the product as they are'],
  ['mirror', 'mirror "$1" "$2"', 'copy one product directory to another'],
  ['manifest', 'manifest "$1"', 'the file listing everything the build produced'],
];

// the language a node's text is shown in, in the item's documentation
const LANGS = { heading: 'markdown', fmkey: 'yaml', key: 'toml', path: 'json', function: 'shellscript', marker: 'shellscript' };

const IDENT = /^[\p{L}_][\p{L}\p{N}_]*$/u;
const RESERVED = new Set([...loom.METHODS, 'as', 'frontmatter', 'body', ...Object.values(loom.KIND_CALLS).flatMap(Object.keys)]);

// nodeText writes one name after a dot, as lang/write.go's NameText does: an identifier where it
// can be, a string otherwise, and a space becomes an underscore — the rule the compiler resolves
// by. An identifier already holding a _ would match a space too, so those stay strings.
//
// Writing the quoted form here instead also works, and read alone looks tidier. But then
// completion and the build write the same name two ways, and lm sync rewrites one into the other
// the next time upstream renames something.
//
// RESERVED holds more than the compiler does: a heading called `section` is not ambiguous to it,
// since a kind is only ever a call. Quoting it anyway costs nothing and spares the reader the
// question.
function nodeText(name) {
  if (name === '' || name.includes('_') || RESERVED.has(name)) return loom.quote(name);
  const id = name.replace(/ /g, '_');
  if (/ {2}/.test(name) || name.startsWith(' ') || name.endsWith(' ') || !IDENT.test(id)) return loom.quote(name);
  return id;
}

// nodeTextFor writes a name for a tree that declares a version, as lang.NameTextFor does. From
// Loom 2 an unquoted name is the name as written, so a space cannot become an underscore: a name
// that is not already an identifier is quoted.
function nodeTextFor(name, version) {
  if (!loom.atLeast(version, 2)) return nodeText(name);
  return IDENT.test(name) && !RESERVED.has(name) ? name : loom.quote(name);
}

// typedAs: what an identifier typed for this name looks like, so `Usage_T` still finds "Usage Tips".
function typedAs(name) {
  return `${name.replace(/ /g, '_')} ${name}`;
}

function item(label, kind, fields) {
  return { label, kind, ...fields };
}

function completions(docPath, text, line, character) {
  if (path.basename(docPath) === 'loom.om') return settings(text, line, character);
  const t = loom.open(docPath, text);
  if (!t) return [];
  if (inCommentOrLiteral(t.toks, text, line, character)) return [];
  const cache = new Map();
  const cx = {
    t,
    line,
    character,
    read(file) {
      if (!cache.has(file)) cache.set(file, file ? loom.readText(file) : null);
      return cache.get(file);
    },
  };

  const inside = t.toks.find((k) => k.line === line && k.s < character &&
    (k.t === 'id' ? character <= k.e : k.t === 'str' && (character < k.e || !k.closed)));
  if (inside) {
    const ref = t.refs.find((r) => r.tok === inside);
    if (!ref) return [];
    const whole = { line, s: inside.s, e: inside.e };
    if (inside.t === 'str') {
      if (ref.what === 'step') return stepItems(cx, ref.chain, ref.index, whole, true);
      if (ref.what === 'arg') return argItems(cx, ref.argOf, whole, true);
      if (ref.what === 'import' && ref.tok === ref.imp.spec) return pathItems(cx, ref.imp, inside);
      return [];
    }
    if (ref.what === 'step') return stepItems(cx, ref.chain, ref.index, whole, false);
    if (ref.what === 'pred') return predicateItems(whole);
    if (ref.what === 'root' && ref.chain.argOf) return argItems(cx, ref.chain.argOf, whole, false);
    if (ref.what === 'root' && isFirstOnLine(t.toks, inside)) return statementItems(whole);
    return [];
  }

  // Nothing is being typed: what comes before the cursor decides.
  const k = loom.lastBefore(t.toks, line, character);
  const prev = k >= 0 ? t.toks[k] : null;
  const empty = { line, s: character, e: character };
  if (!prev || prev.t === 'nl') {
    const call = loom.enclosingCall(t, k, line);
    return call ? argItems(cx, call, empty, false) : statementItems(empty);
  }
  if (prev.t === '[' || prev.t === 'and' || prev.t === 'or') return predicateItems(empty);
  if (prev.t === '(' || prev.t === ',' || prev.t === '{') {
    const call = loom.enclosingCall(t, k, line);
    return call ? argItems(cx, call, empty, false) : [];
  }
  if (prev.t !== '.' || k === 0) return [];
  const owner = t.toks[k - 1];
  if (owner.t === ']') {
    // after a predicate: base.sections[level == 2].
    let depth = 0;
    let open = k - 1;
    for (; open >= 0; open--) {
      if (t.toks[open].t === ']') depth++;
      else if (t.toks[open].t === '[' && --depth === 0) break;
    }
    const ref = open > 0 && loom.stepRef(t, t.toks[open - 1]);
    return ref ? stepItems(cx, ref.chain, ref.index + 1, empty, false) : [];
  }
  if (owner.t === ')') {
    // only .as(type) is followed by more steps
    const open = matchingOpen(t.toks, k - 1);
    const ref = open > 0 && loom.stepRef(t, t.toks[open - 1]);
    return ref ? stepItems(cx, ref.chain, ref.index + 1, empty, false) : [];
  }
  const ref = t.refs.find((r) => r.tok === owner && (r.what === 'root' || r.what === 'step'));
  if (!ref) return [];
  return stepItems(cx, ref.chain, ref.what === 'root' ? 0 : ref.index + 1, empty, false);
}


function isFirstOnLine(toks, tok) {
  const k = toks.indexOf(tok);
  return k === 0 || toks[k - 1].t === 'nl';
}


// inCommentOrLiteral: nothing is completed after // or inside a `literal`.
function inCommentOrLiteral(toks, text, line, character) {
  for (const k of toks) {
    if (k.t !== 'raw') continue;
    const afterStart = line > k.line || (line === k.line && character > k.s);
    const beforeEnd = line < k.endLine || (line === k.endLine && character < k.e);
    if (afterStart && beforeEnd) return true;
  }
  const src = text.split('\n')[line] || '';
  for (let at = src.indexOf('//'); at !== -1 && at < character; at = src.indexOf('//', at + 1)) {
    if (!toks.some((k) => k.t === 'str' && k.line === line && k.s < at && at < k.e)) return true;
  }
  return false;
}

function matchingOpen(toks, k) {
  let depth = 0;
  for (let i = k; i >= 0; i--) {
    if (toks[i].t === ')') depth++;
    else if (toks[i].t === '(' && --depth === 0) return i;
  }
  return -1;
}

// ── after a dot ─────────────────────────────────────────────────────────────

function stepItems(cx, chain, index, range, quoted) {
  const r = loom.walk(cx.t, chain, index);
  if (!r || r.bad || r.method || r.asked) return [];
  const out = [];
  if (r.place) return chain.root.v === 'base' && !quoted ? methodItems(r, range) : [];
  if (r.many) {
    if (quoted) return [];
    out.push(item('first', 'part', { detail: 'the first of the group', markdown: markdownFor('first'), range, sortText: '1' }));
    out.push(item('last', 'part', { detail: 'the last of the group', markdown: markdownFor('last'), range, sortText: '1' }));
    for (const q of loom.QUESTIONS) out.push(item(q, 'part', { detail: q === 'any' ? 'if: did the predicate find anything' : 'if: how many it found; holds when not zero', markdown: markdownFor(q), range, sortText: '3' }));
    if (chain.root.v === 'base' && !chain.argOf) out.push(...methodItems(r, range));
    return out;
  }
  const text = cx.read(r.obj.file);
  if (text != null && !(r.node && r.node.axis)) {
    const src = { ...loom.viewLines(text, r.view), where: path.relative(cx.t.cfg.root, r.obj.file), typ: r.typ };
    // The compiler writes the two sides differently, and this follows it: an anchor into upstream
    // is written as NameText does it, because lm sync rewrites that token when upstream renames
    // something; content of ours is always quoted, because that is what anchor completion appends.
    // Matching both is what keeps a template from being rewritten the moment the build touches it.
    const name = r.obj.layer === 'base' ? (n) => nodeTextFor(n, cx.t.cfg.loom) : loom.quote;
    const write = quoted
      ? (parts) => ({ insertText: parts.map(loom.quote).join('.'), filterText: loom.quote(parts[parts.length - 1]) })
      : (parts) => ({
        insertText: parts.length === 1 ? name(parts[0]) : parts.map(loom.quote).join('.'),
        filterText: typedAs(parts[parts.length - 1]),
      });
    if (!r.node) {
      out.push(...nodeItems(src, loom.DEFAULT_KIND[r.typ], null, write, range));
    } else if (r.node.kind === 'frontmatter') {
      // the keys of the frontmatter: base.frontmatter.description
      out.push(...nodeItems(src, 'fmkey', null, write, range));
    } else if (r.node.kind === loom.DEFAULT_KIND[r.typ] && loom.isValueType(r.typ) && !r.view) {
      // below a key, the keys nested under it: base.jobs.build. They are named by the whole path,
      // so the item shows and inserts only the segment being added.
      const prefix = `${r.node.name}.`;
      for (const n of loom.nodesOf(src.lines.join('\n'), r.node.kind, r.typ)) {
        if (!n.name.startsWith(prefix) || n.name.slice(prefix.length).includes('.')) continue;
        const seg = n.name.slice(prefix.length);
        out.push(item(seg, 'node', { detail: `${src.where}:${n.line + 1}`, ...write([seg]), range }));
      }
    } else if (r.node.kind === 'heading') {
      out.push(...axisItems(range));
      // below a section, only its subsections: base."Example 2"."Phase 1"
      const sel = loom.findNode(text, r.node, r.view);
      if (sel) {
        const within = [...(r.node.within || []), { name: r.node.name, ident: r.node.ident }];
        out.push(...nodeItems(src, 'heading', { from: sel.line - src.offset, within }, write, range));
      }
    }
  }
  // where an axis landed has no name to list subsections of, but can walk on
  if (r.node && r.node.axis && r.node.kind === 'heading' && !quoted) out.push(...axisItems(range));
  if (quoted) return out;

  if (r.has) return out;
  if (!r.node) {
    for (const [word, kind] of Object.entries(loom.CLASS_CALLS[r.typ] || {})) {
      out.push(item(word, 'part', { detail: `every ${kind}; [ ... ] picks some`, markdown: markdownFor(word), range, sortText: '3' }));
    }
    if (chain.root.v === 'base') out.push(item('has', 'part', { detail: 'if: does upstream have a part by this name', markdown: markdownFor('has'), insertText: 'has.', retrigger: true, range, sortText: '4' }));
    if (r.typ === 'markdown' && !r.view) out.push(item('frontmatter', 'part', { detail: 'the frontmatter', markdown: markdownFor('frontmatter'), range, sortText: '1' }));
    if (r.obj.layer === 'self' && r.typ === 'markdown') out.push(item('body', 'part', { detail: 'everything after the frontmatter', markdown: markdownFor('body'), range, sortText: '1' }));
    for (const call of Object.keys(loom.KIND_CALLS[r.typ] || {})) {
      out.push(item(call, 'kind', { detail: `${call}("...")`, markdown: markdownFor(call), insertText: `${call}("$1")`, snippet: true, retrigger: call !== 'line', range, sortText: '3' }));
    }
  }
  if (chain.root.v === 'base' && !chain.argOf) out.push(...methodItems(r, range));
  return out;
}

function axisItems(range) {
  return [['children', 'the sections one level down'], ['next', 'the next section at this level'], ['prev', 'the one before'], ['parent', 'the section this one sits in']]
    .map(([a, why]) => item(a, 'part', { detail: why, markdown: markdownFor(a), range, sortText: '3' }));
}

function methodItems(r, range) {
  return methodsFor(r).map((m) => {
    const insertText = m === 'replace' && !r.node && !r.view ? 'replace(self, reason: "$1")' : SNIPPETS[m];
    const retrigger = !['drop', 'replace', 'unwrap', 'join', 'promote', 'demote'].includes(m);
    return item(m, 'method', { detail: METHOD_DOCS[m], markdown: markdownFor(m), insertText, snippet: true, retrigger, range, sortText: '2' });
  });
}

// NODE_METHODS are what any single node takes; a markdown section takes the heading operations too.
const NODE_METHODS = ['after', 'before', 'replace', 'drop', 'move', 'wrap', 'swap'];

// methodsFor: the methods the compiler accepts on what a chain selects (objparse.go's edit).
function methodsFor(r) {
  if (r.place) return ['project'];
  if (!r.node) {
    const m = ['start', 'append', 'end'];
    if (!r.view) m.push('replace', 'merge');
    return m;
  }
  // a group takes what means the same done to each of them
  if (r.many) return r.node.kind === 'heading' ? ['drop', 'promote', 'demote', 'unwrap'] : ['drop'];
  switch (r.node.kind) {
    case 'frontmatter':
      return ['set'];
    case 'fmkey':
      return ['set', 'start', 'append', 'end'];
    case 'body':
      return [];
    case 'key':
    case 'path':
      return r.view ? NODE_METHODS : [...NODE_METHODS, 'set', 'start', 'append', 'end', 'as'];
    case 'heading':
      return [...NODE_METHODS, 'promote', 'demote', 'unwrap', 'split', 'join'];
    default:
      return NODE_METHODS;
  }
}

// predicateItems: what a predicate can ask about each node, inside base.sections[ ... ].
const PREDICATES = {
  level: ['level == ${1:2}', 'the heading level, 1 to 6: == != < <= > >='],
  name: ['name ${1|==,~|} "$2"', 'the name: == exactly, ~ a regular expression'],
  value: ['value == "$1"', 'what a key holds'],
  empty: ['empty', 'nothing under it'],
  calls: ['calls "$1"', 'a shell function that runs this command'],
  has: ['has."$1"', 'a part by this name inside it'],
};

function predicateItems(range) {
  return Object.entries(PREDICATES).map(([word, [insertText, detail]]) =>
    item(word, 'argument', { detail, markdown: markdownFor(word), insertText, snippet: true, range, sortText: '1' }));
}

// nodeItems lists the nodes of one kind in file order. `chapter` narrows headings to one
// section's chapter, reached by the path `within`. A heading name that is not unique there comes
// with the parent headings that tell it apart — a path the compiler resolves back to exactly
// that heading, as anchor completion writes it; a heading no path tells apart is left out.
// `write` turns the path into insertText and filterText for where the item goes, or null to skip it.
function nodeItems(src, kind, chapter, write, range) {
  if (kind === 'line') return [];
  const { lines, offset, where } = src;
  const text = lines.join('\n');
  const all = kind === 'heading' ? loom.headings(lines) : loom.nodesOf(text, kind, src.typ);
  let nodes = all;
  if (chapter) {
    const top = all.find((h) => h.line === chapter.from);
    nodes = top ? all.filter((h) => h.line > top.line && h.line < top.chapter) : [];
  }
  const out = [];
  nodes.forEach((n, k) => {
    const parts = kind === 'heading' ? headingPath(text, all, n, chapter) : [n.name];
    const written = parts && write(parts);
    if (!written) return;
    out.push(item(parts.join(' › '), kind === 'heading' ? 'section' : 'node', {
      detail: `${n.level ? '#'.repeat(n.level) + ' ' : ''}${where}:${n.line + offset + 1}`,
      documentation: lines.slice(n.line, Math.min(n.end, n.line + 8)).join('\n').trim(),
      lang: LANGS[kind],
      ...written,
      range,
      sortText: `0${String(k).padStart(5, '0')}`,
    }));
  });
  return out;
}

function headingPath(text, all, n, chapter) {
  const within = chapter ? chapter.within : [];
  const lands = (path) => {
    const hit = loom.findNode(text, { kind: 'heading', name: n.name, ident: false, within: [...within, ...path] });
    return hit && hit.line === n.line;
  };
  if (lands([])) return [n.name];
  const parents = []; // nearest first, inside the chapter
  let level = n.level;
  for (let i = all.indexOf(n) - 1; i >= 0 && !(chapter && all[i].line <= chapter.from); i--) {
    if (all[i].level < level) {
      parents.push(all[i].name);
      level = all[i].level;
    }
  }
  for (let depth = 1; depth <= parents.length; depth++) {
    const names = parents.slice(0, depth).reverse();
    if (lands(names.map((name) => ({ name, ident: false })))) return [...names, n.name];
  }
  return null;
}

// ── inside a call ───────────────────────────────────────────────────────────

function argItems(cx, argOf, range, quoted) {
  const { chain, index } = argOf;
  const st = chain.steps[index];
  const r = loom.walk(cx.t, chain, index);
  if (!r || r.bad) return [];
  const self = cx.t.objects.self;
  const single = (parts) => parts.length === 1;
  const asString = (parts) => ({ insertText: loom.quote(parts[0]), filterText: quoted ? loom.quote(parts[0]) : typedAs(parts[0]) });
  const source = (file, view) => {
    const text = cx.read(file);
    // r.typ is the type after any as(...) view: inside as(markdown) the lines are markdown,
    // whatever the file around them is.
    return text == null ? null : { ...loom.viewLines(text, view), where: path.relative(cx.t.cfg.root, file), typ: r.typ };
  };

  // .section("...") / .function("...") / .key("..."): that kind of node in the file being changed
  const kind = (loom.KIND_CALLS[r.typ] || {})[st.name];
  if (kind) {
    const src = source(r.obj.file, r.view);
    if (!src) return [];
    return nodeItems(src, kind, null, (parts) => (single(parts) ? asString(parts) : null), range);
  }
  // .sections(|) / .keys(|) / .lines(|): a group takes predicates, not a name
  const cls = (loom.CLASS_CALLS[r.typ] || {})[st.name];
  if (cls) {
    if (quoted) return [];
    const out = [
      item('match:', 'argument', { detail: 'names matching this regular expression', insertText: 'match: "$1"', snippet: true, range, sortText: '0' }),
      item('empty', 'argument', { detail: 'only nodes with nothing under them', range, sortText: '2' }),
    ];
    if (cls === 'heading') {
      out.push(item('level:', 'argument', { detail: 'headings at this depth, 1 to 6', insertText: 'level: ${1:2}', snippet: true, range, sortText: '1' }));
    }
    return out;
  }
  if (st.name === 'as') {
    return quoted ? [] : [...loom.TYPES].map((ty) => item(ty, 'type', { range }));
  }
  if (st.name === 'merge') {
    return quoted ? [] : [item('self', 'object', { detail: 'our file at this path', range })];
  }
  // a key's value: ours, from the same kind of key in our file
  if (['set', 'start', 'append', 'end'].includes(st.name) && loom.isValue(r)) {
    if (quoted) return [];
    const src = source(self.file, null);
    if (!src) return [];
    const fm = r.node.kind === 'fmkey';
    const keys = loom.nodesOf(src.lines.join('\n'), fm ? 'fmkey' : loom.DEFAULT_KIND[r.typ], r.typ);
    return keys
      .map((n, k) => {
        const written = `self.${fm ? 'frontmatter.' : ''}${nodeText(n.name)}`;
        return item(written, 'node', { detail: `${src.where}:${n.line + 1}`, insertText: written, range, sortText: `${n.name === r.node.name ? 0 : 1}${String(k).padStart(5, '0')}` });
      })
      .sort((a, b) => a.sortText.localeCompare(b.sortText));
  }
  // where a node goes, or what it trades places with: a node of upstream
  if (st.name === 'move' || st.name === 'swap' || st.name === 'split') {
    if (quoted) return [];
    const out = [item('base', 'object', { detail: 'a node of upstream', insertText: 'base.', retrigger: true, range, sortText: '1' })];
    if (st.name === 'move') {
      for (const side of ['after', 'before']) out.push(item(`${side}:`, 'argument', { detail: `${side} this node`, insertText: `${side}: base.`, retrigger: true, range, sortText: '0' }));
    }
    return out;
  }
  if (!loom.CONTENT_METHODS.has(st.name) && st.name !== 'drop') return [];
  // From Loom 2 a bare string is the text itself, not a name: nothing to complete inside one, and
  // our sections are written on self.
  const literal = loom.literalNames(cx.t);
  if (literal && quoted) return [];
  if (st.name === 'set' && r.node && r.node.kind === 'frontmatter') {
    return quoted ? [] : [item('self.frontmatter', 'part', { detail: 'frontmatter can only be set to another frontmatter', range })];
  }

  const out = [];
  if (st.name !== 'drop') {
    // a bare string names our content; a path is not a string, so it is written on self
    const src = source(self.file, r.view);
    const write = (parts) => (single(parts) && !literal
      ? asString(parts)
      : { insertText: `self.${parts.map(loom.quote).join('.')}`, filterText: quoted ? loom.quote(parts[parts.length - 1]) : typedAs(parts[parts.length - 1]) });
    if (src) out.push(...nodeItems(src, loom.DEFAULT_KIND[r.typ], null, write, range));
  }
  if (quoted) return out;
  if (st.name !== 'drop') {
    for (const [name, obj] of Object.entries(cx.t.objects)) {
      if (obj.layer === 'self' && obj.file) out.push(item(name, 'object', { detail: path.relative(cx.t.cfg.root, obj.file), range, sortText: '1' }));
    }
  }
  if (st.name === 'replace' || st.name === 'drop') {
    out.push(item('reason:', 'argument', { detail: 'why upstream content is changed', markdown: markdownFor('reason'), insertText: 'reason: "$1"', snippet: true, range, sortText: '2' }));
  }
  return out;
}

// ── import paths ────────────────────────────────────────────────────────────

function pathItems(cx, imp, tok) {
  const typed = imp.spec.v.slice(0, cx.character - tok.s - 1);
  const layer = imp.name && imp.name.v === 'base' ? 'base' : 'self';
  const start = tok.s + 1;
  const end = tok.closed ? tok.e - 1 : tok.e;
  if (!/^(\/|\.\/|\.\.\/)/.test(typed)) {
    const range = { line: cx.line, s: start, e: end };
    return ['/', './', '../'].map((p) => item(p, 'folder', { detail: p === '/' ? `the root of the ${layer} layer` : "relative to the product's directory", range, retrigger: true }));
  }
  const slash = typed.lastIndexOf('/');
  const rel = loom.importRel(typed.slice(0, slash + 1), cx.t.target);
  const root = loom.layerDir(cx.t.cfg, layer);
  if (rel == null || !root) return [];
  let entries;
  try {
    entries = fs.readdirSync(path.join(root, rel), { withFileTypes: true });
  } catch {
    return [];
  }
  const range = { line: cx.line, s: start + slash + 1, e: end };
  // templates live beside our files; they are not something to import
  const files = entries.filter((d) => d.isFile() && !d.name.endsWith('.lm') && d.name !== '.DS_Store');
  const stems = new Map();
  for (const f of files) {
    const stem = f.name.replace(/\.[^.]*$/, '');
    stems.set(stem, (stems.get(stem) || 0) + 1);
  }
  const out = entries.filter((d) => d.isDirectory()).map((d) => item(`${d.name}/`, 'folder', { range, retrigger: true, sortText: `0${d.name}` }));
  for (const f of files) {
    const stem = f.name.replace(/\.[^.]*$/, '');
    // the extension may be left out when no other file has that name
    const name = stem !== f.name && stems.get(stem) === 1 ? stem : f.name;
    out.push(item(name, 'file', { detail: f.name, range, sortText: `1${name}` }));
  }
  return out;
}

// ── statements and settings ─────────────────────────────────────────────────

function statementItems(range) {
  return [
    item('base', 'object', { detail: 'the upstream file at this path; the only object a statement changes', insertText: 'base.', retrigger: true, range }),
    item('import', 'keyword', { detail: 'import [name] "path": another file of our layer', markdown: markdownFor('import'), insertText: 'import "$1"', snippet: true, retrigger: true, range }),
    item('if', 'keyword', { detail: 'ask the document something, and write only if it holds', markdown: markdownFor('if'), insertText: 'if base.has.${1:Name} {\n\t$0\n}', snippet: true, range }),
    item('if / else if', 'keyword', { detail: 'a chain of questions: the first that holds decides', insertText: 'if base.has.${1:One} {\n\t$2\n} else if base.has.${3:Two} {\n\t$0\n}', snippet: true, range }),
    item('fn', 'keyword', { detail: 'group statements inside this file; inlined where it is called', markdown: markdownFor('fn'), insertText: 'fn ${1:name}() {\n\t$0\n}', snippet: true, range }),
    item('return', 'keyword', { detail: 'return self: the product is our file; needs a reason', markdown: markdownFor('return'), insertText: 'return self // reason: $1', snippet: true, range }),
    item('Self', 'keyword', { detail: 'a resource section: our content, written in this file', markdown: markdownFor('Self'), insertText: '---\nSelf:\n    ```${1:markdown}\n    $0\n    ```', snippet: true, range }),
  ];
}

function settings(text, line, character) {
  const typed = (text.split('\n')[line] || '').slice(0, character);
  const m = /^(\s*)([A-Za-z]*)$/.exec(typed);
  if (!m) return [];
  const range = { line, s: m[1].length, e: m[1].length + m[2].length };
  return SETTINGS.map(([label, insertText, detail]) => item(label, 'keyword', { detail, insertText, snippet: true, range }));
}

module.exports = { completions, nodeText, nodeTextFor };
