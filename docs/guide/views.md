# Views

<!-- guide-sources:
web/src/components/database/DatabaseViewLayoutOptions.tsx
web/src/components/database/DatabaseViewSettingsPanel.tsx
web/src/components/database/queryProjection.ts
web/src/components/database/boardViewModel.ts
web/src/components/database/databasePersonalQuery.ts
web/src/components/database/NativeDatabaseViewMount.tsx
web/src/components/database/ChartSettingsPanel.tsx
web/src/components/database/DatabaseDashboardWidgets.tsx
web/src/lib/dashboardLayout.ts
internal/app/database_view_limit.go
contracts/content-graph-wire.json
-->

This chapter covers a database's views: adding one, the eleven layouts, and
the filters, sorts, groups and other settings a view keeps. It is for anyone
who wants to see the same rows in different ways, and for agents that make
or change views through MCP. Making a database is in
[Databases](databases.md) and its columns in [Properties](properties.md);
the form layout is described in [Forms](forms.md).

## Add a view

A database's views are the tabs above its rows.

1. Press **+** after the last tab (**Add view**), which shows while the
   pointer is over the tabs. **Add a new view** lists
   **Table**, **Board**, **Gallery**, **List**, **Chart**, **Dashboard**,
   **Timeline**, **Feed**, **Map**, **Calendar** and **Form**, and
   **New data source**, which adds a data source with a view of its own.
2. Choose a layout. The view is added and its settings open under
   **New view**: a name field ("View name"), the layouts to switch between,
   the layout's options and its **Source**.
3. Type a name and press **Done**, at the end of the settings; the panel
   then shows the view's **View settings**, which **✕** closes. A view left
   without a name shows its layout's name on its tab ("Chart"); a form view
   is named "Form builder", and the first view of a database made through
   MCP "Default view".

A database holds at most 100 views.

Pressing a view's tab while it is open opens its menu: **Rename**,
**Display as** (**Text and icon**, **Text only** or **Icon only**, for you
alone), **Edit view**, **Source**, **Copy link to view**,
**Add view to sidebar**, **Duplicate view** and **Delete view**. Drag a tab to
reorder the views.

## View settings

**Settings** in the database toolbar opens **View settings** for the view on
screen:

| Setting | What it does |
| --- | --- |
| View name and icon | the tab's name and icon |
| **Layout** | the layout and its options (below, under each layout) |
| **Property visibility** | which properties show, with how many show; drag to reorder |
| **Filter** | which rows show |
| **Sort** | the order of the rows |
| **Group** | splits the rows into groups by a property |
| **Conditional color** | colors rows or values that match rules |
| **Copy link to view** | copies an address that opens this view |

Changes to a view are saved for everyone who opens it, with one exception:
on a database other people can open, filters and sorts you change apply for
you alone at first. The filter bar then says "Only you see these filters and
sorts until they're saved for everyone.", with **Reset** and
**Save for everyone**; **More save options** has **Save as new view**. On a
database only you can open, they save at once.

### Filter

1. Press **Filter** in the toolbar and choose a property. A filter pill
   ("Bed") is added under the toolbar, with **+ Filter** beside it.
2. Choose the condition (for example **Is**, **Contains** or **Is empty**)
   and the value. The pill then reads like "Bed: North".

A pill's **⋯** menu has **Add to advanced filter** and **Remove filter**.
**Add advanced filter** adds a list of rules that all (**And**) or any
(**Or**) must match; **Add filter rule** adds a rule and
**Add filter group** a group that holds rules of its own. A group can hold
one more group, and that one rules only. A filter given through the API
nests one group less: its top **and** or **or** can hold another, and that
one conditions only.

The conditions depend on the property type:

