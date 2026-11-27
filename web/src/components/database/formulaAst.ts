import {
  FORMULA_NESTING_COST, FORMULA_NESTING_ROOM, FormulaSyntaxError, formulaInvalidCharacters, formulaNameCalled, formulaOnlyComments,
  type FormulaToken, type FormulaTokenType,
} from "./formulaSyntax";

// A formula is read into a tree before it is evaluated, so if(), ifs(), the
// ternary and the boolean operators evaluate only the branches they take.
// The server reads the same grammar (database_formula_syntax.go). What keeps
// a formula from being read is a FormulaSyntaxError at the token the reading
// stopped at, or the last token when the formula ended first, as Notion's
// editor places it.

// A node keeps where it is written, from its first token's start to its
// last token's end, for the editor to place a problem with it.
export type FormulaNode = (
  | { kind: "number"; value: number }
  | { kind: "string"; value: string }
  | { kind: "identifier"; name: string }
  | { kind: "list"; items: FormulaNode[] }
  | { kind: "call"; name: string; args: FormulaNode[] }
  // A method's nameEnd is where its name ends, for a problem with the name.
  | { kind: "method"; name: string; receiver: FormulaNode; args: FormulaNode[]; nameEnd?: number }
  // A value followed by parentheses, which the editor reads as a call of that value (parseFormulaTokensRecovering).
  | { kind: "called"; callee: FormulaNode; args: FormulaNode[] }
  | { kind: "unary"; operator: "-" | "!"; operand: FormulaNode }
  | { kind: "binary"; operator: string; left: FormulaNode; right: FormulaNode }
  | { kind: "ternary"; condition: FormulaNode; whenTrue: FormulaNode; whenFalse: FormulaNode }
  // missingAt is where a value the editor's reading found missing was looked for (parseFormulaTokensRecovering).
) & { start?: number; end?: number; missingAt?: number };

export const EMPTY_FORMULA_NODE: FormulaNode = { kind: "string", value: "" };

// The tokens that may follow a closing parenthesis, bracket or ":" (and are
// that token too), where a reading that skips what it can't read stops: an
// opening parenthesis among them, a call of the value before it.
const FOLLOWS_CLOSE = new Set<FormulaTokenType>(["comma", "colon", "question", "dot", "operator", "paren"]);
const followsClose = (token: FormulaToken) => FOLLOWS_CLOSE.has(token.type) || (token.type === "bracket" && token.value === "]");

export function parseFormulaTokens(tokens: FormulaToken[]): FormulaNode {
  // A character no token starts with keeps it from being read, as Notion's (formulaSyntax.ts).
  const [invalid] = formulaInvalidCharacters(tokens);
  if (invalid) throw new FormulaSyntaxError("Formula has a character it can't read", invalid.start, invalid.end);
  // An empty formula is empty text; a value missing anywhere else is an
  // error, as Notion reads it, and so is a formula of comments alone.
  if (tokens.length === 0) {
    if (formulaOnlyComments(tokens)) throw new FormulaSyntaxError("Formula value is missing", undefined, undefined, undefined, true);
    return EMPTY_FORMULA_NODE;
  }
  const syntax = new FormulaSyntax(tokens);
  const node = syntax.expression();
  if (!syntax.done()) throw syntax.error("Unexpected formula token");
  return node;
}

// What keeps a formula from being read, where the editor reads on past it; `ended` when the formula ended first.
export type FormulaFault = { message: string; expected?: string; start: number; end: number; ended?: true };
export type FormulaRecovered = { node: FormulaNode; faults: FormulaFault[]; skipped: [number, number][] };

/**
 * A formula read for the editor, on past what keeps it from being read, as
 * Notion's editor reads it, so the problems of what it could read are named
 * beside the syntax's own (seen 2026-11-15, 1440 dark): a value missing is an
 * empty one of a type not known, at the token it was looked for at, which is
 * then read on ("+ foo" is foo's problem and "예상 표현식입니다. [0,1]", and
 * "abs(1," gives abs() a second value placed [0,1]); a call, list, parenthesis
 * or condition not closed skips what it can't read up to a token that may come
 * after its close, and takes its close when that is the one ("abs(foo bar)" is
 * [8,9] and names no bar; "abs(foo bar, 1)" gives abs() 1 too), else ends where
 * what it read ends ("abs(foo bar" is abs() [0,7]); a value followed by
 * parentheses is a call of that value, read with its values ("abs(1)(foo)"
 * names abs(1)'s type [0,11], then foo; "abs(1 abs(2))" skips the inner abs
 * and calls abs(1 with 2, seen 2026-11-15); a method's missing name is named
 * where it is looked for, after a missing value too (".5" is [0,1] and [1,2]);
 * input past the formula's end is left unread ("abs(foo) bar" names foo, not bar). Each fault is marked at the
 * first letter of the token it is found at, or at the formula's last letter
 * when the formula ends first, one at a place. `length` is the formula's.
 * The values read where faults are skipped are given as `skipped`, from and
 * to. Too deep a formula still throws.
 */
