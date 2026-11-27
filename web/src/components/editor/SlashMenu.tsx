"use client";

import { useAppPlatformKnown } from "@/lib/appPlatformAvailability";
import {
  type HTMLAttributes,
  type ReactNode,
  useCallback,
  useDeferredValue,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneMenuHeader } from "./PhoneMenuHeader";
import phoneStyles from "./PhoneMenu.module.css";
import { i18next } from "@/i18n";
import { shortcutCompactLabel } from "@/lib/keyboardShortcuts";
import { placeRowTip, popoverBackdropStyle, type PopoverPlacement, type PopoverRect, rowTipStyle, samePopoverRect, usePopoverPlacement } from "@/lib/popoverPlacement";
import {
  blockDefDescription,
  blockDefLabel,
  matchBlocks,
  type BlockDef,
  type BlockSearchBounds,
} from "./blocks";
import { useAppBlockDefs } from "./appBlockDefs";
import { BlockIcon } from "./BlockIcon";
import { SlashTipPreview } from "./SlashTipPreview";
import { useSlashMenuPages } from "./slashMenuPages";
import { typedBlockShortcutHint } from "./textBlockInputModel";
import styles from "./editorStyles.generated.module.css";
import { CaretPickerOption, useCaretPicker } from "@/lib/caretPicker";
import { CheckIcon, ChevronRight } from "@/icons/leafen";
import type { Page, Session } from "@/types";
import { PageInPicker } from "./PageInPicker";
import { useEdgeFade } from "@/lib/edgeFade";
import { useMenuLineHandlesAway } from "./menuLineHandles";
import { slashRowWhileSuggesting } from "./suggestedSlashRows";
import { rememberSlashPick, slashSuggestions } from "./slashSuggestions";
import { useSlashHtmlLead } from "./slashHtmlLead";
import { useSlashMenuPlace } from "./slashMenuPlace";
import { afterNextPaint } from "@/lib/afterPress";
import { currentStripPanel } from "./stripPanelState";
import { PhoneStripPanel } from "./PhoneStripPanel";
import { stripAddOrder } from "./phoneStripTools";

// Its size and place at the "/" are slashMenuPlace.ts's.
// The rows a menu at its tallest (448px) shows, drawn when it opens.
const FIRST_ROWS = 16;

function groupLabel(group: BlockDef["group"]) {
  return i18next.t(`slashMenu:groups.${group}`);
}

// Untyped, Notion's list has its "가져오기" after the embeds (seen
// 2026-10-27; on 10-12 only a query found them), then "전환", "작업" and the
// colors, on an empty line or after words, drawn as the list is scrolled on
// (slashMenuPages — seen 2026-11-17; on 11-01 a walk with the arrows wrapped
// before the rows past the fifth page came in). A phone's sheet, not seen
// that far, ends with the imports. An installed app's blocks stay with the blocks.
// Notion's "잠깐의 휴식" between "작업" and the colors holds "커피 내리기", which
// copies a page of Notion's template gallery (a game in an HTML block) into the
// private pages (seen 2026-11-17 twice); it is left out with that gallery.
const GROUP_ORDER: BlockDef["group"][] = [
  "Basic", "Media", "Database", "Advanced", "Inline", "Embed", "Apps", "Import",
];
const LINE_GROUP_ORDER: BlockDef["group"][] = ["TurnInto", "Actions", "TextColor", "BackgroundColor"];

// Named, Notion's menu is one list without group titles; a "Turn into", a
// database or an embed item names its group after it ("Heading 1 · Turn
// into", "보드 보기 · 데이터베이스", seen 2026-10-25).
// An app's line says "App · its name" at its end, so "Apps" takes no suffix (2026-10-16).
const NAMED_GROUP_SUFFIX = new Set<BlockDef["group"]>(["TurnInto", "Database", "Embed", "Import"]);

// A row of "추천" stands for an item of its group, which keeps its own row;
// it names a group from the list above only ("페이지" has none, seen 2026-11-17).
type MenuDef = BlockDef & { suggestedOf?: BlockDef };

