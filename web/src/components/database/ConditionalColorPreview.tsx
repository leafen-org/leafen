import { useId, type ReactNode } from "react";
import type { GraphViewType } from "@/contentGraphWire.generated";

/*
 * Over a view's empty conditional colors, a small picture of that layout
 * with tinted items, as Notion shows one for each layout (seen 2026-11-12,
 * 1440 dark: a table's tinted rows, a board's and a gallery's tinted cards,
 * a list's tinted rows, a calendar's tinted cards under three days, a
 * timeline's tinted bars each with its color down its start, a map's pins
 * in three colors, a feed's tinted post). Drawn here, in the app's own
 * tints, with bars for words, not taken from Notion.
 */

const WORDS = "var(--text-default)";
const LINE = "var(--border-default)";
const tint = (color: string) => `var(--c-bg-${color})`;
const strong = (color: string) => `var(--c-${color})`;

/** A word drawn as a bar. */
function Words({ x, y, width, height = 5, shade = 0.55 }: { x: number; y: number; width: number; height?: number; shade?: number }) {
  return <rect x={x} y={y} width={width} height={height} rx={height / 2} fill={WORDS} opacity={shade} />;
}

function Table() {
  const rows = ["green", null, "green", "yellow", "red", "yellow"];
  return <>
    {[30, 110, 184].map((x, column) => <Words key={x} x={x} y={10} width={[22, 24, 34][column]} height={4} shade={0.4} />)}
    {rows.map((color, row) => (
      <g key={row}>
        <rect x={26} y={22 + row * 18} width={230} height={18} fill={color ? tint(color) : "transparent"} />
        <Words x={32} y={29 + row * 18} width={[26, 22, 30, 28, 20, 24][row]} />
        <Words x={106} y={29 + row * 18} width={[22, 30, 22, 22, 22, 26][row]} shade={0.4} />
        <Words x={228} y={29 + row * 18} width={14} shade={0.4} />
        <line x1={26} x2={256} y1={40 + row * 18} y2={40 + row * 18} stroke={LINE} />
      </g>
    ))}
    <line x1={26} x2={256} y1={22} y2={22} stroke={LINE} />
    {[101, 175, 248].map((x) => <line key={x} x1={x} x2={x} y1={4} y2={126} stroke={LINE} />)}
  </>;
}

/** A card: its tint, a name, a line under it, and a mark. */
function Card({ x, y, width, height, color, mark = true }: { x: number; y: number; width: number; height: number; color: string; mark?: boolean }) {
  return <g>
    <rect x={x} y={y} width={width} height={height} rx={5} fill={tint(color)} />
    <Words x={x + 7} y={y + 10} width={46} shade={0.7} />
    <Words x={x + 7} y={y + 22} width={64} shade={0.35} />
    {mark ? <circle cx={x + 10} cy={y + 37} r={3} fill={strong(color)} /> : null}
  </g>;
}

function Board() {
  return <>
    {[25, 170].map((x, column) => (
      <g key={x}>
        <rect x={x} y={21} width={[22, 34][column]} height={9} rx={2} fill="var(--bg-hover)" />
        <Words x={x + 4} y={23.5} width={[14, 26][column]} height={4} shade={0.45} />
      </g>
    ))}
    <Card x={25} y={36} width={130} height={50} color="yellow" />
    <Card x={25} y={92} width={130} height={50} color="green" mark={false} />
    <Card x={170} y={36} width={130} height={50} color="green" />
    <Card x={170} y={92} width={130} height={50} color="red" mark={false} />
  </>;
}

function Gallery() {
  return <>
    {[{ x: 15, color: "yellow" }, { x: 154, color: "green" }].map(({ x, color }) => (
      <g key={x}>
        <rect x={x} y={14} width={130} height={98} rx={6} fill={tint(color)} />
        <rect x={x} y={14} width={130} height={36} rx={6} fill={strong(color)} opacity={0.3} />
        <circle cx={x + 65} cy={32} r={7} fill={strong(color)} />
        <Words x={x + 6} y={58} width={50} shade={0.7} />
        <Words x={x + 6} y={70} width={78} shade={0.35} />
        <rect x={x + 6} y={82} width={44} height={9} rx={2} fill="var(--bg-hover)" />
      </g>
    ))}
  </>;
}

function List() {
  const rows = ["green", null, "green", "yellow", "red"];
  return <>
    {rows.map((color, row) => (
      <g key={row}>
        {color ? <rect x={29} y={16 + row * 19} width={199} height={16} rx={3} fill={tint(color)} /> : null}
        <circle cx={38} cy={24 + row * 19} r={3.5} fill={WORDS} opacity={0.55} />
        <Words x={46} y={21.5 + row * 19} width={[38, 42, 56, 46, 44][row]} />
        <Words x={150} y={21.5 + row * 19} width={[34, 24, 22, 24, 22][row]} shade={0.4} />
        <rect x={190} y={19 + row * 19} width={34} height={10} rx={2} fill={color ? strong(color) : "var(--bg-hover)"} opacity={color ? 0.35 : 1} />
      </g>
    ))}
  </>;
}