export function parseFormulaTokensRecovering(tokens: FormulaToken[], length: number): FormulaRecovered {
  const recovered: FormulaRecovered = { node: EMPTY_FORMULA_NODE, faults: [], skipped: [] };
  if (tokens.length === 0) {
    if (formulaOnlyComments(tokens)) recovered.faults.push({ message: "Formula value is missing", start: Math.max(0, length - 1), end: length });
    return recovered;
  }
  const syntax = new FormulaSyntax(tokens, { ...recovered, length });
  recovered.node = syntax.expression();
  syntax.rest();
  return recovered;
}

// A missing value, where the editor reads on: empty, its type not known,
// placed [0,1] as Notion places it ("abs(1," names abs()'s second value [0,1]).
const missingValue = (at: number): FormulaNode => ({ kind: "identifier", name: "", start: 0, end: 1, missingAt: at });

class FormulaSyntax {
  private index = 0;
  private depth = 0;
  // Where a value left open ends: the end of what it read before the skip (close).
  private openEnd?: number;

  constructor(private readonly tokens: FormulaToken[], private readonly recovery?: FormulaRecovered & { length: number }) {}

  // fault names what keeps the formula from being read where the reading
  // stands, or throws it when the reading doesn't go on.
  private fault(message: string, expected?: string) {
    if (!this.recovery) throw this.error(message, expected);
    const token = this.tokens[this.index];
    const length = this.recovery.length;
    const start = token ? token.start ?? 0 : Math.max(0, length - 1);
    if (this.recovery.faults.some((fault) => fault.start === start)) return;
    this.recovery.faults.push({ message, expected, start, end: token ? start + 1 : length, ...(token ? {} : { ended: true as const }) });
  }

  // close takes the token a value is closed by, or names it missing and skips
  // what it can't read up to a token that may follow the close, taking it when
  // it is the close; whether it then stands at a comma, for a list or call
  // to go on.
  private close(type: FormulaTokenType, value: string, message: string): "closed" | "comma" | "open" {
    if (this.match(type, value)) return "closed";
    this.fault(message, value);
    const from = this.index;
    // What is opened in what it skips is skipped up to its own close.
    for (let open = 0; !this.done() && (open > 0 || !followsClose(this.tokens[this.index])); this.index += 1) {
      const { type, value } = this.tokens[this.index];
      if (type === "paren" || type === "bracket") open += value === "(" || value === "[" ? 1 : -1;
    }
    if (this.index > from) this.recovery?.skipped.push([this.tokens[from].start ?? 0, this.tokens[this.index - 1].end ?? 0]);
    if (this.match(type, value)) return "closed";
    if (this.tokens[this.index]?.type === "comma") return "comma";
    this.openEnd = this.tokens[from - 1]?.end;
    return "open";
  }

  // rest names input past the formula's end and leaves it unread.
  rest() {
    if (this.done()) return;
    this.fault("Unexpected formula token");
    this.recovery?.skipped.push([this.tokens[this.index].start ?? 0, this.tokens[this.tokens.length - 1].end ?? 0]);
  }

  done() {
    return this.index >= this.tokens.length;
  }

  error(message: string, expected?: string) {
    const token = this.tokens[this.index] ?? this.tokens[this.tokens.length - 1];
    return new FormulaSyntaxError(message, token?.start, token?.end, expected, this.done());
  }

  private match(type: FormulaTokenType, value?: string) {
    const token = this.tokens[this.index];
    if (!token || token.type !== type || (value !== undefined && token.value !== value)) return false;
    this.index += 1;
    return true;
  }

  // and/or/not written as words are operators unless a call's parenthesis
  // follows right after (formulaSyntax.ts formulaNameCalled).
  private keyword(value: string) {
    const token = this.tokens[this.index];
    if (!token || token.type !== "identifier" || token.value !== value) return false;
    if (formulaNameCalled(this.tokens, this.index)) return false;
    this.index += 1;
    return true;
  }

  // not written as a word is an operator only when a value may follow it;
  // alone it reads as a name, as Notion reads it.
  private notOperator() {
    const next = this.tokens[this.index + 1];
    if (!next || next.type === "comma" || next.type === "colon" || next.type === "question" ||
      (next.type === "paren" && next.value === ")") || (next.type === "bracket" && next.value === "]")) return false;
    return this.keyword("not");
  }

