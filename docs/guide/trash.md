# Trash

<!-- guide-sources:
web/src/components/TrashView.tsx
web/src/components/TrashFilters.tsx
web/src/components/trashFilterModel.ts
web/src/components/settings/WorkspaceDeletedPages.tsx
web/src/locales/en/trashView.json
web/src/locales/en/sidebar.json
web/src/locales/en/pageArchive.json
internal/app/page_trash_lifecycle_schema.go
-->

This chapter covers what happens when a page goes to the trash, how to bring
it back, and what deleting it forever means. Taking a page out of sight
without deleting it is **Archive** ([Pages](pages.md#move-copy-and-lock));
going back to an earlier version of a page that still exists is
[Page history](page-history.md).

## Move a page to the trash

- The page menu (⋯ in the top bar) › **Move to Trash**, or
- the page's ⋯ in the sidebar › **Move to Trash**, or
- Delete on the page's block, where it is a sub-page in another page.

The page leaves the sidebar, search and its parent page, with its sub-pages
and the databases inside it. A notice says **Moved to Trash**, with
**Restore** to bring it straight back.

## Open the trash

The four squares at the foot of the sidebar, beside the workspace name (its
tip reads **Library, Trash, more…**), open **Library**, **Apps**, **My
tasks** and **Trash**. **Trash** opens the trash beside the sidebar:

- **Search pages in Trash** finds a page by its title.
- The filters **Last edited by**, **In** and **Teamspace** narrow it. **Last
  edited by** starts with you, so the trash first shows the pages you edited
  last; remove yourself to see every page you can open.
- Each row names the page and, for a page that was inside another, the
  pages above it. ↶ (**Restore**) brings it back, and the bin (**Delete
  forever**) deletes it for good.
- Its foot says: "Pages are deleted automatically after 30 days in
  Trash".

Choosing a row opens the trashed page, read only, under a banner such as
"Alex Example put this page in Trash just now. It’s deleted permanently
after 30 more days.", with **Restore page** and **Permanently delete**.

## Restore a page

**Restore**, **Restore page** or the notice's **Restore** puts the page back
where it was, with its sub-pages. If the page
it was in is gone too, Leafen asks **Restore this page?** and adds it to your
private pages instead; its old permissions are lost.

## Delete forever

Only the workspace's owner can delete a page forever: others see **Delete
forever** and **Permanently delete** unavailable and can restore. Leafen asks
"Delete this page forever?" first. A page also
leaves the trash by itself after 30 days.

Even then, a page deleted for good is kept, hidden from everyone, for another
30 days. In that time the workspace's owner finds it in Settings ›
**Security** › **Data retention** › **Deleted page retention**, with who
deleted it and when and the day it is gone for good, and can **Restore** it
or **Delete now**. A page under an active legal hold is kept until the hold
is released.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Retention for pages in Trash | 30 days | Then the page is deleted for good. |
| Deleted page retention | 30 days | Only the workspace's owner can restore it in this time. |
| Who can delete forever | The workspace's owner | |
| Trash filter **Last edited by** | You | |

## For agents and the API

- `notion-delete-pages` moves pages to the trash with their sub-pages and
  databases, only with `user_confirmed: true` (without it, it lists what
  would move); `notion-delete-databases` does the same for databases.
- `notion-restore-pages` brings pages or databases back to where they were,
  as the trash's **Restore** does.
- The native API lists and restores a workspace's deleted pages:
  `GET /api/leafen/v1/workspaces/{workspaceId}/deleted-pages` and
  `POST /api/leafen/v1/workspaces/{workspaceId}/deleted-pages/{pageId}/recover`.
- See the [MCP reference](../../MCP.md).

## Differences from Notion

- Any workspace owner can bring back a page deleted forever, for 30 days,
  from Settings › **Security** › **Data retention**.
- Deleting forever is left to the workspace's owner; a member with full
  access can restore but not delete forever.
- The trash notice's **Restore** brings the page back.
- A full-page database is one row in the trash.

## Related chapters

- [Pages](pages.md)
- [Page history](page-history.md)
- [Concepts](concepts.md)
