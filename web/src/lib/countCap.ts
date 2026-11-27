/**
 * A count over 99 reads "99+", as Notion's do: the inbox's unread, a large
 * database's calculations, a data source's views (seen 2026-11-18 in two
 * tabs: "99개 보기" at 99 views, "99+개 보기" at 100).
 */
const COUNT_CAP = 99;

export function cappedCount(count: number): string {
  return count > COUNT_CAP ? `${COUNT_CAP}+` : String(count);
}

/**
 * A translation's options for a count it names: its plural the number's,
 * the number it shows capped (cappedCount) — i18next fills "{{count}}" from
 * `replace` while it picks the plural by `count`.
 */
export function cappedCountOptions(count: number): { count: number; replace?: { count: string } } {
  return count > COUNT_CAP ? { count, replace: { count: cappedCount(count) } } : { count };
}
