import type { ReactNode } from "react";
import { NativeRadioChoices } from "@/lib/nativeRadioChoices";
import styles from "./DatabaseCardLayoutTiles.module.css";

// Notion's tiles, the compact one first (seen 2026-10-11).
const CARD_LAYOUTS = ["compact", "list"] as const;
type CardLayout = (typeof CARD_LAYOUTS)[number];

/**
 * A board's or gallery's "카드 레이아웃", as Notion's (seen 2026-10-11): in a
 * faintly shaded box under its centered name, a small picture of a card for
 * each choice — a band, a round icon and lines across for "작게", lines down
 * for "리스트" — with its name under it, the chosen one ringed and tinted
 * blue.
 */
export function DatabaseCardLayoutTiles({ value, label, names, disabled, onChange, children }: {
  value: CardLayout;
  label: string;
  names: Record<CardLayout, string>;
  disabled?: boolean;
  onChange(layout: CardLayout): void;
  /** What follows the tiles in their box: "작게"'s "컴팩트 카드 설정 ›". */
  children?: ReactNode;
}) {
  return (
    <div className={styles.section} data-card-layout-tiles>
      <div className={styles.title}>{label}</div>
      <NativeRadioChoices options={CARD_LAYOUTS} value={value} onChange={onChange} label={label} disabled={disabled}
        className={styles.tiles} itemClassName={styles.choice} marker={(option) => ({ "data-layout-choice": `cardLayout:${option}` })}>
        {(option) => (
          <>
            <span className={styles.tile} aria-hidden="true">
              <span className={styles.band} />
              <span className={styles.dot} />
              {option === "compact" ? (
                <span className={styles.compact}>
                  <span><i /><i /><i /></span>
                  <span><i /><i /></span>
                </span>
              ) : (
                <span className={styles.list}><i /><i /><i /></span>
              )}
            </span>
            <span className={styles.name}>{names[option]}</span>
          </>
        )}
      </NativeRadioChoices>
      {children}
    </div>
  );
}
