# Relations and rollups

<!-- guide-sources:
web/src/components/database/PropertyRelationConfig.tsx
web/src/components/database/RelationPropertyCreationDialog.tsx
web/src/components/database/PropertyRelationCell.tsx
web/src/components/database/RollupCalculationSelect.tsx
web/src/components/database/rollupPolicy.ts
internal/app/database_relation_limit.go
internal/app/database_rollup_reached_sources.go
internal/app/mcp_tool_database_create.go
-->

This chapter covers Relation properties, which link a row to rows of
another data source (or of its own), and Rollup properties, which calculate
over the related rows. It is for anyone connecting two databases — tasks to
projects, orders to customers — and for agents that build such databases
through MCP. Other property types are in [Properties](properties.md);
calculating within a row is in [Formulas](formulas.md).

## Add a relation

1. In a table, press **+** at the end of the column headers, type a name
   ("Bed page") and choose **Relation**.
2. Under **Related to**, choose the data source to link to: the field
   "Connect a data source" searches, and **Existing data sources** lists
   them. The data source the property is in is listed first, by its name,
   with an ⓘ that reads "This database".
3. The **New relation** window has:
   - the property's name;
   - **Related to**, the data source you chose;
   - **Limit**: **No limit** (the default) or **1 page**;
   - **Two-way relation**, off at first: "Add a matching property to the
     other database, so the relation shows from both sides". Turned on, it asks
     for the **Related property name**, which is the name of the property it
     adds to the other data source; left empty, it takes this data source's
     name.
4. Press **Add relation**.

