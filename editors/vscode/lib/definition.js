'use strict';

// Go to definition for Loom templates: what does the name under the cursor refer to?
//
//   import cmd "/.claude/commands/ship"   the path, or `cmd`  → that file in our layer
//   base / self / cmd                     the object          → its file
//   base.Install / base."How it compares" a node              → that heading (key, function...) upstream
//   base."Example 2"."Phase 1"            a section path      → that subsection, under that parent
//   "Install XSDD" inside .after(...)    content by name     → that heading in our file
//   self.frontmatter / cmd.body           a part of a file    → where that part starts
//   self.frontmatter.description          a key's value       → that key in our file
//   if base.has.Overview                  a node asked about  → that heading upstream
//   bilingual(...)                        a function call     → its fn declaration in this file
//
// The result says what was clicked (origin: the whole token, so a string with spaces is one
// link) and where it leads (the node's first line, the name on it, and where the node ends).
// Where the compiler would reject the template, this returns what it can (the file) or
// nothing — never a guess at a different node.

const loom = require('./loom');
const { isKeywordCall } = require('./hover');

// locate answers where a reference points. An object whose content is written in the template
// itself points back into the template, at the fence that holds it.
function locate(file, node, view, inline, docPath) {
  if (inline) return inTemplate(inline, node, docPath);
  if (!file || !loom.isFile(file)) return null;
  const text = loom.readText(file);
  const lines = text.split('\n').length;
  const hit = node && loom.findNode(text, node, view, loom.typeOf(file));
  if (!hit) return { file, line: 0, end: lines, s: 0, e: 0 };
  return { file, ...hit };
}

// inTemplate finds a named part inside the resource sections, and gives its line in the template.
// The walk has already read the address self[.section][.kind].part; `inline` carries the section
// and kind it named. Found in exactly one document, or nowhere — two is the compiler's error too.
function inTemplate(inline, node, docPath) {
  const { resources, r } = inline;
  if (!node) {
    // self.n1 → its `Self as n1:` line; self.json → the fence holding it
    const docs = loom.resourceDocs(resources, r);
    if (r.resKind && docs.length === 1) return { file: docPath, line: docs[0].line - 1, end: docs[0].line, s: 0, e: 0 };
    const res = r.res && resources.find((x) => x.name === r.res);
    return res ? { file: docPath, line: res.line, end: res.line + 1, s: 0, e: 0 } : null;
  }
  if (!node.name) return null;
  const hits = [];
  for (const doc of loom.resourceDocs(resources, r)) {
    const hit = loom.findNode(doc.text, node, null, doc.kind);
    if (hit) hits.push({ file: docPath, line: doc.line + hit.line, end: doc.line + hit.end, s: doc.indent + hit.s, e: doc.indent + hit.e });
  }
  return hits.length === 1 ? hits[0] : null;
}

function definition(docPath, text, line, character) {
  const t = loom.open(docPath, text);
  if (!t) return null;

  const covers = (tok) => {
    if (tok.t === 'raw') {
      if (line < tok.line || line > tok.endLine) return false;
      if (line === tok.line && character < tok.s) return false;
      return !(line === tok.endLine && character >= tok.e);
    }
    return tok.line === line && tok.s <= character && character < tok.e;
  };
  const ref = t.refs.find((r) => covers(r.tok));
  if (!ref) return null;
  const hit = resolve(t, ref);
  return hit && { ...hit, origin: { line: ref.tok.line, s: ref.tok.s, e: ref.tok.e } };
}

// target is where a walk leads: the node it found, or the file it stopped in.
function target(t, r) {
  if (!r) return null;
  if (r.bad) return r.obj.inline ? null : locate(r.obj.file, null, r.view, null, t.docPath);
  return locate(r.obj.file, r.node, r.view, r.obj.inline && { resources: r.obj.inline, r }, t.docPath);
}

// agree: one place, when every way of reading the name leads there; otherwise nowhere, rather
// than a guess at which call was meant.
function agree(hits) {
  if (hits.length === 0 || hits.some((h) => !h)) return null;
  const same = (a, b) => a.file === b.file && a.line === b.line && a.s === b.s;
  return hits.every((h) => same(h, hits[0])) ? hits[0] : null;
}

function resolve(t, ref) {
  const docPath = t.docPath;
  switch (ref.what) {
    case 'import':
      return locate(t.importFile.get(ref.imp), null);
    case 'root': {
      const obj = t.objects[ref.tok.v];
      if (obj && obj.inline) return { file: docPath, line: obj.inline[0].line, end: obj.inline[0].line + 1, s: 0, e: 0 };
      if (obj) return locate(obj.file, null, null, null, docPath);
      // a function's parameter leads where every call's argument does, when they all agree
      const bound = loom.bindings(t, ref.chain);
      if (bound) return agree(bound.map((b) => target(t, loom.walk(t, b.chain, b.shift))));
      if (ref.chain.steps.length) return null;
      // a call of a function declared in this file leads to its declaration, and a result caught
      // earlier — if !ok, err.format("...", ok) — to where it was caught
      const fn = t.fns.find((f) => f.name.v === ref.tok.v);
      if (fn) return { file: docPath, line: fn.name.line, end: fn.name.line + 1, s: fn.name.s, e: fn.name.e };
      const caught = t.refs.filter((x) => x.what === 'caught' && x.tok.v === ref.tok.v && x.tok.line <= ref.tok.line).pop();
      return caught ? { file: docPath, line: caught.tok.line, end: caught.tok.line + 1, s: caught.tok.s, e: caught.tok.e } : null;
    }
    case 'step': {
      const st = ref.chain.steps[ref.index];
      // keywords (methods, as, node kinds) explain themselves on hover; they lead nowhere
      if (isKeywordCall(st)) return null;
      const r = loom.walk(t, ref.chain, ref.index + 1);
      if (!r) return null;
      // a chain on a parameter is walked once per call; it leads somewhere when they all agree
      return agree([r, ...(r.alts || [])].map((x) => target(t, x)));
    }
    case 'param': {
      // fn bilingual(up, ours): where the first call's argument for it leads, if all calls agree
      const k = ref.fn.params.indexOf(ref.tok);
      const chain = { root: ref.tok, steps: [], argOf: null, fn: ref.fn };
      const bound = k < 0 ? null : loom.bindings(t, chain);
      return bound ? agree(bound.map((b) => target(t, loom.walk(t, b.chain, b.shift)))) : null;
    }
    case 'arg': {
      if (ref.tok.t !== 'str') return null;
      const { chain, index } = ref.argOf;
      const st = chain.steps[index];
      const r = loom.walk(t, chain, index);
      if (!r || r.bad) return null;
      if ((loom.KIND_CALLS[r.typ] || {})[st.name]) {
        const sel = loom.walk(t, chain, index + 1);
        return locate(sel.obj.file, sel.node, sel.view, sel.obj.inline && { resources: sel.obj.inline, r: sel }, docPath);
      }
      // from Loom 2 a bare string is text, not the name of a section of ours
      if (loom.CONTENT_METHODS.has(st.name) && !loom.literalNames(t)) {
        return locate(t.objects.self.file, { kind: loom.DEFAULT_KIND[r.typ], name: ref.tok.v, ident: false }, r.view);
      }
      return null;
    }
    default:
      return null;
  }
}

module.exports = { definition };
