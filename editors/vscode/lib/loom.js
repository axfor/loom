'use strict';

// The model shared by go to definition and completion: settings, the template's tokens and
// statements, what a chain of steps selects, and the nodes of the files a template names.
//
// No `vscode` import, so all of it runs (and is tested) in plain Node. Every rule here mirrors
// the compiler (ast/*.go, objparse.go, weave.go); where the two disagree, the compiler is right.
//
// The tables below are held to that by lang/mirror_test.go, which runs this module and compares
// what it exports against the compiler's own tables. A mirror that drifts raises no error of its
// own — it produces an editor that quietly lies about a correct template — so something has to
// watch, and that is what watches.

const fs = require('fs');
const path = require('path');

const METHODS = new Set(['after', 'before', 'start', 'append', 'replace', 'drop', 'move', 'promote', 'demote', 'set', 'merge']);
const CONTENT_METHODS = new Set(['after', 'before', 'start', 'append', 'replace', 'set']);
// The plural form selects a group and takes predicates instead of a name.
const CLASS_CALLS = {
  markdown: { sections: 'heading', lines: 'line' },
  shell: { functions: 'function', markers: 'marker', lines: 'line' },
  toml: { keys: 'key' },
  yaml: { keys: 'key' },
  json: { keys: 'path' },
  text: { lines: 'line' },
};
const KIND_CALLS = {
  markdown: { section: 'heading', line: 'line' },
  shell: { function: 'function', marker: 'marker', line: 'line' },
  toml: { key: 'key' },
  yaml: { key: 'key' },
  json: { key: 'path' },
  text: { line: 'line' },
};
const DEFAULT_KIND = { markdown: 'heading', shell: 'function', toml: 'key', yaml: 'key', json: 'path', text: 'line' };
const TYPES = new Set(Object.keys(DEFAULT_KIND));

function typeOf(p) {
  switch (path.extname(p).toLowerCase()) {
    case '.md':
    case '.markdown':
      return 'markdown';
    case '.toml':
      return 'toml';
    case '.yaml':
    case '.yml':
      return 'yaml';
    case '.json':
      return 'json';
    case '.sh':
    case '.bash':
      return 'shell';
    default:
      return 'text';
  }
}

function isFile(p) {
  try {
    return fs.statSync(p).isFile();
  } catch {
    return false;
  }
}

function readText(p) {
  try {
    return fs.readFileSync(p, 'utf8');
  } catch {
    return null;
  }
}

// ── settings ────────────────────────────────────────────────────────────────

// findConfig walks up from the template to the nearest loom.om and reads the three
// settings that decide where files live.
function findConfig(from) {
  let dir = path.dirname(from);
  for (;;) {
    const p = path.join(dir, 'loom.om');
    if (isFile(p)) {
      const cfg = { root: dir, base: null, self: null, templates: null, output: null };
      for (const line of fs.readFileSync(p, 'utf8').split('\n')) {
        const m = /^\s*(base|self|templates|output)\s+"((?:[^"\\]|\\.)*)"/.exec(line);
        if (m) cfg[m[1]] = unquote(m[2]);
      }
      // without a templates setting, templates live next to our files in the self layer
      if (cfg.templates == null) cfg.templates = cfg.self != null ? cfg.self : 'templates';
      if (cfg.base) return cfg;
    }
    const parent = path.dirname(dir);
    if (parent === dir) return null;
    dir = parent;
  }
}

