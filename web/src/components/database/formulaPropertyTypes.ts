import { DATE_ROLLUP_FUNCTIONS } from "./databaseFilterLabels";
import { formulaPageField, resolveFormulaPropertyReference, type FormulaPropertyReferenceTarget } from "./formulaReferences";
import { formulaPropertyShape, formulaValueTypeOf, type FormulaPropertyType, type FormulaShape } from "./formulaTypes";

// The editor reads a property a formula names as Notion's does (seen
// 2026-11-20 in its editor and through its API, a synthetic database under
// a Notion test page; notion_formula_property_kinds.go): a formula by the type
// of what it gives, read by the same rule — so prop("F") * 2 is judged as F
// written out (math on text and number), the
// formula being edited by what it gives as saved, one met again while its own
// is read (a cycle) by none —, and a rollup by its calculation: a date for
// the earliest, latest or range of dates, a list for its values shown as they
// are or each once (math on array and number, where Notion's API check
// says text), a number for the rest.

type TypedProperty = { id: string; name: string; type: string; config?: unknown; databaseId?: string };

// The rollups that show their related values, a list, rather than work one out.
const ROLLUP_VALUES = new Set(["show_original", "show_unique", "count_per_group", "percent_per_group"]);

/**
 * The type of the property a formula names among properties, by its name or
 * ID; with pageFields, a page field a formula reads by name (생성 일시) as the
 * field's. A page a relation gives is of its database, whose property its
 * prop() names (related, by database, or properties for its own).
 */
export function formulaPropertyTypes(
  properties: readonly TypedProperty[],
  pageFields = false,
  related?: Readonly<Record<string, readonly TypedProperty[] | undefined>>,
): FormulaPropertyType {
  const own = properties.find((property) => property.databaseId)?.databaseId;
  const known = new Map<string, FormulaShape>();
  const reading = new Set<string>();
  const typeOf = (property: TypedProperty | undefined, among: readonly TypedProperty[]): FormulaShape => {
    if (!property) return { type: "unknown" };
    const config = (property.config ?? {}) as Record<string, unknown>;
    if (property.type === "rollup") {
      const calculation = String(config.rollupFunction || config.function || "show_original");
      return { type: DATE_ROLLUP_FUNCTIONS.has(calculation) ? "date" : ROLLUP_VALUES.has(calculation) ? "array" : "number" };
    }
    if (property.type === "relation") return formulaPropertyShape("relation", String(config.relationDatabaseId ?? property.databaseId ?? ""));
    if (property.type !== "formula") return formulaPropertyShape(property.type);
    const done = known.get(property.id);
    if (done) return done;
    if (reading.has(property.id)) return { type: "unknown" };
    reading.add(property.id);
    const read = formulaValueTypeOf(String(config.formula ?? config.expression ?? ""), within(among, among === properties && pageFields));
    const found: FormulaShape = read ? { type: read.type, ...(read.item ? { item: read.item } : {}) } : { type: "unknown" };
    reading.delete(property.id);
    known.set(property.id, found);
    return found;
  };
  // A page's property, of its database's properties: null where it hasn't it, undefined where they aren't known.
  const ofPage = (name: string, source: string) => {
    const among = source === own ? properties : related?.[source];
    if (!among) return undefined;
    const found = resolveFormulaPropertyReference(among as readonly (TypedProperty & FormulaPropertyReferenceTarget)[], name);
    return found ? typeOf(found, among) : null;
  };
  const within = (among: readonly TypedProperty[], fields: boolean): FormulaPropertyType => (name, source) => {
    if (source !== undefined) return ofPage(name, source);
    const targets = among as readonly (TypedProperty & FormulaPropertyReferenceTarget)[];
    const field = fields ? formulaPageField(targets, name) : undefined;
    const found = field ? undefined : resolveFormulaPropertyReference(targets, name);
    // A name it hasn't: null, as a page's (formulaTypes.ts takes it as not found).
    return field ? formulaPropertyShape(field.type) : found ? typeOf(found, among) : null;
  };
  return within(properties, pageFields);
}
