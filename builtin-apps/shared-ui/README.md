# Shared screen modules

Modules more than one built-in app's screen uses, kept here once. The build
(`node scripts/build-builtin-apps.mjs`) copies each into the `ui/` of the
apps whose `builtin-apps/<app>/shared-ui.json` names it, and `--check` fails
when a copy differs.

| Module | What it does | Used by |
| --- | --- | --- |
| `zip.js` | Reads a ZIP someone chose (stored or deflated, no ZIP64); writes one of stored entries | import-toolbox, webdav |
| `bundle.js` | Writes a Leafen workspace bundle (docs/bundle.md) — pages, blocks, databases, files — with its SHA-256s and losses | import-toolbox, webdav |
| `markdown.js` | A Markdown line as Leafen's text runs, with each tool's own syntax added | import-toolbox, webdav |
| `notes-convert.js` | A folder of Markdown notes as a bundle: folders and notes as pages, [[links]], embeds, callouts, front matter | import-toolbox, webdav |
| `outside-address.js` | Whether an address an outside program takes is https (or public https), and the note under one that isn't: what the program takes and the next step — the way to Access from outside for the server's administrator, who to ask for anyone else | vault, webdav, web-clip |