A two-way relation keeps both sides in step: linking task A to bed B shows
bed B's related property holding task A, and unlinking either side unlinks
both. A relation to the same data source with **Two-way relation** on shows
each direction in its own property ("For a relation inside this database, give
each direction its own property").

**Edit property** on a relation shows **Related to** (which can't change
once made), **Limit** and **Two-way relation** (**On** or **Off**). Turning a
two-way relation off, or changing the property's type, ends the sync with
the other data source after asking "The related database stops syncing this
relation both ways, too".

## Link rows

Press a relation cell. The window has a search field ("Link or create a
page…"), a filter button and **Search in 〈data source〉**, and lists the
related data source's rows under **Select a page**. Press a row to link it.
Linked rows are listed first, under "1 selected" (or how many), each with a
**−** button that unlinks it; the others follow under **Select more**.
Typing a name no row has offers **New 〈name〉 in 〈data source〉**, which makes
that row and links it.

With **Limit** set to **1 page**, linking a second row replaces the first,
on both sides of a two-way relation.

A relation cell shows the linked rows' titles; pressing one opens that row.
A row's page lists the related rows by title, up to 200; past that, the rest
show when opened from their database.

## Add a rollup

A rollup needs a relation in the same data source.

1. Add a property and choose **Rollup**. The column is added with empty
   cells.
2. Press one of its cells, or open the column's menu and choose
   **Edit property**, and choose:
   - **Relation**: the relation to follow;
   - **Property**: the property of the related rows to calculate over (the
     related rows' title until you choose another);
   - **Calculate**: what to show. **Sum** is under **More options**.

**Property** and **Calculate** stay dimmed until a relation is chosen.

For example, with Harvests related to Crops by a two-way relation, a rollup
on Crops with **Relation** Harvests, **Property** Kg and **Calculate**
**Sum** shows each crop's total kilograms.

### Calculations

**Calculate** opens a menu with **Show original** (the related values
themselves, the default), **Show unique values** (each value once), and
groups whose items depend on the property's type:

| Group | Calculations | For |
| --- | --- | --- |
| **Count** | **Count all**, **Count values**, **Count unique values**, **Count empty**, **Count not empty** | every type except Checkbox and Status |
| **Count** | **Checked**, **Unchecked** | Checkbox, Formula, Rollup |
| **Count** | **Count per group** | Status |
| **Percent** | **Percent empty**, **Percent not empty** | every type except Checkbox and Status |
| **Percent** | **Percent checked**, **Percent unchecked** | Checkbox, Formula, Rollup |
| **Percent** | **Percent per group** | Status |
| **More options** | **Sum**, **Average**, **Median**, **Min**, **Max**, **Range** | Number, Formula, Rollup |
| **Date** | **Earliest date**, **Latest date**, **Date range** | Date, Created time, Last edited time, Last visited time, Formula, Rollup |

A Checkbox offers **Show original**, **Count all** and the checked counts and
percents only. A Status offers **Show original**, **Count all**,
**Count per group** and **Percent per group**; the per-group ones then ask
which status group to count. A rollup that gives a number has
**Number format**, **Decimal places** and **Show as** (**Number**, **Bar** or
**Ring**), as a Number property has.

A rollup can roll up another rollup; Leafen follows rollups of rollups up to
8 steps away.

### Filtering by a rollup

A filter on a rollup that shows original values also asks which of the
related values must match: **Any**, **None** or **Every**
([Views](views.md#filter)). A rollup that counts or sums filters as a number.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| **Limit** | **No limit** | **1 page** keeps one linked row on each side. |
| **Two-way relation** | off | |
| Related property name | the data source's name | |
| Rollup **Calculate** | **Show original** | |
| Rollups of rollups | 8 steps | |
| Related rows named on a row's page | 200 | |

## For agents and the API

In a `CREATE TABLE` schema (`notion-create-database`) or an
`ADD COLUMN` statement (`notion-update-data-source`, in its `statements`):

- `"Bed page" RELATION('〈data source ID〉')` makes a one-way relation. The
  ID is the related data source's: its `collection://` address, which
  `notion-create-database` and `notion-fetch` give in the `<data-source>`
  tag, or the ID after `collection://` alone;
- `RELATION('〈data source ID〉', DUAL)` makes a two-way relation, and
  `RELATION('〈data source ID〉', DUAL 'Bed page')` names the property it adds
  to the related data source;
- `"Total hours" ROLLUP('Tasks', 'Hours', 'sum')` makes a rollup that follows
  the relation named first (Tasks) and calculates over the property named
  second (Hours) of the related rows. It can follow a relation made in the
  same schema.

Two databases joined by a two-way relation, with a rollup that sums a number
over the related rows:

1. `notion-create-database` with the schema
   `CREATE TABLE "Tasks" ("Name" TITLE, "Hours" NUMBER)`. Its answer gives
   the Tasks data source's `collection://` address.
2. `notion-create-database` with the schema
   `CREATE TABLE "Beds" ("Name" TITLE, "Tasks" RELATION('〈Tasks ID〉', DUAL 'Bed page'), "Total hours" ROLLUP('Tasks', 'Hours', 'sum'))`.
   Beds gets the relation Tasks and the rollup Total hours, and Tasks gets
   the other side, Bed page.
3. Add rows with `notion-create-pages`, its `parent` naming the data source
   (`data_source_id`); the answer gives each new row's `url`.
4. Link rows with `notion-update-page` (`command` `update_properties`),
   giving the relation a JSON list of the related rows' addresses, written
   as text: `"Tasks": "[\"https://leafen.example.com/p/…\"]"`. The other
   side's Bed page follows.
5. Read the rollup's values with `notion-query-data-sources` in rows mode,
   `{"data": {"mode": "rows", "data_source_url": "collection://…"}}`, which
   gives each row's Total hours. `notion-fetch` and `leafen_fetch` on a row
   give its rollups' values too, the same values rows mode gives; a SQL query
   has no column for a rollup.

A rollup's function is one of `show_original`, `show_unique`, `count`,
`count_values`, `unique`, `empty`, `not_empty`, `percent_empty`,
`percent_not_empty`, `sum`, `average`, `median`, `min`, `max`, `range`,
`earliest_date`, `latest_date`, `date_range`, `checked`, `unchecked`,
`percent_checked`, `percent_unchecked`, `count_per_group` and
`percent_per_group`. See the
[MCP reference](../../MCP.md#notion-compatible-tool-surface).

## Differences from Notion

- A rollup given an unknown function is saved as **Show original** rather
  than kept as a calculation that can't run.
- A relation keeps a linked row that is in the trash when an API write
  leaves it out, so restoring the row restores the link.
- A one-syllable Korean search in the relation window matches anywhere in a
  row's name.
- The relation settings have no links to a help center.
- An agent's `notion-fetch` of a row gives each rollup's value; Notion's
  names it by a `rollupResult://` address that its fetch does not open.

## Related chapters

- [Properties](properties.md)
- [Formulas](formulas.md)
- [Views](views.md)
- [Databases](databases.md)
