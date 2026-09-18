'use strict';

// Preview: the product a template builds, woven by the compiler itself, from the template as it is in
// the editor — saved or not. No `vscode` import here, so it is tested in plain Node.

const os = require('os');
const path = require('path');
const { spawn } = require('child_process');
const loom = require('./loom');

// findLm: the lm command to run — the configured path, else lm on PATH, else Go's install directory.
function findLm(configured, env = process.env) {
  if (configured) return configured;
  for (const dir of (env.PATH || '').split(path.delimiter)) {
    if (dir && loom.isFile(path.join(dir, 'lm'))) return path.join(dir, 'lm');
  }
  const gobin = env.GOBIN || path.join(env.GOPATH || path.join(os.homedir(), 'go'), 'bin');
  return loom.isFile(path.join(gobin, 'lm')) ? path.join(gobin, 'lm') : null;
}

// productName: the file a template builds, for the preview's title and language — SKILL.lm gives
// SKILL.md. When it can't be told, the template name minus .lm.
function productName(templatePath) {
  const cfg = loom.findConfig(templatePath);
  const target = cfg && loom.targetOf(cfg, templatePath);
  return path.basename(target || templatePath.replace(/\.lm$/, ''));
}

// upstreamFile: the upstream file a template weaves onto — where `import base` points, else the same
// path upstream — for the review view's left side; null when there is none.
function upstreamFile(templatePath, text) {
  const t = loom.open(templatePath, text);
  const file = t && t.objects.base.file;
  return file && loom.isFile(file) ? file : null;
}

// weave runs `lm weave -stdin <template>` with the editor's text and resolves to the product, or to
// the compiler's error as { error }.
function weave(lm, templatePath, text, timeoutMs = 10000) {
  return new Promise((resolve) => {
    if (!lm) {
      resolve({ error: 'lm is not installed: go install github.com/axfor/loom/cmd/lm@latest, or set loom.path' });
      return;
    }
    const child = spawn(lm, ['weave', '-stdin', templatePath], { cwd: path.dirname(templatePath) });
    let out = '';
    let err = '';
    const timer = setTimeout(() => child.kill(), timeoutMs);
    child.stdout.on('data', (d) => (out += d));
    child.stderr.on('data', (d) => (err += d));
    // An lm that exits before reading the template breaks the pipe mid-write; its own exit says why.
    child.stdin.on('error', () => {});
    child.on('error', (e) => {
      clearTimeout(timer);
      resolve({ error: `cannot run ${lm}: ${e.message}` });
    });
    child.on('close', (code) => {
      clearTimeout(timer);
      resolve(code === 0 ? { product: out } : { error: (err || `lm weave exited with ${code}`).trim() });
    });
    child.stdin.end(text);
  });
}

module.exports = { findLm, productName, upstreamFile, weave };
