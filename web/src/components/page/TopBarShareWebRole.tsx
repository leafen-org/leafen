import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CalendarDate, getLocalTimeZone, today } from "@internationalized/date";
import { CheckIcon, ChevronDown, ChevronRight } from "@/icons/leafen";
import { activeDateLocale } from "@/lib/i18n";
import { useWeekStartDay } from "@/lib/displayPreferences";
import { usePendingChoice } from "@/lib/pendingChoice";
import { NativeDateCalendar } from "@/lib/nativeDateCalendar";
import { NativeMenuItem, useNativeMenu, useNativeSubmenu } from "@/lib/nativeMenu";
import { placeAround, placeBeside, popoverPlacementStyle, popoverRectOf, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import cellStyles from "@/components/database/databaseSheet";
import editorStyles from "@/components/database/PropertyDateEditor.module.css";
import { sharePermissionDescription, sharePermissionLabel } from "./TopBarSharePermissionPicker";
import { expiryAt, expiryChoices, type ExpiryChoice } from "./topBarShareModel";
import { styles } from "./topBarStyles";
import access from "./TopBarShareAccess.module.css";
import inherited from "./TopBarShareInherited.module.css";
import expiryStyles from "./TopBarShareWebExpiry.module.css";

export type WebLinkRole = "edit" | "comment" | "view";

type WebRoleItem = WebLinkRole | "expiry" | "remove";

const WEB_ROLES: WebLinkRole[] = ["edit", "comment", "view"];

type T = ReturnType<typeof useTranslation>["t"];

const sameDay = (a: Date, b: Date) => a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
const timeOf = (at: Date) => at.toLocaleTimeString(activeDateLocale(), { hour: "numeric", minute: "2-digit" });

/** When the link stops, in the role menu's row, as Notion's (seen 2026-11-05):
 * "오늘 오후 4:00" today, else the whole date, "2026년 11월 20일 오전 12:00". */
export function expiryValue(expiresAt: string, t: T) {
  const at = expiresAt ? new Date(expiresAt) : null;
  if (!at || !Number.isFinite(at.getTime())) return t("topBar:never");
  if (sameDay(at, new Date())) return t("topBar:expiresToday", { time: timeOf(at) });
  return at.toLocaleString(activeDateLocale(), { year: "numeric", month: "long", day: "numeric", hour: "numeric", minute: "2-digit" });
}

/** A choice's own words and, under them, when it would end: the time today, else the day and time. */
export function choiceLabel(choice: ExpiryChoice, t: T) {
  return t(choice === "never" ? "topBar:never" : choice === "1h" ? "topBar:inOneHour" : choice === "1d" ? "topBar:inOneDay" : "topBar:inOneWeek");
}
export function choiceWhen(choice: ExpiryChoice) {
  const at = expiryAt(choice);
  if (!at) return "";
  const date = new Date(at);
  return sameDay(date, new Date()) ? timeOf(date)
    : date.toLocaleString(activeDateLocale(), { month: "long", day: "numeric", hour: "numeric", minute: "2-digit" });
}

/**
 * The role on "링크가 있는 웹의 모든 사용자", as Notion's (seen 2026-11-05):
 * the roles with what each allows, then "링크 만료 시점" with when the link
 * stops and a list under it (none, an hour, a day or a week from now, or a
 * day picked on a calendar, from its start), then "제거", which takes the
 * web's row alone off, the workspace's left as set (seen 2026-11-08).
 * Picking an expiry keeps the menu open. Anyone reads through the
 * link; editing or commenting is for the organization's people who open it
 * signed in, who then work in the page in the app.
 */
export function TopBarShareWebRole({ role, expiresAt, disabled, onRole, onExpiry, onRemove, note }: {
  role: WebLinkRole; expiresAt: string; disabled: boolean; onRole(next: WebLinkRole): void; onExpiry(expiresAt: string): void; onRemove(): void;
  /** A line under the menu, as a page's inherited web link says a change unlinks it. */
  note?: string;
}) {
  const { t } = useTranslation("topBar");
  const trigger = useRef<HTMLButtonElement>(null);
  const [anchor, setAnchor] = useState<PopoverRect | null>(null);
  // The role picked shows at once, dimmed with the locked button and ticked
  // in the menu that stays open, while the server decides
  // (lib/pendingChoice.ts), as a person's role does beside it: both kept the
  // role before for some 150ms (seen 2026-11-13).
  const pending = usePendingChoice(role, disabled);
  return <span className={styles.sharePermissionWrap} data-share-web-role>
    <button ref={trigger} type="button" className={styles.sharePermissionButton} disabled={disabled} aria-haspopup="menu"
      aria-expanded={Boolean(anchor)} data-menu-item data-waiting-note={pending.note}
      onClick={() => setAnchor(anchor ? null : popoverRectOf(trigger.current))}>
      {sharePermissionLabel(pending.shown, t)} <ChevronDown size="tiny" aria-hidden="true" />
    </button>
    {anchor ? <WebRoleMenu anchor={anchor} trigger={trigger} role={pending.shown} expiresAt={expiresAt} onExpiry={onExpiry} onRemove={onRemove}
      onRole={(next) => { if (next !== role) pending.pick(next); onRole(next); }}
      note={note} onClose={() => setAnchor(null)} /> : null}
  </span>;
}

function WebRoleMenu({ anchor, trigger, role, expiresAt, onRole, onExpiry, onRemove, note, onClose }: {
  anchor: PopoverRect; trigger: React.RefObject<HTMLButtonElement | null>; role: WebLinkRole; expiresAt: string;
  onRole(next: WebLinkRole): void; onExpiry(expiresAt: string): void; onRemove(): void; note?: string; onClose(): void;
}) {
  const { t } = useTranslation("topBar");
  const menuRef = useRef<HTMLDivElement>(null), expiryRef = useRef<HTMLButtonElement>(null), expiryMenuRef = useRef<HTMLDivElement>(null);
  const items: WebRoleItem[] = [...WEB_ROLES, "expiry", "remove"];
  const picker = useNativeMenu({ items, open: true, menuRef, label: t("webLinkAccess"), onClose,
    keyOf: item => item,
    textOf: item => item === "expiry" ? t("linkExpiresAt") : item === "remove" ? t("removeAccess") : sharePermissionLabel(item, t),
    onAction: item => {
      if (item === "expiry") return;
      onClose();
      if (item === "remove") onRemove();
      else if (item !== role) onRole(item);
    } });
  const submenu = useNativeSubmenu({ triggerKey: "expiry", rootTrigger: picker.rootTrigger,
    parentMenuRef: menuRef, submenuRef: expiryMenuRef, triggerRef: expiryRef });
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeAround(anchor, size, viewport, { align: "end", gap: 4 }),
    [anchor], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose, inside: () => [trigger.current] });
  // Notion's list opens over the menu from its row's right end, its top on
  // the row's bottom, no gap (seen 2026-11-15: row 467–495, list from 495).
  const expiryPlace = usePopoverPlacement(expiryMenuRef, submenu.state.isOpen ? (size, viewport) =>
    placeAround(popoverRectOf(expiryRef.current) ?? anchor, size, viewport, { align: "end", gap: 0 }) : null,
  [submenu.state.isOpen, place?.left, place?.top], undefined,
  { kind: "menu", inside: () => [menuRef.current], onEscape: submenu.state.close, onOutsidePress: onClose });
  const fixed = { position: "fixed" as const, right: "auto", bottom: "auto" };
  return createPortal(<>
    <div {...picker.menuProps} ref={menuRef} className={`${styles.sharePermissionMenu} ${access.roleMenu}`} data-share-web-role-menu
      style={{ ...popoverPlacementStyle(place), ...fixed }}>
      {WEB_ROLES.map(item => <NativeMenuItem key={item} state={picker.state} itemKey={item} role="menuitemradio"
        aria-checked={item === role} data-native-menu-item data-share-web-role-choice={item}>
        {({ labelProps }) => <><span className={access.roleText}><span {...labelProps}>{sharePermissionLabel(item, t)}</span>
          {sharePermissionDescription(item, t) ? <span>{sharePermissionDescription(item, t)}</span> : null}</span>
          {item === role ? <CheckIcon size="small" className={access.roleCheck} aria-hidden="true" /> : null}</>}
      </NativeMenuItem>)}
      <NativeMenuItem state={picker.state} itemKey="expiry" buttonRef={expiryRef} itemOptions={submenu.submenuTriggerProps}
        className={`${access.roleRemove} ${access.webRoleExpiry}`} data-native-menu-item data-share-web-expiry>
        {({ labelProps }) => <><span {...labelProps}>{t("linkExpiresAt")}</span>
          <small>{expiryValue(expiresAt, t)}</small><ChevronDown size="small" aria-hidden="true" /></>}
      </NativeMenuItem>
      <NativeMenuItem state={picker.state} itemKey="remove" className={access.roleRemove} data-native-menu-item>
        {({ labelProps }) => <span {...labelProps}>{t("removeAccess")}</span>}
      </NativeMenuItem>
      {note ? <p className={inherited.roleNote} data-share-role-note>{note}</p> : null}
    </div>
    {submenu.state.isOpen ? <ExpiryMenu menuRef={expiryMenuRef} menuOptions={submenu.submenuProps} expiresAt={expiresAt}
      focusOnOpen={submenu.state.focusStrategy !== null}
      style={{ ...popoverPlacementStyle(expiryPlace), ...fixed }}
      onPick={next => { submenu.state.close(); onExpiry(next); }} /> : null}
  </>, document.body);
}

