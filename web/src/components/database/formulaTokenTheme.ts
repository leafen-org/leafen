import "../editor/codeTheme.css";
import type { FormulaHighlightKind } from "./formulaEditorModel";

// A formula's tokens take a code block's colours wherever a formula is
// written — its editor, its help's examples, an automation's formula line —
// as Notion's do (seen 2026-11-14: the same Prism rules as its code blocks,
// the default theme in light and the dark theme in dark): the formula's
// element carries FORMULA_CODE, each token Prism's names, and codeTheme.css
// colours them.
export const FORMULA_CODE = "code-highlight";

export const formulaTokenClass = (kind: FormulaHighlightKind) => `token ${kind}`;
