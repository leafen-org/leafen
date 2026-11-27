import { createContext, Fragment, useContext, useMemo, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ChevronRight, Trash } from "@/icons/leafen";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { PhoneSheetMenu, PhoneSheetRow } from "../shell/PhoneSheet";
import { AutomationActionListIcon } from "./AutomationActionIcon";
import type { AutomationActionType } from "./automationActionModel";
import styles from "./automationActionEditor.module.css";
import { POPOVER_MARGIN } from "@/lib/popoverPlacement";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { PhoneSheetSearch } from "@/components/shell/PhoneSheetSearch";

/**
 * Whether the editor stands in a phone's sheet: its menus are then lists at
 * the screen's bottom, as Notion's (seen 2026-10-24 at 375px) — an action's
 * "⋯" and "+ 새 작업"'s "작업을 검색하세요" with each action's mark.
 */
export const AutomationPhoneSheets = createContext(false);

/**
 * Whether the editor stands in a desktop's dialog: its menus then float
 * under what opened them, over the dialog, as Notion's (seen 2026-10-24 at
 * 1280px), rather than opening in its flow.
 */
export const AutomationFloatingMenus = createContext(false);

// The room a floating menu wants under its opener unless it says its own: Notion's "작업 추가" list's (seen 2026-11-02, 266×374).
const FLOATING_MENU_ROOM = 374;

/** Where a menu lines up under its opener: its start, its middle, or its end. */
export type AutomationMenuAlign = "start" | "center" | "end";

/**
 * A floating menu's place: under its opener (`gap` below it), or over it
 * where the room below is short of the menu (`room` tall) and more is above,
 * as Notion's open up near the window's bottom; kept on the screen.
 */
function floatingPlace(trigger: HTMLElement | null, width: number, gap = 4, room = FLOATING_MENU_ROOM, align: AutomationMenuAlign = "start"): CSSProperties {
  const rect = trigger?.getBoundingClientRect();
  if (!rect || typeof window === "undefined") return { position: "fixed", top: 0, left: 0, width };
  const start = align === "center" ? rect.left + rect.width / 2 - width / 2 : align === "end" ? rect.right - width : rect.left;
  const left = Math.max(POPOVER_MARGIN, Math.min(start, window.innerWidth - width - POPOVER_MARGIN));
  const below = window.innerHeight - rect.bottom - gap - POPOVER_MARGIN, above = rect.top - gap - POPOVER_MARGIN;
  if (floatingAbove(rect, gap, room)) {
    return { position: "fixed", bottom: window.innerHeight - rect.top + gap, left, width, margin: 0, maxHeight: above };
  }
  return { position: "fixed", top: rect.bottom + gap, left, width, margin: 0, maxHeight: Math.max(160, below) };
}

/** Whether a floating menu opens over its opener: the room under it short of the menu (`room` tall) and more over it. */
export function floatingAbove(rect: { top: number; bottom: number }, gap = 4, room = FLOATING_MENU_ROOM) {
  const below = window.innerHeight - rect.bottom - gap - POPOVER_MARGIN, above = rect.top - gap - POPOVER_MARGIN;
  return below < room && above > below;
}

/** The room the action list wants: its search and a 29px line for each action (Notion's 266×287 for eight, seen 2026-11-10). */
export const actionTypeMenuRoom = (count: number) => 55 + count * 29;

/** A desktop menu in its flow, or floating in the page where the editor asks. */
function Placed({ floating, children }: { floating: boolean; children: ReactNode }) {
  return floating && typeof document !== "undefined" ? createPortal(children, document.body) : <>{children}</>;
}

/** A menu's row: its words and mark, and a line over it where a group starts. */
export type AutomationMenuChoice = { key: string; label: string; run(): void; danger?: boolean; icon?: ReactNode; rule?: boolean };
export function AutomationCardMenu({ choices, onClose, trigger, opener, width = 220, align = "start", gap = 0 }: {
  choices: AutomationMenuChoice[]; onClose(): void; trigger: RefObject<HTMLElement | null>;
  /** Where the focus goes back as it closes when that isn't what it hangs from: a deleted property's
   * card's own button, the card itself taking no focus (Notion's card takes it back, seen 2026-11-11). */
  opener?: RefObject<HTMLElement | null>;
  width?: number; align?: AutomationMenuAlign;
  /** How far under its opener it hangs: a deleted property's card's 8 (Notion's, seen 2026-11-11). */
  gap?: number;
}) {
  const phone = useContext(AutomationPhoneSheets);
  const { t } = useTranslation(["databaseView", "common"]);
  return phone ? <PhoneSheetMenu label={t("databaseView:actionMenu")} onClose={onClose}>
    {choices.map(choice => <PhoneSheetRow key={choice.key} label={choice.danger ? t("common:actions.delete") : choice.label} danger={choice.danger} more={false}
      icon={choice.danger ? <Trash size="regular" /> : choice.icon} indent
      data={{ "data-automation-card-choice": choice.key }} onPress={() => { onClose(); choice.run(); }} />)}
  </PhoneSheetMenu> : <DesktopCardMenu choices={choices} onClose={onClose} trigger={trigger} opener={opener ?? trigger} width={width} align={align} gap={gap} />;
}

