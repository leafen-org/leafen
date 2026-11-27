import type { CSSProperties } from "react";
import type { GraphProperty } from "@/contentGraph";
import { formatNumberValue, numberDisplayForProperty, numberDisplayFraction } from "./numberFormat";
import styles from "./NumberValueDisplay.module.css";

const RING_LENGTH = 2 * Math.PI * 6;

/**
 * A number property's value as the property shows it, as Notion's table
 * does (seen 2026-09-30): the number alone; the number with a bar filling the
 * rest of the cell to its share of "divide by"; or the number with a small
 * ring at the end. The bar and ring take the property's color.
 */
export function NumberValueDisplay({ value, property }: {
  value: unknown;
  property: Pick<GraphProperty, "config">;
}) {
  const display = numberDisplayForProperty(property);
  const text = formatNumberValue(value, display.format, display.decimals);
  if (display.kind === "number") return <span data-number-text>{text}</span>;
  const empty = value == null || value === "";
  const fraction = numberDisplayFraction(value, display.divideBy);
  const color = { "--number-display-color": `var(--c-accent-${display.color})` } as CSSProperties;
  return (
    <span className={styles.display} data-number-display={display.kind} style={color}>
      {display.showValue && <span className={styles.value}>{text}</span>}
      {!empty && display.kind === "bar" && (
        <span className={styles.bar} role="presentation">
          <span className={styles.barFill} style={{ width: `${fraction * 100}%` }} />
        </span>
      )}
      {!empty && display.kind === "ring" && (
        <svg className={styles.ring} width={21} height={21} viewBox="0 0 14 14" aria-hidden="true">
          <path className={styles.ringTrack} d="M 7 0 A 7 7 0 1 1 7 14 A 7 7 0 1 1 7 0 M 7 2 A 5 5 0 1 0 7 12 A 5 5 0 1 0 7 2 Z" />
          <circle className={styles.ringFill} cx={7} cy={7} r={6} fill="none" strokeWidth={2} strokeLinecap="round"
            strokeDasharray={RING_LENGTH} strokeDashoffset={RING_LENGTH * (1 - fraction)} />
        </svg>
      )}
    </span>
  );
}
