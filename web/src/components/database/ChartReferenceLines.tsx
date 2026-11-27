import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon, ChevronDown, ChevronRight, Plus, Trash } from "@/icons/leafen";
import { randomUUID } from "@/lib/browserCrypto";
import { HoverTip } from "../IconTipButton";
import type { PopoverRect } from "@/lib/popoverPlacement";
import { ChartChoiceList, ChartPopover } from "./ChartPanelMenus";
import { chartReferenceLineColor } from "./chartViewPalette";
import { CHART_LINE_COLORS, CHART_LINE_DASHES, CHART_LINE_LIMIT, type ChartReferenceLine } from "./chartViewSettings";
import styles from "./ChartReferenceLines.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

function numberOrNone(text: string) {
  const value = Number(text.trim());
  return text.trim() && Number.isFinite(value) ? value : undefined;
}

/**
 * A chart's "범위", as Notion's (seen 2026-10-05): "사용자 지정 범위 설정"
 * with "최소 - 최대" fields that set the value axis as typed, and "재설정"
 * back to the automatic range. Opened, its first control takes the keys —
 * "최소", ringed, while the range is automatic, else "재설정" — and what was
 * typed is kept when Escape or a press elsewhere closes it, as Notion's
 * (seen 2026-11-18 in Chrome at 1440, two tabs each: the menu opened by a
 * press, then one key typed).
 */
export function ChartRangeMenu({ min, max, onChange }: { min?: number; max?: number; onChange(min?: number, max?: number): void }) {
  const { t } = useTranslation("chartView");
  const ref = useRef<HTMLDivElement>(null);
  const text = (value?: number) => value === undefined ? "" : String(value);
  const typed = useRef({ min: text(min), max: text(max) });
  const latest = useRef({ min, max, onChange });
  latest.current = { min, max, onChange };
  const commit = () => {
    const next = { min: numberOrNone(typed.current.min), max: numberOrNone(typed.current.max) };
    if (next.min !== latest.current.min || next.max !== latest.current.max) latest.current.onChange(next.min, next.max);
  };
  // A range set from elsewhere replaces what was typed; closing the menu keeps what was.
  useEffect(() => { typed.current = { min: text(min), max: text(max) }; }, [min, max]);
  useEffect(() => {
    ref.current?.querySelector<HTMLElement>("button, input")?.focus({ preventScroll: true });
    return () => commit();
  }, []);
  const field = (which: "min" | "max") => (
    <input key={`${which}:${which === "min" ? min : max}`} className={styles.input} inputMode="decimal" defaultValue={which === "min" ? text(min) : text(max)}
      placeholder={t(which === "min" ? "rangeMin" : "rangeMax")} aria-label={t(which === "min" ? "rangeMin" : "rangeMax")}
      data-chart-range-min={which === "min" ? "" : undefined} data-chart-range-max={which === "max" ? "" : undefined}
      onChange={(event) => { typed.current = { ...typed.current, [which]: event.currentTarget.value }; }} onBlur={commit}
      onKeyDown={(event) => { if (fieldEnter(event)) commit(); }} />
  );
  return <div ref={ref} className={styles.range} data-chart-range>
    <div className={styles.rangeHead}>
      <span>{t("rangeTitle")}</span>
      {(min !== undefined || max !== undefined) && <button type="button" className={styles.link} onClick={() => onChange(undefined, undefined)}>
        {t("reset")}</button>}
    </div>
    <div className={styles.rangeFields}>
      {field("min")}
      <span aria-hidden="true">-</span>
      {field("max")}
    </div>
  </div>;
}

/**
 * A chart's "기준선", as Notion's (seen 2026-10-05): a short note and "+
 * 기준선 추가" until there is one, then a card for each — its value (a new
 * one at the middle of the value axis, seen 2026-11-12) and 🗑, label, style
 * (대시 or 실선) and color (검정색, 옅은 회색 or an option color, Notion's names)
 * — and "+ 기준선 추가" under them, dim at ten lines with a tip saying so. Its
 * style and color open their choices flush under their row from its start, its
 * › turned ⌄ meanwhile: a 180 list, "실선" then "대시"; ten 20px chips in two
 * rows of five, each in a 30×24 button 8 apart inside 10, its color rimmed
 * 1px in itself (2px and a white ✓ for the one set), its name in a tip over
 * it. A pick closes them, as a press on the row or elsewhere or Escape does
 * (Notion's, seen 2026-11-12).
 */
