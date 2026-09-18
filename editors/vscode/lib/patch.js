'use strict';

// Patch: what our layer does to upstream, as a unified diff — the +/- form git prints.
//
// No `vscode` import here, so all of it runs (and is tested) in plain Node, like the rest of lib/.
//
// The product is woven by lm itself, one template at a time with `lm weave`. Never `lm build`:
// build completes anchors and writes them back into the templates (autoanchor.go), and opening a
// view must not edit the author's sources.

const path = require('path');
const { spawn } = require('child_process');
const loom = require('./loom');

// How many unchanged lines surround each change, as in `diff -U3`.
const CONTEXT = 3;

// A file that ends without a final newline differs from one that does, even when every line reads
// the same. The last line of such a file is compared under a key of its own so the diff says so,
// the way git does; the marker never reaches the output.
const NOEOL = '\u0000';

// Myers is O((N+M)D): cheap when the two files are close, which is the normal case here — our layer
// inserts into upstream. A file pair that differs almost everywhere is not worth the wait, so the
// search gives up past this many edits and the file is reported as replaced whole.
const MAX_EDITS = 5000;

// How many templates are woven at once. lm starts in milliseconds; this keeps a tree of a few
// hundred templates from spawning them all at the same moment.
const CONCURRENCY = 8;

// ── lines ───────────────────────────────────────────────────────────────────

// split returns a file's lines without the phantom empty line a trailing newline would add, and
// says whether that newline is missing — git notes its absence in the patch, and so do we.
function split(text) {
  if (text === '') return { lines: [], noEol: false };
  const lines = text.split('\n');
  const noEol = lines[lines.length - 1] !== '';
  if (!noEol) lines.pop();
  return { lines, noEol };
}

// ── the diff itself ─────────────────────────────────────────────────────────

// script returns the edit script between two line arrays as { op, text } in file order, where op is
// ' ' for a line both files share, '-' for one only upstream has, '+' for one only the product has.
// It returns null when the two are further apart than MAX_EDITS.
//
// Common lines at the start and the end are taken off first. For Loom that is nearly the whole
// file — a template inserts a section into upstream and leaves the rest alone — so the search
// itself usually runs over a handful of lines.
function script(a, b) {
  let head = 0;
  while (head < a.length && head < b.length && a[head] === b[head]) head++;
  let tail = 0;
  while (tail < a.length - head && tail < b.length - head
    && a[a.length - 1 - tail] === b[b.length - 1 - tail]) tail++;

  const middle = myers(a.slice(head, a.length - tail), b.slice(head, b.length - tail));
  if (middle === null) return null;

  const out = [];
  for (let i = 0; i < head; i++) out.push({ op: ' ', text: a[i] });
  for (const e of middle) out.push(e);
  for (let i = a.length - tail; i < a.length; i++) out.push({ op: ' ', text: a[i] });
  return out;
}

// myers walks the edit graph one edit at a time, keeping each round's furthest reach so the path
// can be walked back once the end is reached. The algorithm is Myers 1986.
function myers(a, b) {
  const n = a.length;
  const m = b.length;
  const max = Math.min(MAX_EDITS, n + m);
  const off = max + 1;
  const v = new Int32Array(2 * max + 3);
  const trace = [];
  for (let d = 0; d <= max; d++) {
    trace.push(Int32Array.from(v));
    for (let k = -d; k <= d; k += 2) {
      // Reach furthest by going down (an insertion) or right (a deletion), then slide along the
      // diagonal for as long as the lines match.
      let x = (k === -d || (k !== d && v[off + k - 1] < v[off + k + 1]))
        ? v[off + k + 1]
        : v[off + k - 1] + 1;
      let y = x - k;
      while (x < n && y < m && a[x] === b[y]) { x++; y++; }
      v[off + k] = x;
      if (x >= n && y >= m) return walkBack(trace, a, b, d, off);
    }
  }
  return null;
}

