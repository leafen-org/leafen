# Databases

<!-- guide-sources:
web/src/components/database/DatabaseStartPanel.tsx
web/src/components/database/DatabaseViewSettingsPanel.tsx
web/src/components/database/DatabaseDataSourcesPanel.tsx
web/src/components/database/databaseStructureLock.ts
web/src/components/sidebar/AddPageWindow.tsx
web/src/components/editor/PageDatabaseStartPicker.tsx
internal/app/database_view_limit.go
internal/app/database_starter_templates.json
-->

This chapter covers what a database is in Leafen, the ways to make one,
inline and full-page databases, data sources, linked views, and locking. It
is for anyone who keeps rows of things — tasks, contacts, notes — and for
agents that build databases through MCP. A database's columns are covered in
[Properties](properties.md), its ways of showing rows in [Views](views.md),
and its row templates in [Database templates](database-templates.md).

## What a database is

A database holds rows. Each row is a page: it has a title, values for the
database's properties, and a body you can write in like any other page.

A database has three parts:

- **Data sources.** A data source is the set of rows and the properties they
  share. A database starts with one data source and can hold more, each with
  its own rows and properties.
- **Views.** A view shows the rows of one data source as a table, a board, a
  calendar or another layout, with its own filters, sorts and groups. A
  database's views are the tabs above its rows.
- **The database itself**, with its title, icon and description, which is
  either a page of its own (a full-page database) or a block on another page
  (an inline database).

## Make a database

### From the sidebar

1. Press **New…** (the pencil at the top of the sidebar) and choose
   **Database**. The add window opens, with **Add to** set to **Private**.
2. Choose where it goes with **Add to**, then choose one of:
   - **Empty database**: a full-page database named "New database" with one
     table view and one property, Name;
   - a card under **Existing data sources**, which makes a new database
     showing a data source you already have;
   - a card under **Templates**: **Tasks Tracker**, **Projects**,
     **Document Hub**, **Brainstorm Session** or **Meeting Notes**, each a
     database with its own properties, views and sample rows.
3. The new database opens. Press its title, "New database", and type its
   name.

### On a page, with "/"

Type `/` on a page and choose one of these from the **Database** group:

| Command | Makes |
| --- | --- |
| **Database - Inline** | a new database inside the page |
| **Database - Full page** | a new database as a sub-page |
| **Table view**, **Board view**, **Gallery view**, **List view**, **Feed view**, **Dashboard view**, **Calendar view**, **Timeline view**, **Map view** | an inline database that starts with that view |
| **Vertical bar chart**, **Horizontal bar chart**, **Line chart**, **Donut chart**, **Number chart** | an inline database that starts with a chart view of that kind |
| **Form** | a form that saves its answers as rows ([Forms](forms.md)) |
| **Linked view of data source** | a view of a data source you already have |

Each of these opens the **New database** window beside the database, every
time:

- the field at the top, "Add or link data source…", names the new data
  source as you type: its first row becomes **New “〈name〉” data source**;
- **New empty data source** makes a data source with one property, Name;
- **Import CSV** makes a data source from a CSV file;
- **Suggested** lists **Tasks Tracker**, **Projects**, **Document Hub** and
  **Meeting Notes**;
- **Link existing data source** shows a data source you already have.

Closing the window with **✕** keeps the new database as it is drawn behind
the window, with an empty data source and the view you chose.

### Inline and full page

An inline database is a block on a page, drawn among the page's other
blocks. A full-page database is a page of its own in the sidebar and in
search. The rows are the same either way.

- To open an inline database as its own page, press the diagonal arrows in
  its toolbar (**Open as full page**), or choose **Open as page** in its
  block menu.
- The block menu of an inline database (the ⋮⋮ handle beside it) also has
  **Turn into page**, **Turn into simple table**, **Edit icon**,
  **Add to Favorites**, **Lock database**, **More options** (**Rename**,
  **Merge with CSV**), **Open in new tab**,
  **Open in side peek**, **Copy link**, **Duplicate**, **Move to**,
  **Archive** and **Move to Trash**.

## The database toolbar

Beside the views, the toolbar has, from left to right: **Filter**, **Sort**,
**Automations** (the lightning bolt), **Search**, **Open as full page** (only
for an inline database), **Settings** and **New**. **New** adds a row; the
arrow beside it, **More add options**, lists the database's templates and has
**New template** and **Add shortcut to sidebar**
([Database templates](database-templates.md)).

**Settings** opens **View settings**:

| Setting | What it does |
| --- | --- |
| View name, **Layout**, **Property visibility**, **Filter**, **Sort**, **Group**, **Conditional color**, **Copy link to view** | settings of the view on screen ([Views](views.md)) |
| **Source** | the data source the view shows |
| **Edit properties** | the data source's properties ([Properties](properties.md)) |
| **Automations** | the data source's automations ([Automations and buttons](automations-and-buttons.md)) |
| **Turn into** › **Turn into Tasks database** | makes it a task database ([Sprints](sprints.md)) |
| **View archived pages** | shows the archived rows instead of the others |
| **More settings** | **Sub-items**, **Dependencies** and **Customize page layout** |
| **Manage data sources** | the database's data sources and linked sources |
| **Lock database** | locks the database's structure |

