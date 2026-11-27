import { useEffect, useState, type KeyboardEvent } from "react";
import { i18next } from "@/i18n";
import { activeNumberLocale } from "@/lib/i18n";
import { usePhoneSheetHead } from "../shell/PhoneSheet";
import { databaseViewLabels } from "./databaseViewLabels";
import { useSettingsScreenHead } from "./settingsScreenHead";
import type { TableNumberGrouping } from "./tableGrouping";
import styles from "./GroupNumberRangeFields.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

type Range = Pick<TableNumberGrouping, "start" | "end" | "step">;

/**
 * Numbers grouped in ranges, as Notion's "숫자별 › 범위" opens its own
 * "← 숫자별" screen (seen 2026-10-11, 2026-11-21 at 1440 — choosing "범위"
 * again opens it again): under "그룹 범위" its from and to with a dash
 * between, under "그룹화 단위" the size of a group, the first two groups
 * named under it; the fields typed numbers without arrows. A field keeps what
 * was typed when left or on Enter.
 */
export function GroupNumberRangeScreen({ grouping, sheet, disabled, onBack, onChange }: {
  grouping: Range; sheet?: boolean; disabled?: boolean; onBack(): void; onChange(range: Range): void;
}) {
  const labels = databaseViewLabels();
  const head = { title: labels.groupNumberBy, onBack };
  useSettingsScreenHead(head);
  usePhoneSheetHead(sheet ? head : null);
  const format = new Intl.NumberFormat(activeNumberLocale());
  const name = (low: number) => i18next.t("tableView:numberGroup.range", { from: format.format(low), to: format.format(low + grouping.step) });
  const field = (key: keyof Range, label: string) => (
    <NumberField key={`${key}:${grouping[key]}`} value={grouping[key]} label={label} disabled={disabled}
      onCommit={(value) => {
        const next = { ...grouping, [key]: value };
        if (key === "step" && !(value > 0)) return;
        onChange({ ...next, end: Math.max(next.start, next.end) });
      }} />
  );
  // The first two groups' names in weight 500, the words between them as the hint says them.
  const [before = "", between = "", after = ""] = labels.groupNumberRangeHint("\u0000", "\u0000").split("\u0000");
  return (
    <div className={styles.screen} data-number-group-range>
      <div className={styles.section}>
        <div className={styles.label}>{labels.groupNumberRangeBounds}</div>
        <div className={styles.bounds}>
          {field("start", labels.groupNumberRangeBounds)}<span className={styles.dash} aria-hidden="true">–</span>{field("end", labels.groupNumberRangeBounds)}
        </div>
      </div>
      <div className={styles.section}>
        <div className={styles.label}>{labels.groupNumberStep}</div>
        <div className={styles.step}>
          {field("step", labels.groupNumberStep)}
          <p className={styles.hint}><span className={styles.hintWords}>
            {before}<span className={styles.hintGroup}>{name(grouping.start)}</span>{between}
            <span className={styles.hintGroup}>{name(grouping.start + grouping.step)}</span>{after}
          </span></p>
        </div>
      </div>
    </div>
  );
}

function NumberField({ value, label, disabled, onCommit }: { value: number; label: string; disabled?: boolean; onCommit(value: number): void }) {
  const [text, setText] = useState(String(value));
  useEffect(() => setText(String(value)), [value]);
  const commit = () => { const number = Number(text); if (text.trim() !== "" && Number.isFinite(number) && number !== value) onCommit(number); else setText(String(value)); };
  return <input className={styles.input} type="number" inputMode="decimal" data-arrows="none" aria-label={label} value={text} disabled={disabled}
    onChange={(event) => setText(event.currentTarget.value)} onBlur={commit}
    onKeyDown={(event: KeyboardEvent<HTMLInputElement>) => { if (fieldEnter(event)) { event.preventDefault(); commit(); } }} />;
}