type ExpiryItem = ExpiryChoice | "date";

// The list of expiries, "날짜 선택" set off below a line as the menu's "제거"
// is, as Notion's (seen 2026-11-13).

function ExpiryMenu({ menuRef, menuOptions, expiresAt, focusOnOpen, style, onPick }: {
  menuRef: React.RefObject<HTMLDivElement | null>;
  menuOptions: NonNullable<Parameters<typeof useNativeMenu>[0]["menuOptions"]>;
  expiresAt: string; focusOnOpen: boolean; style: React.CSSProperties; onPick(expiresAt: string): void;
}) {
  const { t } = useTranslation("topBar");
  const dateRef = useRef<HTMLButtonElement>(null), calendarRef = useRef<HTMLDivElement>(null);
  const [picking, setPicking] = useState(false);
  const items: ExpiryItem[] = [...expiryChoices, "date"];
  const picker = useNativeMenu({ items, open: true, menuRef, label: t("linkExpiresAt"), menuOptions, focusOnOpen,
    keyOf: item => item, textOf: item => item === "date" ? t("pickExpiryDate") : choiceLabel(item, t),
    onAction: item => { if (item === "date") setPicking(true); else onPick(expiryAt(item)); } });
  // The calendar, closed, gives the focus back to its own row in the list,
  // not to the menu under the list: that took the list away under a pointer
  // gone on to "없음", whose press then fell on that menu's "제거" and took
  // the web link off (2026-11-13). It touches the list's side, as Notion's
  // (seen 2026-11-15: calendar 784–1052, list from 1052).
  const calendarPlace = usePopoverPlacement(calendarRef, picking ? (size, viewport) => {
    const menu = menuRef.current?.getBoundingClientRect(), row = dateRef.current?.getBoundingClientRect();
    return placeBeside({ top: row?.top ?? 0, bottom: row?.bottom ?? 0, left: menu?.left ?? 0, right: menu?.right ?? 0 },
      size, viewport, { gap: 0, prefer: "left" });
  } : null, [picking], undefined, { kind: "menu", inside: () => [menuRef.current], onEscape: () => setPicking(false), returnFocusRef: dateRef });
  const weekStart = useWeekStartDay();
  const now = today(getLocalTimeZone());
  // The link's expiry stands chosen on the calendar, which opens at its
  // month, as Notion's (seen 2026-11-07); none, and today is only marked.
  const expiry = expiresAt ? new Date(expiresAt) : null;
  const current = expiry && !Number.isNaN(expiry.getTime())
    ? new CalendarDate(expiry.getFullYear(), expiry.getMonth() + 1, expiry.getDate()) : null;
  return <>
    <div {...picker.menuProps} ref={menuRef} className={`${styles.sharePermissionMenu} ${access.roleMenu} ${access.webExpiryMenu}`}
      data-share-web-expiry-menu style={style}>
      {items.map(item => item === "date"
        ? <NativeMenuItem key={item} state={picker.state} itemKey={item} buttonRef={dateRef} data-native-menu-item data-share-web-expiry-option="date"
          aria-haspopup="dialog" aria-expanded={picking} className={`${access.roleRemove} ${access.webRoleExpiry}`} onPointerEnter={() => setPicking(true)}>
          {({ labelProps }) => <><span {...labelProps}>{t("pickExpiryDate")}</span><ChevronRight size="small" aria-hidden="true" /></>}
        </NativeMenuItem>
        : <NativeMenuItem key={item} state={picker.state} itemKey={item} data-native-menu-item data-share-web-expiry-option={item}
          onPointerEnter={() => setPicking(false)}>
          {({ labelProps }) => <span className={access.roleText}><span {...labelProps}>{choiceLabel(item, t)}</span>
            {choiceWhen(item) ? <span>{choiceWhen(item)}</span> : null}</span>}
        </NativeMenuItem>)}
    </div>
    {picking ? <div ref={calendarRef} className={`${styles.sharePermissionMenu} ${expiryStyles.calendar}`} data-share-web-expiry-calendar
      style={{ ...popoverPlacementStyle(calendarPlace), position: "fixed", right: "auto", bottom: "auto" }}>
      <NativeDateCalendar value={current} focusedValue={current ?? now} weekStart={weekStart} min={now} autoFocus
        label={t("pickExpiryDate")} previousLabel={t("propertyCell:prevMonth")} nextLabel={t("propertyCell:nextMonth")}
        classes={{ head: `${editorStyles.calendarHead} ${expiryStyles.head}`, weekdays: cellStyles.dateWeekdays, grid: cellStyles.dateGrid }}
        onPick={(day: CalendarDate) => onPick(new Date(day.year, day.month - 1, day.day).toISOString())} />
    </div> : null}
  </>;
}
