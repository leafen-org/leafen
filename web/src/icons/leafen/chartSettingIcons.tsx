import { IconSvg, type LeafenIconProps } from "./iconPrimitives";

// The marks of a chart's settings rows, drawn here on Notion's 20 grid with
// its 1.25 lines so each stands where Notion inks its own (seen 2026-11-18
// at 1440 in a chart view's panel and its style options, each mark's inked
// box read at 20px and laid beside Notion's at 120px): what an axis shows,
// how it groups, its range, its reference lines, its colors, what a donut
// splits by, and the style options' axis names, curve, area, label and caption.

/** A category axis's "표시 대상": a line down from the top start, turning to an arrow toward the end. */
export const ArrowTurnDownRightIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="arrow-turn-down-right" strokeWidth="1.25">
    <path d="M3.75 4.75v4.5a2 2 0 0 0 2 2h11.5" />
    <path d="m12.85 6.85 4.4 4.4-4.4 4.4" />
  </IconSvg>
);

/** A value axis's "표시 대상": a line from the bottom end toward the start, turning to an arrow upward. */
export const ArrowTurnLeftUpIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="arrow-turn-left-up" strokeWidth="1.25">
    <path d="M15.25 16.25h-4.5a2 2 0 0 1-2-2V2.75" />
    <path d="m4.35 7.15 4.4-4.4 4.4 4.4" />
  </IconSvg>
);

/** A value axis's "그룹화": an upright box split across its middle, one bar in two parts. */
export const SplitRectangleIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="rectangle-split" strokeWidth="1.25">
    <rect x="4.75" y="3" width="10.5" height="14" rx="2" />
    <path d="M4.75 10h10.5" />
  </IconSvg>
);

/** "범위" and "높이": an arrow up over an arrow down on one line, a gap between them. */
export const ArrowsUpDownStackedIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="arrows-up-down-stacked" strokeWidth="1.25">
    <path d="M10 8.6V1.45M6.47 4.95 10 1.42l3.53 3.53" />
    <path d="M10 11.4v7.15M6.47 15.05 10 18.58l3.53-3.53" />
  </IconSvg>
);

/** "기준선" and "격자선": a dotted line, four dots across. */
export const DottedLineIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="dotted-line" fill="currentColor" stroke="none">
    <circle cx="4.75" cy="10" r="1" />
    <circle cx="8.25" cy="10" r="1" />
    <circle cx="11.75" cy="10" r="1" />
    <circle cx="15.25" cy="10" r="1" />
  </IconSvg>
);

/** "색상": a round palette, its lower start side taken out to a thumb's hollow, four dabs of paint round its upper side. */
export const PaletteIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="palette" strokeWidth="1.25">
    <path d={"M11.4 16.68C10.5 16.2 10.55 15.4 10.8 14.6 11 13.8 10.75 12.3 9.4 12.15 8.4 12.05 7.6 12.3 6.4 12.25"
      + " 5 12.2 3.6 11.2 3.23 9.3A6.9 6.9 0 1 1 11.4 16.68Z"} />
    <g fill="currentColor" stroke="none">
      <circle cx="7.1" cy="7.1" r="1" />
      <circle cx="10" cy="5.9" r="1" />
      <circle cx="12.9" cy="7.1" r="1" />
      <circle cx="14.1" cy="10" r="1" />
    </g>
  </IconSvg>
);

/** A donut's "분할 기준": a pie with its upper end quarter cut out and set apart. */
export const ChartPieCutoutIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="chart-pie-cutout" strokeWidth="1.25" overflow="visible">
    <path d="M10 3a7 7 0 1 0 7 7h-7Z" />
    <path d="M12.25.75a7 7 0 0 1 7 7h-7Z" />
  </IconSvg>
);

/** "축 이름": "XY" over a dotted line. */
export const TextXYUnderlineIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="text-xy-underline" strokeWidth="1.25">
    <path d="M3.4 4.75 9.2 12.4M9.2 4.75 3.4 12.4M10.9 4.75l3.1 4.3 3.1-4.3M14 9.05v3.35" />
    <g fill="currentColor" stroke="none">
      {[0, 1, 2, 3, 4, 5, 6, 7, 8].map((dot) => <circle key={dot} cx={2.25 + dot * 1.94} cy="15.25" r="0.62" />)}
    </g>
  </IconSvg>
);

