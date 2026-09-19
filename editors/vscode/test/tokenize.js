// Tokenizes Loom samples with vscode-textmate (the engine VS Code itself uses) and checks
// that each token lands in the expected scope. A grammar that loads but mis-scopes a word
// looks fine until someone opens a file, so every rule here has a case that must match.
'use strict';

const fs = require('fs');
const path = require('path');
const vsctm = require('vscode-textmate');
const oniguruma = require('vscode-oniguruma');

const root = path.join(__dirname, '..');
const wasm = fs.readFileSync(require.resolve('vscode-oniguruma/release/onig.wasm')).buffer;
const onigLib = oniguruma.loadWASM(wasm).then(() => ({
  createOnigScanner: (p) => new oniguruma.OnigScanner(p),
  createOnigString: (s) => new oniguruma.OnigString(s),
}));

const files = {
  'source.loom': 'syntaxes/loom.tmLanguage.json',
  'source.loom-om': 'syntaxes/loom-om.tmLanguage.json',
  'source.loom-env': 'syntaxes/loom-env.tmLanguage.json',
  'loom.placeholder.injection': 'syntaxes/loom-placeholder.injection.json',
};
// A stand-in host for the injection: an empty shell grammar. The injection must add
// placeholder scopes to it without any help from the host.
const host = JSON.stringify({ scopeName: 'source.shell', patterns: [] });

const registry = new vsctm.Registry({
  onigLib,
  loadGrammar: async (scope) => {
    if (scope === 'source.shell') return vsctm.parseRawGrammar(host, 'shell.json');
    // The embedded grammars are VS Code's own; here a shell stands in, so the test checks that
    // the fence hands over to them rather than what they then do.
    if (!files[scope] && (scope.startsWith('text.') || scope.startsWith('source.'))) {
      return vsctm.parseRawGrammar(JSON.stringify({ scopeName: scope, patterns: [] }), 'stub.json');
    }
    const rel = files[scope];
    if (!rel) return null;
    const p = path.join(root, rel);
    return vsctm.parseRawGrammar(fs.readFileSync(p, 'utf8'), p);
  },
  getInjections: (scope) => (scope === 'source.shell' ? ['loom.placeholder.injection'] : undefined),
});

let pass = 0;
let fail = 0;

// scopesAt returns the scopes of the token covering the first character of `text` in `line`.
function scopesAt(grammar, line, text) {
  const col = line.indexOf(text);
  if (col < 0) throw new Error(`"${text}" is not in "${line}"`);
  const { tokens } = grammar.tokenizeLine(line, vsctm.INITIAL);
  const t = tokens.find((x) => x.startIndex <= col && col < x.endIndex);
  return t ? t.scopes : [];
}

function expect(grammar, line, text, scope) {
  const scopes = scopesAt(grammar, line, text);
  if (scopes.includes(scope)) {
    pass++;
  } else {
    fail++;
    console.log(`  ❌ ${JSON.stringify(line)} → "${text}" want ${scope}, got ${scopes.join(' ')}`);
  }
}

function expectNot(grammar, line, text, scope) {
  const scopes = scopesAt(grammar, line, text);
  if (!scopes.includes(scope)) {
    pass++;
  } else {
    fail++;
    console.log(`  ❌ ${JSON.stringify(line)} → "${text}" must not be ${scope}`);
  }
}