// A menu row 28px and 1px apart, a group's line 9px with its room, the menu 4px in.
const menuHeight = (choices: AutomationMenuChoice[]) =>
  choices.length * 29 + choices.filter((choice, index) => index && choice.rule).length * 8 + 7;

/**
 * A desktop's small menu as Notion's (seen 2026-11-08): right under what
 * opened it — an action's "⋯" in its middle, a chip at its end — and over it
 * only where it doesn't fit under; 28px rows of a 20px mark and 14px words,
 * a line 12px in between groups, each mark in its words' colour (seen 2026-11-11:
 * "속성 복원", "조건 삭제", an action's "편집"…"삭제" all rgb(240, 239, 237) dark).
 */
function DesktopCardMenu({ choices, onClose, trigger, opener, width, align, gap }: {
  choices: AutomationMenuChoice[]; onClose(): void; trigger: RefObject<HTMLElement | null>; opener: RefObject<HTMLElement | null>;
  width: number; align: AutomationMenuAlign; gap: number;
}) {
  const { t } = useTranslation("databaseView");
  const ref = useRef<HTMLDivElement>(null);
  usePopoverAppear(true, ref, () => trigger.current);
  const layer = useOverlayLayer(true, { kind: "menu", ref, onEscape: onClose, onOutsidePress: onClose,
    inside: () => [trigger.current], returnFocusRef: opener });
  // Its first row lit as it opens, from the pointer too, as Notion's card
  // menus and its recipient kind list (seen 2026-11-06, 2026-11-08).
  const menu = useNativeMenu({ items: choices, open: true, menuRef: ref, label: t("actionMenu"), highlightOnOpen: true,
    keyOf: choice => choice.key, textOf: choice => choice.label, onAction: choice => { onClose(); choice.run(); } });
  const floating = useContext(AutomationFloatingMenus);
  return <Placed floating={floating}><div ref={ref} {...menu.menuProps} className={styles.menu} data-floating={floating || undefined}
    style={{ zIndex: layer.zIndex, ...(floating ? floatingPlace(trigger.current, width, gap, menuHeight(choices), align) : {}) }}>
    {choices.map((choice, index) => <Fragment key={choice.key}>
      {choice.rule && index ? <div className={styles.menuRule} role="separator" /> : null}
      <NativeMenuItem itemKey={choice.key} state={menu.state} data-automation-card-choice={choice.key}>
        {({ labelProps }) => <>
          {choice.icon ? <span className={styles.menuIcon} aria-hidden="true">{choice.icon}</span> : null}
          <span {...labelProps}>{choice.label}</span>
        </>}
      </NativeMenuItem>
    </Fragment>)}
  </div></Placed>;
}

/** A type that asks more before it is added, its own page of the list ("속성 편집..."'s properties). */
export type AutomationActionTypePage = (type: AutomationActionType, controls: { close(): void; back(): void }) => ReactNode | undefined;

/**
 * The list an action is added from, as Notion's (seen 2026-10-24): searched
 * from "작업을 검색하세요", each action by its mark and words; one that asks
 * more first ("속성 편집... ›") turns the list to its own page. The shared
 * menu's field-owned virtual focus moves through it.
 */
