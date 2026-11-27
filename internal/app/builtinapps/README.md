# Built-in apps

The apps Leafen ships inside its executable (docs: app platform, section 16).
The executable vouches for their bytes, so they are trusted as "builtin". None
is installed at first: an administrator turns one on from "Browse apps", on a
closed network too. A build without the app platform (`leafen_noapps`) leaves
this folder out.

- Each folder here holds one app's package files exactly as a `.leafenapp`
  holds them: `leafen-app.json`, `ui/`, `workers/`, `data/`, `templates/`.
- `builtin.json` lists the folders in order (`{"folder": "…"}`) and marks the
  featured ones (`"featured": true`) and examples (`"example": true`).
- An app's worker source lives in `builtin-apps/<app>/worker/` (a Go module
  outside the executable). `node scripts/build-builtin-apps.mjs` builds it to
  `workers/main.wasm` here and copies the app SDK into `ui/`, with the shared
  screen modules (`builtin-apps/shared-ui`) its `shared-ui.json` names; `--check`
  fails when they are stale (wasm builds match only from the same Go release).
- Approvals (`approvals`): ask for approval, decide, and an overview; its rules
  are the worker's, and its requests database is locked.
- Mail (`mail`): the person's inbox on the organization's mail server (IMAP
  and SMTP at the addresses the administrator sets, with the mail account
  each person gives Leafen) or in Gmail (IMAP and SMTP with the address and
  an app password the person gives, or a Google sign-in where the
  administrator made a Google client), reading, sending after approval, and
  a notice for new mail every five minutes. Its inbox, read and send are also
  agents' tools; `leafen://guides/gmail` walks an agent through connecting
  Gmail.
- PDF Documents (`document-viewer`): opens PDF files from a file block's
  "Open with" in pdf.js's viewer, to read them and, for someone who may change
  the page, fill their forms, add annotations and save them back
  (`builtin-apps/document-viewer/README.md`). Word, Excel and PowerPoint files
  open in the Office Documents app (`catalog-apps/office`), which ships through
  an app catalog.
- Hangul Documents (`hwp-viewer`): opens HWP and HWPX files from a file
  block's "Open with" with rhwp — reading everywhere, editing in rhwp-studio
  (in the frame with no origin apps open in) and saving in the
  file's own format
  (`builtin-apps/hwp-viewer/README.md`).
- Vault (`vault`): a password vault Bitwarden's own apps use as their server,
  at its public endpoint `bw` with the organization after it
  (`/app-endpoints/vault/bw/<organization>`), once an administrator opens it.
  The apps encrypt everything; the worker logs them in with their master
  password's hash and keeps the encrypted items in databases only the app's
  account sees. A person prepares their vault in the app first, which lets one
  Bitwarden app make the account for that email within a day.
- Web Clips (`web-clip`): a clipper that posts to a webhook (MarkSnip, a
  phone's shortcut) sends each clip to the app's endpoint `clip` with the
  person's app password, and it becomes a page in their own "웹 클립"
  database — title, address and summary as values, the markdown as its text.
- Drive (`webdav`): Finder, Windows Explorer, Office and sync tools reach
  Leafen over WebDAV at the endpoint `dav`, logged in with the person's app
  password: "Pages" shows each workspace's page tree as folders and a page's
  file blocks as its files (saving replaces the block's file, the page's
  history keeps the old), "Files" the person's own and the shared folder in
  the app's workspace. An Obsidian vault synced into "My files" is what its
  tools list, search and read for an agent, and its Obsidian card (Settings ›
  Import) moves a synced vault, or a vault folder's ZIP, into pages: notes and
  folders as pages under the vault's, [[links]] and aliases as page mentions,
  and the next move of the same vault carrying the migration on.
- Team Quiz (`team-quiz`, an example): four tables whose room loops ask the
  questions people add, with guests by link; results are kept locked.
- Daily Briefing (`briefing`): a person's tasks by due date and unread
  mentions on its page, beside a page and in a block, and a morning notice at
  the hour they choose (kept in their own store).