/** A calendar's card: its tint, two lines of words, and a tag. */
function DayCard({ x, y, height, color }: { x: number; y: number; height: number; color: string }) {
  return <g>
    <rect x={x} y={y} width={81} height={height} rx={4} fill={tint(color)} />
    <Words x={x + 5} y={y + 7} width={58} shade={0.6} />
    <Words x={x + 5} y={y + 17} width={44} shade={0.6} />
    <rect x={x + 5} y={y + height - 13} width={28} height={9} rx={2} fill={strong(color)} opacity={0.35} />
  </g>;
}

function Calendar() {
  return <>
    <path d={`M0 17H232a6 6 0 0 1 6 6V126`} fill="none" stroke={LINE} />
    {[61, 150].map((x) => <line key={x} x1={x} x2={x} y1={17} y2={126} stroke={LINE} />)}
    {[43, 132, 221].map((x) => <Words key={x} x={x} y={23} width={12} height={4} shade={0.4} />)}
    <DayCard x={-24} y={35} height={31} color="blue" />
    <DayCard x={65} y={35} height={41} color="yellow" />
    <DayCard x={65} y={80} height={41} color="green" />
    <DayCard x={154} y={35} height={41} color="green" />
    <DayCard x={154} y={80} height={41} color="blue" />
  </>;
}

function Timeline() {
  const bars = [
    { x: -6, y: 10, width: 140, color: "yellow", words: 100 },
    { x: 40, y: 31, width: 158, color: "purple", words: 96 },
    { x: 178, y: 53, width: 90, color: "purple", words: 70 },
    { x: 99, y: 75, width: 170, color: "blue", words: 140 },
    { x: 99, y: 96, width: 170, color: "yellow", words: 76 },
  ];
  return <>
    {[59, 98, 197, 237].map((x) => <line key={x} x1={x} x2={x} y1={0} y2={126} stroke={LINE} opacity={0.6} />)}
    {bars.map(({ x, y, width, color, words }, index) => (
      <g key={index}>
        <rect x={x} y={y} width={width} height={17} rx={3} fill={tint(color)} />
        {index > 0 ? <rect x={x + 2} y={y + 3} width={2.5} height={11} rx={1.25} fill={strong(color)} /> : null}
        <Words x={x + 9} y={y + 6} width={words} shade={0.6} />
      </g>
    ))}
  </>;
}

function MapPins() {
  const pin = (x: number, y: number, color: string) => (
    <g key={color}>
      <path d={`M${x} ${y + 15}c-6-7-9-11-9-15a9 9 0 0 1 18 0c0 4-3 8-9 15Z`} fill={strong(color)} />
      <circle cx={x} cy={y} r={3} fill="var(--bg-secondary)" />
    </g>
  );
  return <>
    {["M0 40 C60 50 120 20 256 36", "M30 126 C70 80 150 70 256 96", "M110 0 C120 40 140 80 120 126", "M200 0 C190 50 220 90 256 110"]
      .map((d) => <path key={d} d={d} fill="none" stroke={LINE} strokeWidth={4} />)}
    {pin(63, 62, "purple")}
    {pin(151, 26, "yellow")}
    {pin(198, 64, "blue")}
  </>;
}

function Feed() {
  return <>
    <rect x={25} y={15} width={206} height={130} rx={6} fill={tint("yellow")} />
    <circle cx={35} cy={28} r={4} fill={strong("yellow")} />
    <Words x={43} y={25.5} width={78} shade={0.6} />
    <Words x={31} y={39} width={96} height={8} shade={0.75} />
    {[56, 66, 76, 86].map((y, line) => <Words key={y} x={31} y={y} width={[180, 172, 176, 150][line]} height={4} shade={0.4} />)}
    {[31, 59].map((x) => <rect key={x} x={x} y={100} width={24} height={11} rx={5.5} fill="var(--bg-hover)" />)}
  </>;
}

const PICTURES: Partial<Record<GraphViewType, () => ReactNode>> = {
  table: Table, board: Board, gallery: Gallery, list: List, calendar: Calendar, timeline: Timeline, map: MapPins, feed: Feed,
};

export function ConditionalColorPreview({ className, layout }: { className?: string; layout: GraphViewType }) {
  const picture = PICTURES[layout] ?? Table;
  // A wider box, as a phone's sheet, shows the picture alone, cut at its edges.
  const clip = `conditional-color-clip-${useId().replace(/[^a-zA-Z0-9_-]/g, "")}`;
  return (
    <svg className={className} viewBox="0 0 256 126" role="presentation" aria-hidden="true" data-conditional-color-preview={layout}>
      <clipPath id={clip}><rect width={256} height={126} /></clipPath>
      <g clipPath={`url(#${clip})`}>{picture()}</g>
    </svg>
  );
}
