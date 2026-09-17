'use strict';

// What each Loom keyword does and how it is written, with examples: shown when hovering a keyword
// and next to completion items. Keywords explain themselves instead of jumping somewhere; only the
// objects (base, self, imported names) and the names of nodes lead to a file.

const KEYWORDS = {
  after: {
    usage: 'base.<node>.after(content, ...)',
    what: 'Insert our content after an upstream node (a section, function, key...).',
    args: 'Our sections by name (`"Name"`), a section path on self (`self."A"."B"`), a part of an imported file (`cmd.body`), or a `` `literal` ``. Several arguments go in order.',
    examples: [
      'base.Overview.after("Where this skill sits")',
      'base."Example 2".after{\n    "Example 2 notes"\n    self."Example 2 notes"."Phase 1"\n}',
    ],
  },
  before: {
    usage: 'base.<node>.before(content, ...)',
    what: 'Insert our content before an upstream node.',
    args: 'Same as `after`.',
    examples: ['base.Verification.before("Self-check list")', 'base.main.before(self.boot)'],
  },
  start: {
    usage: 'base.start(content, ...)',
    what: 'Insert our content at the start of the file (after its frontmatter), or of a view.',
    args: 'Same as `after`.',
    examples: ['base.start(`> Generated from mine/README.md — do not edit`)'],
  },
  append: {
    usage: 'base.append(content, ...)',
    what: 'Insert our content at the end of the file, or of a view.',
    args: 'Same as `after`.',
    examples: ['base.append("Troubleshooting")', 'base.prompt.as(markdown).append(cmd.body)'],
  },
  replace: {
    usage: 'base.<node>.replace(content, reason: "...")  ·  base.replace(self, reason: "...")',
    what: 'Replace an upstream node with our content, or the whole file with ours. Upstream content goes away, so a reason is required; a whole-file replace can only be combined with `drop`.',
    args: 'One content argument (a whole-file replace takes a file: `self` or an imported name) and `reason:`.',
    examples: [
      'base.Install.replace("Install XSDD", reason: "our installer replaces the manual steps")',
      'base.replace(self, reason: "this file describes our repository, not upstream\'s")',
    ],
  },
  drop: {
    usage: 'base.<node>.drop(reason: "...")',
    what: 'Leave an upstream node out of the product on purpose. The build checks it really is absent, and a section with subsections needs each subsection accounted for.',
    args: 'Only `reason:`.',
    examples: ['base."How it compares".drop(reason: "compares upstream with other projects; not ours")'],
  },
  set: {
    usage: 'base.frontmatter.set(self.frontmatter)  ·  base.<key>.set(self.<key>)',
    what: 'Take a value from our file: the whole frontmatter of a markdown file, or a toml / json key. With `join` on the same frontmatter, set only matters for the keys join does not name; the build fails when a key of our frontmatter reaches the product neither way.',
    args: 'One value from our layer.',
    examples: ['base.frontmatter.set(self.frontmatter)', 'base.description.set(self.description)'],
  },
  join: {
    usage: 'base.frontmatter.join("key", ...)  ·  base.join("key", ...)',
    what: 'The value becomes ours followed by upstream\'s — a bilingual description, say. In markdown on frontmatter keys; in toml / json on the file.',
    args: 'Quoted key names.',
    examples: ['base.frontmatter.join("description")', 'base.join("description")'],
  },
  merge: {
    usage: 'base.merge(self)',
    what: 'Our file is upstream plus our edits, and it is the product — for files that can\'t be woven by name, like a script changed inside its functions. Edit our file as any file; the build still fails when an upstream section or function is gone without a `drop`. `lm sync <new upstream>` merges each new upstream release into our file, leaving conflict markers where upstream changed the lines we changed. Can only be combined with `drop`.',
    args: '`self`.',
    examples: ['base.merge(self)', 'base.merge(self)\nbase.help.drop(reason: "we print our own help")'],
  },
  as: {
    usage: 'base.<key>.as(type)',
    what: 'View a toml / json key\'s value as another type, then use that type\'s nodes and methods on it. Inside the view, a bare string names a section of the same key in our file.',
    args: 'A type: `markdown`, `shell`, `toml`, `json`, `text`.',
    examples: ['base.prompt.as(markdown).append(cmd.body)', 'base.prompt.as(markdown).Steps.after("Our step")'],
  },
  section: {
    usage: 'base.section("Heading")',
    what: 'A markdown section by its heading — the same as `base."Heading"`: from the heading to the next heading of any level.',
    args: 'The heading text.',
    examples: ['base.section("How it compares").drop(reason: "...")'],
  },
  line: {
    usage: 'base.line("the whole line")',
    what: 'A line matched exactly, in markdown, shell or text files.',
    args: 'The line\'s full text.',
    examples: ['base.line("set -e").after(`set -u`)'],
  },
  function: {
    usage: 'base.function("name")',
    what: 'A shell function — the same as `base.name`.',
    args: 'The function name.',
    examples: ['base.function("main").before(self.boot)'],
  },
  marker: {
    usage: 'base.marker("# ── Name")',
    what: 'A shell banner comment down to the next banner, matched by the start of its line.',
    args: 'The start of the banner line, `#` included.',
    examples: ['base.marker("# ── Test 3").after(self.marker("# ── Test 3b"))'],
  },
  key: {
    usage: 'base.key("name")  ·  base.key("a.b")',
    what: 'A toml key, or a json key by dotted path — the same as `base.name`.',
    args: 'The key, or the dotted path in json.',
    examples: ['base.key("description").set(self.description)'],
  },
  frontmatter: {
    usage: 'base.frontmatter  ·  self.frontmatter',
    what: 'The `---` block at the top of a markdown file.',
    examples: ['base.frontmatter.set(self.frontmatter)', 'base.frontmatter.join("description")'],
  },
  body: {
    usage: 'self.body  ·  cmd.body',
    what: 'Everything after the frontmatter of one of our files — content to insert.',
    examples: ['base.prompt.as(markdown).append(cmd.body)'],
  },
  import: {
    usage: 'import [name] "path"',
    what: 'Another file of our layer, to take content from. `./` and `../` are relative to the product\'s directory, `/` starts at the layer root; the extension may be left out when one file matches. Without a name, the file name is the name. `import base "path"` points base at a renamed upstream file.',
    examples: ['import cmd "/.claude/commands/ship"\nbase.append(cmd.body)', 'import base "/.gemini/commands/planning"'],
  },
  reason: {
    usage: 'reason: "..."',
    what: 'Why upstream content is changed. Required by `replace` and `drop`, not allowed elsewhere; shown in the build report.',
    examples: ['base.Team.drop(reason: "credited in our README\'s License section")'],
  },
};

const OBJECTS = {
  base: 'The upstream file at this path (or where `import base` points) — the only object a template changes.',
  self: 'Our file at this path — a content source; methods are never called on it.',
};

// markdownFor renders a keyword's help as markdown; null for a word that is not a keyword.
function markdownFor(word) {
  const k = KEYWORDS[word];
  if (!k) return null;
  const parts = ['```loom', k.usage, '```', '', k.what];
  if (k.args) parts.push('', `**Arguments:** ${k.args}`);
  parts.push('', '**Examples**', '', '```loom', k.examples.join('\n\n'), '```');
  return parts.join('\n');
}

module.exports = { KEYWORDS, OBJECTS, markdownFor };
