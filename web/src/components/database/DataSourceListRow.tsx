import type { ButtonHTMLAttributes, ReactNode } from "react";
import type { Place } from "@/lib/pagePath";
import { PlaceLine } from "../PlaceLine";
import { DataSourceMark } from "./DataSourceMark";
import styles from "./DataSourceListRow.module.css";

/**
 * A data source where a desktop list offers it to choose — a view's
 * "데이터 소스 연결", a waiting view's and a new chart's "기존 데이터 소스",
 * "기존 데이터 소스 연결하기", a relation's "관계형 대상", a page layout tab's
 * sources: its mark (DataSourceMark), its name and, under it, where it sits
 * when it sits anywhere. Notion's rows are 28px for the name alone and 45 with
 * where it sits (seen 2026-11-19 in Chrome at 1440, its Business-trial owner:
 * People and a task tracker 28 in the start window's and a relation's lists;
 * 2026-11-20 at dpr 2 in "데이터 소스 연결" and "기존 데이터 소스": 45, the
 * mark 20 at 8,5, the name 14/16.8 at 36,4.7, the place 12/16.8 2px under
 * it) — one rule for every list (DataSourceListRow.module.css). A phone's
 * sheets give a source's place as their row's second line the same way
 * (PhoneSheetRow's description, only when it has one).
 */
export function DataSourceListRow({ source, name, place, end, className, ...button }: {
  source: { icon?: string; iconType?: string };
  name: ReactNode;
  /** Where it sits; nothing, or an empty place, leaves the row one line. */
  place?: Place | null;
  /** What follows at its end, as the database's own "ⓘ". */
  end?: ReactNode;
  className?: string;
} & Omit<ButtonHTMLAttributes<HTMLButtonElement>, "type" | "className" | "children">) {
  return (
    <button type="button" {...button} className={className ? `${styles.row} ${className}` : styles.row}>
      <span className={styles.mark}><DataSourceMark source={source} /></span>
      <span className={styles.text}>
        <span className={styles.name}>{name}</span>
        {place?.length ? <small className={styles.place}><PlaceLine place={place} /></small> : null}
      </span>
      {end}
    </button>
  );
}
