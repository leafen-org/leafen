import { useId, useMemo, useState, type PointerEvent } from "react";
import { useTranslation } from "react-i18next";
import { chosenTimeZone } from "@/lib/dateReminders";
import { activeDateLocale, activeNumberLocale } from "@/lib/i18n";
import type { PageAnalyticsDay } from "@/pageAnalyticsWire.generated";
import styles from "./PageAnalyticsPanel.module.css";

// A page's views by day, as Notion's analytics tab draws them (seen
// 2026-10-18): total views a blue area, unique views a yellow one, every day
// to today since the page was made (seen 2026-11-05: a new page's one day
// alone, under its date), a day without views at 0. A view is on its viewer's
// day, as Notion keeps them (seen 2026-11-26: views at 03:47 in Seoul came back
// on the 26th), and the range ends on the reader's today, the one the server
// read it to; a page's first day is its making on the reader's clock. Notion's
// chart lays its days out by UTC instead, so before 09:00 in Seoul its last day
// is yesterday and today's views are in the total but on no day (seen
// 2026-11-26; the 2026-11-05 "a view at 7:00 fell on the 4th" was that last
// day, at 0); Leafen's draws them on today.

const WIDTH = 353, HEIGHT = 175;
const LEFT = 28, RIGHT = 6, TOP = 8, BOTTOM = 22;

type Point = { day: string; total: number; unique: number };

/** A moment's day on the reader's clock, "YYYY-MM-DD". */
function dayOnClock(at: Date) {
  try {
    const parts = Object.fromEntries(new Intl.DateTimeFormat("en-CA", {
      timeZone: chosenTimeZone() || undefined, year: "numeric", month: "2-digit", day: "2-digit",
    }).formatToParts(at).map(({ type, value }) => [type, value]));
    return `${parts.year}-${parts.month}-${parts.day}`;
  } catch {
    return at.toISOString().slice(0, 10);
  }
}

/** The days of the range ending on the reader's today, as the server names them. */
function rangeDays(days: readonly PageAnalyticsDay[], count: number, today?: string, made?: string): Point[] {
  const madeDay = made ? dayOnClock(new Date(made)) : "";
  const byDay = new Map(days.map((day) => [day.day, day]));
  const last = dayDate(today || dayOnClock(new Date()));
  const points: Point[] = [];
  for (let back = count - 1; back >= 0; back--) {
    const date = new Date(Date.UTC(last.getUTCFullYear(), last.getUTCMonth(), last.getUTCDate() - back));
    const day = date.toISOString().slice(0, 10);
    if (day < madeDay && back > 0) continue;
    const views = byDay.get(day);
    points.push({ day, total: views?.totalViews ?? 0, unique: views?.uniqueViews ?? 0 });
  }
  return points;
}

/** Ticks of 1, 2 or 5 times a power of ten, at most five above 0. */
function niceStep(most: number) {
  for (let power = 1; ; power *= 10) {
    for (const step of [1, 2, 5]) if (most / (step * power) <= 5) return step * power;
  }
}

function dayDate(day: string) {
  const [year, month, date] = day.split("-").map(Number);
  return new Date(Date.UTC(year, month - 1, date));
}

