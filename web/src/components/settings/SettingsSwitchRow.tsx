import type { ReactNode } from "react";

import { SwitchMark } from "@/components/Switch";
import { styles } from "./workspaceSettingsOverviewStyles";

/** A settings row: what it does, and a switch that saves as it turns, as Notion's rows do. */
export function SettingsSwitchRow({ title, description, checked, disabled, attributes, onChange, children }: {
  title: string;
  description?: ReactNode;
  checked: boolean;
  disabled?: boolean;
  /** Marks for finding the switch, such as a data attribute. */
  attributes?: Record<string, string>;
  onChange(next: boolean): void;
  children?: ReactNode;
}) {
  return (
    <div className={styles.securityRow}>
      <div className={styles.securityInstruction}>
        <strong>{title}</strong>
        {description ? <span>{description}</span> : null}
        {children}
      </div>
      <button type="button" role="switch" aria-checked={checked} aria-label={title} disabled={disabled} {...attributes}
        onClick={() => onChange(!checked)}>
        <SwitchMark on={checked} />
      </button>
    </div>
  );
}
