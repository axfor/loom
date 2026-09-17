'use strict';

// Signature help: inside a method's parentheses (or its block), the method's typed signature for the
// receiver it is called on, with the argument being written highlighted. No `vscode` import here.

const loom = require('./loom');
const { KEYWORDS, typeDoc } = require('./docs');

const KIND_NAMES = new Set(Object.values(loom.KIND_CALLS).flatMap(Object.keys));

// receiverOn names what a call's receiver is, to pick among a keyword's signatures.
function receiverOn(r, method) {
  if (KIND_NAMES.has(method)) return 'file';
  if (!r || r.bad) return null;
  if (!r.node) return 'file';
  if (r.node.kind === 'frontmatter') return 'frontmatter';
  if (loom.isValue(r)) return 'value';
  return 'node';
}

function signature(docPath, text, line, character) {
  let t = loom.open(docPath, text);
  if (!t) {
    const toks = loom.lex(text);
    t = { toks, refs: loom.parse(toks).refs, objects: {} };
  }
  const call = loom.enclosingCall(t, loom.lastBefore(t.toks, line, character), line);
  if (!call) return null;
  const st = call.chain.steps[call.index];
  const kw = KEYWORDS[st.name];
  if (!kw || !st.call) return null;
  const on = receiverOn(t.objects[call.chain.root.v] ? loom.walk(t, call.chain, call.index) : null, st.name);
  const sig = kw.signatures.find((s) => s.on === on) || kw.signatures[0];

  // The argument being written: commas (or lines, in a block) since the call opened; reason: is its own.
  let n = 0;
  let named = false;
  const open = t.toks[call.open];
  for (let i = call.open + 1; i < t.toks.length; i++) {
    const tok = t.toks[i];
    if (tok.line > line || (tok.line === line && tok.s >= character)) break;
    if (tok.t === ',' || (open.t === '{' && tok.t === 'nl' && t.toks[i - 1].t !== '{')) n++;
    if (tok.t === 'id' && tok.v === 'reason' && t.toks[i + 1] && t.toks[i + 1].t === ':') named = true;
  }
  const params = sig.params.map(([label, type]) => ({ label, doc: typeDoc(type) }));
  let active = Math.min(n, Math.max(params.length - 1, 0));
  if (params.length && params[0].label.startsWith('...')) active = 0;
  const reason = params.findIndex((p) => p.label.startsWith('reason'));
  if (named && reason >= 0) active = reason;
  return { label: sig.label, params, active, doc: kw.what };
}

module.exports = { signature };
