// What the formula editor offers beside the formula, as Notion's does: the
// built-in operators and the functions by kind, each with what it gives back
// and examples the editor evaluates to show their results. The descriptions
// live in the formulaEditor locale, by `key`. Groups and entries are in
// Notion's order (seen 2026-10-08), the examples Notion's (seen 2026-11-04).

import { FormulaDate, type FormulaValue } from "./formulaSyntax";

export type FormulaCatalogGroup = "builtins" | "general" | "text" | "number" | "date" | "people" | "list" | "special";

export type FormulaReturnType = "number" | "text" | "boolean" | "date" | "list" | "person" | "any";

/** A kind of value the editor tells apart where the cursor follows one; "numbers" is a list of numbers. */
export type FormulaValueKind = "number" | "text" | "boolean" | "date" | "person" | "page" | "list" | "numbers";

export type FormulaCatalogEntry = {
  // key names the entry's description; for a function it is its name.
  key: string;
  label: string;
  group: FormulaCatalogGroup;
  returns: FormulaReturnType;
  // insert is what choosing the entry writes, with "|" where the cursor goes.
  insert: string;
  examples: readonly string[];
  // after: the values the entry is offered right after — an operator on
  // them, or a function taking them first. Notion offers the others only
  // where a value is expected.
  after?: readonly FormulaValueKind[];
  // start: an operator offered only where an expression starts (not, true, false).
  start?: boolean;
  // names: let() and lets(), which Notion offers only where a value is expected.
  names?: boolean;
  // unlisted: known to the evaluator but not in Notion's list.
  unlisted?: boolean;
  // bare: examples Notion's help shows without results (id(), which reads the
  // page it runs on).
  bare?: boolean;
};

export const FORMULA_CATALOG_GROUPS: readonly FormulaCatalogGroup[] = [
  "builtins", "general", "text", "number", "date", "people", "list", "special",
];

const ANY: readonly FormulaValueKind[] = ["number", "text", "boolean", "date", "person", "page", "list"];
const NUMBER: readonly FormulaValueKind[] = ["number"];
const TEXT: readonly FormulaValueKind[] = ["text"];
const DATE: readonly FormulaValueKind[] = ["date"];
const LIST: readonly FormulaValueKind[] = ["list"];

type Extra = Pick<FormulaCatalogEntry, "after" | "start" | "names" | "unlisted" | "bare">;

const operator = (key: string, label: string, returns: FormulaReturnType, examples: string[], extra: Extra,
  insert = ` ${label} |`): FormulaCatalogEntry => ({ key, label, group: "builtins", returns, insert, examples, ...extra });

// The functions that take nothing: chosen, the cursor goes after their
// "()", the others' between, as Notion's. Measured twice in the worker
// Chrome, in different tabs and at different times on 2026-11-15, no script
// in the page before the keys: the start of a name typed, the suggestion
// chosen by Enter, one letter typed — today()x, now()x and pi()x, while
// empty(x) and split(x) kept it between.
const TAKES_NOTHING = new Set(["pi", "e", "now", "today"]);

const call = (group: FormulaCatalogGroup, returns: FormulaReturnType, name: string, after: readonly FormulaValueKind[] | Extra,
  ...examples: string[]): FormulaCatalogEntry => ({
  key: name, label: `${name}()`, group, returns, insert: TAKES_NOTHING.has(name) ? `${name}()|` : `${name}(|)`, examples,
  ...(Array.isArray(after) ? { after } : after),
});

const date = (text: string) => `parseDate("${text}")`;
const none: Extra = {};

