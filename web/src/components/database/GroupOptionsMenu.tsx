import { useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CheckIcon, EyeIcon, EyeSlashIcon, GroupIcon, Trash } from "@/icons/leafen";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import { placeAround, popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { COLOR_NAMES, chipStyle } from "./colors";
import styles from "./GroupOptionsMenu.module.css";

type Entry = { id: string; label: string; icon?: ReactNode; act(): void; structural: boolean; color?: string };

/**
 * A group's "⋯", a board column's and a table group's alike, as Notion's
 * (seen 2026-10-25 at 1280px): 그룹 편집하기, 집계 표시 or 숨기기, 그룹 숨기기,
 * 휴지통으로 이동 where the group's pages can go, and a select's option group
 * its colors under "색", the chosen one ticked — a status's none; opened by
 * the pointer too, its first row lit (2026-11-06). On a phone the same rows are a
 * whole sheet "작업" with "완료" (seen 2026-10-25 at 375px).
 */
export function GroupOptionsMenu({ name, position, returnTo, countShown, structureLocked, color, onEdit, onToggleCount, onHide, onTrash, onColor, onClose }: {
  name: string; position: PopoverRect; returnTo: HTMLElement; countShown: boolean; structureLocked: boolean;
  /** The option's color, where the group is an option's that can change it. */
  color?: string;
  onEdit(): void; onToggleCount(): void; onHide(): void; onTrash?(): void; onColor?(color: string): void; onClose(): void;
}) {
  const { t } = useTranslation(["boardView", "propertyTypeConfig", "common"]);
  const { phone } = useSidebarLayout(true);
  const ref = useRef<HTMLDivElement>(null), trigger = useRef(returnTo); trigger.current = returnTo;
  const actions: Entry[] = [
    { id: "edit", label: t("boardView:editGroup"), icon: <GroupIcon size="regular" aria-hidden="true" />, act: onEdit, structural: false },
    { id: "count", label: t(countShown ? "boardView:hideAggregate" : "boardView:showAggregate"), structural: true, act: onToggleCount,
      icon: countShown ? <EyeSlashIcon size="regular" weight="regular" aria-hidden="true" /> : <EyeIcon size="regular" weight="regular" aria-hidden="true" /> },
    { id: "hide", label: t("boardView:hideGroup"), icon: <EyeSlashIcon size="regular" weight="regular" aria-hidden="true" />, act: onHide, structural: true },
    ...(onTrash ? [{ id: "trash", label: t("boardView:trashGroupRows"), icon: <Trash size="regular" aria-hidden="true" />, act: onTrash, structural: false }] : []),
  ];
  const colors: Entry[] = onColor ? COLOR_NAMES.map((choice) => ({ id: `color:${choice}`, label: t(`propertyTypeConfig:optionColors.${choice}`),
    act: () => onColor(choice), structural: true, color: choice })) : [];
  const items = [...actions, ...colors];
  // No row waits for a save still out (listViewModel.ts): only a locked database's structure is refused.
  const disabled = (item: Entry) => structureLocked && item.structural;
  const menu = useNativeMenu({ items, open: !phone, menuRef: ref, label: t("boardView:groupOptions", { groupName: name }),
    keyOf: (item) => item.id, textOf: (item) => item.label, onAction: (item) => item.act(), onClose, highlightOnOpen: true,
    disabledKeys: items.filter((item) => disabled(item)).map((item) => item.id) });
  // Right under the "⋯" (seen 2026-11-06), whole, slid up into the window where it doesn't fit there, as Notion's.
  const place = usePopoverPlacement(ref, (size, viewport) => placeAround(position, size, viewport, { gap: 0, minHeight: Math.ceil(size.height) }), [position], undefined,
    { kind: "menu", onEscape: onClose, onOutsidePress: onClose, returnFocusRef: trigger, inside: () => [trigger.current] });
  if (phone) {
    const sheetRow = (item: Entry) => (
      <PhoneSheetRow key={item.id} more={false} disabled={disabled(item)} label={item.label} onPress={() => item.act()} data={{ "data-group-option": item.id }}
        icon={item.color ? <span className={styles.phoneSwatch} style={chipStyle(item.color)} /> : item.icon}
        value={item.color && item.color === (color ?? "default") ? <CheckIcon size="page" className={styles.phoneCheck} aria-hidden="true" /> : undefined} />
    );
    return (
      <PhoneSheet title={t("boardView:groupSheetTitle")} label={t("boardView:groupOptions", { groupName: name })} onClose={onClose}
        action={{ label: t("common:actions.done"), onPress: onClose }} data={{ "data-group-options-menu": "" }}>
        <PhoneSheetGroup>{actions.map(sheetRow)}</PhoneSheetGroup>
        {colors.length ? <PhoneSheetGroup label={t("propertyTypeConfig:optionColorHeading")}>{colors.map(sheetRow)}</PhoneSheetGroup> : null}
      </PhoneSheet>
    );
  }
  const row = (item: Entry) => (
    <NativeMenuItem key={item.id} itemKey={item.id} state={menu.state} className={styles.item} data-group-option={item.id}>
      {() => item.color ? <>
        <span className={styles.swatch} style={chipStyle(item.color)} aria-hidden="true" /><span>{item.label}</span>
        {item.color === (color ?? "default") ? <CheckIcon size="small" aria-hidden="true" /> : null}
      </> : <>{item.icon}<span>{item.label}</span></>}
    </NativeMenuItem>
  );
  return createPortal(
    <div {...menu.menuProps} ref={ref} className={styles.menu} style={{ position: "fixed", ...popoverPlacementStyle(place) }} data-group-options-menu>
      {actions.map(row)}
      {colors.length ? <>
        <div className={styles.rule} role="separator" />
        <div className={styles.heading} role="presentation">{t("propertyTypeConfig:optionColorHeading")}</div>
        {colors.map(row)}
      </> : null}
    </div>, document.body);
}
