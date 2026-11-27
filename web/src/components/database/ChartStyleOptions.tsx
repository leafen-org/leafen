import type { TFunction } from "i18next";
import { useId, useState, type CSSProperties } from "react";
import { useTranslation } from "react-i18next";
import {
  ArrowsUpDownStackedIcon, DottedLineIcon, DragHandleIcon, GraphAreaIcon, HashIcon, LineCurveIcon, ListIcon, NumbersIcon, Plus, SidebarChevronDown,
  SidebarChevronRight, TextJustifyStartIcon, TextShortIcon, TextXYUnderlineIcon, Trash,
} from "@/icons/leafen";
import type { PopoverRect } from "@/lib/popoverPlacement";
import { useDragReorder } from "@/lib/useDragReorder";
import { ChartChoiceList, ChartChoiceRow, ChartMenuStaysOpen, ChartPopover, ChartSwitchRow, ChartTextField, type ChartChoice } from "./ChartPanelMenus";
import { CHART_HEIGHTS, chartHeightOf, type ChartType } from "./chartViewModel";
import { CHART_COLOR_THEMES, chartInkColor, chartThemeSwatches } from "./chartViewPalette";
import {
  CHART_AXIS_LABELS, CHART_DONUT_LABELS, CHART_GRID_LINES, CHART_GROUP_STYLES, CHART_LEGEND_POSITIONS, CHART_RULE_OPERATORS,
  CHART_TEXT_COLORS, type ChartColorRule, type ChartSettings,
} from "./chartViewSettings";
import styles from "./ChartStyleOptions.module.css";

type Patch = Record<string, unknown>;

const OPERATOR_SIGNS: Record<ChartColorRule["operator"], string> = { ">": ">", ">=": "≥", "<": "<", "<=": "≤", "=": "=", "!=": "≠" };
const OPERATOR_KEYS: Record<ChartColorRule["operator"], string> = {
  ">": "greater", ">=": "greaterOrEqual", "<": "less", "<=": "lessOrEqual", "=": "equal", "!=": "notEqual",
};

/**
 * "스타일 옵션 더 보기", as Notion's (seen 2026-10-05): a chart's height,
 * then for columns, bars and lines their group style (when the Y axis
 * groups), grid lines and axis names, a line's curve and fading area, a
 * donut's center value, its label and negative values, value colors (one
 * color's shades, for bars and a donut), the legend (when stacking, a line's own
 * name under it whether it stacks or not — seen 2026-11-18 in Chrome, two
 * tabs: a line ungrouped has eight rows, 280 by 255, a column five — or a
 * donut's place for it), data labels and the caption. Each row's mark is the one
 * Notion draws there (seen 2026-11-18 in a bar's, a line's and a donut's
 * options). A pick or switch keeps what it set, its default too, as Notion's
 * views API then reads it (seen 2026-11-18 in Chrome: height "medium",
 * grid_lines "horizontal", axis_labels "none", show_data_labels, smooth_line,
 * hide_line_fill_area, color_by_value and a donut's legend_position each
 * given at the value picked) — but the legend switch of bars and lines, which
 * that API never reads (legend_position is a donut's choice), and a caption
 * shown with no words (notionViewGivenConfiguration).
 */
