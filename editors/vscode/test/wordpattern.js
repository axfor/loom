// The word pattern, checked the way VS Code uses it: wherever the cursor is inside a string, the
// word under it is the whole string, quotes included; inside a name, the whole name.
//
// VS Code does not match a word pattern from the start of the line. It starts a few characters
// before the cursor and steps further back, so a pattern that only works from the line start
// finds half a string. getWordAtText below is VS Code's algorithm, ported from
// src/vs/editor/common/core/wordHelper.ts (MIT License, Copyright (c) Microsoft Corporation).
'use strict';

const fs = require('fs');
const path = require('path');
const { lex } = require('../lib/loom');

const cfg = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'language-configuration.json'), 'utf8'));
const wordDefinition = new RegExp(cfg.wordPattern, 'g');
const config = { maxLen: 1000, windowSize: 15, timeBudget: 150 };

function getWordAtText(column, re, text, textOffset) {
  if (text.length > config.maxLen) {
    let start = column - config.maxLen / 2;
    if (start < 0) start = 0;
    else textOffset += start;
    text = text.substring(start, column + config.maxLen / 2);
    return getWordAtText(column, re, text, textOffset);
  }
  const t1 = Date.now();
  const pos = column - 1 - textOffset;
  let prevRegexIndex = -1;
  let match = null;
  for (let i = 1; ; i++) {
    if (Date.now() - t1 >= config.timeBudget) break;
    const regexIndex = pos - config.windowSize * i;
    re.lastIndex = Math.max(0, regexIndex);
    const thisMatch = findRegexMatchEnclosingPosition(re, text, pos, prevRegexIndex);
    if (!thisMatch && match) break;
    match = thisMatch;
    if (regexIndex <= 0) break;
    prevRegexIndex = regexIndex;
  }
  if (!match) return null;
  re.lastIndex = 0;
  return { word: match[0], startColumn: textOffset + 1 + match.index, endColumn: textOffset + 1 + match.index + match[0].length };
}

function findRegexMatchEnclosingPosition(re, text, pos, stopPos) {
  let match;
  while ((match = re.exec(text))) {
    const matchIndex = match.index || 0;
    if (matchIndex <= pos && re.lastIndex >= pos) return match;
    if (stopPos > 0 && matchIndex > stopPos) return null;
  }
  return null;
}

const lines = [
  'base.append("离线环境 fallback（无网络 / 内网部署）")',
  'base."Example 1: Understand & Expand".after("例 1：理解与扩展（话）", self."例 1：理解与扩展（话）"."Phase 1: Understand & Expand", "Position in SDLC（XSDD 调度层关系）")',
  'base.Overview.drop(reason: "upstream says \\"hi\\" to other projects, and a path like C:\\\\tmp\\\\ does not end it; that does not apply here")',
  '    self."Example 2"."Phase 1"',
  'import cmd "/.claude/commands/ship"',
  'base.prompt.as(markdown).Steps.after("Our step")',
  // the added syntax: brackets, comparisons and operators end a word, as they end a token
  'base.sections[level==2&&has."Example 1"].first.children.demote()',
  'base.functions[calls "curl"||name~"^x"].drop(reason: "a")',
  'ok=base.Install.after(self.setup)',
  'if !ok {',
  '} else if base.sections[name!="Step 1"].any {',
  'base.keys[value<="x"].drop(reason: "b")',
  'base.Install.after(' + Array.from({ length: 12 }, (_, k) => `"Section number ${k} with a longer title"`).join(', ') + ')',
];

let pass = 0;
let fail = 0;
let slowest = 0;

// A predicate's brackets pair and close like the others, or typing base.sections[ leaves it open.
for (const [open, close] of [['{', '}'], ['(', ')'], ['[', ']']]) {
  const paired = cfg.brackets.some(([a, b]) => a === open && b === close) &&
    cfg.autoClosingPairs.some((p) => p.open === open && p.close === close) &&
    cfg.surroundingPairs.some(([a, b]) => a === open && b === close);
  if (paired) pass++;
  else {
    fail++;
    console.log(`  ❌ ${open}${close} is not a bracket pair that closes and surrounds`);
  }
}
for (const text of lines) {
  for (const tok of lex(text)) {
    if (tok.t !== 'str' && tok.t !== 'id') continue;
    const want = text.slice(tok.s, tok.e);
    for (let c = tok.s; c <= tok.e; c++) {
      const t0 = Date.now();
      const got = getWordAtText(c + 1, wordDefinition, text, 0);
      slowest = Math.max(slowest, Date.now() - t0);
      // at a boundary the neighbouring word may win; a string must still never be split
      const whole = got && got.word === want && got.startColumn === tok.s + 1;
      const neighbour = got && (c === tok.s || c === tok.e) && lex(text).some((o) => o !== tok && (o.t === 'str' || o.t === 'id') && got.startColumn === o.s + 1 && got.word === text.slice(o.s, o.e));
      if (whole || neighbour) pass++;
      else {
        fail++;
        console.log(`  ❌ column ${c} of ${want}: got ${got ? got.word : 'no word'}\n     in: ${text}`);
      }
    }
  }
}
if (slowest > config.timeBudget / 3) {
  fail++;
  console.log(`  ❌ finding a word took ${slowest}ms; VS Code gives up after ${config.timeBudget}ms`);
}
console.log(`loom word pattern: ${pass} passed, ${fail} failed (slowest ${slowest}ms)`);
process.exit(fail === 0 ? 0 : 1);
