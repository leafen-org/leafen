# Formulas

<!-- guide-sources:
web/src/components/database/formulaFunctionCatalog.ts
web/src/components/database/formulaSyntax.ts
web/src/components/database/FormulaEditorDialog.tsx
web/src/components/database/boardViewModel.ts
internal/app/database_formula_functions.go
internal/app/database_formula_syntax.go
internal/app/database_formula_dates.go
internal/app/database_formula_formats.go
internal/app/database_formula_list_values.go
internal/app/business_calendar.go
-->

This chapter covers the Formula property: how to add one, the formula
editor, how a formula is written, and every function Leafen's formulas know,
with an example and its result. It is for anyone who computes a value from a
row's other properties, and for agents that write formulas through MCP. The
other property types are in [Properties](properties.md); rollups, which
compute over related rows without a formula, are in
[Relations and rollups](relations-and-rollups.md).

## Add a formula property

1. In a table, press **+** at the end of the column headers. A field for the
   property's name ("Type property name…") opens over the list of types under
   **Select type**.
2. Choose **Formula**. A column named "Formula" is added.
3. Press one of its cells. The **Edit formula** window opens.
4. Type the formula, for example `prop("Price") * prop("Quantity")`.
5. Close the window with **✕**, Escape or a press outside it. The formula is
   kept as written, even one with a problem; **Discard all changes** goes back
   to the formula the window opened with.

