"use client";

import { DatabaseAutomationReaderNote } from "./DatabaseAutomationReaderNote";
import { DatabaseAutomationReaderPopover } from "./DatabaseAutomationReaderPopover";
import { SwitchMark } from "@/components/Switch";
import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft, Copy, DotsHorizontal, LightningIcon, PauseIcon, Pencil, Plus, Trash, X } from "@/icons/leafen";
import { SearchClearButton } from "@/components/PageActionSearch";
import { WarningFillIcon } from "@/icons/leafen/automationIcons";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeBeside, popoverPlacementStyle, usePopoverPlacement, POPOVER_MARGIN } from "@/lib/popoverPlacement";
import type { AutomationAction, DatabaseAutomationDefinition, DbProperty } from "@/lib/types";
import { AutomationMarks } from "./AutomationMarks";
import type { AutomationTriggerDraft } from "./databaseAutomationPanelModel";
import { useDockedPanelTop } from "./useDockedPanelTop";
import { useAutomationListPopover } from "./automationListPopover";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { useTipPress } from "@/lib/tipTiming";
import dockColumn from "./dockedPanelColumn.module.css";
import styles from "./DatabaseAutomationList.module.css";

// A moment over a card before its name and sentence show beside it, as Notion's.
const TIP_DELAY_MS = 500;

/**
 * The automations a desktop's "⚡" opens: the list's panel, or for a reader
 * with none to read Notion's small window under the button instead
 * (DatabaseAutomationReaderPopover) — at once where the page's read counted
 * none, else once the list comes back empty.
 */
export function DesktopAutomationList({ automationCount, ...props }: Parameters<typeof AutomationListPanel>[0] & { automationCount?: number }) {
  const none = props.readOnly && !props.error && !props.automations.length && (!props.loading || automationCount === 0);
  return none ? <DatabaseAutomationReaderPopover onClose={props.onClose} /> : <AutomationListPanel {...props} />;
}

/**
 * A database's automations on a desktop, as Notion's (seen 2026-10-25 at
 * 1280px): a 290px panel on the right where the view settings open — under a
 * database page's toolbar, the page narrowing beside it — headed "자동화"
 * (after "←" when the view settings led here) with a round "×"; a search;
 * a white 10px card per automation — its marks, its name with "⏸ 중단됨"
 * when off, its sentence in two lines — with "⋯" on hover (AutomationCardMenu)
 * and its name and sentence (an invalid one's: what to fix) in a dark tip
 * beside it after a moment; and a blue "+ 새 자동화". Notion's "ⓘ" and its "트리거
 * 반복 시도 ↗" card lead to its help centre, left out; why one
 * stopped, Leafen's own, stays under its sentence.
 */
