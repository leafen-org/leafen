/**
 * The order a view lists its properties in, whatever its layout: its table's
 * columns, its cards' and list's lines, its "속성 표시 여부" and "속성"
 * panels. Notion's view keeps no order until one is set through it, and
 * shows its title first and the rest by name meanwhile; once set (a column
 * added from its head, inserted, moved, resized, wrapped or hidden, a
 * property shown from its panel), the listed ones keep their places and those
 * made since follow them by name (seen 2026-11-12, every layout; the server's
 * read is database_view_property_order.go).
 */

/**
 * Two property names in Notion's order: compared by their UTF-16 code units,
 * case and accents as they are — "10" < "2a" < "9" < "Banana" < "_x" <
 * "apple" < "zed" < "Éclair" < "ㄱ자음" < "漢字" < "가" < "𝐀" < "😀" < "Ａ"
 * (seen 2026-11-12).
 */
export function comparePropertyNames(left: string, right: string) {
  return left < right ? -1 : left > right ? 1 : 0;
}

type ViewPropertyLike = { id: string; name: string; type: string };

/**
 * The properties in a view's order: the ones `order` lists first, as listed,
 * then the rest by name with the title ahead of them. Properties of the same
 * name keep the order they came in.
 */
export function orderViewProperties<Property extends ViewPropertyLike>(
  properties: readonly Property[],
  order?: readonly string[],
): Property[] {
  const byId = new Map(properties.map((property) => [property.id, property]));
  const listed = [...new Set(order ?? [])].flatMap((id) => byId.get(id) ? [byId.get(id)!] : []);
  const seen = new Set(listed.map((property) => property.id));
  const rest = properties.filter((property) => !seen.has(property.id)).sort((left, right) => (
    Number(right.type === "title") - Number(left.type === "title") || comparePropertyNames(left.name, right.name)
  ));
  return [...listed, ...rest];
}
