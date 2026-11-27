# Leafen workspace bundles

A bundle is a whole workspace, or part of one, in one file: what Leafen's
workspace export writes ("Leafen archive") and its workspace import reads. Other
tools' apps, scripts and agents write bundles to bring their content into
Leafen; this page is the format they write.

A bundle is imported into a workspace that is still empty (Settings ›
Workspace › Import › Leafen archive, or `POST
/api/leafen/v1/workspaces/{id}/imports/archive` with the bytes and
`Content-Type: application/vnd.leafen.archive+zip`; past 8 MB, the resumable
upload under `…/imports/archive/uploads`). Leafen checks the whole bundle first
and refuses it, with nothing written, when anything is out of place.
The zip holds its entries uncompressed, in the order below: `zip -X -0 -D` makes
one.

## The file

A ZIP file whose entries are **stored** (not compressed), in this order:

| Entry | What it holds |
| --- | --- |
| `leafen/entities.ndjson` | One JSON object a line: every page, block, database part and comment |
| `leafen/files.ndjson` | One JSON object a line: every file the bundle carries |
| `leafen/files.bin` | The files' bytes, one after another, in `files.ndjson`'s order |
| `leafen/archive.json` | The descriptor, below |

### `leafen/archive.json`

```json
{
  "format": "leafen.archive",
  "formatVersion": 13,
  "generatedAt": "2026-10-13T00:00:00Z",
  "scope": { "kind": "workspace", "rootIds": ["<top page id>", "…"] },
  "source": { "workspaceId": "<any id of the source>", "workspaceName": "<shown in the import's report>" },
  "counts": {
    "pages": 2, "blocks": 4, "databases": 0, "dataSources": 0, "dbProperties": 0, "dbViews": 0,
    "databaseRows": 0, "ygoDocuments": 0, "dbTemplates": 0, "comments": 0, "databaseContainers": 0,
    "blockTrashTombstones": 0, "customEmojis": 0, "rowPageBlocks": 0, "rowBlockTrashTombstones": 0, "teamspaces": 0,
    "dbAutomations": 0
  },
  "files": { "included": true, "count": 0, "bytes": 0, "strippedReferences": 0 },
  "streams": {
    "entities": { "path": "leafen/entities.ndjson", "records": 6, "bytes": 2325, "sha256": "<hex>" },
    "fileIndex": { "path": "leafen/files.ndjson", "records": 0, "bytes": 0, "sha256": "<hex of nothing>" },
    "fileData": { "path": "leafen/files.bin", "records": 0, "bytes": 0, "sha256": "<hex of nothing>" }
  }
}
```

Each stream's `records`, `bytes` and SHA-256 are of the entry exactly as
stored, and `counts` are of the entity lines by kind (`databases` and
`dataSources` both count `dataSource` lines). `formatVersion` 13 is today's;
10, 11 and 12 still import (10 has no `dbAutomations` count). From 13 a
`rowBlockTrash` line holds a database page's trashed blocks as a
`blockTrash` line holds a page's, with the database page's `rowId`; before,
it held them without the views their databases own, and a linked database
among them comes back with one table view of its source.

A tool's bundle may add `losses`: what the tool couldn't carry, at most 200
lines of `{"what": "Dataview queries", "why": "kept as code blocks", "count": 3}`
(`what` up to 200 characters on one line, `why` up to 500, `count` at least 1).
A migration's report lists them.

## Entity lines