function defKey(def: MenuDef) {
  return def.suggestedOf ? `suggested:${def.id ?? def.type}` : def.id ?? def.type;
}

// A row ends with what makes it from the keyboard, where Leafen has that:
// the Markdown of its block ("#", "[]", "```") or an action's keys.
function rowShortcut(def: BlockDef) {
  if (def.action === "move_to") return shortcutCompactLabel("moveBlock");
  if (def.action === "copy_link") return shortcutCompactLabel("copyBlockLink") || undefined;
  if (def.action === "duplicate") return shortcutCompactLabel("duplicateBlock");
  if (def.action === "delete") return shortcutCompactLabel("deleteBlock");
  if (def.action === "present") return shortcutCompactLabel("presentPage");
  // The Markdown makes the plain block, not a variant ("Code - Mermaid").
  if (def.id || def.action || def.inline) return undefined;
  return typedBlockShortcutHint(def.type);
}

function MenuBackdrop({ onClose, place }: { onClose: () => void; place: PopoverPlacement | null }) {
  const { t } = useTranslation(["slashMenu", "common"]);
  return (
    <button
      type="button"
      className={`${styles.menuBackdrop} ${styles.slashBackdrop}`}
      style={popoverBackdropStyle(place)}
      onClick={onClose}
      tabIndex={-1}
      aria-label={t("slashMenu:closeBlockCommands")}
    />
  );
}

/**
 * What "페이지 위치:" puts the line's block in: the pages beside the menu, as
 * the block menu's (PageInPicker). The menu stays while they have the keys.
 */
export type SlashPageIn = {
  pages: readonly Page[];
  session: Session;
  workspaceId: string;
  sourcePageId: string;
  unavailable?: (page: Page) => string | undefined;
  onChoose: (pageId: string) => void;
  onHold: (held: boolean) => void;
};

export type SlashMenuAnchor = {
  left: number;
  top: number;
  bottom: number;
};

