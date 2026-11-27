# Templates

<!-- guide-sources:
web/src/components/sidebar/AddPageWindow.tsx
web/src/components/database/DatabaseStarterGallery.tsx
web/src/components/database/DatabaseStarterCustomizer.tsx
internal/app/database_starter_templates.json
web/src/components/editor/SpecialBlockContent.tsx
web/src/locales/en/sidebar.json
web/src/locales/en/blockItem.json
-->

This chapter covers starting a page from a template, and the ways to reuse a
page you made. A database's own row templates and repeating rows are in the
Database templates chapter ([Databases](README.md#databases)); buttons are in
the Automations and buttons chapter there.

## The add window's templates

The add window — **Add new** at the end of a short sidebar section, or
**New** › **Database** — lists **Templates**
under **Empty page**, **Empty database** and **Existing data sources**. Each
template makes a database, ready to use:

| Template | What it is for | Starts with the views |
| --- | --- | --- |
| **Tasks Tracker** | "Stay organized with tasks, your way." | **All Tasks**, **By Status**, **My Tasks** (and **Checklist** to add) |
| **Projects** | "Manage projects start to finish." | **By Status**, **All Projects**, **Gantt** (and **My Projects**) |
| **Document Hub** | "Collaborate on docs in one hub." | **All Docs**, **My Docs** (and **By Category**) |
| **Brainstorm Session** | "Spark new ideas together." | **All Ideas**, **By category** (and **Top Ideas**) |
| **Meeting Notes** | "Turn meetings into action." | **Table** |

To use one:

1. Choose the template. **Customize** it: "Select features to turn on or off"
   — each feature adds properties, for example **Priority**, **Task type**,
   **Effort level** and **Description** (on) and **Updated at**, **Past due**
   and **Attach file** (off) for Tasks Tracker. The preview on the right
   follows your choices. Choose **Continue**.
2. "Choose which views 〈database〉 starts with": turn views on or off, and
   choose **Done**.
3. The database opens with a few sample pages, their dates set around today.
   **Get started** at its foot offers **Delete sample pages**, **Tour** (a
   short walk through its buttons) and **Customize**.

**Add to** at the top of the window chooses where the database goes, and the
**Search** field filters the window.

## Reuse a page of your own

Leafen has no gallery of page templates. To start from a page you made:

- **Duplicate** it from its page menu, then rename and fill in the copy
  ([Pages](pages.md#move-copy-and-lock));
- make a **Button** whose step **Insert blocks** adds the same blocks wherever
  it is pressed ([Editor blocks](editor-blocks.md#button-button));
- in a database, make it a row template, which every new row can start from
  (the Database templates chapter, under [Databases](README.md#databases)).

In a row template or a button's blocks, **@** offers **Today**, **Now** and
**Me**: they are filled in with the date, the time or the person when the
template is used ("When duplicated").

A published page can be copied by its visitors into a workspace of their
own when **Duplicate as template** is on in its site settings
([Sharing and permissions](sharing-and-permissions.md#publish-to-the-web)).

## Template blocks from Notion

A page imported from Notion may hold an old Notion template button. It shows
as a button with its name; pressing it adds a copy of the blocks it holds
above it ("Template added"). The menu does not make new ones; a **Button**
does the same and more.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Templates in the add window | 5 | Each makes a database. |
| Sample pages | 3 in Tasks Tracker | **Delete sample pages** removes them. |
| Template mentions | **Today**, **Now**, **Me** | Filled in when the template is used. |

## For agents and the API

- `notion-create-database` with `database_type` `tasks` or `projects` makes a
  database with the properties of those templates.
- `notion-update-page` with `apply_template` applies a database's row
  template to a page.
- `notion-duplicate-page` copies a page you use as a template; follow it with
  `notion-get-async-task`.
- See the [MCP reference](../../MCP.md).

## Differences from Notion

- The add window offers the database templates, but no template gallery.
- A template's tour names the buttons as they are on the screen.

## Related chapters

- [Pages](pages.md)
- [Editor blocks](editor-blocks.md)
- [Databases](README.md#databases)
