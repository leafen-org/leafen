# Page history

<!-- guide-sources:
web/src/components/PageHistoryDialog.tsx
web/src/components/PageHistoryVersionList.tsx
web/src/components/PageHistoryRestorePanel.tsx
web/src/components/PageHistoryBookmarks.tsx
web/src/components/PageHistoryRevertButtons.tsx
web/src/components/UpdatesPanel.tsx
web/src/locales/en/updatesPanel.json
web/src/pageHistoryWire.generated.ts
-->

This chapter covers a page's earlier versions: looking at them, seeing what
changed, bookmarking one, and bringing the page back to it. Undo (Cmd+Z)
covers the last few edits ([Keyboard shortcuts](keyboard-shortcuts.md)); a
deleted page comes back from the [Trash](trash.md).

## Open the version history

The page menu (⋯ in the top bar) › **Version history**. It covers the whole
window:

- on the left, the page's **Version history**: each version with its time and
  the people who made it, newest first. **Group by** sets how close changes
  are gathered into one version: **1 minute**, **15 minutes** or **1 hour**,
  or **Bookmarks** to list only the versions you bookmarked;
- on the right, the chosen version as it was, read only, with **Current
  version** on the newest. Its changes are marked: added words in color,
  deleted content folded under **Deleted content**. **1 / 2 changes** at the
  foot steps through them (**Previous change**, **Next change**);
- › beside a version (**See all changes in version**) lists its changes one
  by one, such as "Alex Example edited a text block" or "Alex Example renamed
  the page".

An inline database in an old version shows its rows as they were then, up to
50 rows ("More rows aren't shown here").

The **Updates** tab of the panel that the top bar's **Edited** time opens
lists the page's changes too, newest first, with who made each.

## Bookmark a version

The bookmark beside a version (**Bookmark this version**) asks **Name this
version** and keeps it under **Bookmarks**. A bookmark's menu renames or
deletes it; deleting the bookmark keeps the version in the history. A name
holds up to 200 characters.

## Restore a version

1. Choose the version, then **Begin restore**.
2. **What will be restored** lists what goes back to how it was:
   - **All page content** — text, blocks and other content;
   - **Page metadata** — the icon, cover and other properties;
   - **Moved content** — content moved to other pages comes back here;
   - for databases on the page, **Database metadata**, **Database
     properties**, **Database views and their configurations** and
     **Records and their property values**;
   - **Will remain unchanged** names what stays as it is: **Comments and
     reactions** (comments added after the version stay, other people's too).
3. Choose **Restore this version**. The notice says "Earlier version
   restored."

Restoring deletes no version, so you can always restore again. Inline databases restore their views and, for databases made
on the page, their properties and pages; a full-page database restores only
whether it is on the page.

### Undo an agent's or app's changes

Changes made through a connection — an agent, an app or an API key — are
marked in the history with the connection's name, such as "Alex Example
via Claude Code". **Undo** that connection's changes from this point on reverses only
what came through it; changes others made afterwards stay.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Group by | **1 minute** | Or **15 minutes**, **1 hour**, **Bookmarks**. |
| Bookmark name | Up to 200 characters | |
| Database rows in a version's preview | 50 | |
| Comments on restore | Kept | |

## For agents and the API

- `notion-get-version-changes` lists what changed in a page or database
  between two times (the last 24 hours by default), and who changed it.
- `notion-view-version` reads a page or database as it was at a past time.
- `notion-restore-version` restores a whole page or database to a past time,
  as **Restore this version** does; comments stay.
- The native API: `GET /api/leafen/v1/pages/{pageId}/history` and
  `POST /api/leafen/v1/pages/{pageId}/history/{versionId}/restore`. See
  [Building on Leafen](../developers.md).

## Differences from Notion

- Restoring a version keeps the comments added after it.
- A version's preview shows an inline database's rows as they were then.

## Related chapters

- [Pages](pages.md)
- [Trash](trash.md)
- [Mentions and comments](mentions-and-comments.md)