export function ChartStyleOptions({ type, stacked, config, settings, disabled, onChange }: {
  type: ChartType; stacked: boolean; config: Readonly<Record<string, unknown>>; settings: ChartSettings; disabled: boolean;
  onChange(patch: Patch): void;
}) {
  const { t } = useTranslation("chartView");
  const choices = <T extends string>(values: readonly T[], group: string): ChartChoice[] =>
    values.map((value) => ({ value, label: t(`${group}.${value}`) }));
  const theme = typeof config.chartColorTheme === "string" ? config.chartColorTheme : "auto";
  const singleColor = theme !== "auto" && theme !== "colorful";
  const axes = type === "bar" || type === "horizontal_bar" || type === "line";
  // A choice picked beside a row leaves these options open (ChartMenuStaysOpen).
  return <ChartMenuStaysOpen><div className={styles.options} data-chart-style-options>
    <ChartChoiceRow icon={<ArrowsUpDownStackedIcon size="regular" />} label={t("height")} marker="height" disabled={disabled}
      value={chartHeightOf(config.chartHeight)} options={choices(CHART_HEIGHTS, "heights")}
      onPick={(height) => onChange({ chartHeight: height })} />
    {axes && stacked && <ChartChoiceRow icon={<ListIcon size="regular" />} label={t("groupStyle")} marker="groupStyle" disabled={disabled}
      value={settings.groupStyle} options={choices(CHART_GROUP_STYLES, "groupStyles")}
      onPick={(style) => onChange({ chartGroupStyle: style })} />}
    {axes && <>
      <ChartChoiceRow icon={<DottedLineIcon size="regular" />} label={t("gridLines")} marker="gridLines" disabled={disabled}
        value={settings.gridLines} options={choices(CHART_GRID_LINES, "gridLineChoices")}
        onPick={(lines) => onChange({ chartGridLines: lines })} />
      <ChartChoiceRow icon={<TextXYUnderlineIcon size="regular" />} label={t("axisNames")} marker="axisNames" disabled={disabled}
        value={settings.axisLabels} options={choices(CHART_AXIS_LABELS, "axisNameChoices")}
        onPick={(names) => onChange({ chartAxisLabels: names })} />
    </>}
    {type === "line" && <>
      <ChartSwitchRow icon={<LineCurveIcon size="regular" />} label={t("smoothLine")} marker="smoothLine" disabled={disabled}
        checked={settings.smoothLine} onChange={(on) => onChange({ chartSmoothLine: on })} />
      <ChartSwitchRow icon={<GraphAreaIcon size="regular" />} label={t("lineFill")} marker="lineFill" disabled={disabled}
        checked={!settings.hideLineFill} onChange={(on) => onChange({ chartHideLineFill: !on })} />
    </>}
    {type === "donut" && <>
      <ChartSwitchRow icon={<NumbersIcon size="regular" />} label={t("showValue")} marker="centerValue" disabled={disabled}
        checked={!settings.hideCenterValue} onChange={(on) => onChange({ chartHideCenterValue: on ? undefined : true })} />
      <ChartSwitchRow icon={<TextShortIcon size="regular" />} label={t("showValueLabel")} marker="centerLabel" disabled={disabled}
        checked={!settings.hideCenterLabel} onChange={(on) => onChange({ chartHideCenterLabel: on ? undefined : true })} />
      {!settings.hideCenterLabel && <ChartTextField value={settings.centerLabel} placeholder={t("total")} label={t("showValueLabel")}
        marker="centerLabel" disabled={disabled} onCommit={(label) => onChange({ chartCenterLabel: label || undefined })} />}
      <ChartSwitchRow icon={<HashIcon size="regular" />} label={t("hideNegative")} marker="hideNegative" disabled={disabled}
        checked={settings.hideNegative} onChange={(on) => onChange({ chartHideNegative: on || undefined })} />
    </>}
    {/* Value colors are one color's shades, so only a one-color theme offers them — 색상 없음 too, never 자동 or 다양한 색상 —
        for a donut as for bars, whatever its X axis runs along (Notion's, seen 2026-11-18 in Chrome, two databases: a
        select's, a multi-select's and a date's donut). */}
    {singleColor && type !== "line" && <ChartSwitchRow icon={<HashIcon size="regular" />} label={t("colorByValue")}
      marker="colorByValue" disabled={disabled} checked={config.chartColorByValue === true}
      onChange={(on) => onChange({ chartColorByValue: on })} />}
    {type === "donut"
      ? <ChartChoiceRow icon={<ListIcon size="regular" />} label={t("legendOption")} marker="legend" disabled={disabled}
        value={settings.legend} options={choices(CHART_LEGEND_POSITIONS, "legendPositions")}
        onPick={(position) => onChange({ chartLegendPosition: position, chartLegendHidden: undefined })} />
      : (stacked || type === "line") && <ChartSwitchRow icon={<ListIcon size="regular" />} label={t("legendOption")} marker="legend" disabled={disabled}
        checked={settings.legend !== "off"} onChange={(on) => onChange({ chartLegendHidden: !on, ...(on && config.chartLegendPosition === "off" ? { chartLegendPosition: undefined } : {}) })} />}
    {type === "donut"
      ? <ChartChoiceRow icon={<NumbersIcon size="regular" />} label={t("dataLabels")} marker="dataLabels" disabled={disabled}
        value={settings.donutLabels} options={choices(CHART_DONUT_LABELS, "donutLabelChoices")}
        onPick={(labels) => onChange({ chartDonutLabels: labels })} />
      : <ChartSwitchRow icon={<NumbersIcon size="regular" />} label={t("dataLabels")} marker="dataLabels" disabled={disabled}
        checked={settings.showDataLabels} onChange={(on) => onChange({ chartShowDataLabels: on })} />}
    <ChartSwitchRow icon={<TextJustifyStartIcon size="regular" />} label={t("caption")} marker="caption" disabled={disabled}
      checked={settings.showCaption} onChange={(on) => onChange({ chartShowCaption: on || undefined })} />
    {settings.showCaption && <ChartTextField value={settings.caption} placeholder={t("caption")} label={t("caption")} multiline
      marker="caption" disabled={disabled} onCommit={(caption) => onChange({ chartCaption: caption || undefined })} />}
  </div></ChartMenuStaysOpen>;
}