export function AutomationActionTypeMenu({ types, onAdd, onClose, trigger, page, startPage }: {
  types: { type: AutomationActionType; label: string }[]; onAdd(type: AutomationActionType): void;
  onClose(): void; trigger: RefObject<HTMLButtonElement | null>; page?: AutomationActionTypePage;
  /** Opens on that type's page, as "←" from a value comes back to its properties. */
  startPage?: AutomationActionType;
}) {
  const [paged, setPaged] = useState<AutomationActionType | null>(startPage ?? null);
  const shown = paged && page ? page(paged, { close: onClose, back: () => setPaged(null) }) : undefined;
  const choose = (type: AutomationActionType) => {
    if (page?.(type, { close: onClose, back: () => setPaged(null) })) setPaged(type);
    else { onAdd(type); onClose(); }
  };
  const phone = useContext(AutomationPhoneSheets);
  if (phone) return shown ?? <PhoneActionTypeMenu types={types} paged={Boolean(page)} onChoose={choose} onClose={onClose} />;
  return <DesktopActionTypeMenu types={types} paged={page} shown={shown} onChoose={choose} onClose={onClose} trigger={trigger} />;
}

function PhoneActionTypeMenu({ types, paged, onChoose, onClose }: {
  types: { type: AutomationActionType; label: string }[]; paged: boolean; onChoose(type: AutomationActionType): void; onClose(): void;
}) {
  const { t } = useTranslation("databaseView");
  const [query, setQuery] = useState("");
  const choices = types.filter(choice => choice.label.toLocaleLowerCase().includes(query.toLocaleLowerCase().trim()));
  return <PhoneSheetMenu label={t("findActionPhone")} onClose={onClose}>
    <PhoneSheetSearch value={query} autoFocus={fieldFocusOnOpen()} aria-label={t("findActionPhone")} placeholder={t("findActionPhone")}
      onValueChange={setQuery} />
    {choices.map(choice => <PhoneSheetRow key={choice.type} icon={<AutomationActionListIcon type={choice.type} />} label={choice.label}
      more={paged && choice.type === "edit_property"} data={{ "data-automation-action-type": choice.type }} onPress={() => onChoose(choice.type)} />)}
    {!choices.length ? <p className={styles.phoneNoActions}>{t("noActionsFound")}</p> : null}
  </PhoneSheetMenu>;
}

function DesktopActionTypeMenu({ types, paged, shown, onChoose, onClose, trigger }: {
  types: { type: AutomationActionType; label: string }[]; paged?: AutomationActionTypePage; shown: ReactNode | undefined;
  onChoose(type: AutomationActionType): void; onClose(): void; trigger: RefObject<HTMLButtonElement | null>;
}) {
  const { t } = useTranslation("databaseView");
  const [query, setQuery] = useState("");
  const ref = useRef<HTMLDivElement>(null), search = useRef<HTMLInputElement>(null);
  const choices = useMemo(() => types.filter(choice => choice.label.toLocaleLowerCase().includes(query.toLocaleLowerCase().trim())), [query, types]);
  usePopoverAppear(true, ref, () => trigger.current);
  const layer = useOverlayLayer(true, { kind: "menu", ref, onEscape: onClose, onOutsidePress: onClose,
    inside: () => [trigger.current], returnFocusRef: trigger });
  const menu = useNativeMenu({ items: shown ? [] : choices, open: true, menuRef: ref, label: t("actionType"),
    focusOwnerRef: search, highlightOnOpen: false, keyOf: choice => choice.type, textOf: choice => choice.label,
    onAction: choice => onChoose(choice.type) });
  const floating = useContext(AutomationFloatingMenus);
  // In the dialog, Notion's 266px list right under "+ 새 작업" (seen 2026-11-02), over it only where its
  // rows and search don't fit under (seen 2026-11-08: eight rows opened down with 300px to spare).
  const place = { zIndex: layer.zIndex, ...(floating ? floatingPlace(trigger.current, 266, 0, actionTypeMenuRoom(types.length)) : {}) };
  if (shown) return <Placed floating={floating}><div ref={ref} className={styles.menu} style={place} data-floating={floating || undefined}>{shown}</div></Placed>;
  return <Placed floating={floating}><div ref={ref} {...menu.menuProps} className={styles.menu} style={place} data-floating={floating || undefined}>
    <input ref={search} {...menu.ownerProps} aria-label={t("findAction")} placeholder={t("findAction")}
      value={query} onChange={event => setQuery(event.currentTarget.value)} />
    {choices.map(choice => <NativeMenuItem key={choice.type} itemKey={choice.type} state={menu.state}>
      {({ labelProps }) => <>
        <AutomationActionListIcon type={choice.type} />
        <span {...labelProps}>{choice.label}</span>
        {paged && choice.type === "edit_property" ? <ChevronRight size="small" className={styles.menuMore} aria-hidden="true" /> : null}
      </>}
    </NativeMenuItem>)}
    {!choices.length ? <p>{t("noActionsFound")}</p> : null}
  </div></Placed>;
}
