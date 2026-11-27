import { cloneElement, isValidElement, useId, type ReactNode } from "react";
import { styles as rows } from "@/components/account/accountSettingsStyles";
import { SwitchMark } from "@/components/Switch";
import own from "./AdministratorSettingRows.module.css";

/**
 * The server's settings in 설정's own rows: a name and its line on the left,
 * the control on the right (2026-11-05: they were boxed forms with their own
 * sizes, and read as another product beside the pages drawn after Notion's).
 */
export function SettingRow({ title, description, children, ...data }: {
  title: string;
  description?: ReactNode;
  children: ReactNode;
} & Record<`data-${string}`, string | undefined>) {
  return (
    <div className={rows.securityRow} {...data}>
      <div className={rows.securityInstruction}>
        <strong>{title}</strong>
        {description ? <span>{description}</span> : null}
      </div>
      {children}
    </div>
  );
}

export function SettingSwitch({ title, description, on, disabled, onChange, control, ...data }: {
  title: string;
  description?: ReactNode;
  on: boolean;
  disabled?: boolean;
  onChange(on: boolean): void;
  /** Marks for finding the switch itself, such as a data attribute. */
  control?: Record<string, string>;
} & Record<`data-${string}`, string | undefined>) {
  return (
    <SettingRow title={title} description={description} {...data}>
      <button type="button" role="switch" aria-checked={on} aria-label={title} disabled={disabled} onClick={() => onChange(!on)} {...control}>
        <SwitchMark on={on} />
      </button>
    </SettingRow>
  );
}

/**
 * A row naming a set of rows, and those rows under it (one per organization
 * under a policy): they sit 16px in behind a faint line with their names in
 * the line's weight, so they read as that row's parts rather than policies of
 * their own (2026-11-06: an organization's switch under "앱의 모든 동작을
 * 사람이 승인" read as another policy).
 */
export function SettingRowGroup({ title, description, children, ...data }: {
  title?: string;
  description?: ReactNode;
  children: ReactNode;
} & Record<`data-${string}`, string | undefined>) {
  return (
    <div className={own.group} {...data}>
      <div className={rows.securityInstruction}>
        {title ? <strong>{title}</strong> : null}
        {description ? <span>{description}</span> : null}
      </div>
      <div className={own.groupRows}>{children}</div>
    </div>
  );
}

/**
 * A text field as 설정 › 일반's are, as Notion's (seen 2026-10-30): the name and its
 * line over a 400px box, a refusal under it; focus goes back to it after a save
 * (lib/focusThroughWaits.ts).
 */
export function SettingField({ title, description, error = "", children }: {
  title: string;
  description?: ReactNode;
  error?: string;
  children: ReactNode;
}) {
  const errorId = useId();
  const field = error && isValidElement<{ "aria-invalid"?: boolean; "aria-describedby"?: string }>(children)
    ? cloneElement(children, { "aria-invalid": true, "aria-describedby": errorId })
    : children;
  return (
    <label className={`${rows.securityInstruction} ${own.field}`} data-invalid={error ? "true" : undefined}>
      <strong>{title}</strong>
      {description ? <span>{description}</span> : null}
      {field}
      {error ? <small id={errorId} className={own.error} role="alert">{error}</small> : null}
    </label>
  );
}