// walkBack turns the trace into the edit script, from the end of both files to the start.
function walkBack(trace, a, b, d, off) {
  const out = [];
  let x = a.length;
  let y = b.length;
  for (let depth = d; depth > 0; depth--) {
    const v = trace[depth];
    const k = x - y;
    const prevK = (k === -depth || (k !== depth && v[off + k - 1] < v[off + k + 1])) ? k + 1 : k - 1;
    const prevX = v[off + prevK];
    const prevY = prevX - prevK;
    while (x > prevX && y > prevY) { x--; y--; out.push({ op: ' ', text: a[x] }); }
    if (x > prevX) { x--; out.push({ op: '-', text: a[x] }); }
    else if (y > prevY) { y--; out.push({ op: '+', text: b[y] }); }
  }
  while (x > 0 && y > 0) { x--; y--; out.push({ op: ' ', text: a[x] }); }
  while (x > 0) { x--; out.push({ op: '-', text: a[x] }); }
  while (y > 0) { y--; out.push({ op: '+', text: b[y] }); }
  return out.reverse();
}

// ── unified diff ────────────────────────────────────────────────────────────

// unified renders one file pair as a unified diff, headers included, or '' when the two are the
// same. Counts come back with it so the summary need not parse what was just written.
function unified(before, after, fromLabel, toLabel) {
  const a = split(before);
  const b = split(after);
  if (before === after) return { text: '', added: 0, removed: 0 };

  const keyed = (f) => f.lines.map((l, i) => (f.noEol && i === f.lines.length - 1 ? l + NOEOL : l));
  const edits = script(keyed(a), keyed(b));
  if (edits === null) return replacedWhole(a, b, fromLabel, toLabel);

  const body = [];
  let added = 0;
  let removed = 0;
  // Each hunk is a run of changes plus CONTEXT unchanged lines on each side; runs closer together
  // than twice that are one hunk, or their context would overlap and repeat lines.
  for (let i = 0; i < edits.length;) {
    if (edits[i].op === ' ') { i++; continue; }
    let start = i;
    let end = i;
    for (let j = i; j < edits.length; j++) {
      if (edits[j].op !== ' ') { end = j; continue; }
      if (j - end > 2 * CONTEXT) break;
    }
    start = Math.max(0, start - CONTEXT);
    end = Math.min(edits.length - 1, end + CONTEXT);

    let oldStart = 0;
    let newStart = 0;
    for (let j = 0; j < start; j++) {
      if (edits[j].op !== '+') oldStart++;
      if (edits[j].op !== '-') newStart++;
    }
    let oldCount = 0;
    let newCount = 0;
    const lines = [];
    for (let j = start; j <= end; j++) {
      const e = edits[j];
      lines.push(e.op + (e.text.endsWith(NOEOL) ? e.text.slice(0, -NOEOL.length) : e.text));
      if (e.op !== '+') oldCount++;
      if (e.op !== '-') newCount++;
      if (e.op === '+') added++;
      if (e.op === '-') removed++;
      // The last line of a file with no final newline: say so, as git does, right after it.
      if (e.op !== '+' && a.noEol && oldStart + oldCount === a.lines.length) lines.push('\\ No newline at end of file');
      else if (e.op === '+' && b.noEol && newStart + newCount === b.lines.length) lines.push('\\ No newline at end of file');
    }
    body.push(`@@ -${range(oldStart, oldCount)} +${range(newStart, newCount)} @@`, ...lines);
    i = end + 1;
  }

  return { text: header(fromLabel, toLabel) + body.join('\n') + '\n', added, removed };
}

// range writes a hunk's line span. A span of one line is written without its length, and an empty
// one starts at the line before it — both as diff and git write them.
function range(start, count) {
  if (count === 0) return `${start},0`;
  if (count === 1) return `${start + 1}`;
  return `${start + 1},${count}`;
}

