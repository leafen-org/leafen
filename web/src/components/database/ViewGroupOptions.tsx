import { useEffect } from "react";
import type { GraphProperty } from "@/contentGraph";
import type { PopoverRect } from "@/lib/popoverPlacement";
import type { SelectOption } from "@/lib/types";
import { GroupRename } from "./GroupNameField";
import { GroupOptionsMenu } from "./GroupOptionsMenu";
import { optionNamed } from "./optionNameText";
import { tableConfigAfterGroupShown, tableGroupOptionIds } from "./tableGroupSettingsModel";

/** A group's "⋯" pressed, or its name chip (`naming`): an option's group renamed in place (GroupRename). */
export type ViewGroupMenuAt = { group: { key: string; label?: string; color?: string; statusStage?: unknown; rows: ReadonlyArray<{ row: { id: string } }> };
  position: PopoverRect; returnTo: HTMLElement; naming?: boolean };

/**
 * A table's or a list's group "⋯": the board's group menu, as Notion's table
 * and list have it (seen 2026-10-25 at 1280px) — its counts shown or hidden
 * for every group of the view at once, and the group without a value with
 * neither colors nor "휴지통으로 이동" — or, from its name chip, the option's
 * name window (GroupRename), its options written as `onProperty` writes them.
 */
export function ViewGroupOptions({ at, property, config, structureLocked, onConfig, onProperty, onTrash, onOpenSettings, onClose }: {
  at: ViewGroupMenuAt; property: GraphProperty; config: Readonly<Record<string, unknown>>; structureLocked: boolean;
  onConfig(next: Record<string, unknown>): void;
  onProperty(next: GraphProperty): void;
  onTrash?(rowIds: readonly string[]): void;
  onOpenSettings?(trigger: HTMLElement): void;
  onClose(): void;
}) {
  const { group } = at;
  // Its name chip renames an option's group in place; one of no option of its own, or a locked database's, opens nothing (Notion's, seen 2026-11-28).
  const nameable = at.naming === true && !structureLocked && group.key !== "__no_group__" && optionNamed(property.config, group.key);
  useEffect(() => { if (at.naming && !nameable) onClose(); }, [at.naming, nameable, onClose]);
  if (at.naming) {
    return nameable ? <GroupRename property={property} optionId={group.key} name={group.label ?? group.key} returnTo={at.returnTo} onClose={onClose}
      onRename={(options) => onProperty({ ...property, config: { ...(property.config as object), options } } as GraphProperty)} /> : null;
  }
  const counts = config.showGroupCounts === true;
  const valued = group.key !== "__no_group__";
  // A status's groups offer no colors, a select's do (Notion's, seen 2026-11-06).
  const option = valued && property.type !== "status" && tableGroupOptionIds(property.config).includes(group.key);
  const rowIds = group.rows.map((entry) => entry.row.id);
  const then = (act: () => void) => () => { onClose(); act(); };
  const recolor = (color: string) => {
    const config = property.config as Record<string, unknown>;
    const options = Array.isArray(config.options) ? config.options as Array<{ id: string }> : [];
    onClose();
    onProperty({ ...property, config: { ...config, options: options.map((candidate) => candidate.id === group.key ? { ...candidate, color, colorFromGroup: undefined } : candidate) } } as GraphProperty);
  };
  return (
    <GroupOptionsMenu name={group.label ?? group.key} position={at.position} returnTo={at.returnTo} countShown={counts}
      structureLocked={structureLocked}
      color={(property.config as { options?: SelectOption[] }).options?.find((candidate) => candidate.id === group.key)?.colorFromGroup ? "" : group.color}
      onEdit={then(() => onOpenSettings?.(at.returnTo))}
      onToggleCount={then(() => onConfig({ ...config, showGroupCounts: counts ? undefined : true }))}
      onHide={then(() => onConfig(tableConfigAfterGroupShown(config, group.key, false)))}
      onTrash={onTrash && valued ? then(() => { if (rowIds.length) onTrash(rowIds); }) : undefined}
      onColor={option ? recolor : undefined} onClose={onClose} />
  );
}
