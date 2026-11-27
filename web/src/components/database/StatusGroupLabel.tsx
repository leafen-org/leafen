import type { CSSProperties, ReactNode } from "react";
import { StatusCompleteIcon, StatusInProgressIcon, StatusToDoIcon } from "@/icons/leafen";
import { chipStyle } from "./colors";
import type { StatusStage } from "./propertyStatusPolicy";
import styles from "./StatusGroupLabel.module.css";

const MARKS = { todo: StatusToDoIcon, inProgress: StatusInProgressIcon, complete: StatusCompleteIcon };

const accent = (color?: string) => `var(--c-accent-${color && color !== "default" ? color : "gray"})`;

/**
 * A status group's name where a view shows a status by its groups, as
 * Notion's table, board and grouping settings do (seen 2026-10-24 at 1280
 * and 375px): no chip, the group's 20px mark and then its name, both in the
 * group's colour, weight 500.
 */
export function StatusGroupLabel({ stage, color, label }: { stage: StatusStage; color?: string; label: string }) {
  const Mark = MARKS[stage];
  return (
    <span className={styles.label} style={{ "--status-group-color": accent(color) } as CSSProperties} data-status-group={stage}>
      <Mark size="regular" aria-hidden="true" />
      <span className={styles.name}>{label}</span>
    </span>
  );
}

/**
 * A status option's group title, as Notion's (seen 2026-10-24): a 20px pill
 * with an 8px dot in the option's colour 7px in, the name 5px after it.
 */
function StatusOptionChip({ color, label }: { color?: string; label: string }) {
  return (
    <span className={styles.option} style={{ ...chipStyle(color), "--status-group-color": accent(color) } as CSSProperties}
      data-status-option-chip="">{label}</span>
  );
}

/** A group's title when it is a status's group or option, else null for the caller's own. */
export function statusGroupTitle(group: { statusStage?: StatusStage; statusOption?: boolean; color?: string }, label: string): ReactNode {
  if (group.statusStage) return <StatusGroupLabel stage={group.statusStage} color={group.color} label={label} />;
  if (group.statusOption) return <StatusOptionChip color={group.color} label={label} />;
  return null;
}
