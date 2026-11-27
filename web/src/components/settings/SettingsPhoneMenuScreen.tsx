import { useTranslation } from "react-i18next";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import type { SidebarMenuRow } from "@/components/sidebar/SidebarActionMenu";
import { CheckIcon } from "@/icons/leafen";

/**
 * A settings menu on a phone, in the shape of the members' role choice
 * there (WorkspaceMemberRoleMenu's phone screen, Notion's phone "역할 선택",
 * seen 2026-11-12 at 402pt): a whole screen under its title with a blue
 * "완료", a row for each choice — its mark, its name and what it does — the
 * current one ticked at its end, a removing one red. Notion's phone has no
 * screen for a workspace's guests, groups or teamspaces, so their
 * menus take this one's shape rather than a menu under the button. It
 * takes the computer menu's rows (SidebarActionMenu); a row set apart there
 * starts a group of its own, and a row closes the screen before it runs.
 */
export function SettingsPhoneMenuScreen({ title, label, rows, current, close, data }: {
  title: string;
  label?: string;
  rows: SidebarMenuRow[];
  /** The row ticked as the one chosen now, as the computer menu's `current`. */
  current?: string;
  close(): void;
  data?: Record<`data-${string}`, string | undefined>;
}) {
  const { t } = useTranslation("common");
  const groups: SidebarMenuRow[][] = [];
  for (const row of rows) {
    if (row.separatorBefore || groups.length === 0) groups.push([]);
    groups[groups.length - 1].push(row);
  }
  return (
    <PhoneSheet size="screen" title={title} label={label} tone="settings" onClose={close}
      action={{ label: t("actions.done"), onPress: close }} data={data}>
      {groups.map((group) => (
        <PhoneSheetGroup key={group[0].key}>
          {group.map((row) => {
            const ticked = row.checked === true || (current !== undefined && row.key === current);
            return (
              <PhoneSheetRow key={row.key} more={false} label={row.label} icon={row.icon} description={row.description}
                danger={row.danger} disabled={row.disabled || !row.run} data={row.data as Record<`data-${string}`, string> | undefined}
                value={ticked ? <CheckIcon size="regular" aria-hidden="true" /> : row.value}
                onPress={() => { close(); row.run?.(); }} />
            );
          })}
        </PhoneSheetGroup>
      ))}
    </PhoneSheet>
  );
}