| Property types | Conditions |
| --- | --- |
| Title, Text, URL, Email, Phone, and a formula that gives text | **Is**, **Is not**, **Contains**, **Does not contain**, **Starts with**, **Ends with**, **Is empty**, **Is not empty** |
| Number, ID, and a rollup that counts or sums | **=**, **≠**, **>**, **<**, **≥**, **≤**, **Is empty**, **Is not empty** |
| Select, Status | **Is**, **Is not**, **Is empty**, **Is not empty** |
| Multi-select, Person, Created by, Last edited by, Relation | **Contains**, **Does not contain**, **Is empty**, **Is not empty** |
| Date, Created time, Last edited time | **Is**, **Is before**, **Is after**, **Is on or before**, **Is on or after**, **Is between**, **Is relative to today**, **Is empty**, **Is not empty** |
| Checkbox | **Is**, **Is not**: **Checked** or **Unchecked** |
| Files & media, Place | **Is empty**, **Is not empty** |

A date filter takes a day you pick or type, or a day relative to today
(**Today**, **Tomorrow**, **Yesterday**, **One week ago**,
**One week from now**, **One month ago**, **One month from now**), or a range
like **Past**, **Next** or **This** with a number of **Days**, **Weeks**,
**Months** or **Years**. "Filter will update with the current date." A
rollup's filter also chooses whether **Any**, **None** or **Every** related
value has to match.

### Sort

Press **Sort** in the toolbar, choose a property and a direction
(**Ascending** or **Descending**; for a checkbox
**Sort unchecked → checked** or **Sort checked → unchecked**). **Add sort**
adds another, applied when the ones before it tie; drag a sort to reorder
them. **Delete sort** removes them all. A column's menu has
**Sort ascending** and **Sort descending**.

### Group

**Settings** › **Group** › **Group by** chooses the property. Then:

- **Sort** orders the groups: **Manual** (drag them), or by value;
- **Hide empty groups**;
- **Groups** lists every group with a pin, to keep it first, and an eye, to
  hide it (**Hide all**, **Show all**);
- a Status property groups by **Option** or **Group** (**Status by**);
- a Date groups by **Day**, **Week**, **Month**, **Year** or **Relative**
  (**Date by**), with **Start week on**;
- a Text groups by **Exact** value or **Alphabetical** first letter
  (**Text by**);
- a Number groups by **Unique** value or **Range** (**Number by**), with
  **Group range** and **Group every**.

A board can also **Sub-group**, splitting each column into rows.
**Remove grouping** ends it.

### Conditional color

**Settings** › **Conditional color** › **New color setting** colors the rows
(or cards) whose values match a rule: choose a property, a condition, a value
and a color, and under **Apply to**, the **Entire row** or only
**This property**. **Add another** adds a rule.

### Open pages in and load limit

Most layouts have **Open pages in**: **Side peek** (the default; the page
opens beside the view), **Center peek** (a window over the view) or
**Full page**. A table, board, list, gallery, timeline, chart and map have a
**Load limit** — how many rows show before **Load more**: 10, 25, 50 or 100.
Left unchosen, a table, list and gallery show 50 rows at first, a board 25
cards in each column, and a feed 10 posts.

## Layouts

### Table (`table`)

Rows and columns. Its layout options are **Show vertical lines** (on),
**Show page icon** (on), **Wrap all content** (off), **Open pages in** and
**Load limit**. Each column's width is dragged at its edge, and its menu has
**Freeze**, **Hide**, **Wrap content** and **Calculate**. **Calculate** shows
a value under the column: **Count all**, **Count values**,
**Count unique values**, **Count empty**, **Count not empty**,
**Percent empty**, **Percent not empty**, and by type **Sum**, **Average**,
**Median**, **Min**, **Max**, **Range**, **Earliest date**, **Latest date**,
**Date range**, **Checked**, **Unchecked**, **Percent checked**,
**Percent unchecked**, or for a status **Count per group** and
**Percent per group**. The counts can be shown as "99+" above 99
(**Show large counts as 99+**). A grouped table shows 25 rows in each group at first and 50 more at
each **Load more**.

### Board (`board`)

