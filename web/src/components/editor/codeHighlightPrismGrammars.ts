import "./prismManual";
import * as Prism from "prismjs";

// The Prism grammar Notion colors each language with (seen 2026-11-01:
// all 86 drew exactly as Prism's grammar reads them, bash as an older
// Prism's below), each loaded the first time a block shows it. A grammar a
// language extends loads before it; markup, CSS, C-like and JavaScript come
// with Prism itself. "plain text" and "ascii art" Notion leaves uncolored.
const PRISM_GRAMMAR: Readonly<Record<string, string>> = {
  abap: "abap", agda: "agda", arduino: "arduino", assembly: "nasm", bash: "bash", basic: "basic", bnf: "bnf", c: "c",
  "c#": "csharp", "c++": "cpp", clojure: "clojure", coffeescript: "coffeescript", coq: "coq", css: "css", dart: "dart",
  dhall: "dhall", diff: "diff", docker: "docker", ebnf: "ebnf", elixir: "elixir", elm: "elm", erlang: "erlang", "f#": "fsharp",
  flow: "flow", fortran: "fortran", gherkin: "gherkin", glsl: "glsl", go: "go", graphql: "graphql", groovy: "groovy",
  haskell: "haskell", hcl: "hcl", html: "markup", idris: "idris", java: "java", "java/c/c++/c#": "java",
  javascript: "javascript", json: "json", julia: "julia", kotlin: "kotlin", latex: "latex", less: "less", lisp: "lisp",
  livescript: "livescript", "llvm ir": "llvm", lua: "lua", makefile: "makefile", markdown: "markdown", markup: "markup",
  mathematica: "wolfram", matlab: "matlab", mermaid: "mermaid", nix: "nix", "notion formula": "notion-formula",
  "objective-c": "objectivec", ocaml: "ocaml", pascal: "pascal", perl: "perl", php: "php", powershell: "powershell",
  prolog: "prolog", protobuf: "protobuf", purescript: "purescript", python: "python", r: "r", racket: "racket",
  reason: "reason", ruby: "ruby", rust: "rust", sass: "sass", scala: "scala", scheme: "scheme", scss: "scss", shell: "bash",
  smalltalk: "smalltalk", solidity: "solidity", sql: "sql", swift: "swift", toml: "toml", typescript: "typescript",
  "vb.net": "vbnet", verilog: "verilog", vhdl: "vhdl", "visual basic": "visual-basic", webassembly: "wasm", xml: "markup",
  yaml: "yaml",
};

const GRAMMAR_NEEDS: Readonly<Record<string, readonly string[]>> = {
  arduino: ["c", "cpp"], cpp: ["c"], glsl: ["c"], idris: ["haskell"], objectivec: ["c"], php: ["markup-templating"],
  purescript: ["haskell"], racket: ["scheme"], scala: ["java"], vbnet: ["basic"],
};

