# The interface

<!-- guide-sources:
web/src/components/sidebar/Sidebar.tsx
web/src/components/sidebar/SidebarPageSections.tsx
web/src/components/sidebar/SidebarCustomizeList.tsx
web/src/components/search/SearchDialog.tsx
web/src/components/settings/settingsWindowModel.ts
web/src/components/settings/settingsWindowLabels.tsx
web/src/components/UpdatesPanel.tsx
web/src/components/home/HomeView.tsx
web/src/locales/en/sidebar.json
web/src/locales/en/searchDialog.json
web/src/locales/en/updatesPanel.json
internal/app/workspace_membership_wire.generated.go
-->

This chapter is a map of Leafen's screen in a browser or the desktop app: the
sidebar, the page's top bar, search, Home, the Library, the inbox and the
settings window. What each part holds is described where it is used: pages in
[Pages](pages.md), notifications in
[Favorites, recents and notifications](favorites-recents-and-notifications.md),
the keys in [Keyboard shortcuts](keyboard-shortcuts.md).

## The sidebar

The sidebar runs down the left of the window. From the top:

| Part | What it does |
| --- | --- |
| **Close sidebar** | Hides the sidebar (Cmd+\\ or Ctrl+\\ shows it again). Drag its edge to change its width. |
| **Inbox** | Opens the inbox beside the sidebar. |
| **New** | **Page** makes a private page and opens it; **Database** opens the add window for a database. |
| **Search** | Opens search (Cmd+K or Ctrl+K). |
| **Home** | Opens Home. |
| Shortcuts | Shortcuts you added (see below). |
| **Favorites** | Pages you starred. Shown once there is one. |
| **Teamspaces** | Each teamspace you are in, with its pages. Shown once there is one. |
| **Shared** | Pages shared with you directly. Shown once there is one. |
| **Private** | Your own pages. |
| **Customize sidebar** | Changes the sections and shortcuts. |
| The workspace name | Opens the workspace menu. |
| The four squares (tip **Library, Trash, more…**) | **Library**, **Apps**, **My tasks** and **Trash**. |

Each section's name folds it. Beside it, ↗ (**Open in Library**) opens the
same pages in the Library, and ⋯ opens the section's menu: **Sort**
(**Manual** or **Last edited**, which it starts with), **Show** (how many
pages it lists before **Show all**: 5, 10, 15, 20, 50 or 100, starting at 5),
**Move up**, **Move down**, **Remove section** and **Customize sidebar**. The
**+** beside **Private** makes a page there.

