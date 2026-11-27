import { useRef, type CSSProperties, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { CheckIcon, ChevronDown } from "@/icons/leafen";
import { useMenuButtonTrigger } from "@/lib/menuButtonTrigger";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import styles from "./TeamspaceMembers.module.css";

export type TeamspaceChoice<T extends string> = { value: T; label: string; description?: string; icon?: ReactNode; disabled?: boolean };

/** A setting's value with a ⌄ that opens its choices under it, as the rows
 * of Notion's teamspace settings (seen 2026-11-11 and 2026-11-18 at 1440 in
 * two tabs), ending where the button ends: choices saying what they do in
 * the shape of a member's role menu, 264 wide, 4px under it (a page role's);
 * bare choices as wide as their words, right under it (the security tab's
 * "팀스페이스 소유자만 / 모든 팀스페이스 멤버"); a boxed one's as wide as its
 * box, from its start. */
export function TeamspaceChoiceMenu<T extends string>({ value, choices, label, disabled = false, className, children, row, wide = false, gap, marks, markInset, onChange, ...data }: {
  value: T; choices: readonly TeamspaceChoice<T>[]; label: string; disabled?: boolean; className?: string;
  /** What the button shows, when not just the chosen value's name. */
  children?: ReactNode;
  /** A class for a row holding what the button shows and its ⌄, for a look that lays them as one. */
  row?: string;
  /** The button is a box the width of its field, and its menu as wide. */
  wide?: boolean;
  /** How far under the button the menu opens: 4 under the settings box's row, over the line under it (Notion's, seen
   * 2026-11-18), 8 in "팀스페이스 만들기" (seen 2026-11-05). */
  gap?: number;
  /** The colour of its marks, the box's and the choices' (TeamspaceMembers.module.css, data-choice-marks): the icons'
   * secondary one in Notion's "팀스페이스 만들기", the fixed ink of its teamspace settings box (seen 2026-11-18). */
  marks?: "secondary" | "ink";
  /** How much further in its choices' marks stand, as its button's own: 5px in the teamspace settings box, whose
   * mark and menu marks stand 13 in and words 46, not 8 and 36 as in "팀스페이스 만들기" (Notion's, seen 2026-11-18). */
  markInset?: number;
  onChange(value: T): void;
} & Record<`data-${string}`, string | undefined>) {
  const trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const { state, menuProps, buttonProps, autoFocus } = useMenuButtonTrigger(trigger, disabled);
  const marked = choices.some((choice) => choice.icon), described = !marked && choices.some((choice) => choice.description);
  const placement = usePopoverPlacement(menu, state.isOpen ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport,
    { align: wide ? "start" : "end", gap: gap ?? (marked ? 8 : described ? 4 : 0) },
  ) : null, [state.isOpen, wide, gap], undefined, { kind: "menu", onEscape: state.close, onOutsidePress: state.close,
    inside: () => [trigger.current], canRestoreFocus: () => !disabled });
  const current = choices.find((choice) => choice.value === value);
  const shown = <>{children ?? <span>{current?.label ?? value}</span>}<ChevronDown size="tiny" aria-hidden="true" /></>;
  return <>
    <button {...buttonProps} {...data} ref={trigger} className={className ?? styles.roleButton} aria-label={label} data-choice-marks={marks}>
      {row ? <span className={row}>{shown}</span> : shown}
    </button>
    {state.isOpen ? createPortal(<div ref={menu} className={styles.roleMenu} data-choice-menu="" data-with-icons={marked ? "true" : undefined}
      data-role-menu={described ? "page" : undefined} data-wide={wide ? "true" : undefined} data-choice-marks={marks}
      style={{ ...popoverPlacementStyle(placement), position: "fixed", right: "auto", bottom: "auto",
        ...(wide ? { width: trigger.current?.offsetWidth } : {}), ...(markInset ? { "--choice-mark-inset": `${markInset}px` } : {}) } as CSSProperties}>
      <NativeActionMenu {...menuProps} aria-label={label} autoFocus={autoFocus}>
        {choices.map((choice) => <NativeActionMenuItem id={choice.value} key={choice.value} role="menuitemradio"
          aria-checked={value === choice.value} aria-label={choice.label} disabled={choice.disabled}
          onClick={() => { state.close(); if (choice.value !== value) onChange(choice.value); }}>
          {choice.icon}
          <span className={styles.roleText}><span>{choice.label}</span>
            {choice.description ? <span>{choice.description}</span> : null}</span>
          {value === choice.value ? <CheckIcon className={choice.description ? `${styles.roleCheck} ${styles.roleCheckTop}` : styles.roleCheck}
            size="small" aria-hidden="true" /> : null}
        </NativeActionMenuItem>)}
      </NativeActionMenu>
    </div>, document.body) : null}
  </>;
}