The formula runs on every row of the data source. A formula column has no
values of its own: you change the formula, not the cells. To change it later,
press a cell, or open the column's menu and choose
**Edit property** › **Edit formula**. When the formula gives a number, the
same **Edit property** menu also has **Number format**, **Decimal places**
and **Show as** (**Number**, **Bar** or **Ring**), as a Number property has
([Properties](properties.md#number-number)).

## The formula editor

The **Edit formula** window has the formula field ("Your formula") at the
top. A property you name with `prop("…")` turns into a token with the
property's name. Under the field:

- **Preview:** the result on one row of the data source, with the row's name
  as a menu to pick another row, and **Type:** the result's type (Text,
  Number, Checkbox, Date, List, Page, User, or Unknown while there is
  nothing to run). A formula with a problem shows the problem instead, for
  example "Math doesn’t work between text and number values. [0,7]", where
  the numbers in brackets are the first and last character positions of the
  part with the problem
- **Debug mode**, a switch that shows the value of each part of the formula.
  This browser remembers it.
- A list of **Properties** (the data source's properties), **Built-ins** (the
  operators) and the functions by group: **General**, **Text**, **Number**,
  **Date**, **People**, **List** and **Special**. Typing narrows it to what
  fits where the caret is. Up and Down move in it and Enter writes the chosen
  entry at the caret. Beside the list, the chosen entry's description shows
  examples with their results, each with a copy button.

## Writing a formula

### Values

| Kind | Written as | Type shown |
| --- | --- | --- |
| Number | `12`, `-3.5` | Number |
| Text | `"Plan B"` or `'Plan B'`; `\"`, `\\`, `\n` and `\t` inside | Text |
| Checkbox | `true`, `false` | Checkbox |
| List | `[1, 2, 3]` | List |
| Date | from a date property, `now()`, `today()` or `parseDate("2026-03-14")` | Date |
| Person | from a Person, Created by or Last edited by property | User |
| Page | from a Relation property | Page |

A comment is written between `/*` and `*/`.

### Properties

`prop("Name")` reads the property named Name on the same row. The editor
shows each reference as a token with the property's name; renaming the
property renames the reference. A property's value has the type its property
type gives it:

| Property type | Type in a formula |
| --- | --- |
| Number | Number |
| Checkbox | Checkbox |
| Date, Created time, Last edited time | Date |
| Multi-select, Person, Relation | List |
| Title, Text, URL, Email, Phone, Select, Status, Files & media | Text |

A relation's pages are a list of pages. `prop("Relation").first().prop("Due")`
reads the Due property of the first related page.

### Calling functions

A function is called by name, `upper("loud")`, or as a method of its first
value, `"loud".upper()`. Both give the same result, and methods chain:
`"Blueberry".upper().length()` is `9`.

Inside `map()`, `filter()`, `find()`, `findIndex()`, `some()`, `every()`,
`count()` and `sort()`, `current` is the list item being looked at and
`index` its position, starting at 0. `map([1, 2, 3], current * 10)` is
10, 20, 30, and `map(["a", "b"], current + index)` is a0, b1.

`sort()` sorts by its items, or by the value its second value, an
expression, gives each item: `sort(["pear", "fig", "banana"], length(current))`
is fig, pear, banana, and `sort([3, 1, 2], current * -1)` is 3, 2, 1. The
lowest comes first and equal values keep their order. Numbers sort by value,
dates by time and false before true when every value is one of these;
anything else sorts by its text as a person reads it: letters in alphabetical
order whatever their case, an upper case before its lower ("a", "B", "b",
"C"), numbers inside text by their value ("x2", "X3", "x10"), and empty
values last.

`let(name, value, expression)` names a value for use in the expression;
`lets()` names several.

### Empty values

A property with no value is empty. `empty()` is true for an empty value, and
for `0`, `""`, `false` and `[]`. A formula whose result is empty shows an
empty cell.

`empty()` with nothing in the parentheses gives an empty value of no type of
its own, so `if(prop("Done"), now(), empty())` is a date formula: it filters
and reads through the API as a date, empty where Done is unchecked. A
formula that gives nothing but `empty()` is of undefined type; it filters and
reads through the API as text.

## Operators

| Operator | Does | Example | Result |
| --- | --- | --- | --- |
| `+` | adds numbers, or joins text | `2 + 3` | 5 |
| `-` | subtracts | `10 - 4` | 6 |
| `*` | multiplies | `6 * 7` | 42 |
| `/` | divides | `7 / 2` | 3.5 |
| `%` | remainder | `7 % 3` | 1 |
| `^` | power | `2 ^ 10` | 1024 |
| `==` | equal | `2 == 2` | true |
| `!=` | not equal | `"a" != "b"` | true |
| `>` `>=` `<` `<=` | compare numbers, text or dates | `5 > 3` | true |
| `and` (`&&`) | both true | `true and false` | false |
| `or` (`\|\|`) | either true | `true or false` | true |
| `not` (`!`) | the opposite | `not true` | false |
| `? :` | the second value when the first is true, else the third | `10 > 5 ? "big" : "small"` | big |

## Functions

Each table gives what the function takes (as the editor's definition names
it; `?` marks an optional value, `...` more of the same), what it gives back,
an example and the example's result.

### General

| Function | Takes | Gives | Example | Result |
| --- | --- | --- | --- | --- |
| `if()` | condition, ifTrue, ifFalse | the chosen value | `if(3 > 5, "yes", "no")` | no |
| `ifs()` | condition, ifTrue, condition2, ifTrue2, ..., else | the value of the first true condition | `ifs(false, "first", true, "second", "neither")` | second |
| `and()` | boolean, boolean, ... | Checkbox | `and(true, 2 > 1)` | true |
| `or()` | boolean, boolean, ... | Checkbox | `or(false, 1 > 2)` | false |
| `not()` | boolean | Checkbox | `not(2 > 1)` | false |
| `empty()` | value? | Checkbox, or an empty value when given none | `empty("")` | true |
| `length()` | value | Number: characters of text, items of a list | `length("orchard")` | 7 |
| `format()` | value | Text | `format(42)` | 42 |
| `equal()` | value, value | Checkbox | `equal("x", "x")` | true |
| `unequal()` | value, value | Checkbox | `unequal(1, 1)` | false |
| `let()` | variable, value, expression | the expression's value | `let(width, 4, width * width)` | 16 |
| `lets()` | variable, value, variable2, value2, ..., expression | the expression's value | `lets(base, 6, height, 3, base * height / 2)` | 9 |

### Text

Patterns in `test()`, `match()`, `replace()` and `replaceAll()` are regular
expressions. A pattern that nests repetition, such as `(a+)+`, is refused.

| Function | Takes | Gives | Example | Result |
| --- | --- | --- | --- | --- |
| `substring()` | text, startIndex, endIndex? | Text | `substring("Blueberry", 0, 4)` | Blue |
| `contains()` | value, search | Checkbox | `contains("Blueberry", "berry")` | true |
| `startsWith()` | text, search | Checkbox | `startsWith("Blueberry", "Blue")` | true |
| `endsWith()` | text, search | Checkbox | `endsWith("Blueberry", "rye")` | false |
| `test()` | text, pattern | Checkbox | `test("Room 101", "[0-9]+")` | true |
| `match()` | text, pattern | List of every match | `match("a1 b22 c333", "[0-9]+")` | 1, 22, 333 |
| `replace()` | text, pattern, replacement? | Text, the first match replaced | `replace("one-two-three", "-", " ")` | one two-three |
| `replaceAll()` | text, pattern, replacement? | Text, every match replaced | `replaceAll("one-two-three", "-", " ")` | one two three |
| `lower()` | text | Text | `lower("QUIET")` | quiet |
| `upper()` | text | Text | `upper("loud")` | LOUD |
| `trim()` | text | Text without spaces at either end | `trim("  padded  ")` | padded |
| `repeat()` | text, count | Text | `repeat("ab", 3)` | ababab |
| `padStart()` | text, targetLength, padString | Text | `padStart("7", 3, "0")` | 007 |
| `padEnd()` | text, targetLength, padString | Text | `padEnd("7", 3, "*")` | 7** |
| `link()` | text, url | Text that links to the address | `link("Example", "https://example.com")` | Example, linked |
| `style()` | value, styles | Text with styles | `style("Late", "b", "red")` | **Late** in red |
| `unstyle()` | value, styles? | Text without the styles named, or without any | `unstyle(style("Late", "b", "red"))` | Late |
| `concat()` | list, list2, ... | List | `concat([1, 2], [3])` | 1, 2, 3 |
| `join()` | list, joiner | Text | `join(["red", "green", "blue"], ", ")` | red, green, blue |
| `split()` | text, separator | List | `split("red,green,blue", ",")` | red, green, blue |

`style()` takes `"b"` (bold), `"i"` (italic), `"u"` (underline), `"s"`
(strikethrough), `"c"` (code) and the colors `"gray"`, `"brown"`, `"orange"`,
`"yellow"`, `"green"`, `"blue"`, `"purple"`, `"pink"` and `"red"`; a color
with `_background` added (`"red_background"`) colors the background.

### Number

| Function | Takes | Gives | Example | Result |
| --- | --- | --- | --- | --- |
| `formatNumber()` | value, format?, precision? | Text | `formatNumber(1234.5, "usd")` | $1,234.50 |
| `add()` | number, number | Number | `add(8, 5)` | 13 |
| `subtract()` | number, number | Number | `subtract(8, 5)` | 3 |
| `multiply()` | number, number | Number | `multiply(8, 5)` | 40 |
| `mod()` | number, number | Number | `mod(8, 5)` | 3 |
| `pow()` | number, number | Number | `pow(3, 4)` | 81 |
| `divide()` | number, number | Number | `divide(9, 4)` | 2.25 |
| `min()` | numbers or lists of numbers | Number | `min(4, 9, 2)` | 2 |
| `max()` | numbers or lists of numbers | Number | `max([4, 9, 2])` | 9 |
| `sum()` | numbers or lists of numbers | Number | `sum([1, 2], 3)` | 6 |
| `median()` | numbers or lists of numbers | Number | `median(1, 5, 9, 20)` | 7 |
| `mean()` | numbers or lists of numbers | Number | `mean(2, 4, 9)` | 5 |
| `abs()` | number | Number | `abs(-12)` | 12 |
| `round()` | value, precision | Number | `round(2.567, 2)` | 2.57 |
| `ceil()` | number | Number | `ceil(2.1)` | 3 |
| `floor()` | number | Number | `floor(2.9)` | 2 |
| `sqrt()` | number | Number | `sqrt(81)` | 9 |
| `cbrt()` | number | Number | `cbrt(27)` | 3 |
| `exp()` | number | Number | `exp(0)` | 1 |
| `ln()` | number | Number | `ln(1)` | 0 |
| `log10()` | number | Number | `log10(1000)` | 3 |
| `log2()` | number | Number | `log2(8)` | 3 |
| `sign()` | number | Number: 1, -1 or 0 | `sign(-3)` | -1 |
| `pi()` | nothing | Number | `round(pi(), 4)` | 3.1416 |
| `e()` | nothing | Number | `round(e(), 4)` | 2.7183 |
| `toNumber()` | value | Number | `toNumber("42")` | 42 |

`round()` without a precision rounds to a whole number; its precision is a
whole number of at most 12 decimal places. `toNumber()` reads text, a
checkbox (true is 1) and a date (its timestamp in milliseconds); a list is
refused.

`formatNumber()`'s format is `"commas"` (the default), `"percent"`,
`"humanize"`, `"bytes_binary"`, `"bytes_decimal"` or a currency code: `"usd"`,
`"aud"`, `"cad"`, `"sgd"`, `"eur"`, `"gbp"`, `"jpy"`, `"rub"`, `"inr"`,
`"krw"`, `"cny"`, `"brl"`, `"try"`, `"idr"`, `"chf"`, `"hkd"`, `"nzd"`,
`"sek"`, `"nok"`, `"mxn"`, `"zar"`, `"twd"`, `"dkk"`, `"pln"`, `"thb"`,
`"huf"`, `"czk"`, `"ils"`, `"clp"`, `"php"`, `"aed"`, `"cop"`, `"sar"`,
`"myr"`, `"ron"`, `"ars"`, `"uyu"`, `"pen"`, `"vnd"`, `"pkr"` or `"ngn"`. Its
precision is 0 to 12 decimal places.

| Example | Result |
| --- | --- |
| `formatNumber(1234567, "commas")` | 1,234,567 |
| `formatNumber(0.256, "percent", 1)` | 25.6% |
| `formatNumber(1500000, "humanize")` | 1.5M |
| `formatNumber(2048, "bytes_binary")` | 2 KiB |

### Date

| Function | Takes | Gives | Example | Result |
| --- | --- | --- | --- | --- |
| `now()` | nothing | Date: now, with the time | `now()` | the current date and time |
| `today()` | nothing | Date: today, without a time | `today()` | today's date |
| `minute()` | date | Number, 0–59 | `minute(parseDate("2026-03-14T09:26Z"))` | 26 |
| `hour()` | date | Number, 0–23 | `hour(parseDate("2026-03-14T09:26Z"))` | 9 in UTC, 18 in Seoul |
| `day()` | date | Number: the weekday, 1 (Monday) to 7 (Sunday) | `day(parseDate("2026-03-14"))` | 6 |
| `date()` | date | Number: the day of the month, 1–31 | `date(parseDate("2026-03-14"))` | 14 |
| `week()` | date | Number: the ISO week, 1–53 | `week(parseDate("2026-03-14"))` | 11 |
| `month()` | date | Number, 1–12 | `month(parseDate("2026-03-14"))` | 3 |
| `year()` | date | Number | `year(parseDate("2026-03-14"))` | 2026 |
| `dateAdd()` | date, num, unit | Date | `dateAdd(parseDate("2026-01-31"), 1, "months")` | February 28, 2026 |
| `dateSubtract()` | date, num, unit | Date | `dateSubtract(parseDate("2026-03-14"), 2, "weeks")` | February 28, 2026 |
| `dateBetween()` | date, date, unit | Number: the first date minus the second | `dateBetween(parseDate("2026-03-14"), parseDate("2026-03-01"), "days")` | 13 |
| `dateRange()` | start, end | Date range | `dateRange(parseDate("2026-03-01"), parseDate("2026-03-05"))` | March 1, 2026 → March 5, 2026 |
| `dateStart()` | dateRange | Date | `dateStart(dateRange(parseDate("2026-03-01"), parseDate("2026-03-05")))` | March 1, 2026 |
| `dateEnd()` | dateRange | Date | `dateEnd(dateRange(parseDate("2026-03-01"), parseDate("2026-03-05")))` | March 5, 2026 |
| `timestamp()` | date | Number: milliseconds since January 1, 1970 UTC | `timestamp(parseDate("2026-01-01T00:00Z"))` | 1767225600000 |
| `fromTimestamp()` | timestamp | Date and time, to the minute | `fromTimestamp(1767225600000)` | January 1, 2026, 00:00 UTC |
| `formatDate()` | date, format, timezone? | Text | `formatDate(parseDate("2026-03-14"), "YYYY/MM/DD")` | 2026/03/14 |
| `parseDate()` | dateText | Date, from ISO 8601 text | `parseDate("2026-03-14")` | March 14, 2026 |
| `nextWorkday()` | date, calendar? | Date: the date if it is a working day, else the next one | `nextWorkday(parseDate("2026-03-01"))` | March 3, 2026 |
| `addWorkdays()` | date, count, calendar? | Date moved by working days | `addWorkdays(parseDate("2026-03-13"), 2)` | March 17, 2026 |

A date with a time is read in the time zone of the person looking at it:
`hour()` and `minute()` in a cell give the hour and minute there, and the
server, for the API and MCP, uses UTC.

The unit of `dateAdd()`, `dateSubtract()` and `dateBetween()` is `"years"`,
`"quarters"`, `"months"`, `"weeks"`, `"days"`, `"hours"`, `"minutes"`,
`"seconds"` or `"milliseconds"`; the singular (`"day"`) works too.

`formatDate()` writes these parts of a date and keeps any other characters:

| Pattern | Writes | Pattern | Writes |
| --- | --- | --- | --- |
| `YYYY` | 2026 | `dddd` | the weekday's name |
| `YY` | 26 | `ddd` | the weekday's short name |
| `Q` | the quarter, 1–4 | `d` | the weekday, 0 (Sunday) to 6 |
| `MMMM` | the month's name | `W`, `WW` | the ISO week, `WW` with two digits |
| `MMM` | the month's short name | `HH`, `H` | the hour, 0–23 |
| `MM`, `M` | the month, `MM` with two digits | `hh`, `h` | the hour, 1–12 |
| `DD`, `D` | the day of the month | `mm`, `m` | the minute |
| `Do` | the day with its ordinal (14th) | `ss`, `s` | the second |
| `DDD`, `DDDD` | the day of the year | `A`, `a` | AM or PM, am or pm |
| `X` | seconds since 1970 | `Z`, `ZZ` | the time zone offset, `+09:00` or `+0900` |

`formatDate(parseDate("2026-03-14T09:05Z"), "dddd, MMMM Do, h:mm A", "UTC")`
is "Saturday, March 14th, 9:05 AM". The time zone value is a zone name such as
`"UTC"` or `"Asia/Seoul"`, or an offset such as `"UTC+1"`; without one, the
date is written in the time zone of the person looking at it.

### Working days

`nextWorkday()` and `addWorkdays()` skip weekend days and public holidays,
including substitute and temporary holidays, of a calendar. The calendar is
`"KR"` (South Korea), which is also the default; it holds the holidays of
2024 to 2027, and in a year it does not hold only weekends are skipped. A
calendar Leafen does not have gives an empty value, and so does a count of
more than 3,660 working days. A negative count moves back. The time of day is
kept.

### People

| Function | Takes | Gives | Example | Result |
| --- | --- | --- | --- | --- |
| `name()` | person | Text | `name(prop("Created by"))` | the person's name |
| `email()` | person | Text | `email(prop("Created by"))` | the person's email |

For a list of people, use `map()`:
`prop("Assignee").map(name(current)).join(", ")`.

### List

| Function | Takes | Gives | Example | Result |
| --- | --- | --- | --- | --- |
| `at()` | list, index | the item at the index, from 0 | `at([10, 20, 30], 1)` | 20 |
| `first()` | list | the first item | `first([10, 20, 30])` | 10 |
| `last()` | list | the last item | `last([10, 20, 30])` | 30 |
| `slice()` | list, startIndex, endIndex? | List | `slice([10, 20, 30, 40], 1, 3)` | 20, 30 |
| `splice()` | list, startIndex, deleteCount?, ... | List with items removed and the rest inserted | `splice([10, 20, 30], 1, 1, 99)` | 10, 99, 30 |
| `sort()` | list, expression? | List, sorted by its items or by the expression | `sort(["pear", "fig", "banana"])` | banana, fig, pear |
| `reverse()` | list | List | `reverse([1, 2, 3])` | 3, 2, 1 |
| `unique()` | list | List without repeats | `unique([1, 1, 2, 3, 3])` | 1, 2, 3 |
| `includes()` | list, search | Checkbox | `includes(["a", "b"], "b")` | true |
| `find()` | list, condition | the first item that matches | `find([5, 12, 30], current > 10)` | 12 |
| `findIndex()` | list, condition | Number: its index, or -1 | `findIndex([5, 12, 30], current > 10)` | 1 |
| `filter()` | list, condition | List of the items that match | `filter([5, 12, 30], current > 10)` | 12, 30 |
| `some()` | list, expression | Checkbox: any item matches | `some([5, 12, 30], current > 25)` | true |
| `every()` | list, condition | Checkbox: every item matches | `every([5, 12, 30], current > 1)` | true |
| `map()` | list, expression | List of each item's result | `map([1, 2, 3], current * 10)` | 10, 20, 30 |
| `flat()` | list, list2?, ... | List with inner lists opened | `flat([[1, 2], [3]])` | 1, 2, 3 |
| `count()` | list, condition | Number of matching items, or of all items | `count([1, 2, 3, 4], current > 2)` | 2 |

### Special

| Function | Takes | Gives | Example | Result |
| --- | --- | --- | --- | --- |
| `id()` | value? | Text: the ID of the page or person given, or of the row the formula is on | `id()` | the row's ID |

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Formula length | 4,096 characters | A longer formula shows "The formula is too long." |
| `round()` precision | 0 to 12 decimal places | |
| `formatNumber()` precision | 0 to 12 decimal places | |
| `formatNumber()` format | `"commas"` | |
| Working-day calendar | `"KR"`, years 2024–2027 | |
| `addWorkdays()` count | at most 3,660 either way | More gives an empty value. |
| Date units | years, quarters, months, weeks, days, hours, minutes, seconds, milliseconds | |
| List positions | from 0 | `at()`, `slice()`, `splice()`, `findIndex()`, `index` |

## For agents and the API

An agent adds a formula property with `notion-create-database` or
`notion-update-data-source`, writing it in the schema as
`"Total" FORMULA('prop("Price") * prop("Quantity")')`; to an existing data
source, `notion-update-data-source` takes it in its `statements` as
`ADD COLUMN "Total" FORMULA('prop("Price") * prop("Quantity")')`. It reads
the values with `notion-query-data-sources` in rows mode,
`{"data": {"mode": "rows", "data_source_url": "collection://…"}}`, or in view
mode for a saved view's rows. `notion-fetch` and `leafen_fetch` on a row give
its formulas' values too, the same values rows mode gives. A SQL query through
`notion-query-data-sources` cannot read a formula column; its error says so
and names the columns it can read. The server evaluates
formulas with the same functions as the editor, so a value read through MCP or
the API is the value the cell shows. The tools are described in the
[MCP reference](../../MCP.md#notion-compatible-tool-surface).

## Differences from Notion

- Leafen adds `nextWorkday()` and `addWorkdays()`, which count working days on
  a public holiday calendar.
- `startsWith()` and `endsWith()` work, though the editor's list does not show
  them.
- A formula that cuts an emoji in half writes the half as the replacement
  character (�).
- An agent's `notion-fetch` of a row gives each formula's value; Notion's
  names it by a `formulaResult://` address that its fetch does not open.
- When a function call fails, the problem names the function, for example
  "round() could not run."
- The browser and the server compute the same digits, so a cell, a filter and
  the API always agree on a formula's value.
- When rows are sorted by a formula whose value is not a number (NaN), those
  rows go after the numbers, in either direction.

## Related chapters

- [Properties](properties.md)
- [Relations and rollups](relations-and-rollups.md)
- [Views](views.md), for filtering and sorting by a formula
- [Automations and buttons](automations-and-buttons.md), whose actions take formulas