A page's row shows its icon and title; ▸ unfolds its sub-pages. Hovering the
row shows ⋯ (the page's actions, as in [Pages](pages.md#the-sidebars-page-menu)) and
**+** (**Add a page inside**). Drag a row to put the page elsewhere in the
tree.

### Customize the sidebar

**Customize sidebar** turns the sidebar into a list you arrange; **Done**
ends it.

- **Add shortcut** adds a row at the top: **Create** › **New page** or **New
  database**, or **Go to** › **Page**, **Settings**, **Library**,
  **Documents** or **My tasks**. A shortcut's menu renames or removes it.
- Drag the sections to reorder them, or remove one from its menu.
- **Add section** adds one: **Recents**, a single teamspace, or a **Database
  view** (one view of a database, listed in the sidebar), or, under
  **Advanced**, **Developer**.

The sidebar holds up to 25 shortcuts and 10 database views, and its whole
setup up to 32 KB. Workspace owners make sidebar presets in Settings ›
**Sidebar** › **Tab presets**: a default one new members start with, and
optional ones members add from **Add section**.

### The workspace menu

The workspace name at the foot of the sidebar opens it:

- the workspace's name and member count;
- **Settings**, **Invite members**, **Add account** (log in to another
  account, on this server or another), **Add a device** and **Connect an AI
  agent**;
- your accounts, each with its workspaces (a check beside the one you are
  in), **New workspace** and **Browse apps**;
- **Log out**.

Members and workspaces are in
[Workspaces and members](workspaces-and-members.md).

## The page's top bar

Above an open page:

- the **breadcrumb**: the page's place, each step a link;
- its access, such as **Private** with a lock, which opens the share menu's
  access;
- **Edited** and the time, which opens the **Updates** and **Analytics**
  panel beside the page;
- **Share**, the link button (**Copy link**), ☆ (**Add to Favorites**) and ⋯ (**Page
  settings & actions**, the page menu).

While you type the top bar fades, and it comes back when the pointer moves.
Sharing is in [Sharing and permissions](sharing-and-permissions.md); the page
menu in [Pages](pages.md#the-page-menu).

## Search

**Search** in the sidebar, or Cmd+K (Ctrl+K), opens search over the
workspace. With nothing typed it lists the pages you visited recently.

- Type to find pages by their title and their text. Each result names who
  made it and when it was last edited; the pane on the right previews the
  chosen one, with **Copy link** and **Open**.
- Filters under the field narrow it: **Title only**, **Created by**, **In**
  (a page and what is under it), and **Filter** for **Date** (**Last
  edited** or **Created**: **Today**, **Last 7 days**, **Last 30 days** or
  days chosen on a calendar), **Teamspace** and **Content** (**All pages
  (excluding archived)**, **Archived pages only**, **All pages (including
  archived)** or **Verified pages only**).
- The order reads **Best matches**; it also sorts by **Last edited** or
  **Created**, newest or oldest first.
- Enter opens the result; Cmd+Enter (Ctrl+Enter) opens it in a new tab.
  With no result, **Search deleted pages** looks in the trash.
- The settings button at the foot holds **Persist filters across sessions**.

## Home

**Home** greets you and gathers what you need next:

- **Recently visited** — the pages you opened lately, and **New page**;
- **Recently edited** — the pages you changed lately;
- **Upcoming events** — dated tasks coming up;
- **My tasks** — the tasks assigned to you across the workspace, from the
  task databases you choose with **Add task sources**.

## The Library

**Library**, under the four squares at the foot of the sidebar, lists the
workspace's pages by tab: **Teamspaces**, **Recents**, **Favorites**,
**Documents** (Office, PDF and Hangul files), **Shared** and **Private**. Each
tab can be searched and filtered, and a teamspace unfolds to its pages. **New
teamspace** is at the top right.

## The inbox

**Inbox** opens a panel beside the sidebar with your notifications: mentions,
comments, invitations, access requests, reminders and edits to pages you
follow. Its header pins it open, closes it, and filters it (**Unread &
read**, **Unread**, **Archived** or **All workspace updates**, and by type).
Notifications and their settings are in
[Favorites, recents and notifications](favorites-recents-and-notifications.md).

## Settings

The workspace menu › **Settings** opens the settings window. A field at its
top searches every setting. Its list is in groups:

| Group | Sections |
| --- | --- |
| **Account** | Your name (**Account**), **Preferences**, **Notifications**, **Server accounts** |
| **Workspace** | **General**, **Requests**, **People**, **Sidebar**, **Analytics**, **Import**, **Usage** |
| **Features** | **Connections**, **Agents**, **Verified pages**, **Public pages**, **Emoji**, **Developer** |
| **Admin** | **Teamspaces**, **Security**, **Identity**, **Enterprise controls**, **Organization** |
| **Server** | **General**, **Users**, **Workspaces**, **Apps**, **Audit log**, **Email delivery**, **Sign-up and login**, **Access from outside**, **Map**, **Updates**, **Backups**, **Standby server**, **Restore and move** |

You see only the sections your role covers: a guest sees the **Account**
group alone, only the workspace's owner sees **Sidebar**, **Usage** and
**Teamspaces**, a restricted member has no **Connections**, **Agents** or
**Developer**, and only the server's administrators see **Server**. Leaving a
section with unsaved changes asks first.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Sidebar shortcuts | Up to 25 | |
| Database views in the sidebar | Up to 10 | |
| Sidebar setup | Up to 32 KB | |
| Section shows | 5 pages | 5, 10, 15, 20, 50 or 100, then **Show all**. |
| Section order | **Last edited** | Or **Manual**. |
| Search order | **Best matches** | |
| Inbox filter | **Unread & read** | |

## For agents and the API

- `notion-get-sidebar-tabs` reads your sidebar (Leafen's one list, as one
  tab) and `notion-update-sidebar-tabs` replaces it.
- `notion-search` and `leafen_search` search as the search window does;
  `notion-list-recent-pages`, `notion-list-favorite-pages`,
  `notion-list-private-pages` and `notion-list-shared-pages` read the
  sidebar's lists.
- `leafen_inbox` reads the inbox. See the [MCP reference](../../MCP.md).

## Differences from Notion

- The sidebar is one list, without Notion's tabs for its AI chats and
  meetings; the four squares at its foot (tip **Library, Trash, more…**) hold
  Leafen's own **Apps** beside the Library, My tasks and the Trash.
- The settings window also holds the server's settings, for its
  administrators.
- The Library has a **Documents** tab listing Office, PDF and Hangul files.
- Home leaves out what only Notion's service can fill.

## Related chapters

- [Getting started](getting-started.md)
- [Pages](pages.md)
- [Keyboard shortcuts](keyboard-shortcuts.md)
- [Favorites, recents and notifications](favorites-recents-and-notifications.md)
- [Workspaces and members](workspaces-and-members.md)
