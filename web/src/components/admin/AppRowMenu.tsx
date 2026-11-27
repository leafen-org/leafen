import { SettingsRowMenu } from "@/components/settings/SettingsRowMenu";

export type AppRowMenuItem = { key: string; label: string; data: string; danger?: boolean; onSelect: () => void };

/**
 * What else an app's row offers, behind the settings tables' one "…" menu
 * (2026-11-06: a menu of its own here kept the focus on the button, so Escape
 * and the arrow keys did nothing).
 */
export function AppRowMenu({ label, disabled, items }: { label: string; disabled: boolean; items: AppRowMenuItem[] }) {
  return (
    <SettingsRowMenu label={label} disabled={disabled} data={{ "data-app-row-menu": "" }}
      rows={items.map((item) => ({ key: item.key, label: item.label, data: { [item.data]: "" }, danger: item.danger, run: item.onSelect }))} />
  );
}
