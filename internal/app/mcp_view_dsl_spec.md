# Leafen view configuration

`notion-create-view` and `notion-update-view` take a view's settings as a
`configure` string, as Notion's tools do: directives separated by semicolons
or new lines, property names in double quotes, keywords in any case.

## Directives

- `FILTER "Property" operator value` — filters rows. `AND`, `OR` and
  parentheses join conditions; several FILTER directives must all hold.
  A FILTER replaces the view's filter.
  - Operators: `=`, `!=`, `>`, `<`, `>=`, `<=`, `CONTAINS`, `DOES NOT CONTAIN`,
    `STARTS WITH`, `ENDS WITH`, `IS EMPTY`, `IS NOT EMPTY`, `IN ("a", "b")`,
    and for dates `BETWEEN "2026-01-01" AND "2026-01-31"`.
  - Select and status: an option's name, or for a status a group's name.
  - Date: `2026-01-31`, or today, tomorrow, yesterday, one_week_ago,
    one_week_from_now, one_month_ago, one_month_from_now — a comparison with
    such a word stays relative, read on the day the view is looked at. With
    `=`, these stay relative too: this_week, this_month, this_year,
    past_week, past_month, past_year, next_week, next_month, next_year,
    past_3_days, next_2_weeks. `BETWEEN`'s ends stand for their days: a
    word's is the day it is for the connection's person when written.
  - Number: a number. Checkbox: TRUE or FALSE.
  - Person: a `user://…` address, a user's ID, or `me`. Relation: a page's
    address or ID, or `"this page"` — the page the view is on, and in a
    database template's view each page made from the template. Names don't
    read for either.
  - Files and places: only `IS EMPTY` and `IS NOT EMPTY`.
- `QUICK FILTER "Property" [operator value], "Property"` — adds properties to
  the view's filter bar, with a condition or for viewers to pick. The bar's
  other properties stay.
- `SORT BY "Property" [ASC|DESC], "Property" [ASC|DESC]`.
- `ROW ORDER "row page ID", "row page ID"` — the rows' order as dragged on the
  screen: those named first, in that order, the rest after them, newest
  first. Rows are named by their page IDs or addresses, all of one database.
  A view with sorts shows its rows by them; the order only breaks their ties.
- `GROUP BY "Property" [BY OPTION | BY GROUP | BY relative|day|week|month|year]
  [SHOW EMPTY | HIDE EMPTY]` — tables, boards, lists, galleries, timelines; a
  chart's X axis. BY OPTION and BY GROUP are for status properties; a date
  groups its days by a unit (fetch's `groupBy.groupBy`).
- `SUB GROUP BY "Property" [BY OPTION | BY GROUP] [SHOW EMPTY | HIDE EMPTY]` —
  boards.
- With a view grouped by an option or status property: `GROUPS "Done",
  "Doing", NO VALUE` — the groups' order (those named first, the rest after
  as they were); `HIDE GROUPS "Done", NO VALUE` and `SHOW GROUPS "Done"`;
  `GROUP COUNTS true|false`; and on a board `COLOR COLUMNS true|false`.
  Groups are named as their options are, a status's by its groups unless
  grouped BY OPTION. Fetch reads them back as `groups`, `hiddenGroups`,
  `groupCounts` and `colorColumns` once they are set.
- `CARD LAYOUT default|compact` — boards and galleries.
- `CARD SIZE small|medium|large` — galleries; give it or COVER's SIZE, not both.
- `CALENDAR BY "Date"`, `TIMELINE BY "Start" [TO "End"]`, `MAP BY "Place"`.
- `SHOW "Property", "Property"` — the properties shown, in order.
- `HIDE "Property", "Property"`.
- `COVER "Property" [SIZE small|medium|large] [ASPECT cover|contain]` — boards
  and galleries.
- `WRAP CELLS true|false` — tables. `WRAP PROPERTIES true|false` — galleries
  whose properties were set, or with SHOW in the same string; fetch's
  `wrapProperties` then lists every property.
