# Import Toolbox

A built-in app that moves content from other tools into Leafen through the
migration's plan, run and report (docs/bundle.md, "Moving from another tool").
Each tool is a card under Settings › Import › "Import from other tools"
(`surfaces.importSources`); its screen reads what the person chooses, makes a
workspace bundle and hands it to Leafen with `importBundle`.

| Tool | Reads | Screen |
| --- | --- | --- |
| Confluence | A space from the person's Confluence Cloud account (REST API v2, email and API token), or its HTML export (Space settings › Export space › HTML) | `ui/confluence.html` |
| Evernote | Notebooks' ENEX exports (a notebook's Export notebook… › ENEX), several at once or in a ZIP | `ui/evernote.html` |
| Logseq | A graph folder (pages, journals, assets), as a ZIP | `ui/logseq.html` |
| SiYuan | The workspace's data folder, or a notebook's .sy.zip export, as a ZIP | `ui/siyuan.html` |
| Markdown tools | Bear (Markdown or TextBundle), Joplin (MD), Craft, Roam and other tools' Markdown exports, as a ZIP | `ui/markdown.html` |

An app shows at most eight cards (docs/apps.md).

It has no worker and reaches none of the person's data: everything runs in its
screen. The Confluence account is a credential Leafen seals (`confluence`:
email and API token) and fills into relayed requests as HTTP Basic; the relay
reaches the person's site (`*.atlassian.net`), Atlassian's gateway for scoped
tokens and its file store for attachments, and the administrator lets people
give the credential in the app's settings.
Its package lives in `internal/app/builtinapps/import-toolbox`:

- `ui/zip.js` and `ui/bundle.js` are the shared screen modules
  (`builtin-apps/shared-ui`, which the Drive app's Obsidian card uses too): ZIP
  reading and writing, and the bundle — pages, blocks with their text as runs,
  full-page databases with their rows and row pages, files (a file Leafen
  doesn't keep, such as an SVG, is left out for the tool to count), the
  descriptor with its SHA-256s and what the tool couldn't carry, file types by
  name and code languages as Leafen names them.
- `ui/confluence-convert.js` turns a Confluence export into one: the space as
  one top page with its page tree (`index.html`), each page's `#main-content` as
  blocks — paragraphs, headings, lists, task lists, code, quotes, rules, info
  panels as callouts, expands as toggles, tables, pictures and attachments as
  files, links to other exported pages as page mentions — and what has no place
  (other macros by name, layout columns, user mentions and status lozenges kept
  as text, links to pages or files the export lacks) counted as losses. Page ids
  come from Confluence's page ids, files' from their paths, the source from the
  space's key (so a renamed space carries on and another space doesn't), and blocks' from
  what they are — kind, text and content, and how many alike came before on the
  page — not from where they stand, so the space's next export carries the same
  migration on: an unchanged block keeps its id however much is added around
  it, and an edited one comes as a new block. `spaceBundle` makes the bundle
  from a space's pages however they were read.
- `ui/confluence-api.js` reads a space through the API: the site's cloud id
  (`/_edge/tenant_info`), then the gateway or the site itself, whichever takes
  the token; the space's pages in their storage format, each page's
  attachments (up to 5 MB, the relay's limit — larger ones are counted), the
  names of people it mentions and how many comments it has (counted, not yet
  moved). Pages and attachments are named as the export names them, so a space
  moved one way carries on the other.
- `ui/evernote-convert.js` turns ENEX files into a bundle as Notion's Evernote
  import makes one: an "Evernote" page with each notebook a database — its
  notes rows with their tags (multi-select), created and updated dates and a
  clipped note's source URL — and each note's ENML its row's page: formatting,
  highlights, headings, lists, the new (`--en-todo`) and old (`<en-todo>`)
  checklists, code blocks, tables, quotes, rules, pictures and files by the MD5
  `<en-media>` names them with (`ui/md5.js`), files a note keeps without showing
  at its end, and Evernote 10's tasks as to-dos. Encrypted text, links to other
  notes, reminders, locations and text colors are counted. Every Evernote move
  into a workspace is one source; notes are named by their notebook, title and
  when they were made, so a notebook's next export carries the migration on.
- `ui/logseq-convert.js` turns a graph into one: the graph as a page, its
  pages under it (a namespace's under its parent, which is made when the graph
  has none) and journals under one page, newest first, titled in the graph's
  `:journal/page-title-format`; each outline as nested bullets, with headings,
  task markers as to-dos, code, math, quotes, tables, pictures and files from
  `assets/`; `[[links]]`, `#tags` and aliases as page mentions, `((references))`
  and block embeds as the text they point at, page properties as a table, and
  queries, macros, priorities, dates, block properties, Org pages, whiteboards
  and unused assets counted. Its inline syntax goes through the shared
  `markdown.js`.
- `ui/siyuan-convert.js` reads the .sy block trees SiYuan's Lute engine
  writes: notebooks by their `.siyuan/conf.json`, documents and their
  sub-documents (in a folder named by the parent's id), headings, paragraphs
  and their marks, lists and task lists, quotes, code, math, tables, rules,
  super blocks one under another, pictures and files from `assets/`;
  references to documents as page mentions and to blocks as their words.
  Databases (attribute views), embedded queries and HTML, widget, video and
  audio blocks are counted. Block ids are SiYuan's own.
- `ui/markdown-tools.js` tells which tool made a Markdown export (Bear's
  TextBundles, Joplin's `_resources/`, Roam's to-dos and block references),
  makes its syntax Markdown — Bear's `::highlights::` and TextBundle folders,
  Roam's `{{[[TODO]]}}`, `^^highlights^^`, `__italics__` and `((references))`,
  counted — and moves the folder through the shared notes converter
  (`notes-convert.js`, the Obsidian card's).
- `ui/confluence-storage.js` makes the storage format (`ac:` macros, `ri:`
  resources) into the export's HTML: panels, code, expands, status, task
  lists, pictures, page and user links, emoticons, layouts, the new editor's
  panels and dates; other macros keep their name to be counted.