Each line of `entities.ndjson` is `{"kind": "<kind>", "value": {…}}`, at most
4 MB. IDs are the source's own — any string up to 255 characters, unique
within its kind; Leafen gives everything new IDs and rewrites the references
between them. A reference to something the bundle doesn't carry stays as it
was (a mention of it shows as a page the reader can't open).

| Kind | One of | Identity and links |
| --- | --- | --- |
| `page` | a page, a database's page | `id`, `parentId` (`""` at the top), `title`, `icon` |
| `block` | a block of a page's body | `id`, `pageId`, `parentId` (`null` at the top of the page), `position`, `type`, `content`, `text` |
| `dataSource` | a database's table | `id`, `pageId`, `title` |
| `property` | a database column | `id`, `dataSourceId`, `type`, `name`, `position`, `config` |
| `view` | a database view | `id`, `dataSourceId`, `type`, `name`, `position`, `config` |
| `row` | a database row (its values by property `id`) | `id`, `dataSourceId`, `position`, `values` |
| `rowPageBlock` | a block of a row's page | `id`, `rowId`, `pageId` (the database's page), `parentId` (`""` at the top), `position`, `type`, `content`, `text`, `createdAt`, `updatedAt` |
| `databaseContainer` | where a database sits on a page | `id`, `pageId`, `blockId`, `dataSourceIds`, `viewIds` |
| `comment` | a comment or discussion reply | `id`, `pageId`, and the block or row it is on |
| `teamspace`, `customEmoji`, `template` | as their names say | `id` |
| `ygoDocument`, `blockTrash`, `rowBlockTrash` | Leafen's own page documents and undo history | written by Leafen's export only |

A page's line, as an export writes it (fields left out take these defaults
when another tool writes the bundle):

```json
{"kind":"page","value":{"id":"src-1","parentId":"","title":"Meeting notes","icon":"","iconType":"","cover":"",
 "coverPosition":0,"kind":"page","position":1,"inTrash":false,"trashedAt":null,"archivedAt":null,"isLocked":false,
 "content":"","cursor":0,"revision":1,"font":"default","fullWidth":false,"smallText":false,"teamspaceId":"",
 "backlinksDisplay":"default","pageCommentsDisplay":"default","inlineCommentsDisplay":"default",
 "tableOfContentsDisplay":"default","movedDatabase":null,"updatedAt":"2026-10-13 00:00:00.000Z","workspaceId":"source"}}
```

Leafen's own export adds `createdBy` and `lastEditedBy`, the IDs of who made
and last changed the page on the server it came from. They matter only when a
workspace moves between Leafen servers, where the people are paired with
accounts on the new one; another tool leaves them out.

`position` orders a page among its siblings. A block's `position` is a string
that sorts its blocks among their siblings (`p0000000000`, `p0000000001`, …);
a block under another block (a list item's children, a toggle's content)
names it as `parentId`.

### Text

Leafen keeps a block's text in a page document an export writes as
`ygoDocument` lines. A bundle another tool writes gives each block its text
instead, in `text`: runs of text, each with its formatting.

```json
{"kind":"block","value":{"id":"b-2","pageId":"src-1","parentId":null,"position":"p0000000001","type":"paragraph","content":{},
 "text":[
  {"insert":"The budget is "},
  {"insert":"30 million","attributes":{"bold":true}},
  {"insert":"; see "},
  {"insert":"the plan","attributes":{"link":"https://example.com/plan"}},
  {"insert":" and "},
  {"insert":"Project plan","attributes":{"mention":"page","pageId":"src-2"}},
  {"insert":"."}]}}
```