export const FORMULA_CATALOG: readonly FormulaCatalogEntry[] = [
  operator("times", "*", "number", ["3 * 2", "multiply(-1, 2)"], { after: NUMBER }),
  operator("divide", "/", "number", ["3 / 2", "divide(-1, 2)"], { after: NUMBER }),
  operator("plus", "+", "number", ["3 + 2", "add(-1, 2)"], { after: ["number", "text"] }),
  operator("minus", "-", "number", ["3 - 2", "subtract(-1, 2)"], { after: NUMBER }),
  operator("greater", ">", "boolean", ["3 > 2", `now() > ${date("2040-01-01")}`], { after: ["number", "text", "date"] }),
  operator("greaterOrEqual", ">=", "boolean", ["3 >= 2", `now() >= ${date("2040-01-01")}`], { after: ["number", "text", "date"] }),
  operator("less", "<", "boolean", ["3 < 2", `now() < ${date("2040-01-01")}`], { after: ["number", "text", "date"] }),
  operator("lessOrEqual", "<=", "boolean", ["3 <= 2", `now() <= ${date("2040-01-01")}`], { after: ["number", "text", "date"] }),
  operator("equal", "==", "boolean", ["3 == 2", 'equal("A", "A")'], { after: ANY }),
  operator("notEqual", "!=", "boolean", ["3 != 2", 'unequal("A", "A")'], { after: ANY }),
  operator("and", "and", "boolean", ["3 > 2 and 2 > 3", 'prop("Naughty") && prop("Nice")'], { after: ["boolean"] }),
  operator("or", "or", "boolean", ["3 > 2 or 2 > 3", 'prop("Naughty") || prop("Nice")'], { after: ["boolean"] }),
  operator("not", "not", "boolean", ["not true", "not (2 > 3)", '!prop("Checked")'], { start: true }, "not |"),
  operator("true", "true", "boolean", ["true", 'prop("Checked") == true'], { start: true }, "true|"),
  operator("false", "false", "boolean", ["false", 'prop("Checked") == false'], { start: true }, "false|"),
  operator("remainder", "%", "number", ["10 % 3"], { unlisted: true }),
  operator("power", "^", "number", ["2 ^ 3"], { unlisted: true }),
  operator("condition", "? :", "any", ['3 > 2 ? "yes" : "no"'], { unlisted: true }, " ? | : "),

  call("general", "any", "if", ["boolean"], "if(true, 1, 2)", "if(false, 1, 2)", 'prop("Checked") == true ? "Complete" : "Incomplete"'),
  call("general", "any", "ifs", ["boolean"], "ifs(true, 1, true, 2, 3)", "ifs(false, 1, false, 2, 3)"),
  call("general", "boolean", "and", ANY, "and(true, true, true)", "and(3 > 2, 2 < 3)"),
  call("general", "boolean", "or", ANY, "or(true, false, false)", "or(3 > 2, 2 < 3)"),
  call("general", "boolean", "not", ANY, "not(true)", "not(2 > 3)"),
  call("general", "boolean", "empty", ANY, "empty()", "empty(0)", "empty([])"),
  call("general", "number", "length", ["text", "list"], 'length("hello")', "length([1, 2, 3])"),
  call("general", "text", "format", ANY, "format(1234)", "format(now())"),
  call("general", "boolean", "equal", ANY, "equal(1, 1)", '"a" == "b"'),
  call("general", "boolean", "unequal", ANY, "unequal(1, 2)", '"a" != "a"'),
  call("general", "any", "let", { names: true }, 'let(name, "Doug Engelbart", "Hello, " + name + "!")', "let(radius, 4, round(pi() * radius ^ 2))"),
  call("general", "any", "lets", { names: true }, 'lets(a, "Hello", b, "world", a + " " + b)', "lets(base, 3, height, 8, base * height / 2)"),

  call("text", "text", "substring", TEXT, 'substring("Notion", 0, 3)', 'substring("Notion", 3)'),
  call("text", "boolean", "contains", TEXT, 'contains("Notion", "ot")'),
  call("text", "boolean", "test", TEXT, 'test("Notion", "Not")', 'test("Notion", "\\d")'),
  call("text", "list", "match", TEXT, 'match("Notion Notion", "Not")', 'match("Notion 123 Notion 456", "\\d+")'),
  call("text", "text", "replace", TEXT, 'replace("Notion Notion", "N", "M")'),
  call("text", "text", "replaceAll", TEXT, 'replaceAll("Notion Notion", "N", "M")', 'replaceAll("Notion 123", "\\d", "")'),
  call("text", "text", "lower", TEXT, 'lower("NOTION")'),
  call("text", "text", "upper", TEXT, 'upper("notion")'),
  call("text", "text", "trim", TEXT, 'trim("  notion  ")'),
  call("text", "text", "repeat", TEXT, 'repeat("0", 4)', 'repeat("~=", 10).style("blue")'),
  call("text", "text", "padStart", TEXT, 'padStart("A", 3, ".")', "padStart(123, 5, 0)"),
  call("text", "text", "padEnd", TEXT, 'padEnd("A", 3, ".")', "padEnd(123, 5, 0)"),
  call("text", "text", "link", TEXT, 'link("Notion", "https://notion.so")'),
  call("text", "text", "style", TEXT, 'style("Notion", "b", "u")', 'style("Notion", "blue", "gray_background")'),
  call("text", "text", "unstyle", TEXT, 'unstyle("Text")', 'unstyle("Text", "blue")'),
  call("text", "list", "concat", LIST, "concat([1, 2], [3, 4])", 'concat(["a", "b"], ["c", "d"])'),
  call("text", "text", "join", LIST, 'join(["a", "b", "c"], ", ")', 'join(["dog", "go"], "")'),
  call("text", "list", "split", TEXT, 'split("apple,pear,orange", ",")'),
  call("text", "boolean", "startsWith", { after: TEXT, unlisted: true }, 'startsWith("Notion", "No")'),
  call("text", "boolean", "endsWith", { after: TEXT, unlisted: true }, 'endsWith("Notion", "on")'),

  call("number", "text", "formatNumber", NUMBER, 'formatNumber(12.345, "usd")', 'formatNumber(12.345, "usd", 0)',
    'formatNumber(1234567890, "commas")', 'formatNumber(1234567890, "humanize")'),
  call("number", "number", "add", NUMBER, "add(5, 10)", "5 + 10"),
  call("number", "number", "subtract", NUMBER, "subtract(5, 10)", "5 - 10"),
  call("number", "number", "multiply", NUMBER, "multiply(5, 10)", "5 * 10"),
  call("number", "number", "mod", NUMBER, "mod(5, 10)", "5 % 10"),
  call("number", "number", "pow", NUMBER, "pow(5, 10)", "5 ^ 10"),
  call("number", "number", "divide", NUMBER, "divide(5, 10)", "5 / 10"),
  call("number", "number", "min", ["number", "numbers"], "min(1, 2, 3)", "min([1, 2, 3])"),
  call("number", "number", "max", ["number", "numbers"], "max(1, 2, 3)", "max([1, 2, 3])"),
  call("number", "number", "sum", ["number", "numbers"], "sum(1, 2, 3)", "sum([1, 2, 3], 4, 5)"),
  call("number", "number", "median", ["number", "numbers"], "median(1, 2, 4)", "median([1, 2, 3], 4)"),
  call("number", "number", "mean", ["number", "numbers"], "mean(1, 2, 3)", "mean([1, 2, 3], 4, 5)"),
  call("number", "number", "abs", NUMBER, "abs(10)", "abs(-10)"),
  call("number", "number", "round", NUMBER, "round(1.2345)"),
  call("number", "number", "ceil", NUMBER, "ceil(0.4)", "ceil(-0.6)"),
  call("number", "number", "floor", NUMBER, "floor(0.4)", "floor(-0.6)"),
  call("number", "number", "sqrt", NUMBER, "sqrt(4)", "sqrt(7)"),
  call("number", "number", "cbrt", NUMBER, "cbrt(9)", "cbrt(64)"),
  call("number", "number", "exp", NUMBER, "exp(1)", "exp(-1)"),
  call("number", "number", "ln", NUMBER, "ln(2.718281828459045)", "ln(10)"),
  call("number", "number", "log10", NUMBER, "log10(10)", "log10(100000)"),
  call("number", "number", "log2", NUMBER, "log2(4)", "log2(1024)"),
  call("number", "number", "sign", NUMBER, "sign(-10)", "sign(10)"),
  call("number", "number", "pi", none, "pi()"),
  call("number", "number", "e", none, "e()"),
  call("number", "number", "toNumber", ["number", "text", "boolean", "date"], 'toNumber("2")', "toNumber(now())", "toNumber(true)"),

  call("date", "date", "now", none, "now()"),
  call("date", "date", "today", none, "today()"),
  call("date", "number", "minute", DATE, `minute(${date("2023-07-10T17:35Z")})`),
  call("date", "number", "hour", DATE, `hour(${date("2023-07-10T17:35Z")})`),
  call("date", "number", "day", DATE, `day(${date("2023-07-10T17:35Z")})`),
  call("date", "number", "date", DATE, `date(${date("2023-07-10T17:35Z")})`),
  call("date", "number", "week", DATE, `week(${date("2023-01-02")})`),
  call("date", "number", "month", DATE, `month(${date("2023-07-10T17:35Z")})`),
  call("date", "number", "year", DATE, "year(now())"),
  call("date", "date", "dateAdd", DATE, 'dateAdd(now(), 1, "days")', 'dateAdd(now(), 2, "months")', 'dateAdd(now(), 3, "years")'),
  call("date", "date", "dateSubtract", DATE, 'dateSubtract(now(), 1, "days")', 'dateSubtract(now(), 2, "months")',
    'dateSubtract(now(), 3, "years")'),
  call("date", "number", "dateBetween", DATE, `dateBetween(now(), ${date("2022-09-07")}, "days")`,
    `dateBetween(${date("2030-01-01")}, now(), "years")`),
  call("date", "date", "dateRange", DATE, 'dateRange(prop("Start Date"), prop("End Date"))'),
  call("date", "date", "dateStart", DATE, 'dateStart(prop("Date Range"))',
    'dateBetween(dateStart(prop("Date Range")), dateEnd(prop("Date Range")), "days")'),
  call("date", "date", "dateEnd", DATE, 'dateEnd(prop("Date range"))',
    'dateBetween(dateEnd(prop("Date Range")), dateStart(prop("Date Range")), "days")'),
  call("date", "number", "timestamp", DATE, "timestamp(now())"),
  call("date", "date", "fromTimestamp", NUMBER, "fromTimestamp(1689024900000)"),
  call("date", "text", "formatDate", DATE, 'formatDate(now(), "MMMM D, Y")', 'formatDate(now(), "YYYY-MM-DD HH:mm")',
    'formatDate(now(), "h:mm A", "UTC+1")'),
  call("date", "date", "parseDate", TEXT, date("2022-01-01"), date("2022-01-01T00:00Z")),
  // Leafen's, beside Notion's: working days on a calendar of public holidays.
  call("date", "date", "nextWorkday", DATE, `nextWorkday(${date("2026-09-26")})`),
  call("date", "date", "addWorkdays", DATE, `addWorkdays(${date("2026-09-23")}, 2)`),

  call("people", "text", "name", ["person"], 'name(prop("Created By"))', 'prop("Pioneers").map(name(current)).join(", ")'),
  call("people", "text", "email", ["person"], 'email(prop("Created By"))', 'prop("People").map(email(current)).join(", ")'),

  call("list", "any", "at", LIST, "at([1, 2, 3], 1)"),
  call("list", "any", "first", LIST, "first([1, 2, 3])"),
  call("list", "any", "last", LIST, "last([1, 2, 3])"),
  call("list", "list", "slice", LIST, "slice([1, 2, 3], 1, 2)", 'slice(["a", "b", "c"], 1)'),
  call("list", "list", "splice", LIST, "splice([1, 2, 3], 1, 1)", 'splice(["a", "b", "c"], 0, 1, "d")'),
  call("list", "list", "sort", LIST, "sort([3, 1, 2])", "sort([1, 2, 3]).reverse()",
    'sort(["Yash", "Stephen", "Paul", "Simon", "Ryo"])', 'sort(prop("Task"), current.Date)'),
  call("list", "list", "reverse", LIST, 'reverse(["green", "eggs", "ham"])'),
  call("list", "list", "unique", LIST, "unique([1, 1, 2])"),
  call("list", "boolean", "includes", LIST, 'includes(["a", "b", "c"], "b")', "includes([1, 2, 3], 4)"),
  call("list", "any", "find", LIST, 'find(["a", "b", "c"], current == "b")', "find([1, 2, 3], current > 100)"),
  call("list", "number", "findIndex", LIST, 'findIndex(["a", "b", "c"], current == "b")', "findIndex([1, 2, 3], current > 100)"),
  call("list", "list", "filter", LIST, "filter([1, 2, 3], current > 1)", 'filter(["a", "b", "c"], current == "a")'),
  call("list", "boolean", "some", LIST, "some([1, 2, 3], current == 2)", 'some(["a", "b", "c"], current.length > 2)'),
  call("list", "boolean", "every", LIST, "every([1, 2, 3], current > 0)", 'every(["a", "b", "c"], current == "b")'),
  call("list", "list", "map", LIST, "map([1, 2, 3], current + 1)", "map([1, 2, 3], current + index)"),
  call("list", "list", "flat", LIST, "flat([1, 2, 3])", "flat([[1, 2], [3, 4]])"),
  call("list", "number", "count", LIST, "count([1, 2, 3, 4], current % 2 == 0)", 'count(["a", "b", "c"], current == "d")'),

  call("special", "text", "id", { after: ["person", "page"], bare: true }, "id()", 'id(prop("Relation").first())',
    'id(prop("Person").first())'),
];

