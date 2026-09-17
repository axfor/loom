'use strict';

// What each Loom keyword does and how it is written: typed signatures, parameters and examples. Shown
// when hovering a keyword, next to completion items, and as signature help while typing arguments.
// Keywords explain themselves instead of jumping somewhere; only objects and node names lead to a file.

// The kinds of argument a method takes.
const TYPES = {
  Content: '`"Name"` a section of our file · `self."A"."B"` a section path · `self.body` / `cmd.body` a part of a file · `` `literal` ``',
  Value: '`self.frontmatter.description` a frontmatter key of ours · `self.description` a toml / json key of ours · `` `literal` ``',
  Frontmatter: '`self.frontmatter`',
  File: '`self` or an imported name',
  Reason: '`reason: "..."` — why upstream content is changed; shown in the build report',
  Type: '`markdown` · `shell` · `toml` · `json` · `text`',
  Name: 'a quoted name',
};

// A signature's `on` says which receiver it is for: a node, the file (or a view), a key's value, the
// whole frontmatter. Signature help picks the one that fits; hover and completion show them all.
const KEYWORDS = {
  after: {
    what: 'Insert our content after an upstream node (a section, function, key...). Several arguments go in order.',
    signatures: [{ on: 'node', label: 'base.<node>.after(...content: Content)', params: [['...content: Content', 'Content']] }],
    examples: ['base.Overview.after("Where this skill sits")', 'base."Example 2".after{\n    "Example 2 notes"\n    self."Example 2 notes"."Phase 1"\n}'],
  },
  before: {
    what: 'Insert our content before an upstream node.',
    signatures: [{ on: 'node', label: 'base.<node>.before(...content: Content)', params: [['...content: Content', 'Content']] }],
    examples: ['base.Verification.before("Self-check list")', 'base.main.before(self.boot)'],
  },
  start: {
    what: 'On the file (or a view): insert our content at the start, after the frontmatter. On a key: the value becomes ours followed by upstream\'s — a bilingual description, say.',
    signatures: [
      { on: 'file', label: 'base.start(...content: Content)', params: [['...content: Content', 'Content']] },
      { on: 'value', label: 'base.<key>.start(value: Value)', params: [['value: Value', 'Value']] },
    ],
    examples: ['base.start(`> Generated from mine/README.md — do not edit`)', 'base.frontmatter.description.start(self.frontmatter.description)'],
  },
  append: {
    what: 'On the file (or a view): insert our content at the end. On a key: the value becomes upstream\'s followed by ours.',
    signatures: [
      { on: 'file', label: 'base.append(...content: Content)', params: [['...content: Content', 'Content']] },
      { on: 'value', label: 'base.<key>.append(value: Value)', params: [['value: Value', 'Value']] },
    ],
    examples: ['base.append("Troubleshooting")', 'base.prompt.as(markdown).append(cmd.body)', 'base.frontmatter.description.append(self.frontmatter.description)'],
  },
  replace: {
    what: 'Replace an upstream node with our content, or the whole file with ours. Upstream content goes away, so a reason is required; a whole-file replace can only be combined with `drop`.',
    signatures: [
      { on: 'node', label: 'base.<node>.replace(content: Content, reason: Reason)', params: [['content: Content', 'Content'], ['reason: Reason', 'Reason']] },
      { on: 'file', label: 'base.replace(file: File, reason: Reason)', params: [['file: File', 'File'], ['reason: Reason', 'Reason']] },
    ],
    examples: [
      'base.Install.replace("Install XSDD", reason: "our installer replaces the manual steps")',
      'base.replace(self, reason: "this file describes our repository, not upstream\'s")',
    ],
  },
  drop: {
    what: 'Leave an upstream node out of the product on purpose. The build checks it really is absent, and a section with subsections needs each subsection accounted for.',
    signatures: [{ on: 'node', label: 'base.<node>.drop(reason: Reason)', params: [['reason: Reason', 'Reason']] }],
    examples: ['base."How it compares".drop(reason: "compares upstream with other projects; not ours")'],
  },
  set: {
    what: 'Take ours: the whole frontmatter, or one key\'s value. On a frontmatter key upstream lacks, set adds it. The build fails when a key of our frontmatter reaches the product no way at all.',
    signatures: [
      { on: 'frontmatter', label: 'base.frontmatter.set(frontmatter: Frontmatter)', params: [['frontmatter: Frontmatter', 'Frontmatter']] },
      { on: 'value', label: 'base.<key>.set(value: Value)', params: [['value: Value', 'Value']] },
    ],
    examples: ['base.frontmatter.set(self.frontmatter)', 'base.frontmatter."argument-hint".set(self.frontmatter."argument-hint")', 'base.description.set(self.description)'],
  },
  merge: {
    what: 'Our file is upstream plus our edits, and it is the product — for files that can\'t be woven by name, like a script changed inside its functions. Edit our file as any file; the build still fails when an upstream section or function is gone without a `drop`. `lm sync <new upstream>` merges each new upstream release into our file, leaving conflict markers where upstream changed the lines we changed. Can only be combined with `drop`.',
    signatures: [{ on: 'file', label: 'base.merge(file: self)', params: [['file: self', '`self` — our file at this path']] }],
    examples: ['base.merge(self)', 'base.merge(self)\nbase.help.drop(reason: "we print our own help")'],
  },
  as: {
    what: 'View a toml / json key\'s value as another type, then use that type\'s nodes and methods on it. Inside the view, a bare string names a section of the same key in our file.',
    signatures: [{ on: 'value', label: 'base.<key>.as(type: Type)', params: [['type: Type', 'Type']] }],
    examples: ['base.prompt.as(markdown).append(cmd.body)', 'base.prompt.as(markdown).Steps.after("Our step")'],
  },
  section: {
    what: 'A markdown section by its heading — the same as `base."Heading"`: from the heading to the next heading of any level.',
    signatures: [{ on: 'file', label: 'base.section(heading: Name)', params: [['heading: Name', 'the heading text']] }],
    examples: ['base.section("How it compares").drop(reason: "...")'],
  },
  line: {
    what: 'A line matched exactly, in markdown, shell or text files.',
    signatures: [{ on: 'file', label: 'base.line(text: Name)', params: [['text: Name', 'the line\'s full text']] }],
    examples: ['base.line("set -e").after(`set -u`)'],
  },
  function: {
    what: 'A shell function — the same as `base.name`.',
    signatures: [{ on: 'file', label: 'base.function(name: Name)', params: [['name: Name', 'the function name']] }],
    examples: ['base.function("main").before(self.boot)'],
  },
  marker: {
    what: 'A shell banner comment down to the next banner, matched by the start of its line.',
    signatures: [{ on: 'file', label: 'base.marker(start: Name)', params: [['start: Name', 'the start of the banner line, `#` included']] }],
    examples: ['base.marker("# ── Test 3").after(self.marker("# ── Test 3b"))'],
  },
  key: {
    what: 'A toml key, or a json key by dotted path — the same as `base.name`.',
    signatures: [{ on: 'file', label: 'base.key(name: Name)', params: [['name: Name', 'the key, or the dotted path in json']] }],
    examples: ['base.key("description").set(self.description)'],
  },
  frontmatter: {
    what: 'The `---` block at the top of a markdown file. Its keys are nodes of their own: `base.frontmatter.description`, quoted when a key has a `-`.',
    signatures: [{ on: 'node', label: 'base.frontmatter  ·  base.frontmatter.<key>  ·  self.frontmatter.<key>', params: [] }],
    examples: ['base.frontmatter.description.start(self.frontmatter.description)', 'base.frontmatter.set(self.frontmatter)'],
  },
  body: {
    what: 'Everything after the frontmatter of one of our files — content to insert.',
    signatures: [{ on: 'node', label: 'self.body  ·  cmd.body', params: [] }],
    examples: ['base.prompt.as(markdown).append(cmd.body)'],
  },
  import: {
    what: 'Another file of our layer, to take content from. `./` and `../` are relative to the product\'s directory, `/` starts at the layer root; the extension may be left out when one file matches. Without a name, the file name is the name. `import base "path"` points base at a renamed upstream file.',
    signatures: [{ on: 'file', label: 'import [name: Identifier] path: String', params: [] }],
    examples: ['import cmd "/.claude/commands/ship"\nbase.append(cmd.body)', 'import base "/.gemini/commands/planning"'],
  },
  reason: {
    what: 'Why upstream content is changed. Required by `replace` and `drop`, not allowed elsewhere; shown in the build report.',
    signatures: [{ on: 'node', label: 'reason: String', params: [] }],
    examples: ['base.Team.drop(reason: "credited in our README\'s License section")'],
  },
};

const OBJECTS = {
  base: 'The upstream file at this path (or where `import base` points) — the only object a template changes.',
  self: 'Our file at this path — a content source; methods are never called on it.',
};

// typeDoc explains a parameter's type: a name from TYPES, or the text itself.
function typeDoc(t) {
  return TYPES[t] || t;
}

// markdownFor renders a keyword's help as markdown; null for a word that is not a keyword.
function markdownFor(word) {
  const k = KEYWORDS[word];
  if (!k) return null;
  const parts = ['```loom', k.signatures.map((s) => s.label).join('\n'), '```', '', k.what];
  const params = [];
  for (const sig of k.signatures) {
    for (const [label, type] of sig.params) {
      const line = `- \`${label}\` — ${typeDoc(type)}`;
      if (!params.includes(line)) params.push(line);
    }
  }
  if (params.length) parts.push('', '**Parameters**', '', ...params);
  parts.push('', '**Examples**', '', '```loom', k.examples.join('\n\n'), '```');
  return parts.join('\n');
}

module.exports = { KEYWORDS, OBJECTS, TYPES, typeDoc, markdownFor };
