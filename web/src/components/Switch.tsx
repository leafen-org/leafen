import type { ButtonHTMLAttributes, InputHTMLAttributes } from "react";
import { useSidebarLayout } from "@/components/shell/AppShell";
import styles from "./Switch.module.css";

/**
 * The app's one switch, as Notion's (seen 2026-10-28): on a desktop 30×18, a
 * white 14px knob 2px in, grey off and blue on; on a touch phone its sheets'
 * 51×31 (2026-10-27). A row that is the switch itself draws this mark at its
 * end; a lone switch is `Switch`, a form's checkbox `SwitchInput`.
 */
export function SwitchMark({ on, className }: { on: boolean | null | undefined; className?: string }) {
  const { phone } = useSidebarLayout(true);
  return <span className={className ? `${styles.mark} ${className}` : styles.mark} data-on={on ? "true" : undefined}
    data-size={phone ? "phone" : undefined} data-switch-mark="" aria-hidden="true"><span /></span>;
}

/** A switch on its own, its name from a label or aria-label. */
export function Switch({ on, className, ...rest }: { on: boolean } & Omit<ButtonHTMLAttributes<HTMLButtonElement>, "role" | "type">) {
  return <button type="button" role="switch" aria-checked={on} className={className ? `${styles.button} ${className}` : styles.button} {...rest}>
    <SwitchMark on={on} />
  </button>;
}

/** A checkbox drawn as the switch, for a row or form that keeps its own input. */
export function SwitchInput({ className, ...rest }: Omit<InputHTMLAttributes<HTMLInputElement>, "type" | "role">) {
  const { phone } = useSidebarLayout(true);
  return <input type="checkbox" role="switch" className={className ? `${styles.input} ${className}` : styles.input}
    data-size={phone ? "phone" : undefined} {...rest} />;
}
