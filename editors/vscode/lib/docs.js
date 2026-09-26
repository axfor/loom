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
  Node: '`base.X` an upstream node · `base.X.after` / `base.X.before` a place next to one',
  Group: '`base.sections[level == 2]` a predicate over upstream',
  Template: '`` `- [{name}](#{anchor})` `` — fields `{name}` `{level}` `{body}` `{anchor}`',
};

// PLACE is how a bare place word reads, which every place method's help also says.
const PLACE = (word) => ({ on: 'place', label: `base.<node>.${word}  ·  base.${word}.project(...)`, params: [] });

// A signature's `on` says which receiver it is for: a node, the file (or a view), a key's value, the
// whole frontmatter. Signature help picks the one that fits; hover and completion show them all.
const KEYWORDS = {
  after: {
    what: 'Insert our content after an upstream node (a section, function, key...). Several arguments go in order.',
    signatures: [{ on: 'node', label: 'base.<node>.after(...content: Content)', params: [['...content: Content', 'Content']] }, PLACE('after')],
    examples: ['base.Overview.after("Where this skill sits")', 'base.Install.move(base.Usage.after)', 'base."Example 2".after{\n    "Example 2 notes"\n    self."Example 2 notes"."Phase 1"\n}'],
  },
  before: {
    what: 'Insert our content before an upstream node.',
    signatures: [{ on: 'node', label: 'base.<node>.before(...content: Content)', params: [['...content: Content', 'Content']] }, PLACE('before')],
    examples: ['base.Verification.before("Self-check list")', 'base.main.before(self.boot)'],
  },
  start: {
    what: 'On the file (or a view): insert our content at the start, after the frontmatter. On a key: the value becomes ours followed by upstream\'s — a bilingual description, say.',
    signatures: [
      { on: 'file', label: 'base.start(...content: Content)', params: [['...content: Content', 'Content']] },
      { on: 'value', label: 'base.<key>.start(value: Value)', params: [['value: Value', 'Value']] },
      PLACE('start'),
    ],
    examples: ['base.start(`> Generated from mine/README.md — do not edit`)', 'base.frontmatter.description.start(self.frontmatter.description)'],
  },
  append: {
    what: 'On the file (or a view): insert our content at the end. On a key: the value becomes upstream\'s followed by ours.',
    signatures: [
      { on: 'file', label: 'base.append(...content: Content)', params: [['...content: Content', 'Content']] },
      { on: 'value', label: 'base.<key>.append(value: Value)', params: [['value: Value', 'Value']] },
      PLACE('append'),
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
    what: 'The product is upstream\'s file and ours together; what that means follows the type.\n\n**A script, or any file that can\'t be woven by name** (one changed inside its functions): our file is the product. Edit it as any file; the build still fails when an upstream section or function is gone without a `drop`. `lm sync <new upstream>` merges each new upstream release into our file, leaving conflict markers where upstream changed the lines we changed. Can only be combined with `drop`.\n\n**A json registry** (an event → handler table, like a hooks file): the objects are merged key by key, and in a list an entry of ours takes the place of the upstream entry calling the same scripts — so our version of a handler wins, one upstream adds of its own arrives on its own, and none is ever registered twice. A registry takes this statement and nothing else.',
    signatures: [{ on: 'file', label: 'base.merge(file: self)', params: [['file: self', '`self` — our file at this path']] }],
    examples: ['base.merge(self)', 'base.merge(self)\nbase.help.drop(reason: "we print our own help")'],
  },
  end: {
    what: 'The same as `append`: on the file (or a view), our content at the end; on a key, upstream\'s value followed by ours.',
    signatures: [
      { on: 'file', label: 'base.end(...content: Content)', params: [['...content: Content', 'Content']] },
      { on: 'value', label: 'base.<key>.end(value: Value)', params: [['value: Value', 'Value']] },
      PLACE('end'),
    ],
    examples: ['base.end(self.Troubleshooting)', 'base.description.end(self.description)'],
  },
  move: {
    what: 'Put an upstream node somewhere else. Its own bytes do not change, so no reason is needed.',
    signatures: [
      { on: 'node', label: 'base.<node>.move(to: Node)', params: [['to: Node', 'Node']] },
      { on: 'node', label: 'base.<node>.move(after: Node)  ·  move(before: Node)', params: [['after: Node', 'Node']] },
    ],
    examples: ['base.Install.move(base.Usage.after)', 'base.Install.move(after: base.Usage)'],
  },
  promote: {
    what: 'Take a markdown section, and everything under it, up one heading level. The words do not change.',
    signatures: [{ on: 'node', label: 'base.<section>.promote()', params: [] }],
    examples: ['base."Step 1".promote()', 'base.sections[name ~ "^Step "].promote()'],
  },
  demote: {
    what: 'Take a markdown section, and everything under it, down one heading level. The words do not change.',
    signatures: [{ on: 'node', label: 'base.<section>.demote()', params: [] }],
    examples: ['base.sections[name ~ "^Step "].demote()'],
  },
  wrap: {
    what: 'Put our content before and after an upstream node in one statement: `before` and `after` together.',
    signatures: [{ on: 'node', label: 'base.<node>.wrap(before: Content, after: Content)', params: [['before: Content', 'Content'], ['after: Content', 'Content']] }],
    examples: ['base.Examples.wrap(self.top, self.tail)'],
  },
  swap: {
    what: 'Trade places with another upstream node. Both are read where upstream has them, before either moves.',
    signatures: [{ on: 'node', label: 'base.<node>.swap(other: Node)', params: [['other: Node', 'Node']] }],
    examples: ['base.Install.swap(base.Usage)'],
  },
  unwrap: {
    what: 'Take a markdown section\'s heading out and lift what was under it one level. The heading is upstream content, so a reason is required.',
    signatures: [{ on: 'node', label: 'base.<section>.unwrap(reason: Reason)', params: [['reason: Reason', 'Reason']] }],
    examples: ['base.Details.unwrap(reason: "one level of nesting reads better in our docs")'],
  },
  split: {
    what: 'Cut a markdown section in two. At a heading inside it, that heading becomes the second half at this section\'s level; anywhere else, the second half needs a name, and its heading is ours.',
    signatures: [
      { on: 'node', label: 'base.<section>.split(at: Node)', params: [['at: Node', 'a heading inside the section']] },
      { on: 'node', label: 'base.<section>.split(at: Node, name: Name)', params: [['at: Node', 'where the second half starts'], ['name: Name', 'the second half\'s heading']] },
    ],
    examples: ['base.Setup.split(base.Setup."Step B")', 'base.Setup.split(base.line("Then:"), "Setup, part two")'],
  },
  join: {
    what: 'Run this markdown section and the next together: the next one\'s heading goes. That heading is upstream content, so a reason is required.',
    signatures: [{ on: 'node', label: 'base.<section>.join(reason: Reason)', params: [['reason: Reason', 'Reason']] }],
    examples: ['base.Install.join(reason: "upstream splits one procedure over two headings")'],
  },
  project: {
    what: 'Write content derived from upstream\'s shape rather than its words — a table of contents, say. Written on a place; the template is repeated for each node the predicate finds.',
    signatures: [{ on: 'place', label: 'base.<place>.project(from: Group) { template: Template }', params: [['from: Group', 'Group'], ['template: Template', 'Template']] }],
    examples: ['base.start.project(base.sections[level == 2]){\n    ```markdown\n    - [{name}](#{anchor})\n    ```\n}'],
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
    what: 'Why upstream content is changed. Required by `replace`, `drop`, `unwrap` and `join`, not allowed elsewhere; shown in the build report. After `return self`, it is a comment: `return self // reason: ...`.',
    signatures: [{ on: 'node', label: 'reason: String', params: [] }],
    examples: ['base.Team.drop(reason: "credited in our README\'s License section")'],
  },
  if: {
    what: 'Ask the document something, and write only if it holds. The question is read-only and decided against upstream before anything is written: a part by name, whether a predicate finds anything, or a result caught earlier.',
    signatures: [{ on: 'node', label: 'if base.has.Name { ... } else if ... { ... } else { ... }', params: [] }],
    examples: ['if base.has.Overview {\n    base.Overview.after(self.job)\n} else {\n    base.start(self.job)\n}', 'ok = base.Install.after(self.setup)\nif !ok {\n    return err.format("upstream has no Install: %s", ok)\n}'],
  },
  else: {
    what: 'What an `if` does when its question does not hold. `else if` asks the next question.',
    signatures: [{ on: 'node', label: '} else { ... }  ·  } else if ... { ... }', params: [] }],
    examples: ['if base.has.Overview {\n    base.Overview.after(self.job)\n} else if base.has.Usage {\n    base.Usage.before(self.job)\n}'],
  },
  fn: {
    what: 'Group statements inside this file. A function runs where it is called and returns nothing; its parameters can be addresses.',
    signatures: [{ on: 'node', label: 'fn name(params...) { ... }', params: [] }],
    examples: ['fn bilingual(up, ours) {\n    up.after(ours)\n}\n\nbilingual(base.Overview, self.job)'],
  },
  return: {
    what: '`return self`: the product is our file, and the reason says why. `return err.format(...)`: stop the build with our own message. A bare `return` stops here.',
    signatures: [{ on: 'node', label: 'return self // reason: ...  ·  return err.format(message, ...results)  ·  return', params: [] }],
    examples: ['return self // reason: we maintain this file ourselves', 'return err.format("upstream has no %s", ok)'],
  },
  err: {
    what: 'The build\'s own error, written by `return err.format(...)`: a message, and the results caught earlier that go into it. It never reaches the product.',
    signatures: [{ on: 'node', label: 'return err.format(message: Name, ...results)', params: [] }],
    examples: ['return err.format("upstream has no Install: %s", ok)'],
  },
  Self: {
    what: 'A resource section, after the line of dashes: our content written in this file instead of beside it. Each fence\'s language tag says what kind of document it is; `Self as name:` names a second one.',
    signatures: [{ on: 'node', label: 'Self:  ·  Self as name:', params: [] }],
    examples: ['---\nSelf:\n    ```markdown\n    ## job\n    ```'],
  },
  has: {
    what: 'In an `if`: does upstream have a part by this name. In a predicate: does the node hold a part by this name.',
    signatures: [{ on: 'node', label: 'if base.has.Name  ·  base.sections[has."Name"]', params: [] }],
    examples: ['if base.has.Overview {\n    base.Overview.after(self.job)\n}', 'base.sections[has."Example"]'],
  },
  any: {
    what: 'In an `if`: does the group have anything in it — a predicate\'s, or a bare class for any node of the kind.',
    signatures: [{ on: 'node', label: 'if base.sections[...].any  ·  if base.sections.any', params: [] }],
    examples: ['if base.sections[name ~ "^Step "].any {\n    base.sections[name ~ "^Step "].demote()\n}'],
  },
  count: {
    what: 'In an `if`: the number of nodes in the group, which holds when it is not zero — the same question as `any`.',
    signatures: [{ on: 'node', label: 'if base.sections[...].count', params: [] }],
    examples: ['if base.sections[level == 2].count {\n    base.start.project(base.sections[level == 2], `- {name}`)\n}'],
  },
};

// Words that pick a group, walk the document's structure, or ask about each node of a group.
const GROUP = (word, what) => ({
  what: `Every ${what} of the file; \`[ ... ]\` picks some by a predicate. A group takes what means the same done to each: \`drop\`, and for sections \`promote\`, \`demote\`, \`unwrap\`.`,
  signatures: [{ on: 'node', label: `base.${word}  ·  base.${word}[predicate]`, params: [] }],
  examples: [`base.${word}[empty].drop(reason: "...")`],
});
Object.assign(KEYWORDS, {
  sections: GROUP('sections', 'markdown section'),
  functions: GROUP('functions', 'shell function'),
  markers: GROUP('markers', 'shell banner'),
  lines: GROUP('lines', 'non-blank line'),
  keys: GROUP('keys', 'key'),
  values: GROUP('values', 'key, by what it holds'),
});
const AXIS = (word, what, example) => ({
  what,
  signatures: [{ on: 'node', label: word === 'first' || word === 'last' ? `base.sections[...].${word}` : `base.<section>.${word}`, params: [] }],
  examples: [example],
});
Object.assign(KEYWORDS, {
  children: AXIS('children', 'The sections one level down: a group.', 'base.Usage.children.demote()'),
  next: AXIS('next', 'The next section at the same level.', 'base.Install.next.before(self.note)'),
  prev: AXIS('prev', 'The section before, at the same level.', 'base.Usage.prev.after(self.note)'),
  parent: AXIS('parent', 'The section this one sits in.', 'base."Step 1".parent.after(self.note)'),
  first: AXIS('first', 'The first node of a group.', 'base.sections[level == 2].first.before(self.intro)'),
  last: AXIS('last', 'The last node of a group.', 'base.sections[level == 2].last.after(self.outro)'),
});
const PRED = (word, label, what, example) => ({ what, signatures: [{ on: 'node', label, params: [] }], examples: [example] });
Object.assign(KEYWORDS, {
  level: PRED('level', 'level == 2  ·  != < <= > >=', 'In a predicate: the heading level, 1 to 6.', 'base.sections[level == 2]'),
  name: PRED('name', 'name == "X"  ·  name ~ "regex"', 'In a predicate: the node\'s name, exactly or by a regular expression.', 'base.sections[name ~ "^Step "]'),
  value: PRED('value', 'value == "X"', 'In a predicate: what a key holds. Unlike `empty`, it tells `a = 1` from `b = ""`.', 'base.keys[value == ""]'),
  empty: PRED('empty', 'empty  ·  !empty', 'In a predicate: nothing under the node.', 'base.sections[empty].drop(reason: "...")'),
  calls: PRED('calls', 'calls "command"', 'In a predicate: a shell function that runs this command — where a command goes, not in a comment or a string.', 'base.functions[calls "curl"]'),
});

// The fields a projection template writes for each node it reads (objparse.go's projectFields).
// Any other {word} is refused by the build where it is written.
const PROJECT_FIELDS = {
  name: 'The node\'s name: a heading\'s text, a function\'s name, a key.',
  level: 'The heading level, 1 to 6.',
  body: 'Everything under the heading, as upstream wrote it.',
  anchor: 'GitHub\'s link target for the heading: lower-cased, punctuation removed, spaces made hyphens, a repeat numbered -1, -2. It is GitHub\'s rule because renderers do not agree; a product read elsewhere may need its own.',
};

function fieldMarkdown(field) {
  const what = PROJECT_FIELDS[field];
  if (!what) return null;
  return ['```loom', `{${field}}`, '```', '', `A projection template field. ${what}`, '', '**Example**', '', '```loom',
    'base.start.project(base.sections[level == 2]){\n    ```markdown\n    - [{name}](#{anchor})\n    ```\n}', '```'].join('\n');
}

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

module.exports = { KEYWORDS, OBJECTS, TYPES, PROJECT_FIELDS, typeDoc, markdownFor, fieldMarkdown };
