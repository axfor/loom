# What Loom is for

A design note about the shape of the language, written while deciding what it should become. The
README says what it does today; this says what it is, which is larger.

## The thesis

> Take documents and data that are scattered across kinds and places, and organise them into one
> structured, **referenceable** fabric.

Weaving an upstream layer and your own into a product is one thing you can do once that fabric
exists. It is not the point. It is the first use.

Three words carry the weight.

**Scattered.** Markdown, shell, toml, json, yaml, csv, source — a skill set, a config tree, a
documentation set is all of them at once, and every tool that touches them treats each kind as a
separate world with its own editor, its own patcher, its own way of going wrong.

**Structured.** A document is not a string. It is a set of parts with names: a heading and what
follows it, a function, a key, a frontmatter field, a row. Every one of those already has a name its
author gave it. Nothing needs inventing; the names are there to be picked up.

**Referenceable.** This is the part that makes it a fabric rather than a pile. If every part of
every document can be named, then one document can point at part of another instead of copying it,
a product can be assembled from many sources rather than two layers, and the question "what depends
on this section" becomes answerable.

## Why the guarantee comes free

Every operation in the language resolves a name to a range of a document and splices around it.
Nothing else. Because no operation can touch a range that was not named, "did anything unnamed
change" is decidable — not by running the build and looking, but by construction.

That is worth stating plainly because it is the whole advantage, and it generalises past weaving:

> A view over the fabric cannot silently lose what it did not name.

`lm build` asserts it today by comparing bytes. Whatever the fabric grows into — more sources than
two, products assembled rather than woven, documents that reference instead of copy — that sentence
is the thing to keep true. Anything that cannot keep it true belongs behind a written reason and a
line in the report saying nobody verified it.

## What the fabric still lacks

**Kinds.** Five so far. A kind costs roughly a hundred lines — text 27, shell 93, toml 99, markdown
111; json is 207 plus a 220-line parser only because merging a registry needs real parsing. What
limits "all documents" is a switch statement, not the design.

**Identity.** A thread is tied to the words its author wrote: a section is addressed by its heading
text, a function by its name. Rename the heading upstream and every reference to it breaks. Today
that fails loudly, which is the right answer for a build and the wrong one for a fabric meant to be
referenced over years. A referenceable corpus eventually needs names that survive an edit — and
that is a real design problem, not a feature request.

**Saying something once.** Across the 81 templates of the one project using Loom, 291 statements
carry 68 occurrences of four identical lines, and 24 of the templates contain nothing that is not
boilerplate. There is no way to state a rule once and have it apply. Abstraction that expands into
the same closed set of operations keeps the guarantee; abstraction that returns computed text does
not, and would put the answer to "did anything unnamed change" back to "run it and look".

**Direction.** References point one way, at build time, and vanish. The fabric knows that a template
names a heading; it cannot answer who names this heading, what would break if it moved, or what a
product is made of, without being asked to build.

## The question that decides the next step

The argument rests on one claim: that what people want to do to documents can be said as *name a
part, and place something in relation to it*. It is falsifiable, and worth testing before any syntax
is drawn.

> Is there a real change you wanted to make, and could not express as naming a part of a document?

If there is, that is where the design starts, and it is worth more than any proposed syntax. If
there is not, then what is missing is narrower than it looks — more kinds, stable identity, the
ability to say something once — and the order to build them in is the order they are listed above.
