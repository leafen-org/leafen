import type { DbProperty } from "@/lib/types";
import { formulaPageField, resolveFormulaPropertyReference } from "./formulaReferences";
import { formulaPieces, type FormulaProblem, type FormulaScopeName } from "./formulaEditorModel";
import { formulaPropertyTypes } from "./formulaPropertyTypes";
import { formulaPropertyReading } from "./formulaTypes";

// The formula editor writes a property a formula names as a chip, a page's
// too — its database's property, of what isn't a page its own database's —,
// as Notion's editor does (seen 2026-11-21 twice: prop("Rel2").first().prop("Text2")
// writes Text2 as a chip and counts it one letter, "[0,15]"; .prop("Text") of a
// page whose database hasn't Text stays text). Once a property it names isn't
// found, none it names after it is a chip, nor named as not found
// (prop("Nope") + prop("Text") * 2 names Nope alone, Text as text).

/** The formula as its editor writes it, with its chips. */
export function formulaEditorPieces(
  text: string, properties: readonly DbProperty[], names: readonly FormulaScopeName[], related?: Record<string, DbProperty[]>,
) {
  const { sources, lostAt } = formulaPropertyReading(text, formulaPropertyTypes(properties, true, related));
  return formulaPieces(text, (name, at) => {
    if (lostAt !== undefined && at !== undefined && at > lostAt) return undefined;
    const source = at === undefined ? undefined : sources.get(at);
    const among = source ? related?.[source] ?? (source === properties[0]?.databaseId ? properties : undefined) : properties;
    const property = (!source ? formulaPageField(properties, name) : undefined) ?? (among ? resolveFormulaPropertyReference(among, name) : undefined);
    return property ? { name: property.name, type: property.type } : undefined;
  }, names, true);
}

/** The problems a formula's names make, none for a property named after one not found. */
export function formulaWarningsKept(warnings: readonly FormulaProblem[], text: string, properties: readonly DbProperty[], related?: Record<string, DbProperty[]>) {
  const { lostAt } = formulaPropertyReading(text, formulaPropertyTypes(properties, true, related));
  return lostAt === undefined ? warnings : warnings.filter((item) => item.key !== "unknownProperty" || (item.start ?? 0) <= lostAt);
}