function header(fromLabel, toLabel) {
  return `diff --loom ${fromLabel} ${toLabel}\n--- ${fromLabel}\n+++ ${toLabel}\n`;
}

// replacedWhole is the fallback for a pair too far apart to diff: every line out, every line in.
// Saying that plainly beats making the reader wait for a diff that would be unreadable anyway.
function replacedWhole(a, b, fromLabel, toLabel) {
  const lines = [`@@ -${range(0, a.lines.length)} +${range(0, b.lines.length)} @@`];
  for (const l of a.lines) lines.push('-' + l);
  if (a.noEol) lines.push('\\ No newline at end of file');
  for (const l of b.lines) lines.push('+' + l);
  if (b.noEol) lines.push('\\ No newline at end of file');
  return { text: header(fromLabel, toLabel) + lines.join('\n') + '\n', added: b.lines.length, removed: a.lines.length };
}

module.exports = { CONTEXT, script, split, unified };

// ── the whole tree ──────────────────────────────────────────────────────────

// weaveOne runs `lm weave <template>` and resolves to the product, or to { error }. The template is
// read from disk, so the patch describes the tree as it is saved.
function weaveOne(lm, root, template, timeoutMs) {
  return new Promise((resolve) => {
    const child = spawn(lm, ['weave', template], { cwd: root });
    let out = '';
    let err = '';
    const timer = setTimeout(() => child.kill(), timeoutMs);
    child.stdin.on('error', () => {});
    child.stdout.on('data', (d) => (out += d));
    child.stderr.on('data', (d) => (err += d));
    child.on('error', (e) => { clearTimeout(timer); resolve({ error: e.message }); });
    child.on('close', (code) => {
      clearTimeout(timer);
      resolve(code === 0 ? { product: out } : { error: (err || `lm weave exited with ${code}`).trim() });
    });
    child.stdin.end();
  });
}

// listTemplates runs `lm list` and resolves to its entries, or to { error }.
function listTemplates(lm, root, timeoutMs) {
  return new Promise((resolve) => {
    const child = spawn(lm, ['list'], { cwd: root });
    let out = '';
    let err = '';
    const timer = setTimeout(() => child.kill(), timeoutMs);
    child.stdin.on('error', () => {});
    child.stdout.on('data', (d) => (out += d));
    child.stderr.on('data', (d) => (err += d));
    child.on('error', (e) => { clearTimeout(timer); resolve({ error: `cannot run ${lm}: ${e.message}` }); });
    child.on('close', (code) => {
      clearTimeout(timer);
      if (code !== 0) return resolve({ error: (err || `lm list exited with ${code}`).trim() });
      try {
        resolve({ entries: JSON.parse(out) });
      } catch (e) {
        resolve({ error: `lm list did not print JSON: ${e.message}` });
      }
    });
    child.stdin.end();
  });
}

// pool runs jobs over the list, CONCURRENCY at a time, keeping the results in order.
async function pool(items, run) {
  const out = new Array(items.length);
  let next = 0;
  const workers = Array.from({ length: Math.min(CONCURRENCY, items.length) }, async () => {
    for (;;) {
      const i = next++;
      if (i >= items.length) return;
      out[i] = await run(items[i], i);
    }
  });
  await Promise.all(workers);
  return out;
}

// bar draws git's +/- histogram for one file, scaled so the widest file fits in width columns.
function bar(added, removed, widest, width) {
  const total = added + removed;
  if (total === 0) return '';
  const scaled = widest > width ? Math.max(1, Math.round((total * width) / widest)) : total;
  const plus = total === 0 ? 0 : Math.round((added / total) * scaled);
  return '+'.repeat(plus) + '-'.repeat(Math.max(0, scaled - plus));
}

