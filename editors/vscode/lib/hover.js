'use strict';

// Hover: a keyword explains how it is used, with examples; an object says which file it stands for.
// Like the rest of lib/, no `vscode` import here.

const path = require('path');
const loom = require('./loom');
const { OBJECTS, markdownFor } = require('./docs');

const KIND_NAMES = new Set(Object.values(loom.KIND_CALLS).flatMap(Object.keys));

// isKeywordCall: a step that is a method, as(...) or a node kind such as section(...).
function isKeywordCall(st) {
  return st.call && (loom.METHODS.has(st.name) || st.name === 'as' || KIND_NAMES.has(st.name));
}

function hover(docPath, text, line, character) {
  const toks = loom.lex(text);
  const k = toks.findIndex((t) => t.t === 'id' && t.line === line && t.s <= character && character < t.e);
  if (k < 0) return null;
  const tok = toks[k];
  const range = { line, s: tok.s, e: tok.e };
  const reply = (word) => {
    const markdown = markdownFor(word);
    return markdown && { markdown, range };
  };

  const firstOnLine = k === 0 || toks[k - 1].t === 'nl';
  if (tok.v === 'import' && firstOnLine) return reply('import');
  if (tok.v === 'reason' && toks[k + 1] && toks[k + 1].t === ':') return reply('reason');

  const t = loom.open(docPath, text);
  const refs = t ? t.refs : loom.parse(toks).refs;
  // tokens from lex(text) and from open() are different objects; match them by position
  const ref = refs.find((r) => r.tok.line === line && r.tok.s === tok.s && r.tok.t === 'id');
  if (!ref) return null;
  if (ref.what === 'step') {
    const st = ref.chain.steps[ref.index];
    if (isKeywordCall(st) || (!st.str && (st.name === 'frontmatter' || st.name === 'body'))) return reply(st.name);
    return null;
  }
  if (ref.what === 'root' && t) {
    const obj = t.objects[tok.v];
    if (!obj) return null;
    const where = obj.file ? path.relative(t.cfg.root, obj.file) : '(no file)';
    const what = OBJECTS[tok.v] || 'A file of our layer, imported by name — a content source.';
    const exists = obj.file && loom.isFile(obj.file) ? 'Cmd-click (Ctrl-click) opens it.' : 'The file does not exist.';
    return { markdown: `**${tok.v}** · \`${where}\`\n\n${what}\n\n${exists}`, range };
  }
  return null;
}

module.exports = { hover, isKeywordCall };
