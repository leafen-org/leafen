import { ArrowUpRightIcon, DatabaseCylinderIcon } from "@/icons/leafen";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { BUILT_IN_ICON_SHARE, builtInIcon } from "@/components/page/nativeIcons";
import type { GraphDataSourceIconType } from "@/contentGraphWire.generated";
import styles from "./DataSourceMark.module.css";

/**
 * A data source where a list names it, as Notion's (seen 2026-11-15:
 * 데이터 소스 관리's source rows, the source screen's "추천" and "기존
 * 데이터 소스", the search's "/ source", a sidebar database view section, a
 * new relation's list and its "관계형 대상", "옮기기"'s search, a form's
 * "〈데이터베이스〉 내 검색"): its own icon — an emoji 18px in the 20px box,
 * the box's own 14px font not the emoji's (measured 2026-11-15), a built-in
 * icon 19.2 (BUILT_IN_ICON_SHARE: People's person in a ring 19.19 in its
 * 20px box in "데이터 소스 연결", "관계형 대상" and the new relation's row,
 * seen 2026-11-19; the small box's in proportion) — else the cylinder, a database of one source
 * showing that source's. A source of no icon is the cylinder in every one of
 * them; one with an icon is never the cylinder. People's source shows
 * Notion's People icon, the built-in person in a ring in gray, which the
 * server reads it with while it has none of its own (people_database_icon.go).
 */
export function DataSourceMark({ source, size = "regular" }: {
  source: { icon?: string; iconType?: GraphDataSourceIconType | string };
  /** "small": a 16px row's (a sidebar section's in its customize list), the emoji in proportion. */
  size?: "regular" | "small";
}) {
  const cylinder = <DatabaseCylinderIcon size={size} aria-hidden="true" />;
  if (!source.icon || !source.iconType || source.iconType === "none") return cylinder;
  const box = size === "small" ? 16 : 20;
  const glyph = builtInIcon(source) ? box * BUILT_IN_ICON_SHARE : box - 2;
  return (
    <span className={`${styles.mark} ${size === "small" ? styles.small : ""}`} aria-hidden="true" data-data-source-mark="icon">
      <PageIconGlyph page={{ icon: source.icon, iconType: source.iconType as GraphDataSourceIconType }} size={glyph} box={box}
        fallback={cylinder} />
    </span>
  );
}

/**
 * A source another database owns where 데이터 소스 관리 lists it under
 * "연결됨": the cylinder in the icons' grey with a ↗ (16px on a phone, 14 on
 * a desktop) at its foot's end, 2px past its box, in the icons' dark ringed
 * 1.5px (Notion's, seen 2026-11-18 on a Galaxy A90 at 411 and in Chrome at
 * 1440: own and linked cylinders alike in icoSec, the linked one's
 * arrowDiagonalUpRightSmall stroked under its fill).
 */
export function LinkedDataSourceMark() {
  return (
    <span className={styles.linked} aria-hidden="true" data-data-source-mark="linked">
      <DatabaseCylinderIcon size="regular" />
      <ArrowUpRightIcon size="small" className={styles.linkedBadge} />
    </span>
  );
}
