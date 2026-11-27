import { createContext, useContext, useMemo, useState, type ButtonHTMLAttributes, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { ChevronDown, ChevronRight, iconAtSize, Plus } from "@/icons/leafen";
import { PhoneSheetGroup, PhoneSheetMenu, phoneSheetStyles } from "../shell/PhoneSheet";
import { PhoneSheetChoices } from "./PhoneSheetChoices";
import styles from "./PhonePropertySettings.module.css";

/**
 * Where a phone's "속성 편집" lays a type's own settings, as Notion's (seen
 * 2026-11-11 at 402pt in iOS Safari): their rows go on in the white group
 * of "유형 ›" and "속성 사용 권한" ("정렬 수동 ›", "숫자 형식 ›", "제한 ›"...),
 * and what follows them ("옵션 +" over its chips, "표시 옵션" over its cards)
 * hangs under that group, at `after`. Null off a phone's settings.
 */
type PhonePropertySettingsPlace = { after: HTMLElement | null };
const PhonePropertySettingsContext = createContext<PhonePropertySettingsPlace | null>(null);

/** Whether the settings drawn here are a phone's "속성 편집" rows. */
export function usePhonePropertySettings() {
  return useContext(PhonePropertySettingsContext) !== null;
}

/**
 * A phone "속성 편집"'s group of type rows: `rows` first ("유형 ›",
 * "속성 사용 권한"), then the type's settings as rows of the same group,
 * each over a line; a section they open (PhonePropertySection) joins the
 * group's foot, its corners square there.
 */
export function PhonePropertyTypeGroup({ rows, settings }: { rows?: ReactNode; settings: ReactNode }) {
  const [after, setAfter] = useState<HTMLElement | null>(null);
  const place = useMemo(() => ({ after }), [after]);
  return (
    <PhonePropertySettingsContext.Provider value={place}>
      <PhoneSheetGroup className={styles.typeGroup}>
        <div className={styles.typeRows} data-phone-type-settings="">{rows}{settings}</div>
      </PhoneSheetGroup>
      <div ref={setAfter} className={styles.after} data-phone-type-sections="" />
    </PhonePropertySettingsContext.Provider>
  );
}

/**
 * A part of a type's settings under its rows, as Notion's "옵션" with its "+"
 * at the end over the option rows in white, the group's foot round, or
 * "표시 옵션" over its cards on the sheet's own colour (`plain`).
 */
export function PhonePropertySection({ label, add, plain = false, children, data }: {
  label: ReactNode;
  add?: { label: string; onPress(button: HTMLButtonElement): void; data?: Record<`data-${string}`, string> };
  plain?: boolean;
  children: ReactNode;
  data?: Record<`data-${string}`, string | undefined>;
}) {
  const place = useContext(PhonePropertySettingsContext);
  const section = (
    <section className={styles.section} data-plain={plain ? "true" : undefined} data-phone-type-section="" {...data}>
      <div className={styles.sectionHead}>
        <span>{label}</span>
        {add ? (
          <button type="button" className={styles.sectionAdd} aria-label={add.label} title={add.label} {...add.data}
            onClick={(event) => add.onPress(event.currentTarget)}>
            <Plus size="regular" aria-hidden="true" />
          </button>
        ) : null}
      </div>
      {plain ? children : <div className={styles.sectionRows}>{children}</div>}
    </section>
  );
  if (!place) return section;
  return place.after ? createPortal(section, place.after) : null;
}

/**
 * A setting's 45px row in a phone "속성 편집", as Notion's (seen 2026-11-11):
 * its icon where it has one, drawn at the row's 22px and its "›" at 18px,
 * their lines as thin as Notion's there (the label 16px in where not, as
 * "숫자 형식"), its
 * label, its value in grey and "›", turned "⌄" while its choices are open; one
 * that can't change fades without it, its value in its words' colour, as the
 * desktop's (PropertySettingButton) and Notion's "관계형 대상" and a rollup's
 * "대상 속성" and "계산" before its relation (seen 2026-11-19 on the A90's
 * Chrome at 411).
 */
export function PhoneSettingRow({ label, value, valueIcon, icon, caret = "right", buttonRef, ...props }: {
  label: ReactNode; value?: ReactNode; valueIcon?: ReactNode; icon?: ReactNode; caret?: "right" | "down" | "none";
  buttonRef?: RefObject<HTMLButtonElement | null>;
} & Omit<ButtonHTMLAttributes<HTMLButtonElement>, "value">) {
  const open = props["aria-expanded"] === true;
  return (
    <button {...props} ref={buttonRef} type="button" className={`${phoneSheetStyles.row} ${styles.settingRow} ${props.className ?? ""}`}
      data-bare={icon ? undefined : "true"} data-property-setting-row="" data-phone-setting-row="">
      {icon ? <span className={phoneSheetStyles.rowIcon} aria-hidden="true">{iconAtSize(icon, "wide")}</span> : null}
      <span className={phoneSheetStyles.rowLabel} data-property-setting-label="">{label}</span>
      {value !== undefined ? (
        <span className={`${phoneSheetStyles.rowValue} ${styles.settingValue}`} data-property-setting-value="">
          {valueIcon ? <span className={styles.valueIcon} aria-hidden="true">{valueIcon}</span> : null}
          <span className={styles.valueLabel}>{value}</span>
        </span>
      ) : null}
      {caret === "none" || props.disabled ? null : caret === "down" || open
        ? <ChevronDown size="page" className={phoneSheetStyles.rowMore} aria-hidden="true" />
        : <ChevronRight size="page" className={phoneSheetStyles.rowMore} aria-hidden="true" />}
    </button>
  );
}

/**
 * A setting's choices on a phone, as Notion's "정렬", "제한" and "숫자 형식"
 * (seen 2026-11-11): its row, and its choices in a sheet over the settings,
 * as bands across it (PhoneSheetChoices, seen 2026-11-16 for "날짜 형식" and
 * "숫자 형식"), the chosen one ticked where the list ticks one.
 */
export function PhoneSettingChoice({ label, icon, value, shown, valueIcon, options, disabled, showCheck = true, long, onChange }: {
  label: string; icon?: ReactNode; value: string; shown: ReactNode; valueIcon?: ReactNode;
  options: ReadonlyArray<{ value: string; label: string; disabled?: boolean }>;
  disabled?: boolean; showCheck?: boolean; onChange(value: string): void;
  /** A long list, searched on a computer: its sheet rises half way first (PhoneSheetChoices). */
  long?: boolean;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <PhoneSettingRow label={label} icon={icon} value={shown} valueIcon={valueIcon} disabled={disabled || options.length === 0}
        aria-label={label} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(true)} />
      {open ? (
        <PhoneSheetChoices label={label} value={showCheck ? value : undefined} long={long} onPick={onChange} onClose={() => setOpen(false)}
          choices={options.filter((option) => !option.disabled).map((option) => ({ key: option.value, label: option.label }))} />
      ) : null}
    </>
  );
}

/**
 * A setting that opens a small form on a phone, as Notion's "양방향
 * 관계형" (seen 2026-11-11): its row, and its form rising from the
 * screen's foot in white rows ("활성화" and its switch, the related
 * property's name under its grey label).
 */
export function PhoneSettingForm({ label, value, icon, children }: { label: string; value: string; icon?: ReactNode; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <PhoneSettingRow label={label} icon={icon} value={value} aria-label={label} aria-haspopup="dialog" aria-expanded={open}
        onClick={() => setOpen(true)} />
      {open ? (
        <PhoneSheetMenu label={label} onClose={() => setOpen(false)}>
          <div className={styles.form}>{children}</div>
        </PhoneSheetMenu>
      ) : null}
    </>
  );
}