/**
 * A chart's "색상" menu: 자동, 다양한 색상, then each color with its five
 * shades (seen 2026-10-05), the chosen one unmarked (ChartChoiceList's `marked`).
 */
export function ChartThemeMenu({ onPick }: { onPick(theme: string): void }) {
  const { t } = useTranslation("chartView");
  return <ChartChoiceList label={t("color")} marker="theme" marked={false} onPick={onPick}
    options={CHART_COLOR_THEMES.map((value, index) => ({
      value, label: t(`themes.${value}`), dividerBefore: index === 2,
      trailing: value === "auto" ? undefined : <span className={styles.swatches} aria-hidden="true">
        {chartThemeSwatches(value).map((color, at) => <i key={at} style={{ background: color }} />)}
      </span>,
    }))} />;
}

/**
 * A number chart's "색상", as Notion's (seen 2026-10-05): "동적 색상" off
 * shows its value's ten colors; on, rules — "값 기준 > 0" in a color — the
 * first that holds coloring the value, and "+ 규칙 추가하기". Turning the
 * rules on starts one in green; a rule added after is "기본" (Notion's, seen
 * 2026-11-18 in Chrome, second and third rules alike), its swatch an empty
 * square edged in the border where a color's is filled; taking out the last
 * rule turns the rules off, so the first is always the green one. A rule drags
 * by the ⠿ at its card's start, shown while the pointer is over the card,
 * the cards making way as a menu's list does (Notion's, seen 2026-11-18 in
 * Chrome, two tabs: dragged under another rule, the other's color colors
 * the value). The menu is as wide as the ten colors, or 290 with the rules
 * (ChartSettingsPanel's chartMenuWidth). A rule's "색상" row ends in its
 * swatch, the color's name and a 12px › that turns down while its colors
 * are open, as wide as they are, right under the row and ending with it
 * (Notion's, seen 2026-11-18 in Chrome at 1440: a 186 by 84 box, its mark
 * the sidebar's chevron in the tertiary icon color, 6 past the name). Its
 * "> ⌄" — the sign, then 4 on the sidebar's chevron at 12 in the tertiary
 * icon color, 2 by 4 in, 31.73 by 20.8 — drops its six signs right under it
 * from its start, 180 by 176, a select of its own: "> 큼", the chosen one in
 * the hover's ground, no ✓ (seen 2026-11-18 in Chrome at 1440 dpr 1, two
 * tabs: at the button's 1085.42, its top on the button's foot).
 */