function AutomationListPanel({ automations, loading, readOnly, busy, error, nameOf, statusOf, reasonOf, noteOf, summaryOf, triggerOf, actionsOf,
  properties, dockRef: toolbarRef, searchRef: givenSearchRef, onNew, onOpen, onToggle, onDuplicate, onDelete, onClose, onBack }: {
  automations: DatabaseAutomationDefinition[];
  loading: boolean;
  readOnly: boolean;
  busy: boolean;
  error: string;
  /** Its name, or its maker's for one left unnamed. */
  nameOf(automation: DatabaseAutomationDefinition): string;
  /** A word on one that isn't running ("중단됨", "유효하지 않음"), else nothing. */
  statusOf(automation: DatabaseAutomationDefinition): { label: string; invalid: boolean } | null;
  /** Why one stopped and what to fix, else nothing. */
  reasonOf(automation: DatabaseAutomationDefinition): string | null;
  /** What one turned on or off says once its change has waited ten seconds. */
  noteOf?(automation: DatabaseAutomationDefinition): string | undefined;
  summaryOf(automation: DatabaseAutomationDefinition): string;
  triggerOf(automation: DatabaseAutomationDefinition): AutomationTriggerDraft;
  actionsOf(automation: DatabaseAutomationDefinition): AutomationAction[];
  properties: DbProperty[];
  /** Its database's toolbar: a database page's panel hangs under it, an inline database's popover opens under it (automationListPopover). */
  dockRef?: RefObject<HTMLElement | null>;
  /** Its search, which an automation's editor over it hands the keys back to. */
  searchRef?: RefObject<HTMLInputElement | null>;
  onNew?(): void;
  onOpen(automation: DatabaseAutomationDefinition): void;
  onToggle(automation: DatabaseAutomationDefinition): void;
  onDuplicate(automation: DatabaseAutomationDefinition): void;
  onDelete(automation: DatabaseAutomationDefinition): void;
  onClose(): void;
  /** Back to the view settings, where they led here. */
  onBack?(): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const panelRef = useRef<HTMLElement>(null);
  const ownSearchRef = useRef<HTMLInputElement>(null);
  const searchRef = givenSearchRef ?? ownSearchRef;
  const [query, setQuery] = useState("");
  const [menuOf, setMenu] = useState<{ id: string; trigger: HTMLElement } | null>(null);
  // The one its "⋯" is open for, as it now is: its switch shows it turned on or off.
  const menuAutomation = menuOf ? automations.find(({ id }) => id === menuOf.id) ?? null : null;
  const menu = menuOf && menuAutomation ? { automation: menuAutomation, trigger: menuOf.trigger } : null;
  const [tip, setTip] = useState<{ automation: DatabaseAutomationDefinition; rect: DOMRect } | null>(null);
  const tipTimer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(tipTimer.current), []);
  const ownPage = useDatabaseOwnPage(), dockRef = ownPage ? toolbarRef : undefined;
  const popover = useAutomationListPopover(panelRef, ownPage ? undefined : toolbarRef);
  const { zIndex } = useOverlayLayer(true, { kind: "picker", ref: panelRef, restoreFocus: false, onEscape: onClose, onOutsidePress: popover ? onClose : undefined,
    inside: () => [toolbarRef?.current?.querySelector('[data-database-toolbar-menu="automations"]')] });
  // The page narrows beside the panel rather than going under it, as beside the view settings; a popover's stays.
  useEffect(() => {
    if (popover) return;
    const root = document.documentElement;
    root.dataset.databaseAutomationsOpen = "true";
    return () => { delete root.dataset.databaseAutomationsOpen; };
  }, [Boolean(popover)]);
  const dockTop = useDockedPanelTop(dockRef, true, panelRef);
  useLayoutEffect(() => { searchRef.current?.focus({ preventScroll: true }); }, []);
  const needle = query.trim().toLocaleLowerCase();
  const shown = automations.filter((automation) => nameOf(automation).toLocaleLowerCase().includes(needle));
  const hideTip = () => { window.clearTimeout(tipTimer.current); setTip(null); };
  // A press takes a card's tip and its wait, as Notion's every tip goes as the mouse goes down (lib/tipTiming.ts).
  useTipPress(true, hideTip);
  const showTip = (automation: DatabaseAutomationDefinition, card: HTMLElement) => {
    window.clearTimeout(tipTimer.current);
    tipTimer.current = window.setTimeout(() => setTip({ automation, rect: card.getBoundingClientRect() }), TIP_DELAY_MS);
  };
  const style: CSSProperties = {
    ...(zIndex === undefined ? {} : { zIndex }),
    ...(popover ?? (dockTop === null ? {} : { top: `max(var(--page-panel-top, var(--topbar-height)), ${dockTop}px)` })),
  };
  if (typeof document === "undefined") return null;
  return createPortal(
    <aside ref={panelRef} className={popover ? `${styles.panel} ${styles.popover}` : `${styles.panel} ${dockColumn.column}`} style={style}
      role="dialog" aria-label={t("databaseView:automations")} data-desktop-automations data-popover={popover ? "true" : undefined}>
      <header className={styles.head}>
        {onBack ? (
          <button type="button" className={styles.back} aria-label={t("databaseView:backToViewSettings")} onClick={onBack}>
            <ArrowLeft size="small" aria-hidden="true" />
          </button>
        ) : null}
        <strong className={styles.title}>{t("databaseView:automations")}</strong>
        <button type="button" className={styles.close} aria-label={t("databaseView:closeAutomations")} onClick={onClose}>
          <X size="tiny" aria-hidden="true" />
        </button>
      </header>
      <label className={styles.search}>
        <input ref={searchRef} value={query} placeholder={t("databaseView:automationSearch")} aria-label={t("databaseView:automationSearch")}
          onChange={(event) => setQuery(event.currentTarget.value)} />
        {/* Notion's "입력 값 지우기" here is the shared one: the field keeps the focus
            (seen 2026-11-17 at 1440, a letter typed after the press went into the search). */}
        <SearchClearButton value={query} inputRef={searchRef} onClear={() => setQuery("")} />
      </label>
      <div className={styles.list}>
        {loading ? <p className={styles.note}>{t("databaseView:loadingAutomations")}</p> : null}
        {!loading && !automations.length ? readOnly ? <DatabaseAutomationReaderNote /> : <p className={styles.note}>{t("databaseView:noAutomations")}</p> : null}
        {!loading && automations.length > 0 && !shown.length ? <p className={styles.note} data-automation-no-match>{t("databaseView:automationNoMatch")}</p> : null}
        {shown.map((automation) => {
          const status = statusOf(automation);
          const reason = reasonOf(automation);
          const note = noteOf?.(automation);
          return (
            <article key={automation.id} className={styles.card} data-desktop-automation={automation.id}
              data-menu-open={menu?.automation.id === automation.id || undefined}
              onMouseEnter={(event) => showTip(automation, event.currentTarget)} onMouseLeave={hideTip}>
              <button type="button" className={styles.open} disabled={busy}
                aria-label={t(readOnly ? "databaseView:viewAutomationNamed" : "databaseView:editAutomationNamed", { name: nameOf(automation) })}
                onClick={() => { hideTip(); onOpen(automation); }}>
                <AutomationMarks trigger={triggerOf(automation)} actions={actionsOf(automation)} properties={properties} className={styles.marks} />
                <span className={styles.nameRow}>
                  <strong className={styles.name}>{nameOf(automation)}</strong>
                  {status ? <span className={styles.status} data-automation-status data-invalid={status.invalid || undefined}>
                    {status.invalid ? <WarningFillIcon size="micro" aria-hidden="true" /> : <PauseIcon size="micro" aria-hidden="true" />}{status.label}
                  </span> : null}
                </span>
                <span className={styles.summary} data-automation-summary>{summaryOf(automation)}</span>
                {reason ? <span className={styles.reason} data-automation-paused-reason>{reason}</span> : null}
                {note ? <span className={styles.reason} data-automation-waiting-note>{note}</span> : null}
              </button>
              {readOnly ? null : (
                <button type="button" className={styles.dots} disabled={busy} aria-label={t("databaseView:automationMenuFor", { name: nameOf(automation) })}
                  aria-haspopup="menu" aria-expanded={menu?.automation.id === automation.id}
                  onClick={(event) => { hideTip(); setMenu({ id: automation.id, trigger: event.currentTarget }); }}>
                  <DotsHorizontal size="regular" aria-hidden="true" />
                </button>
              )}
            </article>
          );
        })}
      </div>
      {onNew ? (
        <div className={styles.newArea}>
          <button type="button" className={styles.newButton} disabled={loading || busy} onClick={onNew} data-desktop-automation-new>
            <Plus size="small" aria-hidden="true" />{t("databaseView:newAutomation")}
          </button>
        </div>
      ) : null}
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {menu ? (
        <AutomationCardMenu automation={menu.automation} name={nameOf(menu.automation)} trigger={menu.trigger} onClose={() => setMenu(null)}
          invalid={statusOf(menu.automation)?.invalid === true} onToggle={() => onToggle(menu.automation)} onEdit={() => onOpen(menu.automation)}
          onDuplicate={() => onDuplicate(menu.automation)} onDelete={() => onDelete(menu.automation)} />
      ) : null}
      {tip && !menu ? <AutomationCardTip rect={tip.rect} name={nameOf(tip.automation)} summary={statusOf(tip.automation)?.invalid
        ? t(tip.automation.status === "active" ? "databaseView:automationInvalidActiveCaption" : "databaseView:automationInvalidPausedCaption")
        : summaryOf(tip.automation)} /> : null}
    </aside>,
    document.body,
  );
}

