'use strict';

// Go to definition for Loom templates: what does the name under the cursor refer to?
//
//   import cmd "/.claude/commands/ship"   the path, or `cmd`  → that file in our layer
//   base / self / cmd                     the object          → its file
//   base.Install / base."How it compares" a node              → that heading (key, function...) upstream
//   .after / .drop                        a method            → the node it changes
//   "Install XSDD" inside .after(...)       content by name     → that heading in our file
//   self.frontmatter / cmd.body           a part of a file    → where that part starts
//   "description" inside .join(...)         a key               → that key in our file
//   "x.diff" inside .patch(...)             a patch             → the patch file next to the template
//
// No `vscode` import here, so the resolver runs (and is tested) in plain Node.
// It follows the same rules as the compiler: import paths, `_` matching a space, and which
// layer each object reads from. Where the compiler would reject the template, this returns
// what it can (the file) or nothing — never a guess at a different node.

const fs = require('fs');
const path = require('path');

const METHODS = new Set(['after', 'before', 'start', 'append', 'replace', 'drop', 'set', 'join', 'patch']);
const CONTENT_METHODS = new Set(['after', 'before', 'start', 'append', 'replace', 'set']);
const KIND_CALLS = {
  markdown: { section: 'heading', line: 'line' },
  shell: { function: 'function', marker: 'marker', line: 'line' },
  toml: { key: 'key' },
  json: { key: 'path' },
  text: { line: 'line' },
};
const DEFAULT_KIND = { markdown: 'heading', shell: 'function', toml: 'key', json: 'path', text: 'line' };
const TYPES = new Set(Object.keys(DEFAULT_KIND));