const GRAMMAR_MODULES: Readonly<Record<string, () => Promise<unknown>>> = {
  abap: () => import("prismjs/components/prism-abap"), agda: () => import("prismjs/components/prism-agda"),
  arduino: () => import("prismjs/components/prism-arduino"), basic: () => import("prismjs/components/prism-basic"),
  bash: () => import("prismjs/components/prism-bash"), bnf: () => import("prismjs/components/prism-bnf"),
  c: () => import("prismjs/components/prism-c"), clojure: () => import("prismjs/components/prism-clojure"),
  coffeescript: () => import("prismjs/components/prism-coffeescript"), coq: () => import("prismjs/components/prism-coq"),
  cpp: () => import("prismjs/components/prism-cpp"), csharp: () => import("prismjs/components/prism-csharp"),
  dart: () => import("prismjs/components/prism-dart"), dhall: () => import("prismjs/components/prism-dhall"),
  diff: () => import("prismjs/components/prism-diff"), docker: () => import("prismjs/components/prism-docker"),
  ebnf: () => import("prismjs/components/prism-ebnf"), elixir: () => import("prismjs/components/prism-elixir"),
  elm: () => import("prismjs/components/prism-elm"), erlang: () => import("prismjs/components/prism-erlang"),
  flow: () => import("prismjs/components/prism-flow"), fortran: () => import("prismjs/components/prism-fortran"),
  fsharp: () => import("prismjs/components/prism-fsharp"), gherkin: () => import("prismjs/components/prism-gherkin"),
  glsl: () => import("prismjs/components/prism-glsl"), go: () => import("prismjs/components/prism-go"),
  graphql: () => import("prismjs/components/prism-graphql"), groovy: () => import("prismjs/components/prism-groovy"),
  haskell: () => import("prismjs/components/prism-haskell"), hcl: () => import("prismjs/components/prism-hcl"),
  idris: () => import("prismjs/components/prism-idris"), java: () => import("prismjs/components/prism-java"),
  json: () => import("prismjs/components/prism-json"), julia: () => import("prismjs/components/prism-julia"),
  kotlin: () => import("prismjs/components/prism-kotlin"), latex: () => import("prismjs/components/prism-latex"),
  less: () => import("prismjs/components/prism-less"), lisp: () => import("prismjs/components/prism-lisp"),
  livescript: () => import("prismjs/components/prism-livescript"), llvm: () => import("prismjs/components/prism-llvm"),
  lua: () => import("prismjs/components/prism-lua"), makefile: () => import("prismjs/components/prism-makefile"),
  markdown: () => import("prismjs/components/prism-markdown"),
  "markup-templating": () => import("prismjs/components/prism-markup-templating"),
  matlab: () => import("prismjs/components/prism-matlab"), nasm: () => import("prismjs/components/prism-nasm"),
  // Loaded as the others are: imported with the module, its grammar ran before Prism did
  // wherever the bundle reached this module first (Prism.languages undefined).
  mermaid: () => import("prismjs/components/prism-mermaid"),
  nix: () => import("prismjs/components/prism-nix"), objectivec: () => import("prismjs/components/prism-objectivec"),
  ocaml: () => import("prismjs/components/prism-ocaml"), pascal: () => import("prismjs/components/prism-pascal"),
  perl: () => import("prismjs/components/prism-perl"), php: () => import("prismjs/components/prism-php"),
  powershell: () => import("prismjs/components/prism-powershell"), prolog: () => import("prismjs/components/prism-prolog"),
  protobuf: () => import("prismjs/components/prism-protobuf"), purescript: () => import("prismjs/components/prism-purescript"),
  python: () => import("prismjs/components/prism-python"), r: () => import("prismjs/components/prism-r"),
  racket: () => import("prismjs/components/prism-racket"), reason: () => import("prismjs/components/prism-reason"),
  ruby: () => import("prismjs/components/prism-ruby"), rust: () => import("prismjs/components/prism-rust"),
  sass: () => import("prismjs/components/prism-sass"), scala: () => import("prismjs/components/prism-scala"),
  scheme: () => import("prismjs/components/prism-scheme"), scss: () => import("prismjs/components/prism-scss"),
  smalltalk: () => import("prismjs/components/prism-smalltalk"), solidity: () => import("prismjs/components/prism-solidity"),
  sql: () => import("prismjs/components/prism-sql"), swift: () => import("prismjs/components/prism-swift"),
  toml: () => import("prismjs/components/prism-toml"), typescript: () => import("prismjs/components/prism-typescript"),
  vbnet: () => import("prismjs/components/prism-vbnet"), verilog: () => import("prismjs/components/prism-verilog"),
  vhdl: () => import("prismjs/components/prism-vhdl"), "visual-basic": () => import("prismjs/components/prism-visual-basic"),
  wasm: () => import("prismjs/components/prism-wasm"), wolfram: () => import("prismjs/components/prism-wolfram"),
  yaml: () => import("prismjs/components/prism-yaml"),
};

// Notion's bash is an older Prism's, from before a command's options became
// a token (seen 2026-11-01: "-v" and "-la" stay text); the grammar copies
// the token into a command substitution's grammar too.
function withoutBashOptions(grammar: Prism.Grammar, seen = new Set<Prism.Grammar>()) {
  if (seen.has(grammar)) return;
  seen.add(grammar);
  Reflect.deleteProperty(grammar, "parameter");
  for (const value of Object.values(grammar)) {
    for (const pattern of [value ?? []].flat()) {
      if (!(pattern instanceof RegExp) && pattern.inside) withoutBashOptions(pattern.inside, seen);
    }
  }
}

const GRAMMAR_LOADED: Readonly<Record<string, (grammar: Prism.Grammar) => void>> = { bash: withoutBashOptions };

// Notion's own grammar for its formulas, as it colors one (seen 2026-11-01):
// "/* */" comments ("//" is two operators), its control words, booleans,
// prop as a variable, a lowercase name before "(" as a function ("Now(" is
// text), strings, numbers, operators, and the rest — ":" too — punctuation.
Prism.languages["notion-formula"] = {
  comment: /\/\*[\s\S]*?\*\//,
  string: /"(?:\\.|[^"\\])*"/,
  keyword: /\b(?:if|ifs|let|lets|and|or|not)\b/,
  boolean: /\b(?:true|false)\b/,
  variable: /\bprop\b(?=\s*\()/,
  function: /\b[a-z]\w*(?=\s*\()/,
  number: /\b\d+(?:\.\d+)?\b/,
  operator: /==|!=|>=|<=|[+\-*/%^<>?!]/,
  punctuation: /[()[\],.:]/,
};

const loading = new Map<string, Promise<void>>();

/** The Prism grammar Notion colors a code block's language with. */
export function prismGrammarId(language: string): string | null {
  return PRISM_GRAMMAR[language] ?? null;
}

/** The grammar, once it is loaded. */
export function loadedPrismGrammar(id: string): Prism.Grammar | null {
  return Prism.languages[id] ?? null;
}

/** Loads a grammar and the grammars it extends, once. */
export function loadPrismGrammar(id: string): Promise<void> {
  if (Prism.languages[id]) return Promise.resolve();
  let pending = loading.get(id);
  if (!pending) {
    pending = (async () => {
      for (const need of GRAMMAR_NEEDS[id] ?? []) await loadPrismGrammar(need);
      await GRAMMAR_MODULES[id]?.();
      const grammar = Prism.languages[id];
      if (grammar) GRAMMAR_LOADED[id]?.(grammar);
    })();
    loading.set(id, pending);
    pending.catch(() => loading.delete(id));
  }
  return pending;
}
