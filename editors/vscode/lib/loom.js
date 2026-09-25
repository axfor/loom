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

const METHODS = new Set(['after', 'before', 'start', 'append', 'replace', 'drop', 'move', 'promote', 'demote', 'set', 'merge', 'wrap', 'swap', 'unwrap', 'split', 'join', 'end', 'project']);
const CONTENT_METHODS = new Set(['after', 'before', 'start', 'append', 'end', 'replace', 'set', 'wrap']);
// Words that are not names, mirroring objparse.go and newparse.go: a bare place word is a
// derived address, an axis walks the document's structure, a predicate asks about each node of a
// group, and any / count ask whether a predicate found something.
const PLACES = new Set(['after', 'before', 'start', 'end', 'append']);
const AXES = new Set(['children', 'next', 'prev', 'parent', 'first', 'last']);
const PRED_FIELDS = new Set(['level', 'name', 'value', 'empty', 'calls', 'has']);
const QUESTIONS = new Set(['any', 'count']);
// The statement keywords of the added syntax. `Self` opens a resource section, after the dashes.
const STATEMENT_WORDS = new Set(['if', 'else', 'fn', 'return']);
// The plural form selects a group and takes predicates instead of a name.
const CLASS_CALLS = {
  markdown: { sections: 'heading', lines: 'line' },
  shell: { functions: 'function', markers: 'marker', lines: 'line' },
  toml: { keys: 'key', values: 'key' },
  yaml: { keys: 'key', values: 'key' },
  json: { keys: 'path', values: 'path' },
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
// isValueType mirrors lang.HasValues: these are the types whose nodes are keys holding a value.
const isValueType = (typ) => typ === 'toml' || typ === 'yaml' || typ === 'json';
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
      const cfg = { root: dir, base: null, self: null, templates: null, output: null, loom: null };
      for (const line of fs.readFileSync(p, 'utf8').split('\n')) {
        const m = /^\s*(base|self|templates|output|loom)\s+"((?:[^"\\]|\\.)*)"/.exec(line);
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

// atLeast mirrors objparse.go's: a declared version of that major or newer. An unstated version is
// the oldest, so a tree keeps the meaning it was written with.
function atLeast(loom, major) {
  const m = /^(\d+)\.(\d+)$/.exec(loom || '');
  return Boolean(m) && Number(m[1]) >= major;
}

// literalNames: from Loom 2 an unquoted name is the name as written, and a bare string is text.
function literalNames(t) {
  return Boolean(t && t.cfg && atLeast(t.cfg.loom, 2));
}