export function ChartReferenceLinesMenu({ lines, newValue, onChange }: {
  lines: readonly ChartReferenceLine[]; newValue: number; onChange(lines: ChartReferenceLine[]): void;
}) {
  const { t } = useTranslation("chartView");
  const [open, setOpen] = useState<{ index: number; what: "dash" | "color"; anchor: PopoverRect } | null>(null);
  // Each line keeps an id of its own, as Notion's (its API reads it back); one kept from before lines had one takes one as it changes.
  const change = (index: number, patch: Partial<ChartReferenceLine>) =>
    onChange(lines.map((line, at) => at === index ? { id: line.id ?? randomUUID(), ...line, ...patch } : line));
  const full = lines.length >= CHART_LINE_LIMIT;
  const add = () => { if (!full) onChange([...lines, { id: randomUUID(), value: newValue, label: "", color: "default", dashStyle: "dash" }]); };
  const colorName = (color: string) => t(`chartView:lineColors.${color}`);
  const addButton = <button type="button" className={lines.length ? styles.add : styles.addPrimary} data-chart-reference-add
    aria-disabled={full || undefined} onClick={add}>
    <Plus size="regular" aria-hidden="true" />{t("chartView:addReferenceLine")}
  </button>;
  return <div className={styles.lines} data-chart-reference-lines>
    {lines.length === 0 && <p className={styles.note}>{t("chartView:referenceIntro")}</p>}
    {lines.map((line, index) => (
      <div key={index} className={styles.card} data-chart-reference-line-card={index}>
        <div className={styles.cardHead}>
          <span>{t("chartView:lineValue")}</span>
          <button type="button" className={styles.icon} aria-label={t("chartView:deleteLine")}
            onClick={() => onChange(lines.filter((_, at) => at !== index))}><Trash size="small" /></button>
        </div>
        <input key={`value:${line.value}`} className={styles.input} inputMode="decimal" defaultValue={line.value} aria-label={t("chartView:lineValue")}
          data-chart-reference-value onBlur={(event) => { const value = numberOrNone(event.currentTarget.value); if (value !== undefined) change(index, { value }); }}
          onKeyDown={(event) => { if (fieldEnter(event)) { const value = numberOrNone(event.currentTarget.value); if (value !== undefined) change(index, { value }); } }} />
        <span className={styles.cardLabel}>{t("chartView:lineLabel")}</span>
        <input key={`label:${line.label}`} className={styles.input} defaultValue={line.label} placeholder={t("chartView:lineLabel")}
          aria-label={t("chartView:lineLabel")} data-chart-reference-label
          onBlur={(event) => change(index, { label: event.currentTarget.value })}
          onKeyDown={(event) => { if (fieldEnter(event)) change(index, { label: event.currentTarget.value }); }} />
        {(["dash", "color"] as const).map((what) => {
          const expanded = open?.index === index && open.what === what;
          return <button key={what} type="button" className={styles.choice} aria-haspopup="true" aria-expanded={expanded}
            data-chart-reference-dash={what === "dash" ? "" : undefined} data-chart-reference-color={what === "color" ? "" : undefined}
            onClick={(event) => setOpen(expanded ? null : { index, what, anchor: event.currentTarget.getBoundingClientRect() })}>
            <span>{t(what === "dash" ? "chartView:lineStyle" : "chartView:lineColor")}</span>
            <span className={styles.value}>
              {what === "color" && <i className={styles.swatch} style={{ background: chartReferenceLineColor(line.color) }} />}
              {what === "dash" ? t(`chartView:dashes.${line.dashStyle}`) : colorName(line.color)}
              {expanded ? <ChevronDown size="small" aria-hidden="true" /> : <ChevronRight size="small" aria-hidden="true" />}
            </span>
          </button>;
        })}
      </div>
    ))}
    {full ? <HoverTip label={t("chartView:lineLimit")} placement="top" className={styles.addTip}>{addButton}</HoverTip> : addButton}
    {open && (
      <ChartPopover anchor={open.anchor} under label={open.what === "dash" ? t("chartView:lineStyle") : t("chartView:lineColor")}
        width={open.what === "dash" ? 180 : 202} marker={`reference-${open.what}`} onClose={() => setOpen(null)}>
        {open.what === "dash"
          ? <ChartChoiceList label={t("chartView:lineStyle")} marker="reference-dash" value={lines[open.index]?.dashStyle}
            options={[...CHART_LINE_DASHES].reverse().map((dash) => ({ value: dash, label: t(`chartView:dashes.${dash}`) }))}
            onPick={(dashStyle) => { change(open.index, { dashStyle: dashStyle as ChartReferenceLine["dashStyle"] }); setOpen(null); }} />
          : <div className={styles.colors} role="group" aria-label={t("chartView:lineColor")}>
            {CHART_LINE_COLORS.map((color) => (
              <HoverTip key={color} label={colorName(color)} placement="top" className={styles.colorTip}>
                <button type="button" className={styles.colorButton} aria-label={colorName(color)}
                  aria-pressed={lines[open.index]?.color === color} data-chart-reference-color-choice={color}
                  onClick={() => { change(open.index, { color }); setOpen(null); }}>
                  <i className={styles.chip} style={{ background: chartReferenceLineColor(color), color: chartReferenceLineColor(color) }}>
                    {lines[open.index]?.color === color && <CheckIcon size="tiny" aria-hidden="true" />}
                  </i>
                </button>
              </HoverTip>
            ))}
          </div>}
      </ChartPopover>
    )}
  </div>;
}