/** A line chart's "곡선": a line that dips and rises in one smooth wave. */
export const LineCurveIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="line-curve" strokeWidth="1.25">
    <path d="M3.15 7C4.2 12.5 5.2 16.2 6.9 16.2c2.6 0 3.4-12.4 6.3-12.4 1.6 0 2.6 4.6 3.65 9.3" />
  </IconSvg>
);

/** A line chart's "그라데이션 영역": the ground under a rising line, closed along the bottom. */
export const GraphAreaIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="graph-area" strokeWidth="1.25">
    <path d={"M4.5 15.25h11a1.5 1.5 0 0 0 1.5-1.5V6.8a1.5 1.5 0 0 0-2.56-1.06L9.9 10.2 7.3 7.9 3.44 11.3"
      + "A1.3 1.3 0 0 0 3 12.3v1.45a1.5 1.5 0 0 0 1.5 1.5Z"} />
  </IconSvg>
);

/** "캡션": four lines of text from the start, the last one short. */
export const TextJustifyStartIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="text-justify-start" strokeWidth="1.25">
    <path d="M2.9 5h14.2M2.9 8.33h14.2M2.9 11.67h14.2M2.9 15h7" />
  </IconSvg>
);

/** A donut's "값 라벨 표시": a short text, one line and a shorter one. */
export const TextShortIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="text-short" strokeWidth="1.25">
    <path d="M2.9 8.33h14.2M2.9 11.67h9.8" />
  </IconSvg>
);

// A chart's kind, as Notion's chart type buttons and "/" items draw it (seen
// 2026-11-18 at 1440 in both, one glyph each: chartBarXAxis, chartBarYAxis,
// chartLine, donutChart, number), drawn here on its 20 grid with 1.25 lines
// over where Notion's ink lies, read at 400px (overlap 0.95 and more), not
// from Notion's paths: three outlined bars with no axis, 10, 6 and 12.5
// long, standing up or lying from the start; axes with a line that rises,
// falls and rises; a ring with its upper end quarter cut away and outlined
// apart; a "#" whose bars lean.

/** A vertical bar chart: three bars standing on one foot, the last the tallest. */
export const BarChartIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="chart-bar-vertical" strokeWidth="1.25">
    <rect x="3.75" y="6.25" width="2.5" height="10" rx="0.6" />
    <rect x="8.75" y="10.25" width="2.5" height="6" rx="0.6" />
    <rect x="13.75" y="3.75" width="2.5" height="12.5" rx="0.6" />
  </IconSvg>
);

/** A horizontal bar chart: three bars from the start, the last the longest. */
export const HorizontalBarChartIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="chart-bar-horizontal" strokeWidth="1.25">
    <rect x="3.75" y="3.75" width="10" height="2.5" rx="0.6" />
    <rect x="3.75" y="8.75" width="6" height="2.5" rx="0.6" />
    <rect x="3.75" y="13.75" width="12.5" height="2.5" rx="0.6" />
  </IconSvg>
);

/** A line chart: two axes meeting round at the bottom start, a line rising, falling and rising off the upright one. */
export const LineChartIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="chart-line" strokeWidth="1.25">
    <path d="M3.75 3.75v10.9a1.6 1.6 0 0 0 1.6 1.6h10.9" />
    <path d="M3.75 11.95 7.68 8.03l3.6 3.6 4.97-4.98" />
  </IconSvg>
);

/** A donut chart: a ring's outer and inner edges round three quarters, its upper end quarter outlined apart. */
export const DonutChartIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="chart-donut" strokeWidth="1.25">
    <path d="M10 3a7 7 0 0 1 7 7h-3.5A3.5 3.5 0 0 0 10 6.5Z" />
    <path d="M8.15 3.25A7 7 0 1 0 16.72 11.96M8.15 7.03A3.5 3.5 0 1 0 12.9 11.96" />
  </IconSvg>
);

/** A number chart: a "#" whose upright bars lean forward, the upper cross bar set further toward the end. */
export const NumberChartIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="chart-number" strokeWidth="1.25">
    <path d="M8.75 3.7 6.5 16.3M13.43 3.7 11.2 16.3M4.9 7.9h10.98M4.12 12.1h11" />
  </IconSvg>
);