function unquote(s) {
  return s.replace(/\\(["\\])/g, '$1');
}

// quote writes a Loom string: the only escapes are \" and \\.
// quote mirrors lang/write.go: `"` always becomes \", but a backslash is doubled only where it
// would otherwise be read as an escape — before a quote or another backslash, or at the end. So a
// \. in a regular expression is written as it was typed. Escaping every backslash also round
// trips, but then the editor and the compiler write the same name two different ways, and a sync
// that rewrites an anchor turns one into the other.
function quote(s) {
  let out = '"';
  for (let i = 0; i < s.length; i++) {
    const ch = s[i];
    if (ch === '"') out += '\\"';
    else if (ch === '\\') out += i + 1 === s.length || s[i + 1] === '"' || s[i + 1] === '\\' ? '\\\\' : '\\';
    else out += ch;
  }
  return `${out}"`;
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
    if (c === '-' && text.startsWith('---', i) && /(^|\n)[ \t]*$/.test(text.slice(0, i))) {
      let j = i;
      while (text[j] === '-') j++;
      push('sep', i, j, '---');
      i = j;
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
      let n = 0;
      while (text[i + n] === '`') n++;
      if (n >= 3) {
        // A fence, as the compiler reads it: opened by three or more backticks and closed by as
        // many at the start of a line. Everything between is content, backticks included — which
        // is the point, and why counting to the next single backtick cannot be the rule. Get this
        // wrong and one ` in a line of prose puts the rest of the file out of step.
        let j = i + n;
        while (j < text.length && text[j] !== '\n') j++; // the language tag, if any
        // The closing run may be indented: a resource section indents everything it holds.
        const close = new RegExp(`\\n[ \\t]*\`{${n}}(?!\`)`);
        const at = close.exec(text.slice(j));
        const end = at ? j + at.index : -1;
        const stop = end < 0 ? text.length : end + at[0].length;
        for (let k = i; k < stop; k++) {
          if (text[k] === '\n') {
            line++;
            lineStart = k + 1;
          }
        }
        toks.push({ t: 'raw', line: startLine, s, endLine: line, e: stop - lineStart, closed: end >= 0 });
        i = stop;
        continue;
      }
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
    if (c >= '0' && c <= '9') {
      let j = i;
      while (j < text.length && text[j] >= '0' && text[j] <= '9') j++;
      push('num', i, j, text.slice(i, j));
      i = j;
      continue;
    }
    ident.lastIndex = i;
    const m = ident.exec(text);
    if (m) {
      push('id', i, i + m[0].length, m[0]);
      i += m[0].length;
      continue;
    }
    // The added syntax's punctuation. Two-character ones first, so && is not two &.
    const two = { '&&': 'and', '||': 'or', '==': 'eq', '!=': 'ne', '<=': 'le', '>=': 'ge' }[text.slice(i, i + 2)];
    if (two) {
      push(two, i, i + 2, text.slice(i, i + 2));
      i += 2;
      continue;
    }
    if ('.,:(){}[]=!<>~'.includes(c)) push(c, i, i + 1);
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
  const fns = [];
  const calls = [];
  let i = 0;
  // An if's condition is followed by its block, so a { there opens the block, not an argument.
  let noBlock = false;
  // The blocks open where the parse is — a function's body, an if's — so an expression knows
  // which function it is written in, and a parameter can be told from an object.
  const blocks = [];
  let pendingFn = null;
  const inFn = () => {
    for (let k = blocks.length - 1; k >= 0; k--) if (blocks[k].fn) return blocks[k].fn;
    return null;
  };
  const peek = (k = 0) => toks[Math.min(i + k, toks.length - 1)];
  const skipNl = () => {
    while (peek().t === 'nl') i++;
  };

  function expr(argOf) {
    const root = toks[i++];
    const chain = { root, steps: [], argOf, fn: inFn() };
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
      if (peek().t === '[') {
        step.pred = true;
        predicate({ chain, index });
      }
      if (peek().t === '(' || (peek().t === '{' && !noBlock)) {
        const close = peek().t === '(' ? ')' : '}';
        i++;
        step.call = true;
        args(close, { chain, index });
        if (close === '}') break;
        // project(what){ template }: the block after the parentheses is an argument too
        if (peek().t === '{' && !noBlock) {
          i++;
          args('}', { chain, index });
          break;
        }
      }
    }
    return chain;
  }

  // predicate reads base.sections[level == 2 && has."X"]: each word where a field is asked about
  // is recorded, so it can be explained and completed; the values are skipped.
  function predicate(of) {
    i++; // [
    let depth = 1;
    for (;;) {
      const t = peek();
      if (t.t === 'nl' || t.t === 'eof') return;
      i++;
      if (t.t === '[') depth++;
      else if (t.t === ']' && --depth === 0) return;
      else if (t.t === 'id' && ['[', '(', '!', 'and', 'or'].includes(toks[i - 2].t)) refs.push({ what: 'pred', tok: t, of });
    }
  }

  // fnDecl reads `fn name(a, b) {`: the name, so a call can lead to it, and its parameters.
  function fnDecl() {
    if (peek().t !== 'id') return;
    const name = toks[i++];
    const fn = { name, params: [], start: name.line, end: Infinity };
    if (peek().t === '(') {
      i++;
      while (peek().t === 'id' || peek().t === ',') {
        if (peek().t === 'id') {
          fn.params.push(toks[i]);
          refs.push({ what: 'param', tok: toks[i], fn });
        }
        i++;
      }
      if (peek().t === ')') i++;
    }
    fns.push(fn);
    refs.push({ what: 'fn', tok: name, fn });
    pendingFn = fn;
  }

  // call reads `name(a, b)` where name is not an object: a function called, with the argument
  // written for each parameter — an address, or a string or literal.
  function call(chain) {
    i++; // (
    const c = { name: chain.root, args: [], fn: inFn() };
    let n = 0;
    for (;;) {
      const t = peek();
      if (t.t === ')') {
        i++;
        break;
      }
      if (t.t === 'nl' || t.t === 'eof') break;
      if (t.t === ',') n++;
      if (t.t === 'id') {
        const a = expr(null);
        a.inCall = c;
        c.args[n] = a;
        continue;
      }
      if (t.t === 'str' || t.t === 'raw') c.args[n] = { literal: t };
      i++;
    }
    calls.push(c);
  }

  // statement reads one line: its keywords, a result being caught (`ok = ...`), and every
  // expression on it — an if's condition and a block-opening `} else {` included.
  function statement() {
    let inIf = false;
    for (;;) {
      const t = peek();
      if (t.t === 'nl' || t.t === 'eof' || t.t === 'sep') return;
      if (t.t === 'id' && STATEMENT_WORDS.has(t.v)) {
        i++;
        refs.push({ what: 'keyword', tok: t });
        if (t.v === 'if') inIf = true;
        if (t.v === 'fn') fnDecl();
        continue;
      }
      if (t.t === 'id' && peek(1).t === '=') {
        refs.push({ what: 'caught', tok: t });
        i += 2;
        continue;
      }
      if (t.t === 'id') {
        noBlock = inIf;
        const chain = expr(null);
        noBlock = false;
        if (chain.steps.length === 0 && peek().t === '(') call(chain);
        continue;
      }
      if (t.t === '{') {
        blocks.push({ fn: pendingFn });
        pendingFn = null;
      } else if (t.t === '}') {
        const b = blocks.pop();
        if (b && b.fn) b.fn.end = t.line;
      }
      i++;
    }
  }

  function args(close, argOf) {
    for (;;) {
      if (close === '}') skipNl();
      const t = peek();
      if (t.t === close) {
        i++;
        return;
      }
      // (...) stays on one line. A { } block holds content, one item a line, so a block still open
      // when base or import starts a line is a block being written, not one that swallows the
      // rest. Inside parentheses base is an argument like any other: move(base.Y.after),
      // swap(base.Y), project(base.sections[...]).
      const lineStart = toks[i - 1] && toks[i - 1].t === 'nl';
      if (t.t === 'eof' || (close === ')' && t.t === 'nl') || (close === '}' && lineStart && t.t === 'id' && (t.v === 'base' || t.v === 'import'))) return;
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
    // After the dashes the file holds our content, not statements.
    if (t.t === 'eof' || t.t === 'sep') break;
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
    } else {
      statement();
    }
    while (peek().t !== 'nl' && peek().t !== 'eof') i++;
  }
  return { refs, imports, fns, calls };
}

// open reads a template: its settings, product path, statements, and the objects it names.
function open(docPath, text) {
  const cfg = findConfig(docPath);
  if (!cfg) return null;
  const target = targetOf(cfg, docPath);
  if (!target) return null;
  const toks = lex(text);
  const { refs, imports, fns, calls } = parse(toks);
  const objects = {
    base: { layer: 'base', file: path.join(layerDir(cfg, 'base'), target), typ: typeOf(target) },
    self: { layer: 'self', file: cfg.self == null ? null : path.join(layerDir(cfg, 'self'), target), typ: typeOf(target) },
  };
  // Resource sections: everything after the line of dashes is our content, written here. They
  // make self an object of this file rather than a file beside it, so the editor has to read them
  // the same way the compiler does or it will look for our content in a file that is not there.
  const resources = readResources(toks, text);
  if (resources.length > 0) objects.self = { layer: 'self', file: null, typ: typeOf(target), inline: resources };
  const importFile = new Map();
  for (const imp of imports) {
    const name = imp.name ? imp.name.v : path.posix.basename(imp.spec.v).replace(/\.[^.]*$/, '');
    const layer = name === 'base' ? 'base' : 'self';
    const file = resolveImport(cfg, layer, imp.spec.v, target);
    importFile.set(imp, file);
    if (name !== 'self' && file) objects[name] = { layer, file, typ: typeOf(file) };
  }
  return { cfg, target, docPath, toks, refs, imports, importFile, objects, resources, fns, calls };
}

// walk follows the first `upto` steps of a chain the way the compiler does and says what they
// select: an object, the type in effect (after .as), a node, the key a view looks into, and
// the method that ended the chain. null when the root is not an object.
function walk(t, chain, upto) {
  const obj = t.objects[chain.root.v];
  if (!obj) {
    // A parameter is what each call of its function passes: the first call answers, and the
    // others ride along for callers that need every one to agree.
    const b = bindings(t, chain);
    if (!b || b.length === 0) return null;
    const all = b.map((x) => walk(t, x.chain, upto + x.shift)).filter(Boolean);
    if (all.length === 0) return null;
    return Object.assign(all[0], { alts: all.slice(1) });
  }
  const r = { obj, typ: obj.typ, node: null, view: null, method: null, bad: false };
  const ident = (st) => !st.str && !literalNames(t);
  // self written in this file's resource sections: a step may name the section, and a step may
  // name the kind — both optional, in that order, as objparse.go reads them. What follows is an
  // address inside those documents, of the kind named or else of the product's own type.
  let first = 0;
  if (obj.inline) {
    const s0 = chain.steps[0];
    if (s0 && !s0.str && !s0.call && obj.inline.some((res) => res.name && res.name === s0.name)) {
      r.res = s0.name;
      first++;
    }
    const s1 = chain.steps[first];
    if (s1 && !s1.str && !s1.call && TYPES.has(s1.name)) {
      r.resKind = s1.name;
      r.typ = s1.name;
      first++;
    }
  }
  if (chain.argOf && chain.root.v === 'self') {
    // Inside a view, self means the same view of our file.
    const outer = walk(t, chain.argOf.chain, chain.argOf.index);
    if (outer && outer.view) {
      r.typ = outer.typ;
      r.view = outer.view;
    }
  }
  for (let k = first; k < upto && k < chain.steps.length; k++) {
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
    if (r.place || r.asked) {
      r.bad = true;
      continue;
    }
    const word = !st.call && !st.str && !st.pred;
    // base.X.after, base.start: a place to write at, which only project is called on
    if (word && PLACES.has(st.name)) {
      r.place = st.name;
      continue;
    }
    // base.sections[...].any: a question an if asks, and the end of the chain
    if (word && r.many && QUESTIONS.has(st.name)) {
      r.asked = st.name;
      continue;
    }
    if (r.node) {
      if ((st.call || st.pred) && (CLASS_CALLS[r.typ] || {})[st.name]) {
        r.bad = true; // a group is chosen from the whole file, so it comes first
        continue;
      }
      // a frontmatter key: base.frontmatter.description
      if (r.node.kind === 'frontmatter' && !st.call) {
        r.node = { kind: 'fmkey', name: st.name, ident: false };
        continue;
      }
      // an axis: base.X.children, base.sections[...].first — first and last pick from a group,
      // the others walk from one node, and children lands on a group again
      if (!st.call && !st.str && AXES.has(st.name)) {
        const one = st.name === 'first' || st.name === 'last';
        if (one !== Boolean(r.many)) {
          r.bad = true;
          continue;
        }
        r.many = st.name === 'children';
        r.node = { kind: r.node.kind, axis: st.name };
        continue;
      }
      if (r.many || r.node.axis) {
        r.bad = true;
        continue;
      }
      // a key path: base.jobs.build — yaml, toml and json address a nested key by the dotted path
      // of its parents, so a name below a key extends that path rather than starting a lookup of
      // its own. Without this the editor stopped at the first segment, and go-to-definition on
      // the rest of the path did nothing.
      if (!st.call && isValueType(r.typ) && r.node.kind === DEFAULT_KIND[r.typ] && !r.view) {
        r.node = { kind: r.node.kind, name: `${r.node.name}.${st.name}`, ident: false };
        continue;
      }
      // a section path: base."Example 2"."Phase 1"
      if (st.call || r.node.kind !== 'heading') {
        r.bad = true;
        continue;
      }
      r.node = { kind: 'heading', name: st.name, ident: ident(st), within: [...(r.node.within || []), { name: r.node.name, ident: r.node.ident }] };
      continue;
    }
    const group = !st.str && (CLASS_CALLS[r.typ] || {})[st.name];
    if (group) {
      // base.sections is every section; base.sections[...] or base.sections(level: 2) some of them
      r.node = { kind: group, group: true };
      r.many = true;
    } else if (word && st.name === 'has' && !r.has) {
      // if base.has.Overview: the name after has is a node, asked about rather than changed
      r.has = true;
    } else if (st.call) {
      const kind = (KIND_CALLS[r.typ] || {})[st.name];
      const a = st.args[0];
      if (kind && a && a.t === 'str') r.node = { kind, name: a.v, ident: false };
      else r.bad = true;
    } else if (!st.str && st.name === 'frontmatter' && r.typ === 'markdown') {
      r.node = { kind: 'frontmatter' };
    } else if (!st.str && st.name === 'body' && obj.layer === 'self') {
      r.node = { kind: 'body' };
    } else {
      r.node = { kind: DEFAULT_KIND[r.typ], name: st.name, ident: ident(st) };
    }
  }
  return r;
}

// bindings: what a chain rooted at a function's parameter stands for at each call of that
// function — the argument written there, with the chain's own steps after it. The compiler inlines
// a function where it is called, so a parameter means nothing else, and a function nobody calls
// binds to nothing. `shift` maps a step of the chain to the same step of the bound one.
function bindings(t, chain, depth = 0) {
  const fn = chain.fn;
  if (!fn || !t.calls || depth > 8) return null;
  const k = fn.params.findIndex((p) => p.v === chain.root.v);
  if (k < 0) return null;
  const out = [];
  for (const c of t.calls) {
    if (c.name.v !== fn.name.v) continue;
    const a = c.args[k];
    if (!a || !a.root) continue; // a string or a literal is content, not an address
    const bound = { root: a.root, steps: [...a.steps, ...chain.steps], argOf: chain.argOf, fn: a.fn, call: c };
    // an argument that is itself a parameter of the calling function binds again, one level up
    const up = t.objects[a.root.v] ? null : bindings(t, bound, depth + 1);
    if (up) out.push(...up.map((u) => ({ chain: u.chain, shift: u.shift + a.steps.length, call: c })));
    else if (t.objects[a.root.v]) out.push({ chain: bound, shift: a.steps.length, call: c });
  }
  return out;
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
const RE_YAML_KEY = new RegExp(`^(${WS}*)(?:-${WS}+)?("[^"]*"|'[^']*'|[^\\s:#][^:]*?)${WS}*:(?:${WS}+(.*))?$`, 'u');
const RE_BLOCK_SCALAR = /^[|>][+-]?[0-9]*[ \t]*$/;
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

// yamlKeys mirrors ast/yaml.go: a key is addressed by the dotted path of its parents, and covers
// whatever is indented under it — a nested map, a sequence, or the body of a block scalar. Line
// based on purpose, like the compiler: a round trip through a real yaml library rewrites quoting,
// key order and comments, and the product has to keep every byte the author wrote.
function yamlKeys(lines) {
  const out = [];
  const stack = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const t = line.trim();
    if (t === '' || t.startsWith('#')) continue;
    const m = RE_YAML_KEY.exec(line);
    if (!m) continue;
    // A list item's key sits deeper than the dash that introduces it, so siblings under the
    // same dash share a parent.
    let indent = m[1].length;
    if (t.startsWith('- ')) indent++;
    while (stack.length && stack[stack.length - 1].indent >= indent) stack.pop();
    const raw = m[2].trim();
    const name = raw.length >= 2 && ((raw[0] === '"' && raw.endsWith('"')) || (raw[0] === "'" && raw.endsWith("'")))
      ? raw.slice(1, -1)
      : raw;
    const path = stack.length ? `${stack[stack.length - 1].name}.${name}` : name;
    stack.push({ indent, name: path });

    let end = i + 1;
    for (let j = i + 1; j < lines.length; j++) {
      if (lines[j].trim() === '') continue; // a blank line inside a block belongs to it
      if (lines[j].length - lines[j].replace(/^[ \t]*/, '').length <= indent) break;
      end = j + 1;
    }
    out.push({ name: path, line: i, s: line.indexOf(raw), end, block: RE_BLOCK_SCALAR.test((m[3] || '').trim()) });
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
function nodesOf(text, kind, typ) {
  const lines = text.split('\n');
  switch (kind) {
    case 'heading':
      return headings(lines);
    case 'key':
      // toml and yaml share the kind name, as they do in the compiler, but not the syntax.
      return typ === 'yaml' ? yamlKeys(lines) : tomlKeys(lines);
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
function findNode(text, node, view, typ) {
  // A group, or where an axis lands, is not one named node the text can be searched for.
  if (node.group || node.axis) return null;
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
      const n = pick(nodesOf(lines.join('\n'), node.kind, typ), node.name, node.ident);
      return shift(n, n ? n.name : '');
    }
  }
}

module.exports = {
  CLASS_CALLS,
  METHODS,
  PLACES,
  AXES,
  PRED_FIELDS,
  QUESTIONS,
  STATEMENT_WORDS,
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
  atLeast,
  literalNames,
  resourceDocs,
  bindings,
  lastBefore,
  stepRef,
  enclosingCall,
  identMatch,
  headings,
  nodesOf,
  readResources,
  isValueType,
  yamlKeys,
  viewLines,
  findNode,
};

// readResources collects the `Self:` sections after the line of dashes: their name, and the fences
// they hold with the language tag that says what each one is.
function readResources(toks, text) {
  const out = [];
  let i = toks.findIndex((t) => t.t === 'sep');
  if (i < 0) return out;
  for (; i < toks.length; i++) {
    const t = toks[i];
    if (t.t !== 'id' || t.v !== 'Self') continue;
    let k = i + 1;
    let name = '';
    if (toks[k] && toks[k].t === 'id' && toks[k].v === 'as' && toks[k + 1] && toks[k + 1].t === 'id') {
      name = toks[k + 1].v;
      k += 2;
    }
    if (!toks[k] || toks[k].t !== ':') continue;
    const docs = [];
    for (let j = k + 1; j < toks.length; j++) {
      if (toks[j].t === 'nl') continue;
      if (toks[j].t !== 'raw') break;
      docs.push(fenceDoc(toks[j], text));
    }
    out.push({ name, docs, line: t.line });
    i = k;
  }
  return out;
}

// resourceDocs: the documents of our resource sections a walk can be reading from — the section
// it named, the kind it named, and otherwise every one, which is where the compiler looks too.
function resourceDocs(resources, r) {
  const out = [];
  for (const res of resources) {
    if (r.res && res.name !== r.res) continue;
    for (const doc of res.docs) {
      if (r.resKind && doc.kind !== r.resKind) continue;
      out.push({ ...doc, res: res.name });
    }
  }
  return out;
}

// fenceDoc reads one fence back out of the source: its language tag, its text with the common
// indentation stripped, and the line its body starts on so a definition can point into it.
function fenceDoc(tok, text) {
  const lines = text.split('\n');
  const open = lines[tok.line] || '';
  const tag = (open.trim().replace(/^`+/, '').trim()) || '';
  const body = [];
  for (let k = tok.line + 1; k <= (tok.endLine ?? tok.line); k++) {
    if (/^\s*`{3,}\s*$/.test(lines[k] || '')) break;
    body.push(lines[k] ?? '');
  }
  let indent = null;
  for (const l of body) {
    if (l.trim() === '') continue;
    const w = l.length - l.replace(/^[ \t]*/, '').length;
    if (indent === null || w < indent) indent = w;
  }
  return { kind: tag, text: body.map((l) => l.slice(indent || 0)).join('\n'), line: tok.line + 1, indent: indent || 0 };
}
