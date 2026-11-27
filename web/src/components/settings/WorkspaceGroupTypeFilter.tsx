import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CheckIcon, GroupPeopleIcon } from "@/icons/leafen";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "@/components/shell/PhoneSheet";
import { placeAround, popoverAnchorOf, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { SettingsChevron } from "./SettingsChevron";
import chipStyles from "./SettingsFilterChip.module.css";
import { SettingsChipIcon } from "./SettingsFilterChip";
import styles from "./WorkspaceGroups.module.css";

/** Who manages a group, as Notion's group list tells its groups apart. */
export type WorkspaceGroupType = "user_managed" | "admin_managed";
export const workspaceGroupTypes: readonly WorkspaceGroupType[] = ["user_managed", "admin_managed"];

/**
 * "그룹 유형 ⌄" over the group list, as Notion's (seen 2026-11-11, and its
 * code): a 320px card under it, "그룹 유형" with "모두 선택" across its head,
 * a box for 사용자 관리 and 관리자 관리, and "지우기" and a blue "적용" at its
 * foot. The boxes ticked apply as "적용" is pressed; none ticked shows every
 * group. The chip is blue while it filters. On a touch screen the card is
 * the sheet the other settings choices rise on there (SettingsTouchChoice):
 * its name with a blue "적용", the two kinds ticked as pressed, then "모두
 * 선택" and "지우기".
 */
export function WorkspaceGroupTypeFilter({ value, onChange }: {
  value: readonly WorkspaceGroupType[];
  onChange(next: WorkspaceGroupType[]): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const buttonRef = useRef<HTMLButtonElement>(null);
  const cardRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [ticked, setTicked] = useState<WorkspaceGroupType[]>([]);
  const touch = useSidebarLayout(true).touch;
  const close = () => setOpen(false);
  const place = usePopoverPlacement(cardRef, open && !touch && buttonRef.current
    ? (size, viewport) => placeAround(popoverAnchorOf(buttonRef.current!), size, viewport)
    : null, [open], undefined, {
    kind: "menu", inside: () => [buttonRef.current], onEscape: close, onOutsidePress: close, initialFocus: "first",
  });
  const label = t("groupsTypeFilter");
  return (
    <>
      <button ref={buttonRef} type="button" className={chipStyles.chip} data-active={value.length ? true : undefined}
        aria-haspopup="dialog" aria-expanded={open} data-group-type-filter
        onClick={() => { setTicked([...value]); setOpen((current) => !current); }}>
        <SettingsChipIcon icon={<GroupPeopleIcon size="small" />} size="small" />
        <span>{label}</span>
        <SettingsChevron />
      </button>
      {open && touch ? (
        <PhoneSheet title={label} tone="settings" onClose={close} data={{ "data-group-type-sheet": "" }}
          action={{ label: t("groupsTypeApply"), onPress: () => { onChange(ticked); close(); } }}>
          <div className={sheetStyles.stack}>
            <PhoneSheetGroup>
              {workspaceGroupTypes.map((type) => (
                <PhoneSheetRow key={type} label={t(type === "user_managed" ? "groupTypeUserManaged" : "groupTypeAdminManaged")} more={false}
                  value={ticked.includes(type) ? <CheckIcon size="regular" aria-hidden="true" /> : undefined}
                  onPress={() => setTicked((current) => current.includes(type) ? current.filter((item) => item !== type) : [...current, type])} />
              ))}
            </PhoneSheetGroup>
            <PhoneSheetGroup>
              <PhoneSheetRow label={t("groupsTypeSelectAll")} more={false} onPress={() => setTicked([...workspaceGroupTypes])} />
              <PhoneSheetRow label={t("groupsTypeClear")} more={false} onPress={() => { setTicked([]); onChange([]); close(); }} />
            </PhoneSheetGroup>
          </div>
        </PhoneSheet>
      ) : null}
      {open && !touch ? createPortal(
        <div ref={cardRef} className={styles.typeCard} role="dialog" aria-label={label}
          style={{ position: "fixed", ...popoverPlacementStyle(place) }}>
          <div className={styles.typeHead}>
            <span>{label}</span>
            <button type="button" onClick={() => setTicked([...workspaceGroupTypes])}>{t("groupsTypeSelectAll")}</button>
          </div>
          {workspaceGroupTypes.map((type) => (
            <label key={type} className={styles.typeRow}>
              <input type="checkbox" checked={ticked.includes(type)}
                onChange={(event) => {
                  const on = event.currentTarget.checked;
                  setTicked((current) => on ? [...current.filter((item) => item !== type), type] : current.filter((item) => item !== type));
                }} />
              <span>{t(type === "user_managed" ? "groupTypeUserManaged" : "groupTypeAdminManaged")}</span>
            </label>
          ))}
          <div className={styles.typeFoot}>
            <button type="button" className={styles.typeClear} onClick={() => { setTicked([]); onChange([]); close(); }}>
              {t("groupsTypeClear")}
            </button>
            <button type="button" className={styles.typeApply} onClick={() => { onChange(ticked); close(); }}>
              {t("groupsTypeApply")}
            </button>
          </div>
        </div>,
        document.body,
      ) : null}
    </>
  );
}