## Data sources

**Settings** › **Manage data sources** lists the database's data sources,
each with how many views show it, under **Sources**, and the data sources it
links to under **Linked**.

- **Add data source** adds another data source, in its own new view, from the
  same choices as the **New database** window.
- **Link existing data source** adds a view of a data source that belongs to
  another database.
- A data source's **⋯** menu has **Copy data source ID**, **Move to** and,
  when the database has more than one data source, **Move to Trash**. Moving
  one to the trash asks first ("This moves the data source and 1 view to
  Trash.") and takes its views with it. Until it comes back from the trash it
  is in no list of data sources to link, relate to or start a database from,
  and it no longer counts in the database's name: a database left with one
  data source goes by that one's name, as a database of one data source does,
  and renaming its title renames that data source. When another comes back,
  the database takes its own name again, unless it was renamed meanwhile.

A database with one data source shares its name with it: renaming one renames
the other. With two data sources or more, each has its own name and the
database keeps the name it had.

### Linked views

A linked view shows a data source that lives in another database. Its title
starts with ↗. Adding, editing and removing rows in a linked view changes the
original data source; the linked view's own filters, sorts and layout belong
to it alone. Make one with **Linked view of data source** in the "/" menu or
**Link existing data source** in **Manage data sources**.

## Lock a database

A locked database keeps its views and properties as they are: the controls
that would change them are hidden or off, while its rows can still be added
and edited.

1. Open **Settings** and press **Lock database**, or turn on **Lock database**
   in the block menu or the page menu (**⋯** at the top right of a full-page
   database).
2. The toolbar shows **Locked**. Press it to choose **Unlock for me** (only
   for you; the toolbar then shows **Re-lock**) or
   **Unlock for everyone**.

On a linked view the setting is **Lock views**: it locks that view, while the
original data source's properties stay editable.

## The page menu of a full-page database

**⋯** at the top right of a full-page database has **Copy link**,
**Duplicate**, **Move to**, **Merge with CSV**, **Version history**,
**Updates & analytics**, **Customize layout**,
**Notify me**, **Lock database**, **Archive**, **Move to Trash** and **More**
(**Undo**, **Import**, **Export**, **Read log**, **Connections** and
**Available offline**).

**Move to Trash** moves the database, with all its rows, to the trash, from
which it can be restored.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Views in a database | 100 | Tabs, feeds and dashboards count; a dashboard's widgets don't. The 101st is refused: "A database holds at most 100 views". |
| New database's name | "New database" | Until you type one. |
| New data source | one property, Name (a Title) | |
| First view | Table, named "Table" | A view made from a "/" view command takes that layout. |
| Data sources | Move to Trash needs at least two | The last data source goes with its database. |

## For agents and the API

- `notion-create-database` makes a full-page database (or, from page content,
  an inline one) with a table view and the properties a schema names, under a
  page or at the top of the person's private pages; `database_type` `tasks` or
  `projects` gives the properties of a task or project database. Its answer
  gives the database's address and, in its `<data-source>` tag, the data
  source's `collection://` address, which the other tools take.
  [Relations and rollups](relations-and-rollups.md#for-agents-and-the-api)
  builds two joined databases this way, step by step.
- `notion-update-data-source` renames a data source, changes its properties
  with SQL DDL statements, changes its page layout, and turns a database inline
  or full page (`is_inline`).
- `notion-fetch` reads a database (`database://` or its page address) and a
  data source's schema (`collection://`).
- `notion-delete-databases` moves databases to the trash with their rows;
  `notion-restore-pages` brings them back.
- `leafen_catalog` lists a workspace's databases in one short answer, with
  each data source's `collection://` and `view://` addresses and its columns.

The tools are described in the
[MCP reference](../../MCP.md#notion-compatible-tool-surface).

## Differences from Notion

- A database made with "/" always opens its **New database** window.
- Turning an empty page into a database keeps that page as the database's
  page, so its address keeps working.
- Adding a second data source keeps the database's current name.
- A database whose only data source is in the trash can't be changed: no new
  rows, properties or views until the data source comes back.
- A full-page database in the trash is one row there, not two.
- **Archive** stays in the database's block menu and page menu.
- **Available offline** is Leafen's own.
- The database's extra settings have no connections to outside services such
  as Google Drive or GitHub.
- ⌘X on a database block cuts it without asking; Backspace and
  **Move to Trash** ask first.
- A full-page database's address always names the view on screen (`v=`), so a
  copied address opens that view.

## Related chapters

- [Properties](properties.md)
- [Views](views.md)
- [Relations and rollups](relations-and-rollups.md)
- [Database templates](database-templates.md)
- [Forms](forms.md)
- [Automations and buttons](automations-and-buttons.md)
- [Sprints](sprints.md)
