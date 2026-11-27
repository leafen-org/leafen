# Pages

<!-- guide-sources:
web/src/components/page/PageHeader.tsx
web/src/components/PageMenu.tsx
web/src/components/MoveToDialog.tsx
web/src/locales/en/pageHeader.json
web/src/locales/en/pageCover.json
web/src/locales/en/emojiPicker.json
web/src/locales/en/topBar.json
web/src/locales/en/rowMenu.json
internal/app/notification_preferences.go
contracts/content-graph-wire.json
-->

This chapter covers a page itself: making one, its title, icon and cover,
sub-pages, moving and copying it, and every row of its menu. What goes inside
a page is in [Editor blocks](editor-blocks.md); who can open it is in
[Sharing and permissions](sharing-and-permissions.md); its earlier versions
are in [Page history](page-history.md).

## Make a page

- **New** at the top of the sidebar › **Page** makes a private page and opens
  it.
- The **+** beside a sidebar section (**Add a page**, or **Add to** a
  teamspace) makes a page there; **Add new** at the end of a short section
  opens the add window.
- **+** on a page's row in the sidebar (**Add a page inside**) makes a
  sub-page of it.
- In a page, **/page** (the **Page** block) makes a sub-page where the caret
  is.

A new page opens with its title field ready and reads **New page** until it
has a title.

## Title, icon and cover

Hover over the space above the title to show **Add icon**, **Add cover**,
**Add verification** and **Add comment**.

- **Title** — type it in the large field. Enter moves into the page's body. A
  title holds up to 200,000 characters; a longer paste goes to the start of
  the body instead, so nothing is lost.
- **Add icon** gives the page a random emoji and opens the icon picker:
  **Emoji** (with **Filter**, **Random** and a skin tone), **Icons** (with a
  color), and **Upload** for your own picture. **Remove** takes the icon away.
  Choose the icon to change it.
- **Add cover** gives the page a cover from the gallery. Hover over the cover
  for **Change**, **Reposition** (drag the picture, then **Save position**)
  and download. **Change** opens:
  - **Gallery** — **Color & Gradient**, **Leafen landscapes**, **Leafen
    patterns** and **Leafen space**;
  - **Upload** — a picture from your device; pictures wider than 1500 pixels
    work best;
  - **Link** — the address of any picture on the web;
  - **Unsplash** — photo search, when the server's administrator set it up
    ([Self-hosting](../self-hosting.md#cover-image-search));
  - **Remove** takes the cover away.
- **Add verification** marks the page as verified, for a number of days or
  indefinitely, with its owners; Settings › **Verified pages** lists them.
- **Add comment** starts a comment on the whole page
  ([Mentions and comments](mentions-and-comments.md)).

## Sub-pages

A page inside another is a **sub-page**. It shows in its parent's body as a
page block and in the sidebar under its parent (▸ unfolds it). The
breadcrumb in the top bar shows the way down to it. A sub-page takes its
parent's sharing until its own is changed
([Sharing and permissions](sharing-and-permissions.md)).

## Move, copy and lock

- **Move to** (Cmd+Shift+P, or Ctrl+Shift+P) — search for a page or teamspace
  to put this page under. **Suggested** places come first, then
  **Teamspaces**; the workspace name at the foot moves it to another of your
  workspaces. A notice says where it went, with **Visit** and **Undo**.
  Dragging the page's row in the sidebar moves it too.
- **Duplicate** — copies the page, with its sub-pages, next to it, named
  with "(1)" after the title, and opens the copy.
- **Lock page** — stops accidental edits: the top bar shows **Locked**, and
  choosing it offers **Unlock for me** (for you, until **Re-lock**) or
  **Unlock for everyone**.
- **Archive** — takes the page out of the sidebar and search without
  deleting it. The page shows a banner ("… archived this page on ….")
  with **Unarchive**, and the notice after archiving offers **Unarchive**
  too. Search's **Content** filter finds archived pages.
- **Move to Trash** — see [Trash](trash.md).

## The sidebar's page menu

⋯ on a page's row in the sidebar offers **Add to Favorites**, **Copy link**,
**Duplicate**, **Rename** (Cmd+Shift+R), **Move to**, **Archive**, **Move to
Trash**, **Turn into wiki**, **Open in new tab** (Cmd+Shift+Enter) and **Open
in side peek** (Option-click the row).

## The page menu

⋯ at the right of the top bar opens the page menu. **Search actions…** at
its top finds any row, those in the submenus included. Its foot names who
last edited the page, when, and how many words it has.

| Row | What it does |
| --- | --- |
| **Copy link** (Cmd+Option+L) | Copies the page's address. The address has no title in it. |
| **Copy page contents** | Copies the page's body. |
| **Duplicate** (Cmd+D while the menu is open) | Copies the page, as above. |
| **Move to** (Cmd+Shift+P) | Moves the page, as above. |
| **Version history** | Earlier versions of the page ([Page history](page-history.md)). |
| **Updates & analytics** | The panel of the page's changes and views. |
| **Appearance & layout** › | **Font style** (**Default**, **Serif** or **Mono**), **Small text**, **Full width**, **Table of contents**, **Page comments** and **Inline comments**. |
| **Suggest edits** | Turns on suggesting: your edits become suggestions others accept or reject. |
| **Notify me** › | **All comments** or **Replies and @mentions**; the menu shows the choice as **Comments** or **Mentions**. |
| **Lock page** | Locks or unlocks the page, as above. |
| **Archive** | Archives the page, as above. |
| **Move to Trash** | Moves the page and its sub-pages to the trash. |
| **More** › | **Present** (Cmd+Option+P, a slide show of the page), **Undo**, **Import**, **Export**, **Read log** (who opened the page, for people with full access), **Turn into wiki**, **Connections** (the apps and agents that can reach the page) and **Available offline**. |

On Windows and Linux, Ctrl takes the place of Cmd and Alt of Option.
**Export** and **Import** are described in [Exporting](exporting.md) and
[Moving from Notion](moving-from-notion.md).

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Page title | Up to 200,000 characters | A longer paste goes to the body. |
| Untitled page | Shows **New page** | |
| New icon | A random emoji | From **Add icon**. |
| New cover | A gallery cover | From **Add cover**. |
| Cover upload | Best wider than 1500 pixels | |
| Duplicate's name | `<title> (1)` | |
| Notify me | **All comments** for whoever made the page | **Replies and @mentions** for everyone else. |
| Font style | **Default** | |

## For agents and the API

- `notion-create-pages` makes 1 to 100 pages at once, under a page, database,
  data source or teamspace, or as private pages, with Markdown content.
- `notion-update-page` changes a page's content, properties, icon and cover;
  `leafen_update_page` is the native endpoint's version.
- `notion-move-pages` moves 1 to 100 pages; `notion-duplicate-page` copies
  one, and `notion-get-async-task` follows the copy.
- `notion-fetch` and `leafen_fetch` read a page; `notion-page-view` gives the
  address that opens it.
- `notion-update-page-favorites` stars or unstars pages.
- See the [MCP reference](../../MCP.md).

## Differences from Notion

- A title stops at 200,000 characters; a longer paste goes to the body.
- A copied link has no page title in it.
- **Archive** stays in the page menu.
- A page's undo history lasts while the app is open, even after you visit
  another page and come back.
- An edit made just before the page closes or reloads is kept.

## Related chapters

- [Editor blocks](editor-blocks.md)
- [Mentions and comments](mentions-and-comments.md)
- [Page history](page-history.md)
- [Trash](trash.md)
- [Sharing and permissions](sharing-and-permissions.md)
- [Templates](templates.md)