// The properties the examples name, with the values Notion's help shows
// their results for (seen 2026-11-04); an example naming another shows none.
export const FORMULA_EXAMPLE_SAMPLES: Readonly<Record<string, FormulaValue>> = {
  "Start Date": new FormulaDate("2022-09-07"),
  "End Date": new FormulaDate("2023-09-07"),
  "Date Range": new FormulaDate("2022-09-07/2023-09-07"),
  "Date range": new FormulaDate("2022-09-07/2023-09-07"),
  Pioneers: [
    { kind: "person", id: "grace-hopper", name: "Grace Hopper" },
    { kind: "person", id: "ada-lovelace", name: "Ada Lovelace" },
  ],
};

// What each function takes, as the editor's help titles a chosen one, in
// Notion's words (seen 2026-11-04); the last four are Leafen's own.
export const FORMULA_SIGNATURES: Readonly<Record<string, string>> = {
  if: "condition, ifTrue, ifFalse", ifs: "condition, ifTrue, condition2, ifTrue2, ..., else",
  and: "boolean, boolean, ...", or: "boolean, boolean, ...", not: "boolean", empty: "value?", length: "value",
  format: "value", equal: "value, value", unequal: "value, value",
  let: "variable, value, expression", lets: "variable, value, variable2, value2, ..., expression",
  substring: "text, startIndex, endIndex?", contains: "value, search", test: "text, pattern", match: "text, pattern",
  replace: "text, pattern, replacement?", replaceAll: "text, pattern, replacement?", lower: "text", upper: "text",
  trim: "text", repeat: "text, count", padStart: "text, targetLength, padString",
  padEnd: "text, targetLength, padString", link: "text, url", style: "value, styles", unstyle: "value, styles?",
  concat: "list, list2, ...", join: "list, joiner", split: "text, separator",
  formatNumber: "value, format?, precision?", add: "number, number", subtract: "number, number",
  multiply: "number, number", mod: "number, number", pow: "number, number", divide: "number, number",
  min: "list", max: "list", sum: "list", median: "list", mean: "list", abs: "number", round: "value, precision",
  ceil: "number", floor: "number", sqrt: "number", cbrt: "number", exp: "number", ln: "number", log10: "number",
  log2: "number", sign: "number", pi: "", e: "", toNumber: "value",
  now: "", today: "", minute: "date", hour: "date", day: "date", date: "date", week: "date", month: "date",
  year: "date", dateAdd: "date, num, unit", dateSubtract: "date, num, unit", dateBetween: "date, date, unit",
  dateRange: "start, end", dateStart: "dateRange", dateEnd: "dateRange", timestamp: "date",
  fromTimestamp: "timestamp", formatDate: "date, format, timezone?", parseDate: "dateText",
  name: "person", email: "person",
  at: "list, index", first: "list", last: "list", slice: "list, startIndex, endIndex?",
  splice: "list, startIndex, deleteCount?, ...", sort: "list, expression?", reverse: "list", unique: "list",
  includes: "list, search", find: "list, condition", findIndex: "list, condition", filter: "list, condition",
  some: "list, expression", every: "list, condition", map: "list, expression", flat: "list, list2?, ...",
  count: "list, condition", id: "value?",
  startsWith: "text, search", endsWith: "text, search", nextWorkday: "date, calendar",
  addWorkdays: "date, count, calendar",
};