A run's `attributes` are any of: `bold`, `italic`, `underline`,
`strikethrough`, `code` (true), `color` (a Notion color name such as `red` or
`blue_background`), `link` (an address), `equation` (TeX), and a mention —
`mention` with `pageId` (`"page"`, whose text is the page's title),
`databaseId` (`"database"`), `userId` (`"person"`), `date` (`"date"`, as
Leafen's editor writes it), `customEmojiId` or `commentId`. An unknown
attribute refuses the bundle. `legacyText` (plain text, no formatting) is the
older way to the same place; a block takes one or the other.

### Block types and content

`type` is one of Leafen's block types, as Notion names them: `paragraph`,
`heading_1` … `heading_4`, `toggle_heading_1` … `toggle_heading_4`,
`bulleted_list_item`, `numbered_list_item`, `to_do`, `toggle`, `quote`,
`callout`, `code`, `equation`, `divider`, `table_of_contents`, `breadcrumb`,
`image`, `file`, `pdf`, `video`, `audio`, `bookmark`, `embed`, `link_preview`,
`table` and `table_row`, `column_list` and `column`, `tab`, `button`,
`synced_block`, `template`, `child_page`, `link_to_page`, `child_database` and
`inline_database`. Text blocks keep `content` empty (`{}`): their text is
`text`. What other types keep in `content` (a to-do's check, a callout's icon,
a code block's language) is what Leafen's export writes: export a page with
such blocks to see it.

### Files

A file block's `content` names its file — all five fields, the SHA-256 in
lowercase hex: `{"fileId": "leafen-archive://files/<id>", "name": "plan.pdf",
"size": 1234, "contentType": "application/pdf", "sha256": "<hex>"}` — and a
page's `icon` and `cover` name theirs the same way when they are uploaded
images. Each file is a line of `files.ndjson`:

```json
{"id":"<id>","offset":0,"name":"plan.pdf","contentType":"application/pdf","scope":"uploads","bytes":1234,"sha256":"<hex>"}
```

A file's `id` is 15 letters, digits, `_` or `-` (unlike the entities' ids),
`name` its name with no space at either end, `bytes` at least 1, and
`contentType` its type in lowercase without parameters — `image/png` for
`.png`, `application/pdf` for `.pdf`. Leafen doesn't keep a file a browser
could run: a type that is XML (`image/svg+xml`, anything `+xml`), CSS or a
script, or a name ending so (`.svg`, `.css`, `.js`, `.xml`…), refuses the
bundle, so leave such files out and count them among the losses.
`scope` is `uploads` for a block's file, `icons` or `covers` for a page's.
`offset` is where its bytes start in `files.bin` (the files follow one another
with nothing between), and `bytes` and `sha256` are of them.

### Comments

A comment is a `comment` line on its page — a discussion's first comment, or a
reply to one:

```json
{"kind":"comment","value":{"id":"c-1","pageId":"src-1","parentId":"","body":{"rich":[{"text":"Shall we move the launch?"}]},
 "resolved":false,"createdAt":"2026-09-30T14:20:00Z","author":"Kim Minsu"}}
{"kind":"comment","value":{"id":"c-2","pageId":"src-1","parentId":"c-1","body":{"rich":[{"text":"Yes, by a week."}]},
 "resolved":false,"createdAt":"2026-09-30T15:02:00Z","author":"Lee Jiwoo"}}
```

`parentId` is the comment a reply answers (`""` opens a discussion). `blockId`
puts a comment on one of the page's blocks; `targetKind` `"database_row"` with
`targetId` puts it on a row. `body.rich` is runs of text, each `{"text"}` with
`bold`, `italic`, `code`, `link` and the like, as Leafen's comment API takes
them. `createdAt` is when it was written (an ISO time). The person importing
the bundle becomes each comment's author in Leafen, so `author` names who wrote
it in the tool: the comment opens with that name and its time (`Kim Minsu ·
2026-09-30 14:20 UTC`), as a comment moved from Notion does. `resolved` on a
discussion's first comment closes the discussion. Leafen's own export adds
`authorId`, who wrote it on the server it came from, which a move between
Leafen servers keeps when that person is paired with an account on the new
one. A migration from a bundle
moves a page's and a row's open discussions as they are; a resolved discussion
and the comments on a block stay behind, as in a migration from Notion. The
descriptor counts the lines in `comments`.

## Databases

A database is a `page` (its page) with a `databaseContainer` that places it, a
`dataSource`, its `property` lines (one with type `title`), its `view` lines
and its `row` lines. A row's `values` are by property `id`, each
`{"type": "<the property's type>", "value": …}` as Leafen's database API takes
them; a row's page body is `rowPageBlock` lines. The exact `config` of each
property and view type is what Leafen's export writes: export a workspace with
the database you mean to write, and write the same.

A view's `name` is the name it was given; `""` is a view never named, which
each reader sees as its layout's name in their own language. In a bundle of
version 11 or before, a view still called its layout's name (`"Table"`,
`"표"` …) without `"namedAsLayout": true` in its `config` comes in never
named, as Leafen read such a view then.

A full-page database another tool writes is these lines (ids are the tool's):

```json
{"kind":"page","value":{"id":"db-1","parentId":"top","title":"Notes","kind":"database","position":1}}
{"kind":"block","value":{"id":"db-1-table","pageId":"db-1","parentId":null,"position":"p0000000000","type":"inline_database","content":{"dataSourceId":"ds-1","viewId":"view-1"}}}
{"kind":"dataSource","value":{"id":"ds-1","pageId":"db-1","title":"Notes","icon":"","iconType":"none"}}
{"kind":"property","value":{"id":"name","dataSourceId":"ds-1","pageId":"db-1","name":"Name","type":"title","config":{},"position":"p0000000000","deletedAt":null}}
{"kind":"property","value":{"id":"tags","dataSourceId":"ds-1","pageId":"db-1","name":"Tags","type":"multi_select","config":{"options":[{"id":"t1","name":"idea","color":"blue"}]},"position":"p0000000001","deletedAt":null}}
{"kind":"view","value":{"id":"view-1","dataSourceId":"ds-1","pageId":"db-1","name":"All","type":"table","config":{},"containerId":null,"position":"a00000000"}}
{"kind":"databaseContainer","value":{"id":"db-1-table","blockId":"db-1-table","kind":"source","pageId":"db-1","parentBlockId":null,"position":"p0000000000","sourceContainerId":null,"dataSourceIds":["ds-1"],"viewIds":["view-1"]}}
{"kind":"row","value":{"id":"row-1","dataSourceId":"ds-1","pageId":"db-1","position":"p0000000000","values":{"name":{"type":"title","value":"First note"},"tags":{"type":"multi_select","value":["t1"]},"made":{"type":"date","value":{"start":"2024-01-01T09:00:00.000Z"}}}}}
{"kind":"rowPageBlock","value":{"id":"row-1-b1","rowId":"row-1","pageId":"db-1","parentId":"","position":"p0000000000","type":"paragraph","content":{},"text":[{"insert":"The note's first line."}],"createdAt":1704099600000,"updatedAt":1704099600000}}
```

A title or text value is a string, a select's an option `id`, a multi-select's a
list of them, a date `{"start", "end"}` (ISO dates or times), a URL a string.
The descriptor counts the database's page among `pages`, its block among
`blocks`, and each kind in its own count (`databases`, `dataSources`,
`dbProperties`, `dbViews`, `databaseContainers`, `databaseRows`,
`rowPageBlocks`). A row page block's `createdAt` and `updatedAt` (milliseconds)
come from the tool, the same in each bundle, so a later bundle changes only
what the tool changed.

## Checking a bundle

The import's first phase checks everything before writing: the descriptor
against the entries, every line's shape and size, IDs, positions, references,
text runs, files' sizes and SHA-256. A refused bundle leaves the workspace as
it was. The import's `errorCode` says what kind of refusal it is
(`LEAFEN_WORKSPACE_IMPORT_INVALID` for a bundle that doesn't fit this format),
and its `errorDetail` says where and what, never the value it found — the
entry and its line, then the field:

- `the bundle holds exactly leafen/entities.ndjson, …, in that order, each
  stored (not compressed) and not encrypted`
- `leafen/archive.json isn't a descriptor this Leafen reads: …`
- `leafen/files.ndjson line 3: id isn't a file ID of 15 letters, digits, '-' or '_'`
  (also `offset`, `name`, `bytes`, `sha256`, `scope` and `contentType`)
- `leafen/files.ndjson line 3: the file's bytes in leafen/files.bin aren't its
  bytes count long with its sha256`
- `leafen/entities.ndjson line 12: not an object with kind and value`, `… its
  kind isn't one a bundle holds`, or `leafen/entities.ndjson line 12 (block):
  doesn't fit the bundle's format` for a value whose fields this format
  doesn't take
- `leafen/entities.ndjson has 40 lines; leafen/archive.json's entities stream
  says 41 records`, and the same for each entry's SHA-256 and the counts by
  kind

Content that fits the bundle but not Leafen — an unknown block type, a
`content` field a block doesn't take — is refused as it is written, with the
content graph's code (`LEAFEN_GRAPH_INVALID`) and its words as the
`errorDetail` (`Unsupported block type.`), without a line. An import refused
while it is being started answers the same words in its `message`.

## Moving from another tool

A bundle can go into an existing workspace, under a page of your choosing and
with its databases mapped onto yours, through the Notion migration: the same
scan, plan, check, run, report and run again. The bundle is its source.

An app does all of this with one call: its import screen hands Leafen the
bundle with `importBundle`, and Leafen's window takes the person through the
rest ([apps.md](apps.md#moving-from-another-tool)). Give the bundle one top
page — the vault, the space — and it moves without a choice. The steps below are
the same for an agent or a script.

1. Make a new, empty workspace for the bundle.
2. `POST /api/leafen/v1/workspaces/{target}/imports/bundle-sources` with
   `{"requestId": "…", "stagingWorkspaceId": "<the empty workspace>", "label":
   "Obsidian"}`: a migration connection for the workspace you move into, which
   reads the empty one with a token Leafen makes for it, for a week. The
   answer's `connectionId` is the connection; its `status` is `importing`.
   `tool` and `origin` (optional, one line each) say which tool made the
   bundle and the source in it — the import screen gives `"<app id>:<key>"`
   and the descriptor's `source.workspaceId` — and `GET …/bundle-sources?tool=`
   lists that tool's sources, to find the one a later bundle of the same
   source goes to, 50 at a time: a `nextCursor` goes on with `&cursor=`,
   until it is empty.
3. Import the bundle into the empty workspace (the archive import, above).
   `GET …/bundle-sources/{connectionId}` then says `ready`, with the bundle's
   `losses`.
4. Migrate from the connection as from Notion's: `GET
   …/workspaces/{target}/notion-import/connections/{connectionId}/scope` gives
   the roots (the bundle's top pages); `POST …/workspaces/{target}/notion-migrations`
   with `{"connectionId", "roots"}` starts it; `POST …/notion-migrations/{id}/scan`
   until it is no longer `scanning`; `GET` and `PUT …/notion-migrations/{id}/plan`
   (`{"baseRevision", "plan"}`) read and save the plan; `POST
   …/notion-migrations/{id}/run` with `{"requestId", "planRevision"}` runs it.
   The report page names the tool and lists its losses.

To bring the tool's next bundle with the same plan: delete the workspace the
last bundle went into, to its end (its IDs are the next bundle's; until the
deletion is done, `PUT` answers 409 "still being deleted"), make a new empty one,
`PUT …/bundle-sources/{connectionId}` with `{"stagingWorkspaceId": "<the new
one>"}`, import the next bundle there, and run the migration again: what is
new comes, what the tool changed is updated in place, and what you changed in
Leafen on what the tool didn't change stays. A block the next bundle no longer
has, on a page it still has, goes to the page's version history unless you
changed it in Leafen; a page it no longer has stays, named in the migration's
report. `PUT` with the same
workspace gives the source a new week's token. `DELETE
…/bundle-sources/{connectionId}` ends the source, its token and its
connection's migrations; the workspace the bundle went into stays until you
delete it.

A key with the structure scope does the same for an agent, naming only
workspaces the key covers, and the MCP tools `leafen_notion_migration_*` list
the connection among the sources.

## License

This format description is licensed under the Apache License 2.0
([its text](../sdk/app/LICENSE)), so a tool may follow it under any license;
Leafen itself is AGPL-3.0-only ([NOTICE](../NOTICE)).