function unquote(s) {
  return s.replace(/\\(["\\])/g, '$1');
}

// quote writes a Loom string: the only escapes are \" and \\.
function quote(s) {
  return `"${s.replace(/[\\"]/g, '\\$&')}"`;
}

function layerDir(cfg, layer) {
  const d = layer === 'base' ? cfg.base : cfg.self;
  return d == null ? null : path.join(cfg.root, d);
}

// withExtension lists the files named rel plus an extension in a layer root (rel.md, rel.min.js),
// templates left out, as slash paths relative to the root. Case is ignored, as the compiler does:
// on macOS and Windows GUIDE.md and guide.sh would want templates that are one file.
function withExtension(root, rel) {
  const abs = path.join(root, rel);
  let ents;
  try {
    ents = fs.readdirSync(path.dirname(abs), { withFileTypes: true });
  } catch {
    return [];
  }
  const stem = path.basename(abs) + '.';
  return ents
    .filter((e) => !e.isDirectory() && e.name.toLowerCase().startsWith(stem.toLowerCase()) && !e.name.endsWith('.lm'))
    .map((e) => path.posix.join(path.posix.dirname(rel), e.name));
}

// targetOf mirrors the compiler: the template's path under the templates directory, minus .lm,
// names the product. The extension may be left out (SKILL.lm builds SKILL.md): a file named exactly
// so in a layer wins, else the one file with that name and an extension; several is null, the
// compiler's error; none is the name as written.
function targetOf(cfg, doc) {
  const rel = path.relative(path.join(cfg.root, cfg.templates), doc);
  if (rel.startsWith('..') || path.isAbsolute(rel) || !rel.endsWith('.lm')) return null;
  const name = rel.slice(0, -3).split(path.sep).join('/');
  const roots = ['base', 'self'].map((l) => layerDir(cfg, l)).filter(Boolean);
  if (roots.some((r) => isFile(path.join(r, name)))) return name;
  const found = [...new Set(roots.flatMap((r) => withExtension(r, name)))];
  if (found.length === 0) return name;
  if (found.length === 1 && path.posix.basename(found[0]).startsWith(path.posix.basename(name) + '.')) return found[0];
  return null;
}

// importRel is an import path relative to its layer root, or null when it leaves the layer.
function importRel(spec, target) {
  let rel;
  if (spec.startsWith('/')) rel = path.posix.normalize(spec.slice(1) || '.');
  else if (spec.startsWith('./') || spec.startsWith('../')) rel = path.posix.normalize(path.posix.join(path.posix.dirname(target), spec));
  else return null;
  if (rel === '..' || rel.startsWith('../')) return null;
  return rel;
}

// resolveImport mirrors the compiler: ./ and ../ are relative to the product's directory,
// / starts at the layer root, and a missing extension is fine when exactly one file matches
// (a template next to the file does not count).
function resolveImport(cfg, layer, spec, target) {
  const root = layerDir(cfg, layer);
  const rel = importRel(spec, target);
  if (!root || rel == null) return null;
  const abs = path.join(root, rel);
  if (isFile(abs)) return abs;
  if (path.posix.extname(rel) !== '') return null;
  const hits = withExtension(root, rel);
  return hits.length === 1 ? path.join(root, hits[0]) : null;
}

// ── tokens and statements ───────────────────────────────────────────────────

// lex returns tokens with their line and UTF-16 columns [s, e), the unit VS Code positions use.
function lex(text) {
  const toks = [];
  let i = 0;
  let line = 0;
  let lineStart = 0;
  const ident = /[\p{L}_][\p{L}\p{N}_]*/uy;
  const push = (t, s, e, v) => toks.push({ t, v, line, s: s - lineStart, e: e - lineStart });
  while (i < text.length) {
    const c = text[i];
    if (c === '\n') {
      push('nl', i, i + 1);
      i++;
      line++;
      lineStart = i;
      continue;
    }
    if (c === ' ' || c === '\t' || c === '\r') {
      i++;
      continue;
    }
    if (c === '/' && text[i + 1] === '/') {
      while (i < text.length && text[i] !== '\n') i++;
      continue;
    }
    if (c === '"') {
      let j = i + 1;
      let v = '';
      while (j < text.length && text[j] !== '"' && text[j] !== '\n') {
        if (text[j] === '\\' && (text[j + 1] === '"' || text[j + 1] === '\\')) {
          v += text[j + 1];
          j += 2;
          continue;
        }
        v += text[j++];
      }
      const closed = text[j] === '"';
      const end = closed ? j + 1 : j;
      toks.push({ t: 'str', v, line, s: i - lineStart, e: end - lineStart, closed });
      i = end;
      continue;
    }
    if (c === '`') {
      // A literal may span lines. It is one token that starts on its first line; the lines
      // it covers belong to it, so the line counter still has to walk through them.
      const startLine = line;
      const s = i - lineStart;
      let j = i + 1;
      while (j < text.length && text[j] !== '`') {
        if (text[j] === '\n') {
          line++;
          lineStart = j + 1;
        }
        j++;
      }
      toks.push({ t: 'raw', line: startLine, s, endLine: line, e: j + 1 - lineStart });
      i = j + 1;
      continue;
    }
    ident.lastIndex = i;
    const m = ident.exec(text);
    if (m) {
      push('id', i, i + m[0].length, m[0]);
      i += m[0].length;
      continue;
    }
    if ('.,:(){}'.includes(c)) push(c, i, i + 1);
    i++;
  }
  push('eof', i, i);
  return toks;
}

// parse reads imports and statements the way the compiler does, but tolerantly: a template
// being edited is usually half-written. Every name it meets is recorded with what it is.
function parse(toks) {
  const refs = [];
  const imports = [];
  let i = 0;
  const peek = (k = 0) => toks[Math.min(i + k, toks.length - 1)];
  const skipNl = () => {
    while (peek().t === 'nl') i++;
  };

  function expr(argOf) {
    const root = toks[i++];
    const chain = { root, steps: [], argOf };
    refs.push({ what: 'root', tok: root, chain });
    while (peek().t === '.') {
      i++;
      const n = peek();
      if (n.t === 'str') {
        i++;
        chain.steps.push({ name: n.v, str: true, tok: n, args: [] });
        refs.push({ what: 'step', tok: n, chain, index: chain.steps.length - 1 });
        continue;
      }
      if (n.t !== 'id') break;
      i++;
      const step = { name: n.v, str: false, tok: n, call: false, args: [] };
      chain.steps.push(step);
      const index = chain.steps.length - 1;
      refs.push({ what: 'step', tok: n, chain, index });
      if (peek().t === '(' || peek().t === '{') {
        const close = peek().t === '(' ? ')' : '}';
        i++;
        step.call = true;
        args(close, { chain, index });
        if (close === '}') break;
      }
    }
    return chain;
  }

  function args(close, argOf) {
    for (;;) {
      if (close === '}') skipNl();
      const t = peek();
      if (t.t === close) {
        i++;
        return;
      }
      // (...) stays on one line; base and import are never arguments, so a block still open
      // when one of them starts a line is a block being written, not one that swallows the rest.
      if (t.t === 'eof' || (close === ')' && t.t === 'nl') || (t.t === 'id' && (t.v === 'base' || t.v === 'import'))) return;
      const step = argOf.chain.steps[argOf.index];
      if (t.t === 'id' && peek(1).t === ':') {
        i += 2;
        const v = peek();
        if (v.t === 'str' || v.t === 'raw') {
          i++;
          refs.push({ what: 'named', tok: v, argOf });
        }
        continue;
      }
      if (t.t === 'str' || t.t === 'raw') {
        i++;
        step.args.push(t);
        refs.push({ what: 'arg', tok: t, argOf });
      } else if (t.t === 'id') {
        step.args.push(t);
        expr(argOf);
      } else {
        i++; // `,` and anything the compiler would reject
      }
    }
  }

  for (;;) {
    skipNl();
    const t = peek();
    if (t.t === 'eof') break;
    if (t.t === 'id' && t.v === 'import') {
      i++;
      const imp = { name: null, spec: null };
      if (peek().t === 'id') imp.name = toks[i++];
      if (peek().t === 'str') imp.spec = toks[i++];
      if (imp.spec) {
        imports.push(imp);
        if (imp.name) refs.push({ what: 'import', tok: imp.name, imp });
        refs.push({ what: 'import', tok: imp.spec, imp });
      }
    } else if (t.t === 'id') {
      expr(null);
    } else {
      i++;
    }
    while (peek().t !== 'nl' && peek().t !== 'eof') i++;
  }
  return { refs, imports };
}

// open reads a template: its settings, product path, statements, and the objects it names.
function open(docPath, text) {
  const cfg = findConfig(docPath);
  if (!cfg) return null;
  const target = targetOf(cfg, docPath);
  if (!target) return null;
  const toks = lex(text);
  const { refs, imports } = parse(toks);
  const objects = {
    base: { layer: 'base', file: path.join(layerDir(cfg, 'base'), target), typ: typeOf(target) },
    self: { layer: 'self', file: cfg.self == null ? null : path.join(layerDir(cfg, 'self'), target), typ: typeOf(target) },
  };
  const importFile = new Map();
  for (const imp of imports) {
    const name = imp.name ? imp.name.v : path.posix.basename(imp.spec.v).replace(/\.[^.]*$/, '');
    const layer = name === 'base' ? 'base' : 'self';
    const file = resolveImport(cfg, layer, imp.spec.v, target);
    importFile.set(imp, file);
    if (name !== 'self' && file) objects[name] = { layer, file, typ: typeOf(file) };
  }
  return { cfg, target, docPath, toks, refs, imports, importFile, objects };
}

// walk follows the first `upto` steps of a chain the way the compiler does and says what they
// select: an object, the type in effect (after .as), a node, the key a view looks into, and
// the method that ended the chain. null when the root is not an object.
function walk(t, chain, upto) {
  const obj = t.objects[chain.root.v];
  if (!obj) return null;
  const r = { obj, typ: obj.typ, node: null, view: null, method: null, bad: false };
  if (chain.argOf && chain.root.v === 'self') {
    // Inside a view, self means the same view of our file.
    const outer = walk(t, chain.argOf.chain, chain.argOf.index);
    if (outer && outer.view) {
      r.typ = outer.typ;
      r.view = outer.view;
    }
  }
  for (let k = 0; k < upto && k < chain.steps.length; k++) {
    const st = chain.steps[k];
    if (r.method || r.bad) {
      r.bad = true;
      break;
    }
    if (st.call && st.name === 'as') {
      const a = st.args[0];
      if (a && TYPES.has(a.v) && r.node && (r.node.kind === 'key' || r.node.kind === 'path') && !r.view) {
        r.view = r.node.name;
        r.typ = a.v;
        r.node = null;
      } else r.bad = true;
      continue;
    }
    if (st.call && METHODS.has(st.name)) {
      r.method = st;
      continue;
    }
    if (r.node) {
      // a frontmatter key: base.frontmatter.description
      if (r.node.kind === 'frontmatter' && !st.call) {
        r.node = { kind: 'fmkey', name: st.name, ident: false };
        continue;
      }
      // a section path: base."Example 2"."Phase 1"
      if (st.call || r.node.kind !== 'heading') {
        r.bad = true;
        continue;
      }
      r.node = { kind: 'heading', name: st.name, ident: !st.str, within: [...(r.node.within || []), { name: r.node.name, ident: r.node.ident }] };
      continue;
    }
    if (st.call) {
      const kind = (KIND_CALLS[r.typ] || {})[st.name];
      const a = st.args[0];
      if (kind && a && a.t === 'str') r.node = { kind, name: a.v, ident: false };
      else r.bad = true;
    } else if (!st.str && st.name === 'frontmatter' && r.typ === 'markdown') {
      r.node = { kind: 'frontmatter' };
    } else if (!st.str && st.name === 'body' && obj.layer === 'self') {
      r.node = { kind: 'body' };
    } else {
      r.node = { kind: DEFAULT_KIND[r.typ], name: st.name, ident: !st.str };
    }
  }
  return r;
}

// isValue: a key whose value set / start / append write — a frontmatter key, or a toml / json key
// outside a view.
function isValue(r) {
  return Boolean(r && r.node && !r.view && (r.node.kind === 'fmkey' || ((r.typ === 'toml' || r.typ === 'json') && r.node.kind === DEFAULT_KIND[r.typ])));
}

// lastBefore: the index of the last token that ends before the cursor, or -1.
function lastBefore(toks, line, character) {
  let k = -1;
  while (k + 1 < toks.length && toks[k + 1].t !== 'eof' &&
    (toks[k + 1].line < line || (toks[k + 1].line === line && toks[k + 1].e <= character))) k++;
  return k;
}

function stepRef(t, tok) {
  return t.refs.find((r) => r.what === 'step' && r.tok === tok);
}

// enclosingCall finds the call whose arguments the token at k is among: an unclosed ( on the cursor's
// line, or an unclosed { since the statement started. It returns the call's step and where it opens.
function enclosingCall(t, k, line) {
  const toks = t.toks;
  let depth = 0;
  for (let i = k; i >= 0; i--) {
    const c = toks[i];
    if (c.t === ')' || c.t === '}') {
      depth++;
    } else if (c.t === '(' || c.t === '{') {
      if (depth > 0) {
        depth--;
        continue;
      }
      if (c.t === '(' && c.line !== line) return null; // (...) stays on one line
      const ref = i > 0 && stepRef(t, toks[i - 1]);
      return ref ? { chain: ref.chain, index: ref.index, open: i } : null;
    } else if (c.t === 'id' && (c.v === 'base' || c.v === 'import') && c.line !== line && (i === 0 || toks[i - 1].t === 'nl')) {
      return null; // an earlier statement starts here and none of its blocks is open
    }
  }
  return null;
}

// ── nodes in a file ─────────────────────────────────────────────────────────

// Go's RE2 \s is ASCII only; the compiler spells out the whitespace it means (ast/ws.go).
const WS = '[\\p{Z}\\t\\n\\f\\r\\v\\x1c-\\x1f\\x85]';
const RE_HEADING = new RegExp(`^(#{2,6})${WS}+(.*)$`, 'u');
const RE_TOML_KEY = new RegExp(`^([A-Za-z_][A-Za-z0-9_-]*)${WS}*=${WS}*(.*)$`, 'u');
const RE_FUNC = new RegExp(`^([A-Za-z_][A-Za-z0-9_]*)${WS}*\\(\\)${WS}*\\{`, 'u');
const RE_BANNER = new RegExp(`^${WS}*#${WS}*[─=—-]{2,}`, 'u');

function identMatch(name, ident) {
  const a = [...name];
  const b = [...ident];
  if (a.length !== b.length) return false;
  return a.every((ch, k) => ch === b[k] || (b[k] === '_' && ch === ' '));
}

function frontmatterEnd(lines) {
  if ((lines[0] || '').trim() !== '---') return -1;
  for (let k = 1; k < lines.length; k++) if (lines[k].trim() === '---') return k;
  return -1;
}

// headings: level 2 to 6 outside ``` fences. end is the next heading of any level (the section);
// chapter is the next heading of the same or a higher level.
function headings(lines) {
  const out = [];
  let fence = false;
  lines.forEach((l, i) => {
    if (l.trim().startsWith('```')) {
      fence = !fence;
      return;
    }
    if (fence) return;
    const m = RE_HEADING.exec(l);
    if (!m) return;
    const name = m[2].trim();
    out.push({ name, level: m[1].length, line: i, s: l.indexOf(name, m[1].length), end: lines.length, chapter: lines.length });
  });
  out.forEach((h, k) => {
    if (k + 1 < out.length) h.end = out[k + 1].line;
    const next = out.slice(k + 1).find((m) => m.level <= h.level);
    if (next) h.chapter = next.line;
  });
  return out;
}

function tomlKeys(lines) {
  const out = [];
  for (let i = 0; i < lines.length; i++) {
    const m = RE_TOML_KEY.exec(lines[i]);
    if (!m) continue;
    if (m[2].startsWith('"""')) {
      let j = i + 1;
      while (j < lines.length && !lines[j].startsWith('"""')) j++;
      out.push({ name: m[1], line: i, s: 0, end: j + 1, block: true });
      i = j;
      continue;
    }
    out.push({ name: m[1], line: i, s: 0, end: i + 1, block: false });
  }
  return out;
}

function shellFunctions(lines) {
  const out = [];
  lines.forEach((l, i) => {
    const m = RE_FUNC.exec(l);
    if (!m) return;
    const close = lines.indexOf('}', i + 1);
    if (close !== -1) out.push({ name: m[1], line: i, s: 0, end: close + 1 });
  });
  return out;
}

function shellMarkers(lines) {
  const at = [];
  lines.forEach((l, i) => {
    if (RE_BANNER.test(l)) at.push(i);
  });
  return at.map((i, k) => ({ name: lines[i].trim(), line: i, s: lines[i].indexOf(lines[i].trim()), end: k + 1 < at.length ? at[k + 1] : lines.length }));
}

function frontmatterKeys(lines) {
  const end = frontmatterEnd(lines);
  const out = [];
  for (let k = 1; k < end; k++) {
    const m = /^([^\s:#][^:]*?)\s*:/.exec(lines[k]);
    if (m) out.push({ name: m[1], line: k, s: 0, end: k + 1 });
  }
  return out;
}

// jsonPaths lists the dotted path of every object member; the line is where its key is written.
function jsonPaths(text) {
  let root;
  try {
    root = JSON.parse(text);
  } catch {
    return [];
  }
  const lines = text.split('\n');
  const out = [];
  let from = 0;
  const visit = (v, prefix) => {
    if (!v || typeof v !== 'object' || Array.isArray(v)) return;
    for (const k of Object.keys(v)) {
      const name = prefix ? `${prefix}.${k}` : k;
      const key = JSON.stringify(k);
      let line = lines.findIndex((l, i) => i >= from && l.includes(key));
      if (line === -1) line = 0;
      else from = line;
      out.push({ name, line, s: Math.max(lines[line].indexOf(key), 0), end: line + 1 });
      visit(v[k], name);
    }
  };
  visit(root, '');
  return out;
}

// nodesOf lists the addressable nodes of one kind: what a name in a template can mean.
function nodesOf(text, kind) {
  const lines = text.split('\n');
  switch (kind) {
    case 'heading':
      return headings(lines);
    case 'key':
      return tomlKeys(lines);
    case 'path':
      return jsonPaths(text);
    case 'function':
      return shellFunctions(lines);
    case 'marker':
      return shellMarkers(lines);
    case 'fmkey':
      return frontmatterKeys(lines);
    default:
      return [];
  }
}

// viewLines narrows a file to the value of a toml key written as a """ block, so a view's
// sections are found where the compiler looks for them. offset is the first line of the value.
function viewLines(text, key) {
  const lines = text.split('\n');
  if (!key) return { lines, offset: 0 };
  const k = tomlKeys(lines).find((n) => n.name === key && n.block);
  if (!k) return { lines, offset: 0 };
  return { lines: lines.slice(k.line + 1, k.end - 1), offset: k.line + 1 };
}

// pick is the compiler's rule for one name: an identifier matches through `_`, and the name must
// lead to exactly one node. Returns the node or null.
function pick(nodes, name, ident, match = (n, want) => n.name === want) {
  let want = name;
  if (ident) {
    const real = [...new Set(nodes.filter((n) => identMatch(n.name, name)).map((n) => n.name))];
    if (real.length !== 1) return null;
    want = real[0];
  }
  const hits = nodes.filter((n) => match(n, want));
  return hits.length === 1 ? hits[0] : null;
}

// findNode locates a node the way the compiler does: {line, end, s, e} with end exclusive, or null.
function findNode(text, node, view) {
  const { lines, offset } = viewLines(text, view);
  const shift = (n, name) => n && { line: n.line + offset, end: n.end + offset, s: Math.max(n.s, 0), e: Math.max(n.s, 0) + name.length };
  switch (node.kind) {
    case 'frontmatter': {
      const end = frontmatterEnd(lines);
      return end === -1 ? null : { line: offset, end: end + 1 + offset, s: 0, e: 3 };
    }
    case 'body': {
      let k = frontmatterEnd(lines) + 1;
      while (k < lines.length - 1 && lines[k].trim() === '') k++;
      return { line: k + offset, end: lines.length + offset, s: 0, e: 0 };
    }
    case 'heading': {
      const all = headings(lines);
      let scope = all;
      for (const seg of node.within || []) {
        const h = pick(scope, seg.name, seg.ident);
        if (!h) return null;
        scope = all.filter((m) => m.line > h.line && m.line < h.chapter);
      }
      const h = pick(scope, node.name, node.ident);
      return shift(h, h ? h.name : '');
    }
    case 'marker': {
      const hits = shellMarkers(lines).filter((m) => m.name.startsWith(node.name.trim()));
      return hits.length === 1 ? shift(hits[0], hits[0].name) : null;
    }
    case 'line': {
      const hits = [];
      lines.forEach((l, i) => {
        if (l === node.name) hits.push(i);
      });
      return hits.length === 1 ? { line: hits[0] + offset, end: hits[0] + 1 + offset, s: 0, e: lines[hits[0]].length } : null;
    }
    case 'path': {
      const n = nodesOf(lines.join('\n'), 'path').find((m) => m.name === node.name);
      return n ? shift(n, JSON.stringify(node.name.split('.').pop())) : null;
    }
    default: {
      const n = pick(nodesOf(lines.join('\n'), node.kind), node.name, node.ident);
      return shift(n, n ? n.name : '');
    }
  }
}

module.exports = {
  CLASS_CALLS,
  METHODS,
  CONTENT_METHODS,
  KIND_CALLS,
  DEFAULT_KIND,
  TYPES,
  typeOf,
  isFile,
  readText,
  findConfig,
  quote,
  layerDir,
  targetOf,
  importRel,
  resolveImport,
  lex,
  parse,
  open,
  walk,
  isValue,
  lastBefore,
  stepRef,
  enclosingCall,
  identMatch,
  headings,
  nodesOf,
  viewLines,
  findNode,
};