export function ChartNumberColorMenu({ settings, disabled, onChange, onPicked }: {
  settings: ChartSettings; disabled: boolean; onChange(patch: Patch): void;
  /** A letter picked without the rules closes the menu (Notion's, seen 2026-11-18); the switch and the rules keep it open. */
  onPicked?(): void;
}) {
  const { t } = useTranslation(["chartView", "editor"]);
  const [open, setOpen] = useState<{ index: number; what: "operator" | "color"; anchor: PopoverRect } | null>(null);
  const rules = settings.colorRules;
  const setRules = (next: ChartColorRule[]) => onChange({ chartColorRules: next.length ? next : undefined });
  const change = (index: number, patch: Partial<ChartColorRule>) => setRules(rules.map((rule, at) => at === index ? { ...rule, ...patch } : rule));
  // A rule is known by its place in the list while it is dragged.
  const reorder = useDragReorder(`chart-color-rules:${useId()}`, (dragged, target, side) => {
    const moved = rules[Number(dragged)], beside = rules[Number(target)];
    if (!moved || !beside) return;
    const next = rules.filter((rule) => rule !== moved);
    next.splice(next.indexOf(beside) + (side === "after" ? 1 : 0), 0, moved);
    setRules(next);
  }, { live: true });
  const colorName = (color: string) => chartTextColorName(t, color);
  // A letter is drawn in the color it gives, "기본" in the letters' own text color (ChartStyleOptions.module.css),
  // and names it in the dark tip 6 over it, in its middle, as Notion's (read 2026-11-19 in Chrome at 1440, dpr 2).
  const swatches = (current: string, pick: (color: string) => void) => <div className={styles.letters} role="group" aria-label={t("chartView:color")}>
    {CHART_TEXT_COLORS.map((color) => (
      <button key={color} type="button" className={styles.letter} aria-pressed={current === color} aria-label={colorName(color)}
        data-icon-tip={colorName(color)} data-tip-side="top" style={color === "default" ? undefined : { "--chart-letter": chartInkColor(color) } as CSSProperties}
        disabled={disabled} data-chart-text-color={color} onClick={() => pick(color)}><span>A</span></button>
    ))}
  </div>;
  return <div className={styles.numberColor} data-chart-number-color>
    <ChartSwitchRow label={t("chartView:dynamicColor")} marker="dynamicColor" disabled={disabled} checked={settings.dynamicColor}
      onChange={(on) => onChange(on
        ? { chartDynamicColor: true, chartColorRules: rules.length ? rules : [{ operator: ">", value: 0, color: "green" }] }
        : { chartDynamicColor: undefined })} />
    {!settings.dynamicColor ? <>
      <div className={styles.split} role="separator" />
      {swatches(settings.textColor, (color) => { onChange({ chartTextColor: color === "default" ? undefined : color }); onPicked?.(); })}
    </> : <div className={styles.dynamic}>
      <div className={styles.split} role="separator" />
      <div className={styles.rules}>{rules.map((rule, index) => (
        <div key={index} className={styles.rule} data-chart-color-rule={index} {...reorder.row(String(index))}>
          {!disabled && <button type="button" className={styles.ruleHandle} aria-label={t("chartView:moveRule", { index: index + 1 })}
            {...reorder.handle(String(index), true, { above: index > 0 ? String(index - 1) : undefined,
              below: index < rules.length - 1 ? String(index + 1) : undefined })}>
            <span><DragHandleIcon size="tiny" aria-hidden="true" /></span>
          </button>}
          <div className={styles.ruleHead}>
            <span>{t("chartView:ruleValue")}</span>
            <button type="button" className={styles.operator} disabled={disabled} data-chart-rule-operator
              aria-expanded={open?.what === "operator" && open.index === index}
              onClick={(event) => setOpen({ index, what: "operator", anchor: event.currentTarget.getBoundingClientRect() })}>
              <span>{OPERATOR_SIGNS[rule.operator]}</span><SidebarChevronDown size="micro" aria-hidden="true" />
            </button>
            <button type="button" className={styles.trash} aria-label={t("chartView:deleteRule")} disabled={disabled}
              onClick={() => rules.length > 1 ? setRules(rules.filter((_, at) => at !== index))
                : onChange({ chartDynamicColor: undefined, chartColorRules: undefined })}><Trash size="small" /></button>
          </div>
          {/* Emptied, it reads Notion's grey "값" ("Value") until a number goes in (seen 2026-11-18, Chrome 1440); left empty it keeps the rule's number. */}
          <input key={`rule:${rule.value}`} className={styles.input} inputMode="decimal" defaultValue={rule.value} aria-label={t("chartView:ruleValue")}
            placeholder={t("chartView:ruleValuePlaceholder")}
            disabled={disabled} data-chart-rule-value
            onBlur={(event) => { const value = Number(event.currentTarget.value); if (event.currentTarget.value.trim() && Number.isFinite(value)) change(index, { value }); }} />
          <button type="button" className={styles.ruleColor} disabled={disabled} data-chart-rule-color
            aria-expanded={open?.what === "color" && open.index === index}
            onClick={(event) => setOpen({ index, what: "color", anchor: event.currentTarget.getBoundingClientRect() })}>
            <span>{t("chartView:lineColor")}</span>
            <span><i className={styles.swatch} data-plain={rule.color === "default" || undefined}
              style={{ background: rule.color === "default" ? undefined : chartInkColor(rule.color) }} />{colorName(rule.color)}
            <SidebarChevronRight size="micro" aria-hidden="true" /></span>
          </button>
        </div>
      ))}</div>
      <button type="button" className={styles.addRule} disabled={disabled} data-chart-rule-add
        onClick={() => setRules([...rules, { operator: ">", value: 0, color: "default" }])}>
        <Plus size="small" trim aria-hidden="true" />{t("chartView:addRule")}
      </button>
    </div>}
    {open && (
      <ChartPopover anchor={open.anchor} under={open.what === "operator"} label={t("chartView:ruleValue")}
        width={open.what === "operator" ? 180 : "content"} marker={`rule-${open.what}`} onClose={() => setOpen(null)}>
        {open.what === "operator"
          ? <ChartChoiceList label={t("chartView:ruleValue")} marker="rule-operator" value={rules[open.index]?.operator} select
            options={CHART_RULE_OPERATORS.map((operator) => ({ value: operator, sign: OPERATOR_SIGNS[operator],
              label: t(`chartView:operators.${OPERATOR_KEYS[operator]}`) }))}
            onPick={(operator) => { change(open.index, { operator: operator as ChartColorRule["operator"] }); setOpen(null); }} />
          : swatches(rules[open.index]?.color ?? "green", (color) => { change(open.index, { color }); setOpen(null); })}
      </ChartPopover>
    )}
  </div>;
}

/**
 * A number chart's text color by the chart's own name for it, as Notion's: its
 * orange is "오렌지색" where the block colors and the chart themes say "주황색"
 * (read 2026-11-19 in Chrome at 1440, dpr 2: the letters' tips, the "색상"
 * row and a rule's color alike; English "Orange" in all three); the others
 * take the block colors' names, "기본" the chart's own.
 */
export function chartTextColorName(t: TFunction, color: string) {
  return color === "default" ? t("chartView:defaultColor") : t(`chartView:textColors.${color}`, { defaultValue: t(`editor:colors.${color}`) });
}