type CardMenuItem = { key: "toggle" | "edit" | "duplicate" | "delete"; text: string };

/**
 * A card's "⋯", as Notion's desktop (seen 2026-11-15 four times, Plus and
 * Business, inline and full-page databases): 180px under its button, "⚡ 사용
 * 중" with its switch lit as it opens, a line, then "삭제" — editing is the
 * card itself; an invalid one's (seen 2026-11-15 in two tabs) has no "사용 중"
 * nor line, as it cannot be turned on until fixed, but "편집", "복제" and
 * "삭제". The phone's keeps "편집" and "복제" (PhoneAutomationList), as
 * Notion's phone.
 */
function AutomationCardMenu({ automation, name, trigger, invalid, onClose, onToggle, onEdit, onDuplicate, onDelete }: {
  automation: DatabaseAutomationDefinition;
  name: string;
  trigger: HTMLElement;
  invalid: boolean;
  onClose(): void;
  onToggle(): void;
  onEdit(): void;
  onDuplicate(): void;
  onDelete(): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const ref = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const items: CardMenuItem[] = [
    ...(invalid ? [
      { key: "edit" as const, text: t("databaseView:edit") },
      { key: "duplicate" as const, text: t("databaseView:automationDuplicate") },
    ] : [{ key: "toggle" as const, text: t("databaseView:automationInUse") }]),
    { key: "delete", text: t("common:actions.delete") },
  ];
  const run: Record<CardMenuItem["key"], () => void> = { toggle: onToggle, edit: onEdit, duplicate: onDuplicate, delete: onDelete };
  const layer = useOverlayLayer(true, { kind: "menu", ref, onEscape: onClose, onOutsidePress: onClose, inside: () => [trigger],
    returnFocusRef: { current: trigger } });
  const menu = useNativeMenu({ items, open: true, menuRef: listRef, label: t("databaseView:automationMenuFor", { name }), highlightOnOpen: true,
    keyOf: (item) => item.key, textOf: (item) => item.text,
    // Each closes the menu first, the switch too, as Notion's (seen 2026-11-02: the card then says "중단됨").
    onAction: (item) => { onClose(); run[item.key](); } });
  // Under its "⋯", centred on it and kept on the screen.
  const rect = trigger.getBoundingClientRect();
  const width = 180;
  const left = Math.max(POPOVER_MARGIN, Math.min(rect.left + rect.width / 2 - width / 2, window.innerWidth - width - POPOVER_MARGIN));
  const on = automation.status === "active";
  const icons = { toggle: <LightningIcon size="regular" />, edit: <Pencil size="regular" />, duplicate: <Copy size="regular" />, delete: <Trash size="regular" /> };
  return createPortal(
    <div ref={ref} className={styles.menu} style={{ top: rect.bottom, left, width, zIndex: layer.zIndex }} onKeyDown={layer.onKeyDown}
      data-desktop-automation-menu>
      <div {...menu.menuProps} ref={listRef} className={styles.menuList}>
        {items.map((item) => (
          <NativeMenuItem key={item.key} state={menu.state} itemKey={item.key} className={styles.menuItem}
            role={item.key === "toggle" ? "menuitemcheckbox" : undefined} aria-checked={item.key === "toggle" ? on : undefined}
            data-menu-rule={(item.key === "delete" && !invalid) || undefined} data-desktop-automation-action={item.key}>
            {({ labelProps }) => (
              <>
                <span className={styles.menuIcon} aria-hidden="true">{icons[item.key]}</span>
                <span className={styles.menuText} {...labelProps}>{item.text}</span>
                {item.key === "toggle" ? <SwitchMark on={on} /> : null}
              </>
            )}
          </NativeMenuItem>
        ))}
      </div>
    </div>,
    document.body,
  );
}

/** A card's name and sentence in a dark tip at its left, its top level with the card's, as Notion's. */
function AutomationCardTip({ rect, name, summary }: { rect: DOMRect; name: string; summary: string }) {
  const ref = useRef<HTMLDivElement>(null);
  // Only shown, not a layer of its own: Escape still closes the panel.
  const place = usePopoverPlacement(ref, (size, viewport) => placeBeside(rect, size, viewport, { prefer: "left", gap: 6 }), [rect], undefined, false);
  return createPortal(
    <div ref={ref} className={styles.tip} style={popoverPlacementStyle(place)} role="tooltip" data-desktop-automation-tip>
      <span>{name}</span>
      {summary ? <small>{summary}</small> : null}
    </div>,
    document.body,
  );
}