export function SlashMenu({
  anchor,
  bounds,
  query,
  templateMode = false,
  ownerBlockId,
  onPick,
  onClose,
  lineCommands = true, suggesting = false, lineEmpty = true,
  pageIn,
  currentType,
  syncedBlocks = true, workspaceId,
}: {
  anchor?: SlashMenuAnchor;
  bounds: { search: BlockSearchBounds };
  query: string;
  templateMode?: boolean;
  ownerBlockId?: string;
  onPick: (def: BlockDef) => void;
  onClose: (reason?: "owner_mismatch") => void;
  /** Without a line (the "+" menu), there is nothing to turn, act on or color. */
  lineCommands?: boolean;
  /** Suggesting mode: the rows it suggests there alone (suggestedSlashRows.ts). */
  suggesting?: boolean;
  /** Whether its line holds nothing but the "/", which "추천" weighs (slashSuggestions). */
  lineEmpty?: boolean;
  pageIn?: SlashPageIn;
  /** The line's kind: its "Turn into" row is ticked, as Notion's (seen 2026-11-03). */
  currentType?: string;
  /**
   * Whether it offers a synced block, made or turned into: inside a synced
   * block it doesn't, as Notion's (seen 2026-11-08 — keptOutOfSyncedBlocks).
   */
  syncedBlocks?: boolean;
  /** The page's workspace, whose guests' "추천" has no HTML lead (slashHtmlLead). */ workspaceId?: string;
}) {
  const { t } = useTranslation(["slashMenu", "common"]);
  const appPlatformKnown = useAppPlatformKnown(), appPlatform = appPlatformKnown ?? false;
  const htmlLead = useSlashHtmlLead(workspaceId, appPlatform);
  const { phone } = useSidebarLayout(true);
  // A phone strip's "+" lists the blocks in the keyboard's place (PhoneStripPanel), as the Notion Android app's.
  const [stripped] = useState(() => phone && currentStripPanel()?.kind === "add");
  // An installed app's blocks, which Notion has none of, and whether they are
  // still on their way (the server not yet saying it has apps, or their list).
  const { defs: appBlocks, reading } = useAppBlockDefs(appPlatform && !templateMode);
  const appRowsReading = !templateMode && (appPlatformKnown === null || reading);
  const menuId = useId();
  const listRef = useRef<HTMLDivElement>(null);
  useMenuLineHandlesAway(ownerBlockId);
  const results = useMemo(() => {
    try {
      // An import acts on the page, so the "+" menu has it too.
      // A phone's sheet has no form, as Notion's "블록 삽입" there (seen 2026-11-11 at 402pt).
      return matchBlocks(query, bounds.search, appBlocks, (def) => (lineCommands || !def.action || def.action === "import")
        && (!def.appPlatform || appPlatform) && (!def.pageIn || Boolean(pageIn)) && (syncedBlocks || def.type !== "synced_block")
        && !(phone && def.id === "inline_database_form") && (!suggesting || slashRowWhileSuggesting(def)));
    } catch (error) {
      // A query past the search's bounds finds nothing.
      if (error instanceof RangeError) return [];
      throw error;
    }
  }, [appBlocks, appPlatform, bounds.search, lineCommands, phone, query, suggesting, syncedBlocks]);
  const trimmedQuery = query.trim();
  const sections = useMemo((): Array<{ label: string; items: MenuDef[] }> => {
    // Typed, the matches sit under "필터링된 결과", as Notion's (seen 2026-10-07),
    // a space alone too ("/ ", seen 2026-11-01).
    if (query) return results.length ? [{ label: t("slashMenu:filteredResults"), items: results }] : [];
    const groups = (phone ? GROUP_ORDER : [...GROUP_ORDER, ...LINE_GROUP_ORDER])
      .map((group) => ({ label: groupLabel(group), items: results.filter((def) => def.group === group) }))
      .filter((section) => section.items.length > 0);
    // Notion's "추천" leads both "/" and "+": the rows picked most of late (slashSuggestions).
    // Enter on the bare menu takes the first. A query finds each once, in its group.
    // A phone's sheet starts at "기본 블록" (Notion's, seen 2026-11-11 at 402pt).
    const suggested = phone ? [] : slashSuggestions(results, { lineEmpty, html: htmlLead });
    return suggested.length
      ? [{ label: t("slashMenu:suggested"), items: suggested.map((def) => ({ ...def, suggestedOf: def })) }, ...groups]
      : groups;
  }, [htmlLead, lineEmpty, phone, query, results, t]);
  const visibleItems = useMemo(() => sections.flatMap((section) => section.items), [sections]);
  // Opened, it draws the rows its first screen shows, and the rest of its
  // first page right after that frame is painted (user decision 2026-11-12: a
  // press answers on the next frame — its 78 rows took the "/" key 50 ms of
  // script and 15 ms of layout); a page more as its list is scrolled on, as
  // Notion's (slashMenuPages). A query typed later draws its first page at once.
  const opening = useDeferredValue(false, true);
  const shownRows = useSlashMenuPages(listRef, query, visibleItems.length, !opening);
  const listedItems = useMemo(() => visibleItems.slice(0, opening ? FIRST_ROWS : shownRows), [opening, shownRows, visibleItems]);
  const listedSections = useMemo(() => {
    if (listedItems.length === visibleItems.length) return sections;
    const listed = new Set(listedItems);
    return sections.flatMap((section) => {
      const items = section.items.filter((item) => listed.has(item));
      return items.length ? [{ ...section, items }] : [];
    });
  }, [listedItems, sections, visibleItems.length]);
  // "페이지 위치:" opens the pages beside its row, the menu staying open, as
  // Notion's (seen 2026-10-25); the rest act on the line at once.
  const [pageInAt, setPageInAt] = useState<{ row: DOMRect; menu: DOMRect } | null>(null);
  const pageInShown = useRef(false);
  const pick = useCallback((chosen: MenuDef) => {
    const def = chosen.suggestedOf ?? chosen, pickedAt = Date.now();
    // Counted for "추천" once the press is painted: the pick draws first.
    afterNextPaint(() => rememberSlashPick(def, pickedAt));
    if (!def.pageIn) { onPick(def); return; }
    const row = listRef.current?.querySelector<HTMLElement>(`[data-key="${CSS.escape(defKey(def))}"]`)?.getBoundingClientRect();
    const box = slashRef.current?.getBoundingClientRect();
    if (!pageIn || !row || !box) return;
    pageInShown.current = true;
    pageIn.onHold(true);
    // Its top is the menu's, not the row's, as Notion's (seen 2026-10-25).
    setPageInAt({ row: new DOMRect(row.x, box.top, row.width, row.height), menu: box });
  }, [onPick, pageIn]);
  const closePageIn = useCallback((leaveList?: boolean) => {
    setPageInAt(null);
    pageIn?.onHold(false);
    // The Escape that closed the pages leaves the menu open.
    window.setTimeout(() => { pageInShown.current = false; }, 0);
    if (leaveList) onClose();
  }, [onClose, pageIn]);
  const pickerState = useCaretPicker({ items: listedItems, query, id: menuId,
    label: t("slashMenu:blockCommands"), listRef, keyOf: defKey, textOf: blockDefLabel,
    onPick: pick, onClose, ownerBlockId, onOwnerMismatch: () => onClose("owner_mismatch"), closeEmptyEnter: true, tab: "close",
    wrap: "throughNone" });
  // Escape or a press outside closes it, but while "페이지 위치:" has the keys.
  const slashRef = useRef<HTMLDivElement>(null);
  const { atCaret, place, fixedStyle } = useSlashMenuPlace(slashRef, stripped ? undefined : anchor, { phone, templateMode,
    dismiss: () => { if (!pageInShown.current) onClose(); }, keys: pickerState });
  // Its rows fade at the edges they scroll on past, as Notion's "/" (seen 2026-11-13).
  useEdgeFade(listRef, listedItems);
  const active = pickerState.active;
  const emptyId = `${menuId}-empty`;
  const statusId = `${menuId}-status`;
  const activeDef = active >= 0 ? listedItems[active] : undefined;
  // As Notion's (seen 2026-10-01), a query that finds nothing leaves the menu
  // open and empty; the fourth letter typed after the last find closes it.
  // App rows still being read are not "nothing": it waits for them, then
  // closes only if the query finds none (2026-11-17, an app's row typed fast).
  const foundAt = useRef(0);
  useEffect(() => {
    if (visibleItems.length > 0) foundAt.current = query.length;
    else if (!appRowsReading && query.length - foundAt.current >= 4) onClose();
  }, [appRowsReading, onClose, query, visibleItems.length]);

  // The chosen row's description follows that row, and goes while the row
  // is scrolled out of the menu.
  const [tipRow, setTipRow] = useState<PopoverRect | null>(null);
  const tipRef = useRef<HTMLDivElement>(null);
  const tip = usePopoverPlacement(tipRef, tipRow ? (size, viewport) => placeRowTip(tipRow, size, viewport) : null, [tipRow], undefined, false);
  const placeTip = useCallback(() => {
    const list = listRef.current;
    const item = list?.querySelector<HTMLElement>(`[data-active="true"]`);
    if (!list || !item || templateMode) {
      setTipRow(null);
      return;
    }
    const menuBox = list.getBoundingClientRect();
    const itemBox = item.getBoundingClientRect();
    if (itemBox.bottom <= menuBox.top || itemBox.top >= menuBox.bottom) {
      setTipRow(null);
      return;
    }
    const next = { top: itemBox.top, bottom: itemBox.bottom, left: itemBox.left, right: itemBox.right };
    setTipRow((current) => samePopoverRect(current, next) ? current : next);
  }, [templateMode]);
  useLayoutEffect(() => {
    placeTip();
  }, [active, fixedStyle, placeTip, listedItems]);
  // A name Leafen's Korean font draws wider than Notion's stays whole on its
  // row, as Notion's does (Korean's page mention item, 2026-11-01;
  // a cut is a defect, font decision B): its letters close up by what it
  // overflows, at most 1px each, before it ends in "…".
  // All are measured first, then those over are closed up: one layout.
  useLayoutEffect(() => {
    const names = [...listRef.current?.querySelectorAll<HTMLElement>("[data-slash-name]") ?? []];
    for (const name of names) name.style.letterSpacing = "";
    const overs = names.map((name) => name.scrollWidth - name.clientWidth);
    names.forEach((name, index) => {
      if (overs[index] > 0) name.style.letterSpacing = `${-Math.min(1, overs[index] / Math.max(1, name.textContent?.length ?? 1))}px`;
    });
  }, [listedItems]);

  if (stripped) {
    // Its first screen's cards are drawn as it opens, the rest after that frame (as the rows, above).
    const drawn = opening ? new Set(visibleItems.slice(0, FIRST_ROWS)) : null;
    return <PhoneStripPanel kind="add" label={t("editor:phoneToolbar.insert")} onClose={onClose}
      sections={sections.map((section) => ({ label: section.label, cards: stripAddOrder(section.items).filter((def) => !drawn || drawn.has(def)).map((def) => ({ key: defKey(def),
        icon: <BlockIcon def={def} size="large" />, label: def.id === "inline_database_linked" ? t("slashMenu:phoneLinkedView") : blockDefLabel(def),
        run: () => pick(def), data: { "data-slash-item": defKey(def), "data-phone-insert-row": "" } })) })).filter((section) => section.cards.length > 0)} />;
  }

  // The rows scroll above a line and a "메뉴 닫기 esc" row that stays at the
  // menu's foot, as Notion's; the template menu has only its rows. A menu at
  // the caret leaves the page under it uncovered.
  const frame = (list: ReactNode, listProps: HTMLAttributes<HTMLDivElement>) => (
    <>
      {atCaret ? null : <MenuBackdrop onClose={onClose} place={place} />}
      <div
        ref={slashRef}
        className={`${styles.slash} ${phone ? phoneStyles.menu : ""}`}
        style={fixedStyle}
        data-slash-owner-block-id={ownerBlockId}
        data-moved-by={pickerState.moved ?? undefined}
        data-filtered={query ? "true" : undefined}
        data-template-slash-menu={templateMode ? "true" : undefined}
        data-phone-insert={phone ? "" : undefined}
        onMouseDown={(e) => e.preventDefault()}
      >
        {phone ? <PhoneMenuHeader title={t("editor:phoneToolbar.insert")} onClose={onClose} /> : null}
        <div
          ref={listRef}
          className={styles.slashList}
          data-phone-menu-list={phone ? "true" : undefined}
          data-edge-fade=""
          {...pickerState.listBoxProps}
          tabIndex={-1}
          {...listProps}
        >
          {list}
        </div>
        {templateMode || phone ? null : (
          <button type="button" className={styles.slashFooter} tabIndex={-1} aria-hidden="true" data-slash-close
            onClick={() => onClose()}>
            <span className={styles.slashFooterRow}>
              <span>{t("slashMenu:closeMenu")}</span>
              <kbd>esc</kbd>
            </span>
          </button>
        )}
      </div>
    </>
  );

  // Empty, Notion's menu shows only its close row; the finding is said aloud
  // (the template menu, without that row, still shows it).
  if (visibleItems.length === 0) {
    const menu = frame(
      <div id={emptyId} className={templateMode ? styles.slashEmpty : styles.srOnly} role="status">
        {appRowsReading ? t("common:appPeople.loading")
          : trimmedQuery ? t("slashMenu:noBlocksFor", { query: trimmedQuery })
          : t("slashMenu:noBlocks")}
      </div>,
      { "aria-describedby": emptyId },
    );
    return typeof document === "undefined" ? menu : createPortal(menu, document.body);
  }

  const list = (
    <>
      <div id={statusId} className={styles.srOnly} aria-live="polite">
        {activeDef
          ? t("slashMenu:itemStatus", {
              label: blockDefLabel(activeDef),
              position: active + 1,
              total: visibleItems.length,
            })
          : t("slashMenu:listStatus", { total: visibleItems.length })}
      </div>
      {listedSections.map((section) => {
        const firstIndex = visibleItems.indexOf(section.items[0]);
        const sectionId = `${menuId}-section-${section.label.replace(/\s+/g, "-").toLowerCase()}`;
        return (
          <div
            key={section.label}
            className={styles.slashSection}
            data-phone-insert-section={phone ? "" : undefined}
            role="group"
            aria-labelledby={section.label ? sectionId : undefined}
            aria-label={section.label ? undefined : t("slashMenu:blockCommands")}
          >
            {section.label ? (
              <div id={sectionId} className={styles.slashLabel} data-phone-insert-label={phone ? "" : undefined}>
                {section.label}
              </div>
            ) : null}
            {section.items.map((d, i) => {
              const itemIndex = firstIndex + i;
              return (
                <CaretPickerOption
                  state={pickerState.state} itemKey={defKey(d)}
                  key={`${section.label}-${defKey(d)}`}
                  className={styles.slashItem}
                  data-phone-insert-row={phone ? "" : undefined}
                  data-page-in={d.pageIn ? "true" : undefined}
                  aria-posinset={itemIndex + 1}
                  aria-setsize={visibleItems.length}
                >{({ labelProps }) => <>
                  <span
                    className={styles.slashGlyph}
                    data-template-block-command-icon={templateMode ? "true" : undefined}
                    aria-hidden="true"
                  >
                    <BlockIcon def={d} size="regular" />
                  </span>
                  <span className={styles.slashText}>
                    <span className={styles.slashName} {...labelProps}>
                      <span className={styles.slashNameText} data-slash-name>{phone && d.id === "inline_database_linked" ? t("slashMenu:phoneLinkedView") : blockDefLabel(d)}</span>
                      {(query || d.suggestedOf) && NAMED_GROUP_SUFFIX.has(d.group) ? (
                        <span className={styles.slashGroupSuffix}>· {groupLabel(d.group)}</span>
                      ) : null}
                      {d.newMark ? <span className={styles.slashNew} data-slash-new>{t("slashMenu:newMark")}</span> : null}
                      {d.betaMark ? <span className={styles.slashBeta} data-slash-beta>{t("slashMenu:betaMark")}</span> : null}
                    </span>
                    {d.app ? (
                      <span className={styles.slashAppMark} data-slash-app-mark>{t("slashMenu:appMark", { app: d.app.name })}</span>
                    ) : d.pageIn ? <span className={styles.slashShortcut} aria-hidden="true"><ChevronRight size="small" /></span>
                    : d.action === "turn_into" && d.type === currentType ? <span className={styles.slashShortcut} aria-hidden="true"><CheckIcon size="small" /></span>
                    : rowShortcut(d) ? <span className={styles.slashShortcut} data-slash-shortcut>{rowShortcut(d)}</span> : null}
                  </span>
                </>}</CaretPickerOption>
              );
            })}
          </div>
        );
      })}
    </>
  );
  const menu = (
    <>
      {frame(list, { "aria-activedescendant": pickerState.activeId, "aria-describedby": statusId, onScroll: placeTip })}
      {pageInAt && pageIn ? (
        <PageInPicker row={pageInAt.row} menu={pageInAt.menu} pages={pageIn.pages} session={pageIn.session}
          workspaceId={pageIn.workspaceId} sourcePageId={pageIn.sourcePageId} unavailable={pageIn.unavailable}
          onChoose={(pageId) => { setPageInAt(null); pageIn.onHold(false); pageIn.onChoose(pageId); }} onClose={closePageIn} />
      ) : null}
      {!pageInAt && tipRow && activeDef && pickerState.moved && blockDefDescription(activeDef) ? (
        <div ref={tipRef} className={styles.slashTip} style={rowTipStyle(tip, place)} role="tooltip" data-slash-tip>
          <SlashTipPreview def={activeDef} />
          {blockDefDescription(activeDef)}
        </div>
      ) : null}
    </>
  );
  return typeof document === "undefined" ? menu : createPortal(menu, document.body);
}