// treePatch renders every template in the tree as a unified diff of upstream against the product
// lm weaves from it, with a summary first, the way `git diff --stat` puts one before the patch.
//
// A merge template is marked: there the product is our own file, so the diff is not what a template
// inserts but the edits we carry — the ones lm sync re-applies to each new upstream. They exist
// nowhere else as a file, which is the reason this view is worth having.
async function treePatch(lm, from, timeoutMs = 20000) {
  if (!lm) return { error: 'lm is not installed: go install github.com/axfor/loom/cmd/lm@latest, or set loom.path' };
  const cfg = configFor(from);
  if (!cfg) return { error: 'no loom.lm above this file — nothing to compare with upstream' };

  const listed = await listTemplates(lm, cfg.root, timeoutMs);
  if (listed.error) return { error: listed.error };

  const woven = await pool(listed.entries, (e) => weaveOne(lm, cfg.root, e.template, timeoutMs));

  const files = [];
  for (let i = 0; i < listed.entries.length; i++) {
    const e = listed.entries[i];
    const r = woven[i];
    const upstream = path.join(cfg.root, cfg.base, e.path);
    const before = loom.readText(upstream);
    if (r.error) {
      files.push({ target: e.target, error: r.error, added: 0, removed: 0, text: '' });
      continue;
    }
    // A template whose base is our own layer weaves onto no upstream file; there is nothing to
    // compare it with, and the added files are not what this view is about.
    if (before === null) continue;
    const d = unified(before, r.product, `upstream/${e.path}`, `product/${e.target}`);
    if (d.text === '') continue;
    files.push({ target: e.target, merge: !!e.merge, added: d.added, removed: d.removed, text: d.text });
  }

  return { text: render(files, cfg.root), files };
}

function render(files, root) {
  const changed = files.filter((f) => !f.error);
  const broken = files.filter((f) => f.error);
  const added = changed.reduce((n, f) => n + f.added, 0);
  const removed = changed.reduce((n, f) => n + f.removed, 0);

  const head = [
    `Loom patch · ${plural(changed.length, 'file')} changed, `
    + `${plural(added, 'insertion')}(+), ${plural(removed, 'deletion')}(-)`,
    `upstream → product · woven from ${root}, nothing written`,
    '',
  ];

  const width = Math.max(0, ...changed.map((f) => f.target.length));
  const widest = Math.max(0, ...changed.map((f) => f.added + f.removed));
  const digits = String(widest).length;
  for (const f of changed) {
    head.push(` ${f.target.padEnd(width)} | ${String(f.added + f.removed).padStart(digits)} `
      + bar(f.added, f.removed, widest, 40) + (f.merge ? '  (merge)' : ''));
  }
  if (changed.some((f) => f.merge)) {
    head.push('', '(merge) our file is the product: the diff is the edits lm sync carries onto each new upstream.');
  }
  for (const f of broken) head.push(` ${f.target}: ⛔ ${f.error}`);
  if (changed.length === 0 && broken.length === 0) head.push('No template changes upstream.');
  head.push('', ''); // the summary and the first diff are separated by a blank line

  return head.join('\n') + changed.map((f) => f.text).join('\n');
}

function plural(n, word) {
  return `${n} ${word}${n === 1 ? '' : 's'}`;
}

// configFor finds the tree a path belongs to, whether the path is a file or a directory.
//
// loom.findConfig searches upward from the *parent* of what it is given, which is what a file
// wants. The patch is addressed by its tree's root — a directory — and searching from its parent
// walks straight past the loom.lm sitting in it. Joining a name onto the path makes a directory
// behave like a file inside it, and leaves a real file's search exactly where it was: a file has no
// children, so the first step up lands on its own directory again.
function configFor(from) {
  return loom.findConfig(path.join(from, 'loom.lm'));
}

// treeRoot is the loom.lm directory a path belongs to — the patch is one document per tree, so the
// root is what identifies it.
function treeRoot(from) {
  const cfg = configFor(from);
  return cfg && cfg.root;
}

module.exports.treePatch = treePatch;
module.exports.treeRoot = treeRoot;
module.exports.bar = bar;