(async () => {
  const lm = await registry.loadGrammar('source.loom');
  const om = await registry.loadGrammar('source.loom-om');
  const env = await registry.loadGrammar('source.loom-env');
  const sh = await registry.loadGrammar('source.shell');

  // Objects, nodes, methods
  const L1 = 'base.Install.after("Install XSDD")';
  expect(lm, L1, 'base', 'variable.language.loom');
  expect(lm, L1, 'Install', 'variable.other.property.loom');
  expect(lm, L1, 'after', 'entity.name.function.method.loom');
  expect(lm, L1, 'Install XSDD', 'string.quoted.double.loom');
  expect(lm, L1, '(', 'punctuation.section.parens.begin.loom');
  for (const m of ['before', 'start', 'append', 'replace', 'drop', 'set', 'merge']) {
    expect(lm, `base.${m}("x")`, m, 'entity.name.function.method.loom');
  }
  expect(lm, 'base.Usage_Tips.after("Usage tips")', 'Usage_Tips', 'variable.other.property.loom');
  expect(lm, 'base.Überblick_Details.after("x")', 'Überblick_Details', 'variable.other.property.loom');

  // A node written as a string; a method with a block
  const L2 = 'base."How it compares".drop(reason: "does not apply to XSDD")';
  expect(lm, L2, '"How it compares"', 'string.quoted.double.loom');
  expect(lm, L2, 'drop', 'entity.name.function.method.loom');
  expect(lm, L2, 'reason', 'variable.parameter.loom');
  expect(lm, L2, ':', 'punctuation.separator.key-value.loom');
  expect(lm, 'base.Overview.after{', 'after', 'entity.name.function.method.loom');
  expect(lm, 'base.Overview.after{', '{', 'punctuation.section.block.begin.loom');
  expect(lm, '}', '}', 'punctuation.section.block.end.loom');

  // self, imported objects, parts of a file, kinds, views
  const L3 = 'base.prompt.as(markdown).append(cmd.body)';
  expect(lm, L3, 'as(markdown)', 'keyword.operator.as.loom');
  expect(lm, L3, 'markdown', 'entity.name.type.format.loom');
  expect(lm, L3, 'cmd', 'variable.other.object.loom');
  expect(lm, L3, 'body', 'support.variable.property.loom');
  expect(lm, 'base.frontmatter.set(self.frontmatter)', 'self', 'variable.language.loom');
  expect(lm, 'base.frontmatter.set(self.frontmatter)', 'frontmatter', 'support.variable.property.loom');
  expect(lm, 'base.marker("Test 3").after("Test 3b")', 'marker', 'support.type.kind.loom');
  expect(lm, 'base.section("Test 3").after("x")', 'section', 'support.type.kind.loom');
  expect(lm, 'base.replace(self, reason: "rewritten in full")', ',', 'punctuation.separator.comma.loom');

  // import
  const L4 = 'import cmd "/.claude/commands/ship"';
  expect(lm, L4, 'import', 'keyword.control.import.loom');
  expect(lm, L4, 'cmd', 'variable.other.object.loom');
  expect(lm, L4, '/.claude', 'string.quoted.double.loom');
  expect(lm, 'import base "/old/guide"', 'base', 'variable.language.loom');
  expect(lm, 'import "./notes"', 'import', 'keyword.control.import.loom');

  // Literals: backticks, across lines
  expect(lm, 'base.start(`Read this first`)', 'Read this first', 'string.quoted.other.raw.loom');
  {
    const { tokens: t1, ruleStack } = lm.tokenizeLine('base.start{', vsctm.INITIAL);
    const r2 = lm.tokenizeLine('    `', ruleStack);
    const r3 = lm.tokenizeLine('    ## A multi-line literal after', r2.ruleStack);
    const tok = r3.tokens.find((x) => x.startIndex <= 12 && 12 < x.endIndex);
    if (t1.length && tok && tok.scopes.includes('string.quoted.other.raw.loom')) pass++;
    else {
      fail++;
      console.log(`  ❌ a line inside a multi-line literal is not literal: ${tok && tok.scopes.join(' ')}`);
    }
  }

  // Typos show up before build does
  expect(lm, 'base.Overview.aftr("x")', 'aftr', 'invalid.illegal.unknown-method.loom');
  expect(lm, 'base.X.drop(resaon: "x")', 'resaon', 'invalid.illegal.unknown-argument.loom');
  expect(lm, 'apend "x"', 'apend', 'invalid.illegal.unknown-statement.loom');
  expectNot(lm, '    "Install XSDD"', '"Install XSDD"', 'invalid.illegal.unknown-statement.loom');
  expectNot(lm, '    self.Café', 'self', 'invalid.illegal.unknown-statement.loom');
  expectNot(lm, '    reason: "x"', 'reason', 'invalid.illegal.unknown-statement.loom');
  expectNot(lm, 'base.Overview.after("x")', 'Overview', 'invalid.illegal.unknown-method.loom');

  // Comments: `//` to end of line, never inside a string
  expect(lm, '// src/templates/README.md.lm', '// src', 'comment.line.double-slash.loom');
  expect(lm, 'base.X.after("a") // trailing comment', '// trailing', 'comment.line.double-slash.loom');
  expect(lm, 'base."C// notes".drop(reason: "x")', 'C//', 'string.quoted.double.loom');
  expectNot(lm, 'base."C// notes".drop(reason: "x")', 'drop', 'comment.line.double-slash.loom');

  // Settings are not template syntax: they live in loom.om, which is its own language. A setting
  // word at the start of a template line is a mistake, and lm rejects it too.
  expect(lm, 'base.Install.after("x")', 'base', 'variable.language.loom');
  expectNot(lm, 'base.Install.after("x")', 'base', 'keyword.control.config.loom');
  for (const k of ['templates', 'output', 'take', 'mirror', 'manifest', 'mark', 'registry']) {
    expect(lm, `${k} "x"`, k, 'invalid.illegal.unknown-statement.loom');
    expectNot(lm, `${k} "x"`, k, 'keyword.control.config.loom');
  }

  // Escapes: only \" and \\; any other backslash (a regex's \.) is plain string text
  expect(lm, 'base."a\\"b".drop(reason: "x")', '\\"', 'constant.character.escape.loom');
  expect(lm, 'registry "hooks" "x\\.(sh|js)"', '\\.', 'string.quoted.double.loom');
  expectNot(lm, 'registry "hooks" "x\\.(sh|js)"', '\\.', 'constant.character.escape.loom');

  // Placeholders inside strings and literals
  expect(lm, 'base.append("repository {{@url}}")', 'url', 'variable.other.placeholder.loom');
  expect(lm, 'base.append(`repository {{@url}}`)', 'url', 'variable.other.placeholder.loom');
  expect(lm, 'base.append("literal {{@@url}}")', '{{@@url}}', 'constant.character.escape.placeholder.loom');

  // Variable files
  expect(env, 'url = https://github.com/axfor/XSDD', 'url', 'variable.other.readwrite.loom-env');
  expect(env, 'url = https://github.com/axfor/XSDD', '=', 'keyword.operator.assignment.loom-env');
  expect(env, 'url = https://github.com/axfor/XSDD', 'https', 'string.unquoted.value.loom-env');
  expect(env, 'doc = https://x.example/#anchor', '#anchor', 'string.unquoted.value.loom-env');
  expectNot(env, 'doc = https://x.example/#anchor', '#anchor', 'comment.line.number-sign.loom-env');
  expect(env, '# lm.e (the default)', '# lm.e', 'comment.line.number-sign.loom-env');
  expect(env, 'not a variable line', 'not', 'invalid.illegal.line.loom-env');
  expect(env, '9url = x', '9url', 'invalid.illegal.line.loom-env');

  // loom.om is its own language: the settings file shares no syntax with a template, and a word
  // that means an object there means a setting here.
  expect(om, 'base      "upstream"', 'base', 'keyword.control.config.loom');
  expect(om, 'self      "mine"', 'self', 'keyword.control.config.loom');
  expect(om, 'output    "../dist"', 'output', 'keyword.control.config.loom');
  expect(om, 'take      "references/**" "LICENSE"', 'take', 'keyword.control.config.loom');
  expect(om, 'base      "upstream"', '"upstream"', 'string.quoted.double.loom');
  expect(om, 'mark      markdown "<!-- B -->" "<!-- E -->"', 'mark', 'keyword.control.config.loom');
  expect(om, 'mark      markdown "<!-- B -->" "<!-- E -->"', 'markdown', 'entity.name.type.format.loom');
  expect(om, '// which layer is upstream', '// which', 'comment.line.double-slash.loom');
  // A template statement is not a setting: in loom.om it is a mistake, and lm says so too.
  expect(om, 'import ship "/x"', 'import', 'invalid.illegal.unknown-setting.loom');
  expect(om, 'bogus "x"', 'bogus', 'invalid.illegal.unknown-setting.loom');
  expectNot(om, 'base      "upstream"', 'base', 'invalid.illegal.unknown-setting.loom');

  // A fence carries a language tag, and its content is highlighted as that language — which is
  // what makes content written inside a template bearable to edit.
  expect(lm, 'base.X.after(```markdown', '```', 'punctuation.definition.string.begin.loom');
  expect(lm, 'base.X.after(```markdown', 'markdown', 'entity.name.type.format.loom');
  // Multi-line: the tokenizer's state has to be carried from line to line, which is how a
  // fence keeps holding its language across the lines inside it.
  const across = (lines, at) => {
    let rules = vsctm.INITIAL;
    let scopes = [];
    for (const l of lines) {
      const r = lm.tokenizeLine(l, rules);
      rules = r.ruleStack;
      scopes = r.tokens.map((t) => t.scopes.join(' '));
    }
    return scopes.join(' | ');
  };
  const md = across(['base.X.after(```markdown', '## A heading']);
  if (/meta\.embedded\.block\.markdown/.test(md)) pass++;
  else { fail++; console.log('  ❌ inside a markdown fence the content is markdown:', md); }
  const shf = across(['base.X.after(```sh', 'echo hi']);
  if (/meta\.embedded\.block\.sh/.test(shf)) pass++;
  else { fail++; console.log('  ❌ a sh fence embeds shell:', shf); }
  const plain = across(['base.X.after(```', 'just text']);
  if (/string\.quoted\.other\.raw/.test(plain)) pass++;
  else { fail++; console.log('  ❌ an untagged fence is still a literal:', plain); }

  // Predicates: match: and level: are named arguments too, and level: takes a number
  expect(lm, 'base.sections(match: "^Step ").demote()', 'match', 'variable.parameter.loom');
  expect(lm, 'base.sections(level: 3).demote()', 'level', 'variable.parameter.loom');
  expect(lm, 'base.sections(level: 3).demote()', '3', 'constant.numeric.loom');
  expect(lm, 'base.sections(levle: 3).demote()', 'levle', 'invalid.illegal.unknown-argument.loom');
  expectNot(lm, 'base.sections(match: "^Step ").demote()', 'match', 'invalid.illegal.unknown-argument.loom');

  // Injection: placeholders in a host language
  expect(sh, 'echo "Repository: {{@url}}"', 'url', 'variable.other.placeholder.loom');
  expect(sh, 'echo "{{@@url}}"', '{{@@url}}', 'constant.character.escape.placeholder.loom');
  expectNot(sh, 'echo "{{ .Values.url }}"', '.Values', 'variable.other.placeholder.loom');

  console.log(`loom grammar: ${pass} passed, ${fail} failed`);
  process.exit(fail === 0 ? 0 : 1);
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
