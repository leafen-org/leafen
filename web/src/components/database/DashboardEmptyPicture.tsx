import type { ReactNode } from "react";
import { IconSvg } from "@/icons/leafen/iconPrimitives";
import styles from "./DatabaseDashboardWidgets.module.css";

// The five marks over an empty dashboard's "+ 조회수 보기" in Edit mode, as
// Notion's (seen 2026-11-07 and 2026-11-12, measured off its 1440 picture):
// a timeline, three bars with no base line, a pie with one slice cut, a
// table and a line over an L of axes, 32 boxes 24 apart, their lines 2px.
const EMPTY_MARKS: readonly { glyph: string; marks: ReactNode }[] = [
  { glyph: "dashboard-empty-timeline", marks: <>
    <rect x="4.5" y="6" width="11" height="8" rx="1.5" />
    <path d="M6.5 8h4.1M7.75 10h4.1M9 12h4.1" />
  </> },
  { glyph: "dashboard-empty-bars", marks: <>
    <rect x="5" y="7" width="2" height="8" rx="1" />
    <rect x="9" y="10.5" width="2" height="4.5" rx="1" />
    <rect x="13" y="5" width="2" height="10" rx="1" />
  </> },
  { glyph: "dashboard-empty-pie", marks: <>
    <circle cx="10" cy="10" r="5.5" />
    <path d="M10 4.5V10l4.72-2.82M10 10l4.55 3.09" strokeLinecap="butt" />
  </> },
  { glyph: "dashboard-empty-table", marks: <>
    <rect x="4.5" y="6" width="11" height="8" rx="1.5" />
    <path d="M4.5 8.5h11M4.5 11.5h11M10 6v8" />
  </> },
  { glyph: "dashboard-empty-line", marks: <>
    <path d="M5 5.5V15h10" />
    <path d="M5 11.2 8 8.5l3 2.9 4-4.2" />
  </> },
];

/** The marks of the first widget's place on an empty dashboard in Edit mode. */
export function DashboardEmptyMarks({ className }: { className?: string }) {
  return (
    <span className={className} aria-hidden="true">
      {EMPTY_MARKS.map(({ glyph, marks }) => <IconSvg key={glyph} glyph={glyph} size="card" line={1}>{marks}</IconSvg>)}
    </span>
  );
}

// Three faint widget cards fanned out under an empty dashboard's "대시보드
// 편집": a list tipped back on the left, a gauge upright in the middle and
// a table tipped forward on the right, fading out at their feet. Leafen's
// own drawing in the place, size and role of Notion's picture
// (Notion's seen 2026-11-07 at 1440: three cards 268×96 under the button,
// the middle one 100 wide and level, the others turned 10° away from it;
// its top 60 under the button and as faint as Notion's in both themes).

const CARD = { width: 100, height: 120, radius: 10 };
const LIST_BARS = [54, 36, 24];

function Card({ x, y, turn, children }: { x: number; y: number; turn: number; children: ReactNode }) {
  const cx = x + CARD.width / 2, cy = y + CARD.height / 2;
  return (
    <g transform={`rotate(${turn} ${cx} ${cy})`}>
      <rect x={x} y={y} width={CARD.width} height={CARD.height} rx={CARD.radius}
        style={{ fill: "var(--bg-app)" }} stroke="currentColor" strokeWidth={1.5} />
      {/* Its name, a short line at its head. */}
      <rect x={x + 12} y={y + 12} width={26} height={4} rx={2} fill="currentColor" />
      {children}
    </g>
  );
}

/** The picture under an empty dashboard out of Edit mode. */
export function DashboardEmptyPicture() {
  // The gauge's arc: three quarters of its ring, from its top round to the left.
  const r = 22, ring = 2 * Math.PI * r;
  return (
    <svg className={styles.emptyPicture} width={272} height={96} viewBox="0 0 272 96" aria-hidden="true" focusable="false">
      <Card x={18} y={20} turn={-10}>
        {LIST_BARS.map((width, row) => (
          <g key={row}>
            <rect x={28} y={48 + row * 16} width={80} height={12} rx={3} fill="none" stroke="currentColor" strokeWidth={1} />
            <rect x={31} y={51 + row * 16} width={width} height={6} rx={2} fill="currentColor" />
          </g>
        ))}
      </Card>
      <Card x={154} y={20} turn={10}>
        {[0, 1].map((row) => [0, 1, 2].map((column) => (
          <rect key={`${row}:${column}`} x={164 + column * 28} y={46 + row * 24} width={24} height={20} rx={3}
            fill="none" stroke="currentColor" strokeWidth={1} />
        )))}
      </Card>
      <Card x={85} y={2} turn={0}>
        <circle cx={136} cy={60} r={r} fill="none" stroke="currentColor" strokeWidth={5} opacity={0.4} />
        <circle cx={136} cy={60} r={r} fill="none" stroke="currentColor" strokeWidth={5} strokeLinecap="round"
          strokeDasharray={`${ring * 0.72} ${ring}`} transform="rotate(-90 136 60)" />
        <text x={136} y={65} textAnchor="middle" fontSize={15} fontWeight={600} fill="currentColor">72</text>
      </Card>
    </svg>
  );
}
