# File apps

<!-- guide-sources:
builtin-apps/webdav/worker/main.go
builtin-apps/webdav/worker/tree.go
catalog-apps/office/leafen-app.json
web/src/components/apps/PersonAppPasswords.tsx
web/src/components/apps/PersonAppsPage.tsx
web/src/locales/en/common.json
web/src/locales/en/library.json
-->

This chapter shows how to reach the files kept in Leafen from other programs,
such as Finder, Windows Explorer, Office or a sync tool, over WebDAV, and how
Leafen's document apps open Word, Excel, PowerPoint, PDF and Hangul files on
pages. It is for everyone. How apps are built, and how an app offers an
endpoint like WebDAV, is in [Building Leafen apps](../apps.md); installing apps
on a server is for its administrators, as the
[Organizations and audit](organization-and-audit.md) chapter notes.

## Apps that work with files

Open **Apps**: the four squares at the foot of the sidebar (tip **Library,
Trash, more…**) → **Apps**.
These apps come with Leafen; an administrator installs them with **Install**,
and the people they choose can use them at once:

| App | What it does |
| --- | --- |
| **Drive** | Opens Leafen's pages and files as folders in Finder, Windows Explorer and Office over WebDAV, with a place for your own and shared files. |
| **PDF Documents** | Opens PDF files on pages to read them, fill their forms and add text, drawings and highlights, saving them back as PDF. |
| **Hangul Documents** | Opens Hangul (HWP and HWPX) documents on pages to read and edit them without Hancom Office, saving them in their own format. |

**Office Documents** opens Word, Excel and PowerPoint files (docx, xlsx, pptx)
in the browser with ONLYOFFICE and saves them in their own format. At about
240 MB it comes through an app catalog rather than inside Leafen; when your
server's catalog offers it, it is installed the same way. Phones and small
screens get a lighter view to read.

The library's **Documents** tab lists the Word, Excel, PowerPoint, PDF and
Hangul files on the pages you can see, with their page, creator and last edit,
and search through their text. Choosing one opens it in its document app.

## Open Leafen as a drive (WebDAV)

1. Install **Drive** (an administrator), then press **Open** on it.
2. Under **Connect**, **Server address** shows the WebDAV address, the
   server's address followed by `/app-endpoints/webdav/dav/`. Press **Copy**.
3. Under **App password**, press **Make one**. It takes you to **App
   passwords** on the **Apps** page: type a name, such as "Work laptop", and
   press **Make**. The **Username** and **Password** show only now; copy them.
4. Connect from your computer:
   - **Mac**: Finder › Go › Connect to Server, paste the address, then log in
     with the username and password.
   - **Windows**: File Explorer › This PC › Map network drive. Windows connects
     only to https addresses; turn on an outside address first
     ([Outside address](outside-address.md)).
   - **Other programs**: any WebDAV client. **Sync with other apps** on the
     Drive page shows the settings for Obsidian (Remotely Save) and rclone.

On an address that isn't https, macOS asks before it sends a username and
password; **Connect** goes on to the login window.

### What you see in the drive

- **Pages**: each workspace's page tree. Pages are folders, and a page's
  files are the files in its folder. You see the pages you can read, and can
  put files only on pages you can edit.
- **Files**: a place for files that belong to no page. **My files** is yours
  alone; everyone in the organization sees **Shared files**.

The top folders are named in the language your computer asks for: English, or
Korean on a Korean system. Opening a file opens it; saving it puts the new
file in the same block, and the page's history keeps the old one. A new file
becomes a file block at the end of the page. Everything runs as you, so you see
and change only what you may in Leafen.

## App passwords

An app password lets one program reach one app's endpoint, such as the drive,
as you. It does only what you may through that app, and it can't log in to
Leafen, its API or MCP. **App passwords** on the **Apps** page lists each one
with the last characters, when it was made and last used; **Revoke** stops it
at once.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| App passwords | 20 per app | Shown once, when made. |
| Wrong app passwords | 10 from the same place and name, then a 15-minute wait | |
| WebDAV address | The server's address + `/app-endpoints/webdav/dav/` | https needed for Windows Explorer. |
| A file sent to the drive | Up to 5 GB | The organization's storage limit applies. |
| Drive's top folders | Pages, Files (My files, Shared files) | Korean names for a Korean client. |

## For agents and the API

An agent reads and replaces a page's files with `leafen_get_file` and
`leafen_replace_file`, as the [MCP reference](../../MCP.md) describes. An app's
endpoints, such as the drive's, are for programs logged in with an app
password, not for agents.

## Differences from Notion

- Notion has no drive. Leafen's **Drive** app opens pages and their files as
  folders in your file manager.
- The library has a **Documents** tab, and Leafen's document apps edit Word,
  Excel, PowerPoint, PDF and Hangul files in place.

## Related chapters

- [Building Leafen apps](../apps.md)
- [Outside address](outside-address.md)
- [Exporting](exporting.md)
- [Agents](agents.md)
