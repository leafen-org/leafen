import { useRef } from "react";
import { DatabaseTouchMenuWindow } from "@/components/database/DatabaseTouchMenuWindow";
import type { SidebarMenuRow } from "@/components/sidebar/SidebarActionMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import styles from "./WorkspaceMemberRoleMenu.module.css";

/**
 * A settings menu on a touch screen wider than a phone, in the shape of a
 * workspace member's role choice there (Notion's iPad "역할 선택", seen
 * 2026-11-13 in iPad Safari at 1210 landscape): not a menu under the button
 * but the window in the screen's middle the touch screen's lists open in
 * (DatabaseTouchMenuWindow) — its title over a line with its blue "완료",
 * then the computer's rows of a mark, a name and what it does, the current
 * one ticked at its end, a removing one red. It takes the computer menu's
 * rows (SidebarActionMenu), as the phone's whole screen does
 * (SettingsPhoneMenuScreen); a pressed row closes it before it runs.
 */
export function SettingsTouchMenuWindow({ title, label, rows, current, close, data }: {
  title: string;
  label?: string;
  rows: SidebarMenuRow[];
  /** The row ticked as the one chosen now; with it the other rows but the red ones are its choices. */
  current?: string;
  close(): void;
  data?: Record<`data-${string}`, string | undefined>;
}) {
  const windowRef = useRef<HTMLDivElement>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: windowRef, onEscape: close });
  return (
    <DatabaseTouchMenuWindow title={title} label={label ?? title} menuRef={windowRef} layer={zIndex}
      shadeLayer={zIndex === undefined ? undefined : zIndex - 1} onClose={close} data={data}>
      <div className={`${styles.menu} ${styles.windowList}`} role="menu" aria-label={label ?? title}>
        {rows.map((row) => {
          const choice = current !== undefined ? !row.danger : row.checked !== undefined;
          const ticked = row.checked === true || (current !== undefined && row.key === current);
          const off = row.disabled || !row.run;
          return (
            <button key={row.key} type="button" role={choice ? "menuitemradio" : "menuitem"} aria-checked={choice ? ticked : undefined}
              data-native-menu-item="" data-danger={row.danger ? "true" : undefined} aria-disabled={off ? "true" : undefined}
              title={off ? row.title : undefined} {...row.data}
              onClick={() => { if (off) return; close(); row.run?.(); }}>
              {row.icon}
              {row.description ? <span className={styles.text}><span>{row.label}</span><span>{row.description}</span></span>
                : <span>{row.label}</span>}
            </button>
          );
        })}
      </div>
    </DatabaseTouchMenuWindow>
  );
}
