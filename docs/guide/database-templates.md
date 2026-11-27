# Database templates

<!-- guide-sources:
web/src/components/database/DatabaseTemplateMenu.tsx
web/src/components/database/DatabaseTemplateEditor.tsx
web/src/components/database/DatabaseTemplateRepeatChooser.tsx
web/src/components/database/templateRepeatModel.ts
web/src/components/database/RowPageTemplatePrompt.tsx
internal/app/database_toolbar_config_wire.generated.go
-->

This chapter covers a database's templates: pages that new rows start from,
with their property values and body filled in, and templates that make a new
row on a schedule. It is for anyone who adds the same kind of row again and
again — a weekly report, a bug, a meeting — and for agents that apply
templates. Page templates outside databases are in the Templates chapter of
the guide; a database's own properties are in [Properties](properties.md).

## Make a template

1. In the database toolbar, press the arrow beside **New**
   (**More add options**). The menu lists the templates under
   "Templates 〈database〉", with **New template** and
   **Add shortcut to sidebar**.
2. Press **New template**. The template opens with the banner "You're editing
   a template in 〈database〉".
3. Give it a title and fill in the properties and the body as new rows should
   start. The template's title becomes the title of the rows made from it.
4. Close the window. The template is saved as you type.

Every database also has **Empty**, a row with nothing filled in, which is the
default until you choose another.

## Use a template

- Press the arrow beside **New** and choose a template: a new row is made
  from it and opens.
- Press **New**, or **New page** under a table: the new row's page opens and
  says "Choose a template with ‘↑’ and ‘↓’, or press ‘enter’ to start from
  an empty page", listing the templates and **New template**.
  Choosing one fills the row from it.
- The database's **default template** fills every new row at once, with no
  choice to make.

Templates that hold uploaded files can't be applied or duplicated yet: make
the row first, then upload new copies of the files.

## Manage templates

Each template's **⋯** in the templates menu has:

| Item | What it does |
| --- | --- |
| **Duplicate every…** | makes a new row from the template on a schedule ([below](#repeat-a-template)) |
| **Set as default** | asks "Use "〈template〉" as the default template for new pages?": **For all views in “〈database〉”** or **Only on “〈view〉” view** |
| **Edit** | opens the template |
| **Duplicate** | adds a copy |
| **Delete** | asks "Delete this template?" |

The default template is marked **Default**. Drag a template by its handle to
reorder the list.

## Repeat a template

A template can make a new row by itself on a schedule:

1. In the template's banner, or in its **⋯** menu, open **Duplicate every…**
   and choose **Day**, **Weekday**, **Week**, **2 Weeks**, **Month**,
   **3 Months**, **6 Months** or **Year**.
2. **Edit cadence** opens: **Every** a number of days, weeks (with the
   weekdays), months or years; **At** (**Around midnight**, between 12 and
   3 AM, at first); **Starting** (tomorrow at first); **Ending** (**Never**
   at first, a specific date, or a number of runs); and **Timezone** (yours
   at first). **Next occurrence:** shows when the next row is made.
3. Press **Save**. The banner reads "Duplicate every… Week" and the template
   is marked with a repeat icon in the menu.

**Cancel recurrence** in **Edit cadence** stops it. A repeat that has made
its last row reads **Off** again. The limits are those of an automation's
schedule ([Automations and buttons](automations-and-buttons.md#schedules)).

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Default template | **Empty** | |
| A template's body | up to 4 MiB | |
| Repeat **At** | **Around midnight** | between 12 and 3 AM |
| Repeat **Starting** | tomorrow | |
| Repeat **Ending** | **Never** | at most 999 runs when limited |

## For agents and the API

- `notion-fetch` on a data source lists its templates.
- `notion-update-page` with `apply_template` and a `template_id` puts a
  template's body after a row's own and fills the properties the row leaves
  empty.
- The native API reads a database's templates with
  `GET /api/leafen/v1/pages/{pageId}/databases/{dataSourceId}/toolbar-config`
  and makes rows from them with
  `POST /api/leafen/v1/pages/{pageId}/databases/{dataSourceId}/toolbar-config/template-rows`
  ([Building on Leafen](../developers.md#find-the-operations)).

## Differences from Notion

- The template banner has no help link.
- A template's **Add a property** is in the reader's language.

## Related chapters

- [Databases](databases.md)
- [Properties](properties.md)
- [Automations and buttons](automations-and-buttons.md)
- [Sprints](sprints.md)
