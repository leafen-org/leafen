/**
 * The colors Notion's charts draw with (seen 2026-11-12, read from its
 * bars, slices and lines in light and dark): each option color's tone, the
 * same on either page; a one-color theme's five shades for stacked groups;
 * the colorful cycle groups without a color of their own take by their
 * place; the pale gray of a group without a value; and a line's own blue.
 */

const TONES: Readonly<Record<string, string>> = {
  gray: "199, 198, 196",
  brown: "189, 149, 118",
  orange: "222, 146, 85",
  yellow: "234, 194, 107",
  green: "114, 188, 143",
  blue: "94, 159, 232",
  purple: "191, 142, 218",
  teal: "79, 185, 201",
  pink: "223, 132, 168",
  red: "233, 115, 102",
};
const SHADES = [1, 0.7, 0.5, 0.3, 0.15];
const COLORFUL = ["blue", "yellow", "green", "purple", "orange", "pink", "teal", "red"];
/** The stable color of a group without an option color, before sorting. */
export function chartAutomaticGroupColor(index: number) {
  return COLORFUL[index % COLORFUL.length];
}
/** The pale fill of a group without a value; dark themes redefine it. */
const CHART_EMPTY_FILL = "var(--chart-empty-fill)";
/** A group of the default color's: dark themes redefine it, as Notion's near-white bar (seen 2026-11-05). */
const CHART_DEFAULT_FILL = "var(--chart-default-fill)";
/** A line under "자동", Notion's own blue rather than the bars' (seen 2026-11-12: #2383E2 on either page). */
const CHART_LINE_BLUE = "rgb(35, 131, 226)";

/** The chart color themes, as Notion's color menu lists them. */
export const CHART_COLOR_THEMES = ["auto", "colorful", "gray", "blue", "yellow", "green", "purple", "teal", "orange", "pink", "red"] as const;

export function chartTone(color: string | undefined, alpha = 1) {
  const tone = color ? TONES[color] : undefined;
  if (!tone) return CHART_EMPTY_FILL;
  return alpha === 1 ? `rgb(${tone})` : `rgba(${tone}, ${alpha})`;
}

// A one-color theme's tone: the option color's, but "회색" the page's dark gray or near white
// rather than the gray option's (Notion's, seen 2026-11-12: 98, 97, 93 light, 227, 226, 224 dark).
function themeTone(theme: string, alpha = 1) {
  return theme === "gray" ? `rgba(var(--chart-gray-theme), ${alpha})` : chartTone(theme, alpha);
}

/** A theme's five swatches, as its menu row shows them. */
export function chartThemeSwatches(theme: string) {
  if (theme === "colorful") return COLORFUL.slice(0, 5).map((color) => chartTone(color));
  return SHADES.map((alpha) => themeTone(theme, alpha));
}

function themeColor(theme: unknown) {
  // Brown, kept by charts made before Notion's themes were followed, stays brown.
  return typeof theme === "string" && (TONES[theme] !== undefined) ? theme : undefined;
}

/**
 * The fill of each of a chart's groups by its theme: its option's color (or
 * the colorful cycle where it has none) under "auto", the colorful cycle, or
 * one color — stacked groups in that color's shades by their place, the
 * group without a value pale and taking its place (seen 2026-11-12).
 */
export function chartGroupFills(groups: readonly { key: string; color?: string; empty?: boolean }[], theme: unknown, stacked: boolean) {
  const single = themeColor(theme);
  return groups.map((group, index) => {
    if (single && !stacked) return themeTone(single);
    if (group.empty) return CHART_EMPTY_FILL;
    if (single) return themeTone(single, SHADES[index % SHADES.length]);
    if (theme === "colorful") return chartTone(COLORFUL[index % COLORFUL.length]);
    if (group.color === "default") return CHART_DEFAULT_FILL;
    return group.color && TONES[group.color] ? chartTone(group.color) : chartTone(COLORFUL[index % COLORFUL.length]);
  });
}

/** A single line's color: Notion's own blue under "자동", the cycle's first under "다양한 색상", else its theme's. */
export function chartLineColor(theme: string) {
  return theme === "auto" ? CHART_LINE_BLUE : theme === "colorful" ? chartTone("blue") : themeTone(theme);
}

/**
 * The tone of a number chart's value and of a color rule's swatch, as
 * Notion's (read 2026-11-18 in Chrome at 1440, dark and light, from its ten
 * letters, the value they color and a rule's swatch): "default" is the text
 * color and every other the charts' own table the reference lines take,
 * "회색" being their "검정색" — dark green 45, 153, 100 where a text's green
 * is 80, 148, 110 — on either page.
 */
export function chartInkColor(color: string) {
  return color === "default" ? "var(--text-default)" : chartReferenceLineColor(color === "gray" ? "default" : color);
}

/** A reference line's tone and its swatch's, Notion's own for each of its colors on either page ("default" its "검정색"). */
export function chartReferenceLineColor(color: string) {
  return `var(--chart-line-${color})`;
}
