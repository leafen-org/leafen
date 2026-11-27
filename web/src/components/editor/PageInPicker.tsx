import { PersonAvatar } from "@/components/people/PersonAvatar";
import { type RefObject, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ChevronRight, LockIcon, Search } from "@/icons/leafen";
import { usePageVisits } from "@/components/library/useLibraryData";
import { useProductPages } from "@/components/product/productPagesContext";
import { usePageNames } from "@/lib/pageNames";
import { placeBeside, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { searchWorkspacePages } from "@/lib/workspacePageSearch";
import type { Page, Session } from "@/types";
import { PageIconGlyph } from "../page/PageIcon";
import { blockItemText } from "./blockItemLabels";
import {
  PAGE_IN_FOUND, isPageInPrivateRow, pageInFoundRows, pageInPrivateRows, pageInSuggestedRows, type PageInListRow,
} from "./pageInPickerModel";
import styles from "./PageInPicker.module.css";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { inlineEndKey } from "@/lib/readingDirection";
import { pageMapOf } from "@/lib/pageIndex";
import { imeComposing } from "@/lib/fieldKeyBoundary";

const WIDTH = 400;
const HEIGHT = 420;
const OVERLAP = 4;
const SEARCH_DELAY_MS = 150;
const NO_SHARED: readonly string[] = [];

/**
 * The pages "페이지 위치:" puts the blocks in, beside the turn list as
 * Notion's (seen 2026-10-01): a 400px window with "추가할 페이지 검색" taking
 * the keys, "추천" over the pages visited last, each opening to its
 * sub-pages, and a search's pages in their place, fitting them. Choosing
 * one puts the blocks there; Escape gives the keys back to the list. The
 * first row, "〈이름〉 개인 페이지" with its lock, opens to the reader's
 * private pages; it puts nothing there itself — pressed it opens or closes,
 * and Enter on it closes the window and the turn list, as Notion's (seen
 * 2026-10-02).
 */
export function PageInPicker({
  row, menu, pages, session, workspaceId, sourcePageId, unavailable, onChoose, onClose, returnFocusRef,
}: {
  /** The "페이지 위치:" row and the list it is in, which the window sits beside. */
  row: DOMRect;
  menu: DOMRect;
  pages: readonly Page[];
  session: Session;
  workspaceId: string;
  sourcePageId: string;
  unavailable?: (page: Page) => string | undefined;
  onChoose: (pageId: string) => void;
  /** Closes the window, and the turn list with it when the keys leave both. */
  onClose: (leaveList?: boolean) => void;
  returnFocusRef?: RefObject<HTMLElement | null>;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("blockHandle");
  const inputRef = useRef<HTMLInputElement>(null);
  const pickerRef = useRef<HTMLDivElement>(null), listRef = useRef<HTMLDivElement>(null);
  const pointerTreeAction = useRef<"private" | "expand" | null>(null);
  // Beside the list, over its edge as a submenu is, level with the row.
  const place = usePopoverPlacement(pickerRef, (size, viewport) => placeBeside(
    { top: row.top, bottom: row.bottom, left: menu.left, right: menu.right }, size, viewport,
    { gap: -OVERLAP, offsetTop: -OVERLAP, cap: HEIGHT },
  ), [row.top, row.bottom, menu.left, menu.right], undefined,
    { kind: "picker", onEscape: () => onClose(), onOutsidePress: () => onClose(true), returnFocusRef });
  const [query, setQuery] = useState("");
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  const [privateOpen, setPrivateOpen] = useState(false);
  const [privateExpanded, setPrivateExpanded] = useState<ReadonlySet<string>>(() => new Set());
  const sharedPageIds = useProductPages()?.sharedPageIds ?? NO_SHARED;
  const owner = session.user.name.trim() || session.user.email;
  const [found, setFound] = useState<readonly Page[]>([]);
  const visits = usePageVisits(session, workspaceId);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  useEffect(() => {
    const trimmed = query.trim();
    if (!trimmed) return undefined;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void searchWorkspacePages(session, trimmed, PAGE_IN_FOUND, workspaceId, { signal: controller.signal, places: true })
        .then(result => { if (!controller.signal.aborted) setFound(result.pages); })
        .catch(() => {
          // The pages already known still answer the search.
        });
    }, SEARCH_DELAY_MS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [query, session, workspaceId]);

  const rows = useMemo<PageInListRow[]>(() => {
    if (!query.trim()) {
      return [
        ...pageInPrivateRows({
          pages, workspaceId, sharedPageIds, sourcePageId, expanded: privateExpanded, open: privateOpen, unavailable,
        }),
        ...pageInSuggestedRows({ pages, visits, sourcePageId, expanded, unavailable }),
      ];
    }
    const known = pageMapOf(pages);
    const more = new Map<string, (typeof found)[number]>();
    for (const page of found) if (!known.has(page.id) && page.workspaceId === workspaceId) more.set(page.id, page);
    return pageInFoundRows({ pages: more.size ? [...pages, ...more.values()] : pages, query, sourcePageId, unavailable });
  }, [expanded, found, pages, privateExpanded, privateOpen, query, sharedPageIds, sourcePageId, unavailable, visits, workspaceId]);


  function toggle(choice: PageInListRow) {
    if (isPageInPrivateRow(choice)) {
      setPrivateOpen((open) => !open);
      return;
    }
    const { page } = choice;
    (choice.inPrivate ? setPrivateExpanded : setExpanded)((now) => {
      const next = new Set(now);
      if (next.has(page.id)) next.delete(page.id);
      else next.add(page.id);
      return next;
    });
  }

  function choose(choice: PageInListRow | undefined) {
    if (!choice || isPageInPrivateRow(choice) || choice.reason) return;
    onChoose(choice.page.id);
  }

  // The transient path distinguishes the same page shown under two native
  // branches; its action still uses the unchanged native page ID.
  const trail: string[] = [];
  let privateBranch = false;
  const choices = rows.map(choice => {
    if (isPageInPrivateRow(choice)) return { key: "__private", label: t("pageInPicker.private", { name: owner }), row: choice };
    if (!!choice.inPrivate !== privateBranch) { trail.length = 0; privateBranch = !!choice.inPrivate; }
    trail.length = choice.depth; trail[choice.depth] = choice.page.id;
    return { key: `${privateBranch ? "private" : "suggested"}:${trail.filter(Boolean).join("/")}`,
      label: pageNames.title(choice.page), row: choice };
  });
  const collection = useNativeAutocomplete({ items: choices, query, onQuery: setQuery, inputRef, listRef,
    popoverRef: pickerRef, label: blockItemText("mention.addPagePlaceholder"), commitTab: false,
    // Opened, and searched, no page is lit until the arrows light one; Enter
    // with none lit closes the window and the turn list, as Notion's (seen
    // 2026-11-14).
    highlightFirst: false, onEnterNone: (event) => { event.preventDefault(); onClose(true); },
    keyOf: choice => choice.key, textOf: choice => choice.label,
    disabledKeys: choices.filter(choice => !isPageInPrivateRow(choice.row) && choice.row.reason).map(choice => choice.key),
    onPick: choice => {
      const treeAction = pointerTreeAction.current;
      pointerTreeAction.current = null;
      if (treeAction === "expand") return;
      if (isPageInPrivateRow(choice.row)) {
        if (treeAction === "private") toggle(choice.row); else onClose(true);
      } else choose(choice.row);
    } });

  const picker = (
    <div ref={pickerRef} className={styles.picker}
      style={{ width: WIDTH, maxHeight: HEIGHT, height: query.trim() ? undefined : HEIGHT, ...popoverPlacementStyle(place) }}
      role="dialog"
      aria-label={t("pageInPicker.label")} data-page-in-picker
      onPointerDownCapture={event => {
        const target = event.target as HTMLElement;
        pointerTreeAction.current = target.closest("[data-page-in-expander]") ? "expand"
          : target.closest("[data-page-in-private]") ? "private" : null;
      }}
      onKeyDownCapture={event => {
        pointerTreeAction.current = null;
        if (event.defaultPrevented || imeComposing(event)) return;
        const target = choices.find(choice => choice.key === collection.activeKey)?.row;
        // Tree expansion must precede the combobox's horizontal text keys.
        if ((event.key === "ArrowRight" || event.key === "ArrowLeft") && target?.expandable
          && !query) {
          event.preventDefault(); event.stopPropagation();
          // Opening a row is toward the reading direction's end, as its chevron points.
          if ((event.key === inlineEndKey(event.currentTarget)) !== target.expanded) toggle(target);
        }
      }} onKeyDown={event => { if (!event.defaultPrevented) event.stopPropagation(); }}>
      <label className={styles.search}>
        <Search size="regular" aria-hidden="true" />
        <input {...collection.inputProps} ref={inputRef} placeholder={blockItemText("mention.addPagePlaceholder")} />
      </label>
      <div {...collection.listBoxProps} ref={listRef} className={styles.list}>
        {rows.length > 0 ? <div className={styles.heading}>{t("pageInPicker.suggested")}</div> : null}
        {choices.map(entry => {
          const choice = entry.row;
          const expand = choice.expandable ? (
            <button type="button" className={styles.expand} data-page-in-expander data-expanded={choice.expanded ? "true" : undefined}
              aria-label={t(choice.expanded ? "pageInPicker.collapse" : "pageInPicker.expand")} tabIndex={-1}
              onMouseDown={(event) => event.preventDefault()}
              onClick={(event) => { event.stopPropagation(); toggle(choice); }}>
              <ChevronRight size="tiny" aria-hidden="true" />
            </button>
          ) : <span className={styles.expandSpace} aria-hidden="true" />;
          if (isPageInPrivateRow(choice)) {
            return (
              <NativePickerOption element="div" key={entry.key} state={collection.state} itemKey={entry.key}
                className={styles.row} aria-expanded={choice.expanded} data-page-in-private
                style={{ paddingInlineStart: 6 }}>
                {({ labelProps }) => <>
                {expand}
                <span className={styles.icon}><PersonAvatar className={styles.owner} userId={session.user.id} name={owner} size={16} /></span>
                <span {...labelProps} className={styles.name}>{entry.label}</span>
                <LockIcon className={styles.lock} size="regular" aria-hidden="true" />
                </>}
              </NativePickerOption>
            );
          }
          return (
            <NativePickerOption element="div" key={entry.key} state={collection.state} itemKey={entry.key}
              className={styles.row} title={choice.reason} data-page-in-row={choice.page.id}
              style={{ paddingInlineStart: 6 + choice.depth * 14 }}>
              {({ labelProps }) => <>
              {expand}
              <span className={styles.icon}><PageIconGlyph page={choice.page} size={16} /></span>
              <span {...labelProps} className={styles.name}>{entry.label}</span>
              </>}
            </NativePickerOption>
          );
        })}
        {rows.length === 0 && query.trim() ? <p className={styles.empty}>{t("pageInPicker.empty")}</p> : null}
      </div>
    </div>
  );
  return typeof document === "undefined" ? picker : createPortal(picker, document.body);
}