  // spanned marks a node with where it is written: from the token at
  // `from` to the last token read, or where a value it leaves open ends.
  private spanned(from: number, node: FormulaNode): FormulaNode {
    node.start = this.tokens[from]?.start;
    node.end = this.openEnd ?? this.tokens[this.index - 1]?.end;
    this.openEnd = undefined;
    return node;
  }

  // What is inside the node being read spends cost of the room until it is read (FORMULA_NESTING_ROOM).
  private nested<T>(cost: number, read: () => T): T {
    this.depth += cost;
    try {
      if (this.depth > FORMULA_NESTING_ROOM) throw this.error("Formula nesting is too deep");
      return read();
    } finally {
      this.depth -= cost;
    }
  }

  expression(): FormulaNode {
    return this.nested(FORMULA_NESTING_COST.expression, () => {
      const from = this.index;
      const condition = this.or();
      if (!this.match("question")) return condition;
      const whenTrue = this.expression();
      if (!this.recovery && !this.match("colon")) throw this.error("Formula condition is missing its ':'", ":");
      if (this.recovery) this.close("colon", ":", "Formula condition is missing its ':'");
      this.openEnd = undefined;
      const whenFalse = this.expression();
      return this.spanned(from, { kind: "ternary", condition, whenTrue, whenFalse });
    });
  }

  private or(): FormulaNode {
    const from = this.index;
    let left = this.and();
    while (this.match("operator", "||") || this.keyword("or")) {
      left = this.spanned(from, { kind: "binary", operator: "||", left, right: this.and() });
    }
    return left;
  }

  private and(): FormulaNode {
    const from = this.index;
    let left = this.equality();
    while (this.match("operator", "&&") || this.keyword("and")) {
      left = this.spanned(from, { kind: "binary", operator: "&&", left, right: this.equality() });
    }
    return left;
  }

  // once reads one operator of the level at most: the reading stops at a
  // second, where the formula is then not closed or not ended.
  private binaryLevel(next: () => FormulaNode, operators: string[], once = false): FormulaNode {
    const from = this.index;
    let left = next();
    while (true) {
      const operator = operators.find((candidate) => this.match("operator", candidate));
      if (!operator) return left;
      left = this.spanned(from, { kind: "binary", operator, left, right: next() });
      if (once) return left;
    }
  }

  // Comparisons don't chain, as Notion's reading takes them: "1 > 2 > 3",
  // "1 < 2 <= 3" and "true == false == true" stop at the second, "예상 입력
  // 끝입니다" there (seen 2026-11-28 through its API and in two Chrome tabs),
  // while "1 > 2 == true" and "(1 > 2) == true" read. The server reads them
  // the same (database_formula_comparison.go).
  private equality(): FormulaNode {
    return this.binaryLevel(() => this.comparison(), ["==", "!="], true);
  }

  private comparison(): FormulaNode {
    return this.binaryLevel(() => this.term(), [">", ">=", "<", "<="], true);
  }

  private term(): FormulaNode {
    return this.binaryLevel(() => this.factor(), ["+", "-"]);
  }

  private factor(): FormulaNode {
    return this.binaryLevel(() => this.power(), ["*", "/", "%"]);
  }

  private power(): FormulaNode {
    const from = this.index;
    const left = this.unary();
    if (!this.match("operator", "^")) return left;
    return this.nested(FORMULA_NESTING_COST.expression, () => this.spanned(from, { kind: "binary", operator: "^", left, right: this.power() }));
  }

  private unary(): FormulaNode {
    const from = this.index;
    let operator: "-" | "!" | undefined;
    if (this.match("operator", "-")) {
      const signed = this.signedNumber(from);
      if (signed) return signed;
      operator = "-";
    } else if (this.match("operator", "!") || this.notOperator()) operator = "!";
    if (!operator) return this.primary();
    const unaryOperator = operator;
    return this.nested(FORMULA_NESTING_COST.unary, () => this.spanned(from, { kind: "unary", operator: unaryOperator, operand: this.unary() }));
  }

