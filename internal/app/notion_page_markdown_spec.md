# Leafen page Markdown

A Leafen page's body reads and writes as Notion's page Markdown does:
CommonMark with GitHub's tables, task lists and strikethrough, and the tags
below for what Markdown has no form for. Fetch a page to see its Markdown;
write it back the same way: written back as fetch gave it, a page stays as
it was — the backslashes before `~`, `_` or `*` and tags such as `<b>` are
this Markdown's escapes and marks, not changes (only a web address bare in
text becomes a link, as in Notion). Don't guess other syntax: what isn't
below is saved as text.

Layout: blocks are separated by a blank line; the items of one list follow
each other without one. A list item's blocks stand under it, a marker's
width in (two spaces under `- `, three under `1. `).

Text blocks:
- Paragraph: its text. Lines of text one after another are one block, their
  line breaks kept, up to a blank line or a line that starts another block.
  An empty paragraph is `<p />`. A line break inside a block is `<br />`, or
  a backslash ending the line.
- Headings: `# `, `## `, `### `, `#### `.
- Lists: `- item`, `1. item`, to-dos `- [ ] item` and `- [x] item`.
- Quote: `> text`; lines of `>` one after another are one quote.
- Divider: `---`.
- Code: a fenced block, ```` ```language ````, never indented as code; a
  fence longer than any run of backticks in the code.

A block that has a color, or holds other blocks (save a list item, whose
blocks stand under it), is written as a tag around it: the opening tag on a
line of its own, the block's text on the next, then after a blank line its
blocks, not indented, and the closing tag. `<p />` stands for empty text.

```
<quote color="purple_bg">
Title text

- a block inside it
</quote>
```

The tags: `<p>`, `<h1>`…`<h4>`, `<quote>`, `<ul>`/`<ol>` around `<li>`,
`<todo checked="true|false">`, and:
- Toggle: `<toggle>`, and a toggle heading `<toggle heading="h2">`.
- Callout: `<callout icon="💡" color="yellow_bg">`. `icon` is an emoji, an
  image address, a custom emoji `:name:` or a picture uploaded to Leafen, by
  its `notion-file-block://…` address as fetch wrote it or `file-upload://ID`
  for a new upload; left out, the callout has none.
  Its lines are its blocks, its first too: a heading or a to-do first stands
  as that block, and `<p />` is an empty first line.
- Columns: `<columns>` around `<column>` (`<column ratio="30">`, its share
  of the row in percent), each holding its blocks.
- Tabs: `<tabs>` around `<tab>`, each holding its name and, after a blank
  line, its blocks.
- Equation: the expression between lines holding only `$$`; a colored one
  `<equation color="blue">` around it. `$$E = mc^2$$` on one line is text.
  Inline math in text is `$x^2$`.
- Table: a pipe table when it has a header row and nothing more; any other
  `<table header-row="true" header-column="true">` with a `<tr>` for each
  row and a `<td>` for each cell, the cell's text on its own line. Cells hold
  inline text only (`<br />` for a line break).
- Table of contents: `<table-of-contents />`.
- An uploaded HTML file: `<embed source="file-upload://ID" />`, the source
  `notion-create-attachment` or an upload gives; it is drawn in a sandbox.
- Web bookmark and embed: `<bookmark source="address">`, `<embed …>`, their
  caption as the tag's text; without an address, `<bookmark />` waits for one.
  A service's link becomes its frame by itself; `service` names the service
  (`google_drive`, `google_maps`, `figma`, `miro`, `excalidraw`, `loom`,
  `typeform`, `codepen`, `replit`, or `html` for an HTML file):
  `<embed source="address" service="figma">`, `<embed service="figma" />`.
- Media: an image is `![caption](address)`, and one that opens a link
  `<image source="address" link="address">caption</image>`; a file, PDF, video or audio file
  is `<file source="address">caption</file>`, `<pdf …>`, `<video …>`,
  `<audio …>`. A file uploaded to Leafen is named by its stable
  `notion-file-block://…` address: keep it as fetch wrote it, and use
  `file-upload://ID` for a new upload.
- Sub-pages and databases: `<page url="…">Title</page>`,
  `<database url="…" inline="true">Title</database>`. Without `url` a tag
  makes a new one where it stands. Leaving out an existing one's tag deletes
  it only when deleting content is allowed.
- `<page url="…">` naming a page elsewhere mentions it, as Notion's does.
- Synced block: `<synced-block>` around its blocks; a copy of one from
  elsewhere is `<synced-block-reference url="…" />` with the original's address.
- A block Markdown can't write reads as `<unknown url="…" alt="type" />`;
  keep it as it is. Fetch writes a breadcrumb, a button and a link to a
  page so too (`alt="alias"` for a link), as Notion's does; Markdown makes
  none of them, and `<breadcrumb>`, `<button>` or `<link-to-page>` written
  is saved as text.

Colors: gray, brown, orange, yellow, green, blue, purple, pink, red, and a
background named `…_bg` (`blue_bg`) — a block's in its tag's `color`.

Inline: `<b>bold</b>`, `<i>italic</i>`, `<s>strikethrough</s>`,
`<u>underline</u>`, `<code>code</code>`, `[link](address)`, `$math$`, and
`<span color="red">text</span>`; nest them to combine. `**`, `*`, `__`, `_`,
`~~` and backticks read too. Escape a literal `\`, `*`, `_`, `~`, `` ` ``,
`$`, `[`, `]`, `<`, `>`, `{`, `}`, `|` or `^` in text with a backslash.
HTML character references (`&amp;`, `&lt;`, `&#65;`, `&copy;`) read as
their characters outside code and math; escape the `&` of a literal one
(`\&amp;`).

Mentions: `<mention url="…" />` — a page by its address, a person by
`user://` and the ID `notion-get-users` gives, a database adding
`type="database"`; fetch writes a page's and a database's with its title,
`<mention url="…">Title</mention>`, and a person's with their email after
the ID and their name, `<mention url="user://ID/email">Name</mention>`. A date:
`<date start="2026-10-28" />`; a range adds `end="2026-10-30"`, and a time
`start-time="09:00"` (and `end-time`) with `time-zone="Asia/Seoul"`.

A page's title and a database page's title and text values are inline
Markdown of this kind: fetch and a query's rows give them so, and
update_properties and notion-create-pages read them so. SQL reads their
words alone. A title goes in properties, not in the content: in
notion-create-pages, content whose first line is a `# ` heading, its only
one, with more after it, has that line taken as the title line — left out
of the body, and the title of a page under a page given none. Writing into a
page that is there keeps such a heading.

Use the addresses fetch and search give. A line holding only a web address
is a paragraph, not a bookmark. A web or mail address written bare in text
is a link to it.

Content is read as Notion's MCP reads it, and the answer's `warnings` say
what the reading changed or kept as text: older forms read as the tags
above (a toggle from `<details>` with its
`<summary>`, mentions from `<mention-page>`, `<mention-user>`,
`<mention-database>` and `<mention-date>`, `<table_of_contents>` and
`<synced_block>`); other HTML is put right or taken out (`<mark>`, `<div>`,
`<script>`); any other tag, and a line ending in `{color="…"}`, is saved as
text.
