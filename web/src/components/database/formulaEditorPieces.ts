import type { DbProperty } from "@/lib/types";
import { formulaPageField, resolveFormulaPropertyReference } from "./formulaReferences";
import { formulaPieces, type FormulaPiece, type FormulaProblem, type FormulaScopeName } from "./formulaEditorModel";
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

/**
 * Where a problem is underlined: every piece of the formula as its editor
 * writes it that the problem's place touches — a token, a chip, or a run of
 * what keeps the text's colour, its spaces and names no token colours with
 * it — as Notion's editor underlines it (seen 2026-11-28 in Chrome, two tabs
 * at different times): " # " in "1 # 2", " qqq" in "1 qqq", "qqq " in
 * "qqq + 1", "==" whole in "true == false == true" where the place named is
 * its first letter [14,15].
 */
export function formulaUnderlined<T extends { start?: number; end?: number }>(pieces: readonly FormulaPiece[], mark: T): T {
  if (mark.start === undefined || mark.end === undefined) return mark;
  let start = mark.start, end = mark.end, at = 0, runStart = 0;
  pieces.forEach((piece, index) => {
    const plain = !piece.kind && !piece.chip;
    if (!plain || index === 0 || pieces[index - 1].kind || pieces[index - 1].chip) runStart = at;
    at += piece.text.length;
    const next = pieces[index + 1];
    // A run of plain pieces ends where a token or chip follows, or the formula does.
    if (plain && next && !next.kind && !next.chip) return;
    if (runStart < mark.end! && at > mark.start!) [start, end] = [Math.min(start, runStart), Math.max(end, at)];
  });
  return start === mark.start && end === mark.end ? mark : { ...mark, start, end };
}