  // A minus right before a number, in parentheses or not, is that number's
  // sign, which a method after it is called on ("-10.abs()" and
  // "-(10).abs()" are 10, "-1.5.round()" -1, "-10.add(1)" -9), and the method
  // is placed from the number ("-10.dateStart()" [1,13]); before anything
  // else a method is called first and the minus taken of what it gives
  // ("-prop("Price").abs()" and "-(1 + 2).abs()" are -3, "-(-(10)).abs()"
  // -10), as Notion reads them (seen 2026-11-28 through its API and in its
  // editor). The server reads them the same (database_formula_syntax.go).
  private signedNumber(from: number): FormulaNode | undefined {
    const operand = this.index;
    let depth = 0;
    while (this.tokens[operand + depth]?.type === "paren" && this.tokens[operand + depth].value === "(") depth += 1;
    const number = this.tokens[operand + depth];
    if (number?.type !== "number") return undefined;
    for (let closing = 1; closing <= depth; closing += 1) {
      const token = this.tokens[operand + depth + closing];
      if (token?.type !== "paren" || token.value !== ")") return undefined;
    }
    if (this.tokens[operand + depth * 2 + 1]?.type !== "dot") return undefined;
    return this.nested(FORMULA_NESTING_COST.unary, () => {
      this.index = operand + depth * 2 + 1;
      const sign: FormulaNode = { kind: "unary", operator: "-", operand: { kind: "number", value: Number(number.value), start: number.start, end: number.end } };
      return this.methods(this.spanned(from, sign), operand);
    });
  }

  private primary(): FormulaNode {
    let node: FormulaNode;
    const from = this.index;
    const token = this.tokens[this.index];
    if (token?.type === "number") {
      this.index += 1;
      node = { kind: "number", value: Number(token.value) };
    } else if (token?.type === "string") {
      this.index += 1;
      node = { kind: "string", value: token.value };
    } else if (this.match("bracket", "[")) {
      node = { kind: "list", items: this.sequence("bracket", "]") };
    } else if ((token?.value === "and" || token?.value === "or") && token.type === "identifier" &&
      this.tokens[this.index + 1]?.value === "(" && !formulaNameCalled(this.tokens, this.index)) {
      // and/or before a space and a parenthesis is the operator, which no value starts with (its left wanted).
      this.fault("Formula value is missing");
      node = missingValue(token.start ?? 0);
    } else if (token?.type === "identifier") {
      this.index += 1;
      // true and false are values, a parenthesis after them a call of the value ("true(1)" is boolean's [0,7]).
      node = token.value !== "true" && token.value !== "false" && this.match("paren", "(")
        ? { kind: "call", name: token.value, args: this.sequence("paren", ")") }
        : { kind: "identifier", name: token.value };
    } else if (this.match("paren", "(")) {
      node = this.expression();
      if (!this.recovery && !this.match("paren", ")")) throw this.error("Formula parentheses are not balanced", ")");
      if (this.recovery) this.close("paren", ")", "Formula parentheses are not balanced");
    } else {
      this.fault("Formula value is missing");
      node = missingValue(token?.start ?? this.recovery?.length ?? 0);
    }
    // A value in parentheses keeps its own place, without them, as Notion
    // names a problem in it (seen 2026-11-14: (1 - "a") + 2 is [1,8],
    // (round(2, 0.5)) [1,14]); a method on it starts at the parenthesis.
    if (token?.type !== "paren" && node.missingAt === undefined) node = this.spanned(from, node);
    else this.openEnd = undefined;
    return this.methods(node, from);
  }

  // methods reads the methods called on a value, each placed from `from`.
  private methods(value: FormulaNode, from: number): FormulaNode {
    let node = value;
    while (true) {
      if (this.recovery && this.match("paren", "(")) {
        node = this.spanned(from, { kind: "called", callee: node, args: this.sequence("paren", ")") });
        continue;
      }
      if (!this.match("dot")) return node;
      const method = this.tokens[this.index];
      // A point with no name after it is named at what follows, a number or text there taken in its place, and the
      // value read on ("1 .5 + foo" is [3,4] and names foo; "1. + foo" [3,4] at the "+"; "1 .(2)" calls it, seen 2026-11-15).
      if (method?.type !== "identifier") {
        this.fault("Formula method name is missing");
        if (!this.recovery) return node;
        if (method?.type === "number" || method?.type === "string") this.index += 1;
        continue;
      }
      this.index += 1;
      const args = this.match("paren", "(") ? this.sequence("paren", ")") : [];
      node = this.spanned(from, { kind: "method", name: method.value, receiver: node, args, nameEnd: method.end });
    }
  }

  private sequence(type: FormulaTokenType, closing: string): FormulaNode[] {
    return this.nested(type === "bracket" ? FORMULA_NESTING_COST.list : FORMULA_NESTING_COST.call, () => {
      const items: FormulaNode[] = [];
      if (this.match(type, closing)) return items;
      // Ended right after it opened, it has no values ("abs(" is abs() missing one, Notion's [0,4]).
      if (this.recovery && this.done()) {
        this.close(type, closing, "Formula list or call is not closed");
        return items;
      }
      while (true) {
        items.push(this.expression());
        if (this.match("comma")) continue;
        if (this.match(type, closing)) return items;
        const closed = this.close(type, closing, "Formula list or call is not closed");
        if (closed !== "comma") return items;
        this.index += 1;
      }
    });
  }
}
