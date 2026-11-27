import { parseFormulaTokens, type FormulaNode } from "./formulaAst";
import { tokenizeFormula } from "./formulaCore";
import { FORMULA_SIGNATURES, type FormulaCatalogEntry } from "./formulaFunctionCatalog";

// Right after a value, or after its ".", a function is chosen to be called on
// that value, and Notion's editor writes it so (seen 2026-11-28 in Chrome,
// two tabs at different times): "1" and and() write "1.and()" with the
// caret in its parentheses, abs() "1.abs()" with it after them, as nothing
// more goes in. Its help is titled with its first value before the name, the rest
// in the parentheses ("boolean.and(boolean, ...)", "number.abs()",
// "value.empty()"), and its examples' calls of it are written on their first
// value, one that is an operation in parentheses ("true.and(true, true)",
// "(3 > 2).and(2 < 3)", "-10.abs()"), what else they write as it is
// ("5 + 10", parseDate("2023-07-10T17:35Z").minute()), an example calling it
// with nothing left out (empty(), id()). Their results are those of the
// examples as written elsewhere ("-10.abs() = 10"). A few are Notion's own,
// written out below. After a value and a space, or with a name typed right after it, it
// is the function as written elsewhere.

/** Where a function is chosen to be called on the value before it: right after it, or after its ".". */
export type FormulaMethodPlace = "after" | "dotted";

const METHOD_TITLES: Readonly<Record<string, string>> = {
  // Notion's keeps this one as the function.
  dateRange: "dateRange(start, end)",
};

// Notion's own examples where they are not its others written on their first value.
const METHOD_EXAMPLES: Readonly<Record<string, readonly string[]>> = {
  if: ["true.if(1, 2)", "false.if(1, 2)"],
  min: ["[1, 2, 3].min()"],
  max: ["[1, 2, 3].max()"],
  sum: ["[1, 2, 3].sum()", "[1, 2, 3].sum(4, 5)"],
  median: ["[1, 2, 4].median()", "[1, 2, 3].median(4)"],
  mean: ["[1, 2, 3].mean()", "[1, 2, 3].mean(4, 5)"],
  round: ["round(1.2345, 2)"],
  first: ["[1, 2, 3].first()", "[].first()"],
  style: ['"Notion".style("b", "u")', '"Notion".style("blue", "blue_background")'],
  dateStart: ['prop("Date Range").dateStart()', 'prop("Date Range").dateStart().dateBetween(prop("Date Range").dateEnd(), "days")'],
  dateEnd: ['prop("Date Range").dateEnd()', 'prop("Date Range").dateEnd().dateBetween(prop("Date Range").dateStart(), "days")'],
};

// What a function takes besides the value it is called on.
const methodRest = (key: string) => (FORMULA_SIGNATURES[key] ?? "").split(", ").slice(1);

/** The title of a function's help, called on the value before it. */
export function formulaMethodTitle(key: string) {
  const [receiver = "value"] = (FORMULA_SIGNATURES[key] ?? "").split(", ");
  return METHOD_TITLES[key] ?? `${receiver.replace(/\?$/, "") || "value"}.${key}(${methodRest(key).join(", ")})`;
}

/** A function's examples called on their first value (`text`), each with what works out its result (`run`). */
export function formulaMethodExamples(entry: FormulaCatalogEntry): { text: string; run: string }[] {
  const own = METHOD_EXAMPLES[entry.key];
  if (own) return own.map((text) => ({ text, run: text }));
  return entry.examples.flatMap((run) => {
    const text = formulaMethodExample(run, entry.key);
    return text === null ? [] : [{ text, run }];
  });
}

/** What choosing a function writes there, the cursor ("|") in its parentheses while it takes more. */
export function formulaMethodInsert(key: string, place: FormulaMethodPlace) {
  return `${place === "after" ? "." : ""}${key}(${methodRest(key).length ? "|)" : ")|"}`;
}

/** An example with its calls of `key` written as calls on their first value; null where it calls it with nothing. */
export function formulaMethodExample(example: string, key: string): string | null {
  let tree: FormulaNode;
  try {
    tree = parseFormulaTokens(tokenizeFormula(example));
  } catch {
    return example;
  }
  let bare = false;
  // The text from `start` to `end` with what each node inside it writes in its place.
  const written = (start: number, end: number, nodes: readonly FormulaNode[]): string => {
    let text = "", at = start;
    for (const node of nodes) {
      if (node.start === undefined || node.end === undefined || node.start < at || node.end > end) continue;
      text += example.slice(at, node.start) + write(node);
      at = node.end;
    }
    return text + example.slice(at, end);
  };
  const write = (node: FormulaNode): string => {
    const start = node.start ?? 0, end = node.end ?? example.length;
    if (node.kind === "call" && node.name === key && !node.args.length) bare = true;
    if (node.kind === "call" && node.name === key && node.args.length) {
      const [first, ...rest] = node.args;
      const operation = first.kind === "binary" || first.kind === "ternary";
      const receiver = write(first);
      const inside = rest.length ? written(rest[0].start ?? 0, rest[rest.length - 1].end ?? 0, rest) : "";
      return `${operation ? `(${receiver})` : receiver}.${key}(${inside})`;
    }
    return written(start, end, formulaNodeParts(node));
  };
  const text = write(tree);
  return bare ? null : text;
}

// A node's parts in the order they are written.
function formulaNodeParts(node: FormulaNode): FormulaNode[] {
  switch (node.kind) {
    case "list": return node.items;
    case "call": return node.args;
    case "method": return [node.receiver, ...node.args];
    case "called": return [node.callee, ...node.args];
    case "unary": return [node.operand];
    case "binary": return [node.left, node.right];
    case "ternary": return [node.condition, node.whenTrue, node.whenFalse];
    default: return [];
  }
}
