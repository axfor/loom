'use strict';

// Diagnostics: lm's own errors and warnings, turned into editor squiggles.
//
// No `vscode` import here, so all of it runs (and is tested) in plain Node, like the rest of lib/.
//
// lm prints one error per line on stderr, the first prefixed `⛔ `, positioned like the Go compiler:
// `path:line:col: message`. Where a template pulls in a file that is at fault, the message carries a
// second position of its own (`template:l:c: content:l:c: variable ... is not defined`). The outer
// position is the template — the file being edited — so that is where the squiggle goes, and the
// inner one stays in the message, which is where it reads.

const path = require('path');
const { spawn } = require('child_process');
const loom = require('./loom');

// POS splits the leading `path:line:col: ` off an error line. The path is taken up to the *first*
// `:line:col:`, so a nested position stays part of the message, and a Windows drive letter (`C:\`)
// is not mistaken for one.
const POS = /^(.*?):(\d+):(\d+): ([\s\S]*)$/;

// UNUSED matches the build report's warning for a variable nothing expands. Unlike an error it
// carries no position, so the name is looked up in the variables file to place it.
const UNUSED = /^\s*⚠ variable (\S+) is defined but never used\s*$/;

function isSettings(file) {
  return path.basename(file) === 'loom.lm';
}

function isVars(file) {
  return path.extname(file) === '.e';
}

// inTree: is this document part of a Loom tree — is there a loom.lm above it?
//
// A .lm or .e file with none is not something lm can build, and that is not the author's mistake:
// the extension also highlights Loom syntax in a repository that merely documents it, and the
// examples it ships are read, not woven. One "found no loom.lm" error across every such file would
// be noise, so there is nothing to say. A loom.lm is itself the root of a tree, so it always counts.
function inTree(file) {
  if (isSettings(file)) return true;
  let dir = path.dirname(path.resolve(file));
  for (;;) {
    if (loom.isFile(path.join(dir, 'loom.lm'))) return true;
    const parent = path.dirname(dir);
    if (parent === dir) return false;
    dir = parent;
  }
}

// commandFor: the lm invocation that diagnoses this document.
//   a template  → `weave -stdin <path>`: the editor's text is piped in, so an edit is diagnosed
//                 before it is saved, and only this one template is woven, which keeps it quick.
//   loom.lm     → `check`: settings errors only surface when the tree is loaded against them.
//   a .e file   → `check -e <path>`: -e reads only that file, so the variables being edited are the
//                 ones checked and lm.e is never silently used instead.
function commandFor(file) {
  if (isSettings(file)) return { args: ['check'], stdin: false };
  if (isVars(file)) return { args: ['check', '-e', file], stdin: false };
  return { args: ['weave', '-stdin', file], stdin: true };
}

function at(line, col, message, severity) {
  return { line, col, message, severity };
}

// lineOf finds where a variables file defines name, for a warning that comes without a position.
function lineOf(varsText, name) {
  const lines = varsText.split('\n');
  const want = new RegExp(`^\\s*${name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\s*=`);
  const i = lines.findIndex((l) => want.test(l));
  return i < 0 ? 0 : i;
}

// parse turns one lm run into this document's diagnostics.
//
// An error naming another file is still reported, at the top of this one: the tree does not build,
// and saying so in the wrong place beats an editor that looks clean. Duplicates are dropped — the
// same fault is often reported once against the content file and once against the template.
function parse(file, stdout, stderr, varsText) {
  const out = [];
  const seen = new Set();
  const add = (d) => {
    const key = `${d.line}:${d.col}:${d.severity}:${d.message}`;
    if (seen.has(key)) return;
    seen.add(key);
    out.push(d);
  };

  for (const raw of stderr.split('\n')) {
    const line = raw.replace(/^⛔ /, '').trimEnd();
    if (!line) continue;
    const m = POS.exec(line);
    if (!m) {
      add(at(0, 0, line, 'error'));
      continue;
    }
    const [, p, l, c, message] = m;
    if (path.resolve(p) === path.resolve(file)) add(at(Number(l) - 1, Number(c) - 1, message, 'error'));
    else add(at(0, 0, line, 'error'));
  }

  // The report only names an unused variable, so it is placed only in the file that defines it.
  if (varsText != null) {
    for (const raw of stdout.split('\n')) {
      const m = UNUSED.exec(raw);
      if (m) add(at(lineOf(varsText, m[1]), 0, `variable ${m[1]} is defined but never used`, 'warning'));
    }
  }
  return out;
}

// run spawns lm and collects both streams. It never rejects: every failure is a diagnostic.
function run(lm, args, cwd, stdin, timeoutMs) {
  return new Promise((resolve) => {
    const child = spawn(lm, args, { cwd });
    let out = '';
    let err = '';
    const timer = setTimeout(() => child.kill(), timeoutMs);
    child.stdout.on('data', (d) => (out += d));
    child.stderr.on('data', (d) => (err += d));
    // An lm that exits before reading the template breaks the pipe mid-write; its exit says why.
    child.stdin.on('error', () => {});
    child.on('error', (e) => {
      clearTimeout(timer);
      resolve({ out: '', err: '', failed: `cannot run ${lm}: ${e.message}` });
    });
    child.on('close', (code) => {
      clearTimeout(timer);
      resolve({ out, err, code });
    });
    if (stdin !== null) child.stdin.end(stdin);
    else child.stdin.end();
  });
}

// diagnose runs the right lm command for one Loom document and resolves to its diagnostics.
// text is the document as the editor has it; it is piped to lm only for a template.
function diagnose(lm, file, text, timeoutMs = 10000) {
  // Not part of a tree comes first: such a file has nothing to say either way, and telling someone
  // who only opened a stray .lm file to install lm would be advice about a build they never asked for.
  if (!inTree(file)) return Promise.resolve([]);
  if (!lm) {
    return Promise.resolve([at(0, 0, 'lm is not installed: go install github.com/axfor/loom/cmd/lm@latest, or set loom.path', 'error')]);
  }
  const { args, stdin } = commandFor(file);
  return run(lm, args, path.dirname(file), stdin ? text : null, timeoutMs).then((r) => {
    if (r.failed) return [at(0, 0, r.failed, 'error')];
    return parse(file, r.out, r.err, isVars(file) ? text : null);
  });
}

module.exports = { commandFor, diagnose, inTree, isSettings, isVars, lineOf, parse };
