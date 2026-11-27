import { type ReactNode, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { CheckIcon, iconAtSize, PersonFilterIcon } from "@/icons/leafen";
import { placeAround, popoverAnchorOf, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { SettingsChoiceSheet } from "./SettingsTouchChoice";
import { SettingsChevron } from "./SettingsChevron";
import styles from "./SettingsFilterChip.module.css";

export type SettingsFilterChoice = { value: string; label: string; detail?: string; icon?: ReactNode };

/**
 * A settings filter's icon, drawn at 20px whatever size it was made at, as
 * Notion's (seen 2026-11-16 in Chrome 1440: the teamspaces' 상태, 공개 수준
 * and 보안 chips' marks 20 by 20, 8px in, 6px before the name). A mark that
 * isn't square sits in a box as wide as its ink, as Notion trims its own (the
 * person 12.63, the verified pages' empty page 11.75 and teamspace 18 across);
 * the groups' "그룹 유형" draws its people at 16px.
 */
export function SettingsChipIcon({ icon, size = "regular" }: { icon: ReactNode; size?: "small" | "regular" }) {
  return <span className={styles.icon} aria-hidden="true">{iconAtSize(icon, size)}</span>;
}

/**
 * A people filter's mark ("소유자"): Notion's person in its own box, as wide
 * as the glyph (PersonFilterIcon, 12.63×20 at 1440 in the teamspaces'
 * "소유자", seen 2026-11-16), so the chip is as wide as Notion's; its other
 * filters' marks fill their 20 box.
 */
export function SettingsPersonFilterIcon() {
  return <PersonFilterIcon size="regular" />;
}

/**
 * A filter over a settings table, as Notion's 팀스페이스 and 인증된 페이지
 * filters (seen 2026-10-12): an icon, a name and "⌄", blue while it filters;
 * it opens its choices under it — a search over them where there are many —
 * and the chosen one is checked. On a touch screen, a phone's or an iPad's,
 * they rise on the sheet a settings row's choices take there, headed by the
 * filter's name with "완료" (SettingsTouchChoice): Notion's phone has no
 * such filters, so they follow the phone's other settings choices.
 */
export function SettingsFilterChip({ icon, label, active, disabled, choices, value, searchPlaceholder, emptyLabel, marker, onChoose }: {
  icon: ReactNode;
  label: string;
  active: boolean;
  disabled?: boolean;
  choices: readonly SettingsFilterChoice[];
  value: string;
  /** A search over the choices, as Notion's people, page and teamspace filters have. */
  searchPlaceholder?: string;
  emptyLabel?: string;
  marker: string;
  onChoose(value: string): void;
}) {
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const touch = useSidebarLayout(true).touch;
  const [query, setQuery] = useState("");
  const close = () => { setOpen(false); setQuery(""); };
  const place = usePopoverPlacement(menuRef, open && !touch && buttonRef.current
    ? (size, viewport) => placeAround(popoverAnchorOf(buttonRef.current!), size, viewport)
    : null, [open], undefined, {
    kind: "menu", inside: () => [buttonRef.current], onEscape: close, onOutsidePress: close, initialFocus: "first",
  });
  const needle = query.trim().toLocaleLowerCase();
  const shown = needle ? choices.filter((choice) => choice.label.toLocaleLowerCase().includes(needle)) : choices;
  return (
    <>
      <button ref={buttonRef} type="button" className={styles.chip} data-active={active || undefined} disabled={disabled}
        aria-haspopup={touch ? "dialog" : "menu"} aria-expanded={open} {...{ [`data-filter-${marker}`]: "" }} onClick={() => setOpen((current) => !current)}>
        <SettingsChipIcon icon={icon} />
        <span>{label}</span>
        <SettingsChevron />
      </button>
      {open && touch ? <SettingsChoiceSheet title={label} value={value}
        options={choices.map((choice) => ({ value: choice.value, label: choice.label, description: choice.detail, icon: choice.icon }))}
        search={searchPlaceholder ? { ariaLabel: searchPlaceholder, placeholder: searchPlaceholder, noResultsLabel: emptyLabel } : undefined}
        onPick={onChoose} onClose={close} /> : null}
      {open && !touch ? createPortal(
        <div ref={menuRef} className={styles.menu} role="menu" aria-label={label}
          style={{ position: "fixed", ...popoverPlacementStyle(place) }}>
          {searchPlaceholder ? (
            <input className={styles.search} value={query} placeholder={searchPlaceholder} aria-label={searchPlaceholder}
              onChange={(event) => setQuery(event.target.value)} />
          ) : null}
          {shown.map((choice) => (
            <button key={choice.value} type="button" role="menuitemradio" aria-checked={value === choice.value} className={styles.item}
              onClick={() => { close(); onChoose(choice.value); }}>
              {choice.icon ? <span className={styles.itemIcon} aria-hidden="true">{choice.icon}</span> : null}
              <span className={styles.itemText}>
                <span>{choice.label}</span>
                {choice.detail ? <small>{choice.detail}</small> : null}
              </span>
              {value === choice.value ? <CheckIcon size="small" aria-hidden="true" /> : null}
            </button>
          ))}
          {shown.length === 0 && emptyLabel ? <div className={styles.empty}>{emptyLabel}</div> : null}
        </div>,
        document.body,
      ) : null}
    </>
  );
}