- `FREEZE COLUMNS 2` — tables. `WIDTH "Property" 320, "Property" 120` — tables,
  whole numbers of at least 32.
- `ICON "🚀"` or `ICON "icons/rocket_blue"` — the view tab's icon, an emoji or one of Leafen's icons.
- `CHART column|bar|line|donut|number [AGGREGATE count|sum|average|… [ON "Property"]]
  [COLOR gray|blue|…|auto|colorful] [HEIGHT small|medium|large|extra_large]
  [SORT x_ascending|x_descending|y_ascending|y_descending]
  [STACK BY "Property"] [CAPTION "text"]`, and its format, each as fetch's
  `chartConfig.chartFormat` shows it:
  `LEGEND off|bottom|side`, `AXIS LABELS none|x_axis|y_axis|both`,
  `GRID LINES none|horizontal|vertical|both`, `GROUP STYLE normal|percent|side_by_side`,
  `LABELS none|value|name|name_and_value` (a donut's), `DATA LABELS true|false`,
  `COLOR BY VALUE true|false`, `CUMULATIVE true|false`, `SMOOTH LINE true|false`,
  `HIDE FILL true|false` (a line's), `HIDE TITLE true|false` (a number's),
  `Y MIN 0`, `Y MAX 10` (or `auto`),
  `REFERENCE LINE 5 "label" [COLOR red] [SOLID|DASH]` — the reference lines a
  CHART names replace the chart's —, a donut's `HIDE VALUE true|false`,
  `SHOW VALUE LABEL true|false` and `HIDE NEGATIVE true|false`,
  `VALUE LABEL "text"` (a number chart's title or a donut's center label),
  and a number chart's `NUMBER FORMAT number|number_with_commas|percent|won|…`,
  `DECIMALS 0`–`5`, `VALUE COLOR default|gray|…|red` and
  `COLOR RULE > 0 green` (`>`, `>=`, `<`, `<=`, `=`, `!=`; the rules a CHART
  names replace the chart's and color its value by them). `GROUP BY` sets
  the X axis. These clauses follow CHART on its line. A format set to the
  chart's default is saved and left out of chartFormat, and the answer's
  `warnings` say so.
- `FORM CLOSE`, `FORM OPEN`, `FORM ANONYMOUS true|false`,
  `FORM PERMISSIONS none|comment_only|reader|read_and_write|editor`.
- `FORM QUESTIONS "Property" [AS "question"] [REQUIRED] [MAX 2] [DROPDOWN], "Property"`
  — the questions a form asks, in order, as fetch's `questions` lists them: a
  question keeps its name unless AS gives it one; MAX is how many a
  multi-select, person, relation or files question takes; DROPDOWN shows a
  choice question's options in a menu. A form first given questions opens to
  the workspace's members.

Several FILTERs are kept as one "and" of all their conditions, as fetch's
`advancedFilter` reads them back: each condition with Notion's operator
(`string_contains`, `number_greater_than`, `enum_is`, `date_is_on_or_after`,
`is_empty`, …) and its value, exact or relative.

## notion-update-view only

`CLEAR FILTER`, `CLEAR QUICK FILTER ["Property", …]`, `CLEAR SORT`,
`CLEAR GROUP BY` (not on a board), `CLEAR SUB GROUP BY`, `CLEAR ICON`,
`CLEAR REFERENCE LINES`, `CLEAR COLOR RULES`. A CLEAR runs before what the
same string sets.

## Beside configure

- `show_data_source_title: false` on notion-create-view or notion-update-view
  hides the data source's title above the view (fetch's `showDataSourceTitle`).
- `position` on notion-create-view with database_id puts the new tab
  `{"type": "start"}`, `{"type": "end"}` or
  `{"type": "after_view", "view_id": "view://…"}`; fetching the database lists
  its views in tab order. On Leafen's own MCP endpoint, `leafen_delete_view`
  takes a tab away, so a database can start with another kind of view than
  its first table.

## Example

```
FILTER "Status" = "In progress" OR "Priority" > 3
SORT BY "Due" DESC
SHOW "Name", "Status", "Due"
```