export function PageAnalyticsChart({ days, range, today, made }: {
  days: readonly PageAnalyticsDay[];
  range: number;
  /** The reader's today the range ends on, as the server read it. */
  today?: string;
  /** When the page was made, which its days start from. */
  made?: string;
}) {
  const { t } = useTranslation("pageAnalytics");
  const clipId = useId();
  const points = useMemo(() => rangeDays(days, range, today, made), [days, range, today, made]);
  const [hover, setHover] = useState<number | null>(null);
  const most = Math.max(1, ...points.map((point) => point.total));
  const step = niceStep(most);
  // The axis ends at the most views, its ticks the round ones under it, as
  // Notion's (seen 2026-11-07: 7 views, ticks 2·4·6 and the axis to 7).
  const top = most;
  const innerWidth = WIDTH - LEFT - RIGHT, innerHeight = HEIGHT - TOP - BOTTOM;
  const x = (index: number) => LEFT + (points.length === 1 ? innerWidth / 2 : index * innerWidth / (points.length - 1));
  const y = (value: number) => TOP + innerHeight - value / top * innerHeight;
  const line = (value: (point: Point) => number) => points.map((point, index) => `${index ? "L" : "M"}${x(index).toFixed(1)},${y(value(point)).toFixed(1)}`).join("");
  const area = (value: (point: Point) => number) => `${line(value)}L${x(points.length - 1).toFixed(1)},${y(0)}L${x(0).toFixed(1)},${y(0)}Z`;
  const ticks: number[] = [];
  for (let value = step; value <= top; value += step) ticks.push(value);
  // Three dates, the last a few days before today, as Notion's axis.
  const every = Math.max(1, Math.round(points.length * 6 / 28));
  const labels = [0, 1, 2].map((index) => points.length - 1 - (every - 1) - index * every).filter((index) => index >= 0);
  const dateLocale = activeDateLocale(), numberLocale = activeNumberLocale();
  const axisDate = new Intl.DateTimeFormat(dateLocale, { month: "short", day: "numeric", timeZone: "UTC" });
  const tipDate = new Intl.DateTimeFormat(dateLocale, { year: "numeric", month: "long", day: "numeric", timeZone: "UTC" });
  const total = points.reduce((sum, point) => sum + point.total, 0);

  function pointAt(event: PointerEvent<SVGRectElement>) {
    const box = event.currentTarget.ownerSVGElement?.getBoundingClientRect();
    if (!box || points.length === 0) return;
    const at = (event.clientX - box.left) / box.width * WIDTH;
    const index = points.length === 1 ? 0 : Math.round((at - LEFT) / innerWidth * (points.length - 1));
    setHover(Math.min(points.length - 1, Math.max(0, index)));
  }

  const shown = hover === null ? null : points[hover];
  return (
    <div className={styles.chart} data-page-analytics-chart>
      <svg viewBox={`0 0 ${WIDTH} ${HEIGHT}`} role="img" aria-label={t("chart.label", { count: total })}>
        <defs><clipPath id={clipId}><rect x={LEFT} y={TOP - 1} width={innerWidth} height={innerHeight + 2} /></clipPath></defs>
        {ticks.map((value) => (
          <g key={value}>
            <line className={styles.chartAxis} x1={LEFT - 5} x2={LEFT} y1={y(value)} y2={y(value)} />
            <text className={styles.chartTick} x={LEFT - 8} y={y(value)} textAnchor="end" dominantBaseline="middle">
              {value.toLocaleString(numberLocale)}
            </text>
          </g>
        ))}
        <line className={styles.chartAxis} x1={LEFT} x2={LEFT} y1={y(top)} y2={y(0)} />
        {labels.map((index) => (
          <text key={index} className={styles.chartTick} x={x(index)} y={HEIGHT - 6}
            textAnchor={index === points.length - 1 ? "end" : "middle"} /* the last day ends at the chart's edge, inside the panel */>
            {axisDate.format(dayDate(points[index].day))}
          </text>
        ))}
        <line className={styles.chartAxis} x1={LEFT} x2={WIDTH - RIGHT} y1={y(0)} y2={y(0)} />
        {points.length === 1 ? (
          // One day alone is a line up from 0 at its date, as Notion's.
          <g>
            <line className={styles.chartTotalLine} x1={x(0)} x2={x(0)} y1={y(0)} y2={y(points[0].total)} />
            <line className={styles.chartUniqueLine} x1={x(0)} x2={x(0)} y1={y(0)} y2={y(points[0].unique)} />
          </g>
        ) : (
          <g clipPath={`url(#${clipId})`}>
            <path className={styles.chartTotalArea} d={area((point) => point.total)} />
            <path className={styles.chartTotalLine} d={line((point) => point.total)} />
            <path className={styles.chartUniqueArea} d={area((point) => point.unique)} />
            <path className={styles.chartUniqueLine} d={line((point) => point.unique)} />
          </g>
        )}
        {shown && hover !== null ? <>
          <line className={styles.chartCross} x1={x(hover)} x2={x(hover)} y1={TOP} y2={y(0)} />
          <line className={styles.chartCross} x1={LEFT} x2={WIDTH - RIGHT} y1={y(shown.total)} y2={y(shown.total)} />
          <circle className={styles.chartDot} cx={x(hover)} cy={y(shown.total)} r={5} />
        </> : null}
        <rect x={LEFT} y={TOP} width={innerWidth} height={innerHeight} fill="transparent"
          onPointerMove={pointAt} onPointerDown={pointAt} onPointerLeave={() => setHover(null)} />
      </svg>
      {shown && hover !== null ? (
        <div className={styles.chartTip} role="status"
          style={{ left: `${Math.min(70, Math.max(0, x(hover) / WIDTH * 100))}%`, top: `${y(shown.total) / HEIGHT * 100}%` }}>
          <div>{t("chart.on", { date: tipDate.format(dayDate(shown.day)) })}</div>
          <div><span className={styles.chartKeyTotal} aria-hidden="true" />{t("chart.total", { count: shown.total })}</div>
          <div><span className={styles.chartKeyUnique} aria-hidden="true" />{t("chart.unique", { count: shown.unique })}</div>
        </div>
      ) : null}
    </div>
  );
}
