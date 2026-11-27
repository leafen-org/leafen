import { createContext, type ButtonHTMLAttributes, type ReactNode, type RefObject, useContext, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { SettingInfoTip } from "../IconTipButton";
import { ChevronDown, ChevronRight, iconAtSize } from "../icons";
import { PhoneSettingForm, PhoneSettingRow, usePhonePropertySettings } from "./PhonePropertySettings";
import styles from "./PropertySettingRow.module.css";
import { fieldEscape } from "@/lib/fieldKeyBoundary";

/**
 * Set where setting rows sit in a phone's settings sheet, whose rows draw
 * their icons at 22px and their "›" at 18px ([data-phone-settings] in
 * PropertySettingRow.module.css): they are drawn at those sizes, their lines
 * as thin as Notion's there, not blown up to them.
 */
export const PhoneSettingsListContext = createContext(false);

/**
 * A place's own "›" for the setting rows inside it, drawn in place of the
 * default whether the row is open or not, its CSS turning it (a conditional
 * colour card's filled chevron, turned to point right until its row opens).
 */
export const PropertySettingCaret = createContext<ReactNode>(undefined);

/** The label and current value share one native setting row. Interaction
 * belongs to its caller (Aria's option menu or the small form popover). */
export function PropertySettingButton({ label, labelTip, value, icon, valueIcon, buttonRef, caret = "right", ...props }: {
  label: string; value?: ReactNode; icon?: ReactNode; valueIcon?: ReactNode;
  /** What the setting does, in a tip on an ⓘ after its name, as Notion's two-way relation (seen 2026-11-07). */
  labelTip?: string;
  buttonRef?: RefObject<HTMLButtonElement | null>; caret?: "right" | "down" | "none";
} & Omit<ButtonHTMLAttributes<HTMLButtonElement>, "value">) {
  // A phone's "속성 편집" draws it as the sheet's own row (PhoneSettingRow), its ⓘ left out.
  const phoneList = useContext(PhoneSettingsListContext);
  if (usePhonePropertySettings()) return <PhoneSettingRow {...props} label={label} value={value} icon={icon} valueIcon={valueIcon} buttonRef={buttonRef} caret={caret} />;
  // Its mark from Notion's 20px set wherever its caller drew it from (seen 2026-11-18 at 1440 in two tabs: a relation's,
  // a date's, a select's, a person's and a rollup's rows all 20 at 8), its value's as given.
  const mark = iconAtSize(icon, phoneList ? "wide" : "regular"), valueMark = phoneList ? iconAtSize(valueIcon, "wide") : valueIcon;
  const placeCaret = useContext(PropertySettingCaret);
  return <button {...props} type="button" ref={buttonRef} className={`${styles.row} ${props.className ?? ""}`} data-property-setting-row aria-description={props["aria-description"] ?? (typeof value === "string" ? value : undefined)}>
    {mark && <span className={styles.icon} aria-hidden="true">{mark}</span>}
    <span className={styles.label} data-property-setting-label data-setting-name>{label}</span>
    {labelTip && <SettingInfoTip label={labelTip} className={styles.labelTip} />}
    {value !== undefined && <span className={styles.value} data-property-setting-value>{valueMark && <span className={styles.icon} aria-hidden="true">{valueMark}</span>}<span className={styles.valueLabel}>{value}</span></span>}
    {/* A dim row, one that cannot change, draws no "›", as Notion's (seen 2026-11-19 in Chrome 1440: a rollup's
        "대상 속성" and "계산" before its relation, from a column's "속성 편집" and the view's, and a relation's "관계형 대상"). */}
    {caret === "none" || props.disabled ? null : placeCaret ? placeCaret : caret === "down" || props["aria-expanded"] === true
      ? <ChevronDown size={phoneList ? "page" : "tiny"} aria-hidden="true" /> : <ChevronRight size={phoneList ? "page" : "tiny"} aria-hidden="true" />}
  </button>;
}

type PropertySettingPopoverProps = { label: string; labelTip?: string; value: string; icon?: ReactNode; children: ReactNode };

/** A setting that opens a form, rather than an option collection: in a
 * phone's "속성 편집" the form rises from the screen's foot (PhoneSettingForm). */
export function PropertySettingPopover(props: PropertySettingPopoverProps) {
  const phone = usePhonePropertySettings();
  return phone ? <PhoneSettingForm label={props.label} value={props.value} icon={props.icon}>{props.children}</PhoneSettingForm>
    : <DesktopPropertySettingPopover {...props} />;
}

/** The same overlay owner handles Escape/outside presses and Floating UI placement. */
function DesktopPropertySettingPopover({ label, labelTip, value, icon, children }: PropertySettingPopoverProps) {
  const [open, setOpen] = useState(false), id = useId();
  const buttonRef = useRef<HTMLButtonElement>(null), panelRef = useRef<HTMLDivElement>(null);
  const close = (restore = false) => {
    if (restore) buttonRef.current?.focus({ preventScroll: true });
    setOpen(false);
  };
  useEffect(() => {
    if (open) (panelRef.current?.querySelector<HTMLInputElement>("input:not(:disabled)") ?? panelRef.current)?.focus({ preventScroll: true });
  }, [open]);
  const place = usePopoverPlacement(panelRef, open ? (size, viewport) => placeAround(popoverRectOf(buttonRef.current)!, size, viewport,
    { gap: 0, align: "end", alignFallback: true }) : null, [open], undefined,
    { kind: "picker", restoreFocus: false, onEscape: () => close(true), onOutsidePress: () => close(), inside: () => [buttonRef.current] });
  return <>
    <PropertySettingButton label={label} labelTip={labelTip} value={value} icon={icon} buttonRef={buttonRef} aria-label={label}
      aria-haspopup="dialog" aria-expanded={open} aria-controls={open ? id : undefined} onClick={() => setOpen(!open)} />
    {open && createPortal(<div ref={panelRef} id={id} role="dialog" tabIndex={-1} aria-label={label} className={styles.panel}
      style={{ position: "fixed", ...popoverPlacementStyle(place) }} onKeyDown={event => {
        if (fieldEscape(event)) { event.preventDefault(); event.stopPropagation(); close(true); }
      }}>{children}</div>, document.body)}
  </>;
}
