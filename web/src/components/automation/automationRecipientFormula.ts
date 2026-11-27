import type { DbProperty } from "@/lib/types";
import { formulaValueTypeOf } from "@/components/database/formulaTypes";
import { formulaPropertyTypes } from "@/components/database/formulaPropertyTypes";

// The editor rejects syntax, known scalars and homogeneous scalar lists. The native
// execution owner authoritatively checks the actual typed people at run time.
export function validAutomationRecipientFormula(expression: string, properties: readonly DbProperty[]) {
  if (!expression.trim()) return false;
  const result = formulaValueTypeOf(expression, formulaPropertyTypes(properties));
  // A person, a page or a list of them stays for the run to read (a page's
  // people); a value of another known type, or a list of them, can't be one.
  const scalar = (type: string | undefined) => type === "number" || type === "text" || type === "boolean" || type === "date" || type === "undefined";
  return result !== null && (result.type === "unknown" || result.type === "person" || result.type === "block"
    || (result.type === "array" && !scalar(result.items)));
}
