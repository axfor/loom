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
};

const SETTINGS = [
  ['base', 'base "$1"', 'the upstream layer directory'],
  ['self', 'self "$1"', 'our layer directory'],
  ['templates', 'templates "$1"', 'where templates live (default: next to our files)'],
  ['output', 'output "$1"', 'the product directory'],
  ['mark', 'mark ${1|markdown,shell,toml,json,text|} "$2" "$3"', 'the begin / end marks around our content'],
  ['registry', 'registry "$1" "$2"', 'a group of files registered by a regex'],
  ['take', 'take "$1"', 'upstream files copied into the product as they are'],
  ['mirror', 'mirror "$1" "$2"', 'copy one product directory to another'],
  ['manifest', 'manifest "$1"', 'the file listing everything the build produced'],
];

// the language a node's text is shown in, in the item's documentation
const LANGS = { heading: 'markdown', fmkey: 'yaml', key: 'toml', path: 'json', function: 'shellscript', marker: 'shellscript' };

const IDENT = /^[\p{L}_][\p{L}\p{N}_]*$/u;
const RESERVED = new Set([...loom.METHODS, 'as', 'frontmatter', 'body', ...Object.values(loom.KIND_CALLS).flatMap(Object.keys)]);

// nodeText writes one name after a dot: an identifier when it is one, a string otherwise.
// An identifier with _ would also match a space, so such names are always strings.
function nodeText(name) {
  return IDENT.test(name) && !name.includes('_') && !RESERVED.has(name) ? name : loom.quote(name);
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
  if (prev.t === '(' || prev.t === ',' || prev.t === '{') {
    const call = loom.enclosingCall(t, k, line);
    return call ? argItems(cx, call, empty, false) : [];
  }
  if (prev.t !== '.' || k === 0) return [];
  const owner = t.toks[k - 1];
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
  if (!r || r.bad || r.method) return [];
  const out = [];
  const text = cx.read(r.obj.file);
  if (text != null) {
    const src = { ...loom.viewLines(text, r.view), where: path.relative(cx.t.cfg.root, r.obj.file) };
    const write = quoted
      ? (parts) => ({ insertText: parts.map(loom.quote).join('.'), filterText: loom.quote(parts[parts.length - 1]) })
      : (parts) => ({
        insertText: parts.length === 1 ? nodeText(parts[0]) : parts.map(loom.quote).join('.'),
        filterText: typedAs(parts[parts.length - 1]),
      });
    if (!r.node) {
      out.push(...nodeItems(src, loom.DEFAULT_KIND[r.typ], null, write, range));
    } else if (r.node.kind === 'frontmatter') {
      // the keys of the frontmatter: base.frontmatter.description
      out.push(...nodeItems(src, 'fmkey', null, write, range));
    } else if (r.node.kind === 'heading') {
      // below a section, only its subsections: base."Example 2"."Phase 1"
      const sel = loom.findNode(text, r.node, r.view);
      if (sel) {
        const within = [...(r.node.within || []), { name: r.node.name, ident: r.node.ident }];
        out.push(...nodeItems(src, 'heading', { from: sel.line - src.offset, within }, write, range));
      }
    }
  }
  if (quoted) return out;

  if (!r.node) {
    if (r.typ === 'markdown' && !r.view) out.push(item('frontmatter', 'part', { detail: 'the frontmatter', markdown: markdownFor('frontmatter'), range, sortText: '1' }));
    if (r.obj.layer === 'self' && r.typ === 'markdown') out.push(item('body', 'part', { detail: 'everything after the frontmatter', markdown: markdownFor('body'), range, sortText: '1' }));
    for (const call of Object.keys(loom.KIND_CALLS[r.typ] || {})) {
      out.push(item(call, 'kind', { detail: `${call}("...")`, markdown: markdownFor(call), insertText: `${call}("$1")`, snippet: true, retrigger: call !== 'line', range, sortText: '3' }));
    }
  }
  if (chain.root.v === 'base' && !chain.argOf) {
    for (const m of methodsFor(r)) {
      const insertText = m === 'replace' && !r.node && !r.view ? 'replace(self, reason: "$1")' : SNIPPETS[m];
      out.push(item(m, 'method', { detail: METHOD_DOCS[m], markdown: markdownFor(m), insertText, snippet: true, retrigger: m !== 'drop' && m !== 'replace', range, sortText: '2' }));
    }
  }
  return out;
}

// methodsFor: the methods the compiler accepts on what a chain selects.
function methodsFor(r) {
  if (!r.node) {
    const m = ['start', 'append'];
    if (!r.view) m.push('replace', 'merge');
    return m;
  }
  switch (r.node.kind) {
    case 'frontmatter':
      return ['set'];
    case 'fmkey':
      return ['set', 'start', 'append'];
    case 'body':
      return [];
    case 'key':
    case 'path':
      return r.view ? ['after', 'before', 'replace', 'drop'] : ['after', 'before', 'replace', 'drop', 'set', 'start', 'append', 'as'];
    default:
      return ['after', 'before', 'replace', 'drop'];
  }
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
  const all = kind === 'heading' ? loom.headings(lines) : loom.nodesOf(text, kind);
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
    return text == null ? null : { ...loom.viewLines(text, view), where: path.relative(cx.t.cfg.root, file) };
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
  if (['set', 'start', 'append'].includes(st.name) && loom.isValue(r)) {
    if (quoted) return [];
    const src = source(self.file, null);
    if (!src) return [];
    const fm = r.node.kind === 'fmkey';
    const keys = loom.nodesOf(src.lines.join('\n'), fm ? 'fmkey' : loom.DEFAULT_KIND[r.typ]);
    return keys
      .map((n, k) => {
        const written = `self.${fm ? 'frontmatter.' : ''}${nodeText(n.name)}`;
        return item(written, 'node', { detail: `${src.where}:${n.line + 1}`, insertText: written, range, sortText: `${n.name === r.node.name ? 0 : 1}${String(k).padStart(5, '0')}` });
      })
      .sort((a, b) => a.sortText.localeCompare(b.sortText));
  }
  if (!loom.CONTENT_METHODS.has(st.name) && st.name !== 'drop') return [];
  if (st.name === 'set' && r.node && r.node.kind === 'frontmatter') {
    return quoted ? [] : [item('self.frontmatter', 'part', { detail: 'frontmatter can only be set to another frontmatter', range })];
  }

  const out = [];
  if (st.name !== 'drop') {
    // a bare string names our content; a path is not a string, so it is written on self
    const src = source(self.file, r.view);
    const write = (parts) => (single(parts)
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
  ];
}

function settings(text, line, character) {
  const typed = (text.split('\n')[line] || '').slice(0, character);
  const m = /^(\s*)([A-Za-z]*)$/.exec(typed);
  if (!m) return [];
  const range = { line, s: m[1].length, e: m[1].length + m[2].length };
  return SETTINGS.map(([label, insertText, detail]) => item(label, 'keyword', { detail, insertText, snippet: true, range }));
}

module.exports = { completions };
