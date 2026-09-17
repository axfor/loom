'use strict';

// Go to definition for Loom templates: what does the name under the cursor refer to?
//
//   import cmd "/.claude/commands/ship"   the path, or `cmd`  → that file in our layer
//   base / self / cmd                     the object          → its file
//   base.Install / base."How it compares" a node              → that heading (key, function...) upstream
//   base."Example 2"."Phase 1"            a section path      → that subsection, under that parent
//   "Install XSDD" inside .after(...)       content by name     → that heading in our file
//   self.frontmatter / cmd.body           a part of a file    → where that part starts
//   "description" inside .join(...)         a key               → that key in our file
//
// The result says what was clicked (origin: the whole token, so a string with spaces is one
// link) and where it leads (the node's first line, the name on it, and where the node ends).
// Where the compiler would reject the template, this returns what it can (the file) or
// nothing — never a guess at a different node.

const loom = require('./loom');
const { isKeywordCall } = require('./hover');

function locate(file, node, view) {
  if (!file || !loom.isFile(file)) return null;
  const text = loom.readText(file);
  const lines = text.split('\n').length;
  const hit = node && loom.findNode(text, node, view);
  if (!hit) return { file, line: 0, end: lines, s: 0, e: 0 };
  return { file, ...hit };
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

function resolve(t, ref) {
  switch (ref.what) {
    case 'import':
      return locate(t.importFile.get(ref.imp), null);
    case 'root': {
      const obj = t.objects[ref.tok.v];
      return obj ? locate(obj.file, null) : null;
    }
    case 'step': {
      const st = ref.chain.steps[ref.index];
      // keywords (methods, as, node kinds) explain themselves on hover; they lead nowhere
      if (isKeywordCall(st)) return null;
      const r = loom.walk(t, ref.chain, ref.index + 1);
      if (!r) return null;
      return locate(r.obj.file, r.bad ? null : r.node, r.view);
    }
    case 'arg': {
      if (ref.tok.t !== 'str') return null;
      const { chain, index } = ref.argOf;
      const st = chain.steps[index];
      const r = loom.walk(t, chain, index);
      if (!r || r.bad) return null;
      if ((loom.KIND_CALLS[r.typ] || {})[st.name]) {
        const sel = loom.walk(t, chain, index + 1);
        return locate(sel.obj.file, sel.node, sel.view);
      }
      if (st.name === 'join') {
        return locate(t.objects.self.file, { kind: r.typ === 'markdown' ? 'fmkey' : loom.DEFAULT_KIND[r.typ], name: ref.tok.v, ident: false });
      }
      if (loom.CONTENT_METHODS.has(st.name)) {
        return locate(t.objects.self.file, { kind: loom.DEFAULT_KIND[r.typ], name: ref.tok.v, ident: false }, r.view);
      }
      return null;
    }
    default:
      return null;
  }
}

module.exports = { definition };
