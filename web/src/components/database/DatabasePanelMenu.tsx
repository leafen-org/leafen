import { useRef, type ReactNode } from "react";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import styles from "./DatabasePanelMenu.module.css";

export type DatabasePanelAction = {
  key: string; label: string; icon: ReactNode; note?: ReactNode; tail: ReactNode; data?: Record<string, string>; run(): void;
};

/** A settings panel screen's rows that open another screen or act, as one
 * menu: it takes the keys as the screen shows; a busy panel takes none. */
export function DatabasePanelMenu({ label, busy = false, actions }: { label: string; busy?: boolean; actions: DatabasePanelAction[] }) {
  const menuRef = useRef<HTMLDivElement>(null);
  const menu = useNativeMenu({ items: actions, open: true, menuRef, label, disabledKeys: busy ? actions.map((action) => action.key) : [],
    keyOf: (action) => action.key, textOf: (action) => action.label, onAction: (action) => action.run() });
  return (
    <div {...menu.menuProps} ref={menuRef} className={styles.menuList}>
      {actions.map((action) => (
        <NativeMenuItem key={action.key} state={menu.state} itemKey={action.key} {...action.data}>
          {({ labelProps }) => <>{action.icon}<span {...labelProps}>{action.label}</span><small>{action.note}</small>{action.tail}</>}
        </NativeMenuItem>
      ))}
    </div>
  );
}
