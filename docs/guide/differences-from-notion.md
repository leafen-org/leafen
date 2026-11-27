# Differences from Notion

<!-- guide-sources:
web/src/components/auth/SignInWays.tsx
web/src/components/settings/WorkspaceSettingsOverview.tsx
web/src/components/PageExportDialog.tsx
web/src/locales/en/workspaceSettingsDialog.json
web/src/locales/en/importDialog.json
-->

Leafen works like Notion wherever it can: the same pages, blocks, databases,
views and menus, in the same places. This chapter lists where it works
differently on purpose, grouped by area, so someone moving from Notion knows
what to expect. Each chapter of this guide repeats the differences that
belong to its own feature in its "Differences from Notion" section. Small
differences in wording or help links are left out here.

## Where Leafen runs

- **Your own server.** Leafen runs on your computer or your server, and the
  desktop app is itself the server. Nothing opens to the internet until an
  administrator chooses how
  ([Outside address](outside-address.md)). Its administrators can open what
  is on it.
- **The first run asks for an internet address for the server** and offers
  to connect your own AI agents ([Getting started](getting-started.md)).
- **No plans or billing.** Every feature, the enterprise ones included, is in
  every Leafen. There are no plan prompts, upgrade banners or billing records.
- **Backups are yours.** Leafen keeps no copy of your data. Backups go to a
  folder, WebDAV server or S3 storage you choose, and they are off until an
  administrator turns them on ([Backups](backups.md)).
- **The settings window also holds the server's settings**, under **Server**,
  for the server's administrators. Enterprise controls and the organization
  are among the **Admin** items.

## What Leafen leaves out

- **No built-in AI.** The editor and the app have no AI of their own. Connect
  an agent you already use, such as Claude Code or Codex, and it works in
  Leafen as you ([Agents](agents.md)).
- **No Notion Calendar, Notion Mail or meeting notes service.**
- **No skills, no template gallery and no marketplace.** The add window offers
  the database templates. Apps made for Leafen run inside it, with their own
  workspace data ([File apps](file-apps.md) covers the document apps).
- **Features that only Notion's own service can provide**: reporting a public
  page, Microsoft Teams and Discord notifications, connected properties and
  synced databases from other tools, the Notion CLI, the web clipper, phone
  widgets and the new-tab extension, choosing where data is stored, and a
  custom domain for each published site. Home leaves out what only Notion's
  service fills in.
- **The HTML block has no AI to write it.** An uploaded HTML file renders in a
  sealed frame with no access to Leafen, under a "User HTML" mark, and
  **User HTML** in Settings → **Security** turns it off. Other active file
  types, such as SVG and JavaScript, are refused at upload. The image window
  has no GIPHY tab.
- **The "/" menu offers only the service embeds a link can draw.**

## Accounts and logging in

- **Leafen asks for no email at the start**: a name is enough, and an email,
  password or passkey can be added later as a backup way in.
- **Devices join with a code.** A new device logs in with a one-time code or
  QR code from a device you are logged in on; an email and a password are
  optional backup ways in ([Accounts and devices](accounts-and-devices.md)).
- **An invitation needs only a name.** Opening it asks only for a name and
  the language, and that device is in. A workspace owner can also invite
  someone by name, with no email, through a one-time link, and every
  invitation ends after 7 days.
- **Accounts are per server.** The account switcher shows each account as
  the server and the person's name, with the email when you point at it.
- **Your name and photo are private by email.** Nobody outside your
  organization finds them by your email address.
- **A device's location stops at its country** in the devices list.

## Sharing and permissions

- **Someone invited to a page starts with read access**; the inviter picks a
  wider role. A page shared with the whole workspace keeps full access.
- **The share menu's top row is the person who holds full access to the
  page**, and it keeps full access.
- **Inviting yourself to a page adds nothing.**
- **The web link has a secret token and can be made anew**; a visitor's open
  tab closes within 30 seconds when the link or site goes.
- **A site has no custom domain of its own**; the whole server can be served
  on your own domain instead.
- **Deleting a page forever stays with the workspace's owner.** Others can
  restore it from the trash. Any workspace owner can bring back a page deleted
  forever within its 30 days, from Settings → **Security** → **Data
  retention**.