Cards in columns, one column for each value of the property it groups by.
A new board groups by the first Status property, else the first Select,
Person, Multi-select or Relation property, in the order the properties are
listed; a board can group by any property a table can. Without one, it says "Group this board". Its layout options are
**Show page icon**, **Wrap all content**, **Group by**, **Color columns**
(each column tinted with its option's color; on), **Open pages in**, and the
card options:

- **Card preview**: **None**, **Page cover**, **Page content** or a Files &
  media property;
- **Card size**: **Small**, **Medium** (the default) or **Large**;
- **Fit media**, when there is a preview;
- **Card layout**: **List** or **Compact**, whose
  **Compact card settings** choose the properties that take a line of their
  own (**Full line display**).

Dragging a card to another column changes its value. A column's **⋯** has
its name, color and **Delete group**. On a database's own page a column
draws more cards as you scroll; elsewhere it ends in **Load more**: 25 cards
at first and 20 more each time. Leaving an empty new card removes it.

### List (`list`)

A plain list of rows with their properties at the end of each line. Its
layout options are **Show page icon**, **Directly edit page name** (a press
on a name edits it instead of opening the page; off), **Open pages in** and
**Load limit**.

### Calendar (`calendar`)

Rows on a month or week calendar by a date. It needs a Date, Created time or
Last edited time property; without one it says "A calendar view needs a date
property to show anything." Its layout options are **Show page icon**,
**Wrap page titles**, **Show calendar by** (the date property),
**Show calendar as** (**Month**, the default, or **Week**),
**Show weekends** (on) and **Open pages in**. **Today** and the arrows move
the calendar; **No date** lists the rows without a date — "Click a page or drag
it onto the calendar to date it." A calendar view can't be added to the sidebar.

### Timeline (`timeline`)

Rows as bars on a time line, by a date. It needs a date property, like the
calendar. Its layout options are **Show page icon**, **Show timeline by**,
**Separate start and end dates** (with two date properties or more; then
**Start date** and **End date**), **Show table** (a table of the rows beside
the bars, with **Table properties**), **Open pages in** and **Load limit**.
The zoom is **Hours**, **Day**, **Week**, **Bi-week**, **Month**,
**Quarter**, **Year** or **5 Years**. Dragging a bar or its ends changes the
dates. With **More settings** › **Dependencies** on, arrows between bars show
which rows block others, and moving one row can shift the rows that depend on
it (**Shift & maintain time between items**, **Shift only when dates
overlap** or **Do not automatically shift**, and **Avoid weekends**). The
timeline reopens where you left it, in this browser.

### Gallery (`gallery`)

Rows as cards in a grid. Its layout options are **Show page icon**,
**Wrap all content**, **Open pages in**, **Load limit** and the card options
a board has (**Card preview**, **Card size**, **Fit media**,
**Card layout**). A card's preview is 16:9 of its width.

### Form (`form`)

A form whose answers become rows of the data source. See [Forms](forms.md).

### Chart (`chart`)

A chart of the rows. **Edit chart** (or **Settings**) opens its settings:

- **Chart type**: **Vertical bar**, **Horizontal bar**, **Line**, **Donut**
  or **Number**;
- **X axis**: **What to show** (the property to group rows by), **Sort by**
  and **Omit zero values**;
- **Y axis**: **What to show** (**Count**, or a calculation such as **Sum** or
  **Average** of a number property), **Group by** (to split each bar),
  **Range** (**Auto** or **Custom**, with **Min** and **Max**) and
  **Reference line** (up to 10 lines, each with a **Value**, **Label**,
  **Style** and **Color**);
- **Style**: **Color** (**Auto**, **Colorful**, **Colorless** or one color)
  and **More style options**, such as **Height**, **Grid line**,
  **Axis name**, **Legend**, **Data labels**, **Smooth line**,
  **Gradient area** and **Cumulative**;
- **Source**, **Filter**, **View archived pages** and **Save chart as…**,
  which shows the picture and offers **Copy as PNG**, **Download PNG** and
  **Download SVG**.

A new chart counts rows by the first Status, Select or Multi-select property,
in the order the properties are listed. A chart with nothing to group by says
"Nothing to chart yet". Pressing a bar shows its rows.

### Map (`map`)

Rows as pins on a map, by a Place property (**Map by**). Without one it says
"Add a Place property to plot database rows". Its layout options are
**Show page icon**, **Map by**, **Open pages in** and **Load limit**, and its
settings have **Height** (**Small**, **Medium**, **Large** or
**Extra large**). The map's tiles come from the tile server the server's
administrator set, by default OpenStreetMap. Rows without a place are listed
under **No place**. A map view can't be added to the sidebar.

### Dashboard (`dashboard`)

Charts, tables, lists and other views side by side, each a widget showing a
data source of its own. **Edit** (or **Edit dashboard** on an empty one)
lets you add, move and resize widgets: **Add to row**, **Add to new row**,
**Resize widgets**, **Move to row above**, **Move to row below**,
**Move left**, **Move right** and **Snap to view height**. A dashboard holds
up to 12 widgets, 4 in a row. A widget's menu has **Duplicate** and
**Delete**. **Done** ends editing. A dashboard's settings have
**Show icons in heading**.

### Feed (`feed`)

Rows as posts, one under another, each showing its page's content, its
comments and how many people viewed it. Its layout options are
**Show page icon**, **Wrap properties**, **Show author byline** (on),
**Open pages in** and **Load limit**. It shows 10 posts at first.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Views in a database | 100 | Dashboard widgets don't count. |
| First rows shown | table, list, gallery 50; board 25 per column; feed 10 | **Load limit** chooses 10, 25, 50 or 100. |
| Board **Load more** | 20 more cards in each column | |
| Grouped table | 25 rows a group, then 50 more | |
| Dashboard widgets | 12, 4 in a row | |
| Chart reference lines | 10 | |
| Card size | Medium | |
| Open pages in | Side peek | |

## For agents and the API

- `notion-create-view` adds a view to a database, as a tab (`database_id`,
  the database's address), or as an inline linked view at the end of a page
  (`parent_page_id`). It takes the data source (`data_source_id`, its
  `collection://` address), a `name` and a `type`; its `configure` sets
  filters, sorts, grouping and display in the view language, which the MCP
  resource `notion://docs/view-dsl-spec` describes. For example, a board of
  one bed's tasks by stage, where Bed is a Select and Stage a Status:
  `type` `board` with `configure`
  `FILTER "Bed" = "North"; GROUP BY "Stage" BY OPTION`. The answer gives the
  view's `view://` address.
- `notion-update-view` changes a view's name, filters, sorts, grouping or
  display, only what it is given; `CLEAR` removes a setting.
- `notion-query-data-sources` with `mode` `view` reads a saved view's rows
  under its filters and sorts:
  `{"data": {"mode": "view", "view_url": "view://…"}}`.
- `notion-fetch` on a `view://` address reads a view's settings.

See the [MCP reference](../../MCP.md#notion-compatible-tool-surface), and
[view links](../../MCP.md#view-links) for showing a view outside Leafen.

## Differences from Notion

- A database page's address always names the view on screen.
- A timeline reopens where you left it without writing that into the view,
  so opening it shows nobody an edit.
- A gallery card's preview is always 16:9 of its width.
- A new chart picks its first property in the order the properties are
  listed.
- A number chart's heading reads as words, such as "Hours sum".
- A saved chart picture carries no "made with" mark.
- Leaving an empty new board card removes it.
- The map's tiles are the ones the server's administrator chose,
  OpenStreetMap's by default.
- The calendar and timeline block menu has no "clear dates" line, which did
  nothing in Notion.
- A form view's form is described in [Forms](forms.md).

## Related chapters

- [Databases](databases.md)
- [Properties](properties.md)
- [Formulas](formulas.md)
- [Forms](forms.md)
- [Sprints](sprints.md)
