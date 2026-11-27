# Exporting

<!-- guide-sources:
web/src/components/PageExportDialog.tsx
web/src/components/WorkspaceExportDialog.tsx
web/src/components/nativeExport.ts
web/src/components/RowMenu.tsx
web/src/locales/en/pageExport.json
web/src/locales/en/workspaceSettingsDialog.json
-->

This chapter lists every way to take your work out of Leafen: a page, a
workspace, its members, a whole server, or through the API. It is for
everyone; the server-wide ways are for administrators. The format of a Leafen
archive is in [Leafen workspace bundles](../bundle.md), and backups are in
[Backups](backups.md).

## Export a page

1. Open the page's **•••** menu (top right) and choose **Export**.
2. Choose the **Export format**:
   - **PDF**: a print window opens; choose **Save as PDF** there. **Page
     format** (A4, A3, Letter, Legal or Tabloid) and **Scale percent** set the
     paper.
   - **HTML**: web pages you can open in any browser.
   - **Markdown & CSV**: Markdown for pages, and CSV for databases.
   - **Leafen archive**: a lossless copy of the page and its subpages, files
     included, that Leafen can import again.
3. Choose what goes in:
   - **Page content**: **Everything**, or **Exclude files and images**.
   - **Database views**: **Current view** or **Default view**, for the rows a
     database exports.
   - **Include subpages**, **Create folders for subpages** and **Export
     comments**.
4. Press **Export**. The file downloads.

A page whose linked database or relation points to a database outside it
can't be exported as a Leafen archive on its own; export the workspace's
archive instead. With **Markdown & CSV**, each database on the page is written
as a CSV file of its rows.

## Export a workspace

Workspace owners and administrators (not members or guests) open Settings →
**General** (under **Workspace**) →
**Export** → **Workspace content** → **Export**. The format is **HTML**,
**Markdown & CSV** or **Leafen archive**, with the same choices as a page,
plus **Include private pages**. The workspace is packaged on the server
first, which takes longer for a large workspace, then downloads.

A **Leafen archive** of a workspace goes into another Leafen through Settings →
**Import** → **Leafen**, keeping its pages, databases, files and comments.

**Members** → **Export** on the same page, or the download button beside the
search on Settings → **People**, saves a CSV of every member and guest in the
workspace.

## Move a workspace or a server

- **Move to another server** on Settings → **General** moves the workspace,
  with its people and page history, to another Leafen server that people
  already use.
- An administrator moves a whole server with **Move to another server** on
  Settings → Server → **Restore and move**.

Both are described in
[Self-hosting: Moving to another server](../self-hosting.md#moving-to-another-server).

## Back up the whole server

An administrator's **Backup file** (Settings → Server → **Backups**) and
scheduled backups hold everything on the server, logins and files included.
See [Backups](backups.md).

## When exports are turned off

An organization can turn page exports off for its workspaces, and a teamspace
can turn them off for its pages. The export window then says **Exports are
turned off by your organization or this page's teamspace.** Ask a workspace
administrator.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Page export formats | PDF, HTML, Markdown & CSV, Leafen archive | PDF only through the browser's print window. |
| Workspace export formats | HTML, Markdown & CSV, Leafen archive | |
| Page content | Everything | Or without files and images. |
| Database views | Current view | Or the default view. |
| Export comments | On | |
| Members export | CSV | Every member and guest. |

## For agents and the API

An agent or script reads every page and database through the API and MCP, as
[Building on Leafen](../developers.md) shows; `leafen_fetch` and
`notion-fetch` read a page's content, and `leafen_get_file` gives a file's
download address. A workspace archive's format is in
[Leafen workspace bundles](../bundle.md).

## Differences from Notion

- Notion's PDF export makes the file on its servers. Leafen opens the
  browser's print window, where you save it as a PDF.
- Notion has no lossless archive that goes back in. Leafen's **Leafen
  archive** brings a page or workspace into any Leafen as it was.

## Related chapters

- [Backups](backups.md)
- [Moving from Notion](moving-from-notion.md)
- [Leafen workspace bundles](../bundle.md)