function typeOf(p) {
  switch (path.extname(p).toLowerCase()) {
    case '.md':
    case '.markdown':
      return 'markdown';
    case '.toml':
      return 'toml';
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

// ── settings ────────────────────────────────────────────────────────────────

// findConfig walks up from the template to the nearest loom.lm and reads the three
// settings that decide where files live.
function findConfig(from) {
  let dir = path.dirname(from);
  for (;;) {
    const p = path.join(dir, 'loom.lm');
    if (isFile(p)) {
      const cfg = { root: dir, base: null, self: null, templates: null };
      for (const line of fs.readFileSync(p, 'utf8').split('\n')) {
        const m = /^\s*(base|self|templates)\s+"((?:[^"\\]|\\.)*)"/.exec(line);
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

function layerDir(cfg, layer) {
  const d = layer === 'base' ? cfg.base : cfg.self;
  return d == null ? null : path.join(cfg.root, d);
}

// targetOf: the product path is the template's path under the templates directory, minus .lm.
function targetOf(cfg, doc) {
  const rel = path.relative(path.join(cfg.root, cfg.templates), doc);
  if (rel.startsWith('..') || path.isAbsolute(rel) || !rel.endsWith('.lm')) return null;
  return rel.slice(0, -3).split(path.sep).join('/');
}

// resolveImport mirrors the compiler: ./ and ../ are relative to the product's directory,
// / starts at the layer root, and a missing extension is fine when exactly one file matches.
function resolveImport(cfg, layer, spec, target) {
  const root = layerDir(cfg, layer);
  if (!root) return null;
  let rel;
  if (spec.startsWith('/')) rel = path.posix.normalize(spec.slice(1));
  else if (spec.startsWith('./') || spec.startsWith('../')) rel = path.posix.normalize(path.posix.join(path.posix.dirname(target), spec));
  else return null;
  if (rel === '..' || rel.startsWith('../')) return null;
  const abs = path.join(root, rel);
  if (isFile(abs)) return abs;
  if (path.posix.extname(rel) !== '') return null;
  let names;
  try {
    names = fs.readdirSync(path.dirname(abs));
  } catch {
    return null;
  }
  const base = path.basename(abs) + '.';
  const hits = names.filter((n) => n.startsWith(base) && isFile(path.join(path.dirname(abs), n)));
  return hits.length === 1 ? path.join(path.dirname(abs), hits[0]) : null;
}

// ── tokens and statements ───────────────────────────────────────────────────

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
      const end = text[j] === '"' ? j + 1 : j;
      push('str', i, end, v);
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
      skipNl();
      const t = peek();
      if (t.t === close) {
        i++;
        return;
      }
      if (t.t === 'eof') return;
      const step = argOf.chain.steps[argOf.index];
      if (t.t === 'id' && peek(1).t === ':') {
        i += 2;
        const v = peek();
        if (v.t === 'str' || v.t === 'raw') {
          i++;
          refs.push({ what: 'named', tok: v });
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

// ── finding a node in a file ────────────────────────────────────────────────

function identMatch(name, ident) {
  const a = [...name];
  const b = [...ident];
  if (a.length !== b.length) return false;
  return a.every((ch, k) => ch === b[k] || (b[k] === '_' && ch === ' '));
}

function escapeRe(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function frontmatterEnd(lines) {
  if ((lines[0] || '').trim() !== '---') return -1;
  for (let k = 1; k < lines.length; k++) if (lines[k].trim() === '---') return k;
  return -1;
}

// findNode returns the 0-based line of a node, or -1.
function findNode(text, node) {
  const lines = text.split('\n');
  const same = (got) => (node.ident ? identMatch(got, node.name) : got === node.name);
  switch (node.kind) {
    case 'frontmatter':
      return 0;
    case 'body': {
      let k = frontmatterEnd(lines) + 1;
      while (k < lines.length - 1 && lines[k].trim() === '') k++;
      return k;
    }
    case 'fmkey': {
      const end = frontmatterEnd(lines);
      for (let k = 1; k < end; k++) if (new RegExp(`^${escapeRe(node.name)}\\s*:`).test(lines[k])) return k;
      return -1;
    }
    case 'heading': {
      let fence = false;
      for (let k = 0; k < lines.length; k++) {
        if (/^\s*(```|~~~)/.test(lines[k])) fence = !fence;
        if (fence) continue;
        const m = /^\s{0,3}#{1,6}\s+(.*?)\s*$/.exec(lines[k]);
        if (m && same(m[1])) return k;
      }
      return -1;
    }
    case 'key':
    case 'path': {
      const last = node.kind === 'path' ? node.name.split('.').pop() : node.name;
      for (let k = 0; k < lines.length; k++) {
        const m = node.kind === 'path'
          ? /^\s*"((?:[^"\\]|\\.)*)"\s*:/.exec(lines[k])
          : /^\s*(?:\[\s*)?["']?([^"'=\]\s]+)["']?\s*(?:=|\])/.exec(lines[k]);
        if (m && (node.ident && node.kind === 'key' ? identMatch(m[1], last) : m[1] === last)) return k;
      }
      return -1;
    }
    case 'function':
      for (let k = 0; k < lines.length; k++) {
        const m = /^\s*(?:function\s+([\w.:-]+)|([\w.:-]+)\s*\(\s*\))/.exec(lines[k]);
        if (m && same(m[1] || m[2])) return k;
      }
      return -1;
    case 'marker':
      for (let k = 0; k < lines.length; k++) if (/^\s*#/.test(lines[k]) && lines[k].includes(node.name)) return k;
      return -1;
    case 'line':
      for (let k = 0; k < lines.length; k++) if (lines[k].trim() === node.name.trim()) return k;
      return -1;
    default:
      return -1;
  }
}

function locate(file, node) {
  if (!file || !isFile(file)) return null;
  if (!node) return { file, line: 0 };
  const line = findNode(fs.readFileSync(file, 'utf8'), node);
  return { file, line: Math.max(line, 0) };
}

// ── the name under the cursor ───────────────────────────────────────────────

function definition(docPath, text, line, character) {
  const cfg = findConfig(docPath);
  if (!cfg) return null;
  const target = targetOf(cfg, docPath);
  if (!target) return null;

  const { refs, imports } = parse(lex(text));
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

  const covers = (tok) => {
    if (tok.t === 'raw') {
      if (line < tok.line || line > tok.endLine) return false;
      if (line === tok.line && character < tok.s) return false;
      return !(line === tok.endLine && character >= tok.e);
    }
    return tok.line === line && tok.s <= character && character < tok.e;
  };
  const ref = refs.find((r) => covers(r.tok));
  if (!ref) return null;

  // typeAt: the type in effect at a step, after any .as(type) before it.
  const typeAt = (chain, index) => {
    const obj = objects[chain.root.v];
    let typ = obj ? obj.typ : typeOf(target);
    for (let k = 0; k < index; k++) {
      const st = chain.steps[k];
      if (st.call && st.name === 'as' && st.args[0] && TYPES.has(st.args[0].v)) typ = st.args[0].v;
    }
    return typ;
  };

  const resolveStep = (chain, index) => {
    const obj = objects[chain.root.v];
    if (!obj) return null;
    let typ = obj.typ;
    if (chain.argOf && chain.root.v === 'self') {
      // Inside a view, a name on self means the same view of our file.
      typ = typeAt(chain.argOf.chain, chain.argOf.index);
    }
    let node = null;
    for (let k = 0; k <= index; k++) {
      const st = chain.steps[k];
      if (st.call && st.name === 'as') {
        if (st.args[0] && TYPES.has(st.args[0].v)) typ = st.args[0].v;
        continue;
      }
      if (st.call && METHODS.has(st.name)) break; // a method: jump to what it changes
      if (st.call) {
        const kind = (KIND_CALLS[typ] || {})[st.name];
        const arg = st.args[0];
        if (!kind || !arg || arg.t !== 'str') return locate(obj.file, node);
        node = { kind, name: arg.v, ident: false };
      } else if (!st.str && st.name === 'frontmatter' && typ === 'markdown') {
        node = { kind: 'frontmatter' };
      } else if (!st.str && st.name === 'body' && chain.argOf) {
        node = { kind: 'body' };
      } else {
        node = { kind: DEFAULT_KIND[typ], name: st.name, ident: !st.str };
      }
    }
    return locate(obj.file, node);
  };

  switch (ref.what) {
    case 'import':
      return locate(importFile.get(ref.imp), null);
    case 'root':
      return objects[ref.tok.v] ? locate(objects[ref.tok.v].file, null) : null;
    case 'step':
      return resolveStep(ref.chain, ref.index);
    case 'arg': {
      if (ref.tok.t !== 'str') return null;
      const { chain, index } = ref.argOf;
      const st = chain.steps[index];
      const typ = typeAt(chain, index);
      if ((KIND_CALLS[typ] || {})[st.name]) return resolveStep(chain, index);
      if (st.name === 'patch') return locate(path.join(path.dirname(docPath), ref.tok.v), null);
      if (st.name === 'join') {
        return locate(objects.self.file, { kind: typ === 'markdown' ? 'fmkey' : 'key', name: ref.tok.v, ident: false });
      }
      if (CONTENT_METHODS.has(st.name)) {
        return locate(objects.self.file, { kind: DEFAULT_KIND[typ], name: ref.tok.v, ident: false });
      }
      return null;
    }
    default:
      return null;
  }
}

module.exports = { definition, lex, parse, findNode, identMatch };