- **A member who leaves keeps their private pages on the server.** They can't
  open them any more; a server administrator can.
- **A restricted member can leave the workspace** from their own row.
- **A removed member's open tab closes within seconds**, not on their next
  move.
- **New workspace asks for a name and how to start, in one window**; there
  are no plans to choose.
- **By default only people with full access to a page let agents into it.**
- Guests and restricted members below full access see only their own line in
  a page's share window.

## Agents and apps

- **Agents can do everything a person saves on screen**, through the API and
  MCP, as the person who connected them.
- **An organization decides which MCP clients may connect**, instead of
  Notion AI's settings.
- **A custom MCP connection serves every agent connected to Leafen.** Leafen
  holds its login, and the agents never see it.
- **Leafen runs third-party apps inside it**, each with its own place for its
  data.
- **A webhook to a private network address is flagged**, and the button that
  calls it stops.

## Pages, blocks and databases

- **The library has a Documents tab** listing the Word, Excel, PowerPoint, PDF
  and Hangul files on pages you can see, and opens them for editing
  ([File apps](file-apps.md)).
- **Changing a property's type says so, with Undo.**
- **Deleting a synced block's original keeps its copies' content.**
- **A block moved to another page keeps its ID**, so links to it still work.
- **A place property is picked on a map**, and nothing typed leaves the
  server. An administrator may add a place search provider.
- **Maps draw the tiles of the server's tile server**, OpenStreetMap's unless
  an administrator changes it.
- **The sidebar is one list** without Notion's chat and meetings tabs, which
  hold Notion AI and its meeting notes; the four squares at its foot (tip
  **Library, Trash, more…**) hold Leafen's own **Apps** beside the Library,
  My tasks and the Trash.
- **A page's undo history lasts while the app is open**, even after you visit
  another page and come back.
- **An edit made just before the page closes or reloads is kept.**
- **A title stops at 200,000 characters**; a longer paste goes to the body.
- **A copied link has no page title in it.**
- **Archive stays in the page menu.**
- **Math does not apply to words in a code block.**
- **A database made with "/" always opens its start window.**
- **Option+Delete at the end of a line, and Control+Option+Backspace at its
  start, join the two lines into one**, as Delete does; Notion copies the
  other line's words and leaves that line too
  ([Keyboard shortcuts](keyboard-shortcuts.md)).
- **The time zone list names each zone once**; a date imported from Notion
  with an old zone name keeps it.
- **A template's tour names the buttons as they are on the screen.**

## Comments, history and trash

- **A database mention in a comment stays a database mention.**
- **A comment written where the last reaction was taken back is kept.**
- **A mention picked with @ while suggesting stays a suggestion**, as does a
  multi-line paste.
- **Restoring a version keeps the comments added after it**
  ([Page history](page-history.md)).
- **A version's preview shows an inline database's rows as they were then.**
- **The trash notice's Restore brings the page back** ([Trash](trash.md)).
- **A full-page database is one row in the trash.**

## Notifications and phones

- **The notification settings have no line for Notion's announcements and
  update emails.**
- **Leafen runs no push service.** Browsers get Web Push; on an iPhone, add
  Leafen to the Home Screen first. The phone apps check for new notices in the
  background instead of receiving push
  ([Desktop and phone apps](desktop-and-phone-apps.md)).

## Moving in and out

- **A Notion import reads the whole workspace through Notion's API** and
  imports it again as Notion changes. Notion's API gives neither automations
  nor sharing, so those don't come, and the report says so
  ([Moving from Notion](moving-from-notion.md)).
- **People from outside services are not imported.** Notion's people are
  matched to members you invite here.
- **Several CSV or PDF files can be imported at once.**
- **PDF export opens the browser's print window**, where you save the PDF.
- **A Leafen archive is a lossless copy** that goes back into any Leafen, and
  exported HTML makes no requests outside its files
  ([Exporting](exporting.md)).
- **Settings → People keeps a download button** that saves the members and
  guests as a CSV file.

## Related chapters

- [Moving from Notion](moving-from-notion.md)
- [Accounts and devices](accounts-and-devices.md)
- [Exporting](exporting.md)
