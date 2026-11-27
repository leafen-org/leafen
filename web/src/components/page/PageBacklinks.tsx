"use client";

import { blockAnchor } from "@/lib/blockAddress";
import { Fragment, useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useRouter } from "@/lib/router";
import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import {
  mergedBlocks,
  pageReferenceHits,
  type BlocksResult,
  type PageReferenceHit,
} from "@/lib/backlinks";
import { ArrowDownLeftIcon } from "@/icons/leafen";
import { pageHref } from "@/lib/navigation";
import { usePageNames } from "@/lib/pageNames";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import type { BacklinksDisplay, Block, Page } from "@/lib/types";
import { LinkIcon } from "../icons";
import { HoverTip } from "../IconTipButton";
import { PageIconGlyph } from "./PageIcon";
import styles from "./PageHeader.module.css";

const blockEdited = (hit: PageReferenceHit) => hit.block.updatedAt ?? hit.block.createdAt ?? "";

/** Where a backlink's line goes: the block that links here, on its page, a
 * page whose title links here, or a database page — its body or its cells —
 * full as its page, at its body's block, as Notion's opens it at its own
 * address (seen 2026-11-14). */
function backlinkHref(hit: PageReferenceHit) {
  const sourceRow = (hit.block as { sourceRow?: { containerPageId: string } }).sourceRow;
  const type = hit.block.type as string;
  const at = type === "page_title" || type === "database_cell" ? "" : blockAnchor(hit.page.id, hit.block.id);
  if (sourceRow) return `${pageHref(sourceRow.containerPageId, new URLSearchParams({ p: hit.page.id, pm: "f" }))}${at}`;
  return `${pageHref(hit.page.id)}${at}`;
}

const BACKLINKS_PAGE_LIMIT = 50;

// The count each page last showed on this browser, the latest 200: a page
// opened again by its address shows it from its first paint, before its
// backlinks are read.
const SHOWN_COUNTS = "pageBacklinkCounts";
const SHOWN_COUNTS_KEPT = 200;
type ShownCount = [count: number, partial: boolean];
function shownCounts(): Record<string, ShownCount> {
  const kept = parseStored(personValue(SHOWN_COUNTS));
  return kept && typeof kept === "object" && !Array.isArray(kept) ? kept as Record<string, ShownCount> : {};
}
function keepShownCount(key: string, count: number, partial: boolean) {
  const counts = shownCounts(), held = counts[key];
  if (count === 0 && !partial ? held === undefined : held?.[0] === count && held[1] === partial) return;
  delete counts[key];
  if (count > 0 || partial) counts[key] = [count, partial];
  const keys = Object.keys(counts);
  for (const old of keys.slice(0, Math.max(0, keys.length - SHOWN_COUNTS_KEPT))) delete counts[old];
  storePersonValue(SHOWN_COUNTS, keys.length > 0 ? JSON.stringify(counts) : null);
}

export type PageBacklinksLoadResult = BlocksResult & {
  hasPrevious: boolean;
  previousCursor?: string | null;
  hasMore: boolean;
  nextCursor?: string | null;
  pages?: Page[];
  /** Pages the viewer cannot read that link here, shown only as private. */
  privateSources?: number;
  restarted?: boolean;
};

export type PageBacklinksLoader = ((
  pageId: string,
  limit: number,
  workspaceId: string,
  options: { sourceCursor?: string; signal: AbortSignal },
) => Promise<PageBacklinksLoadResult>) & {
  /** The first window last read of the page, shown while it is read anew. */
  last?: (pageId: string, limit: number, workspaceId: string) => PageBacklinksLoadResult | undefined;
};

interface RemoteBacklinksState {
  blocks: Block[];
  hasPrevious: boolean;
  hasMore: boolean;
  key: string;
  loadingCursor: string | null;
  nextCursor?: string;
  pages: Page[];
  previousCursor?: string;
  privateSources: number;
}

const EMPTY_REMOTE_BACKLINKS: RemoteBacklinksState = {
  blocks: [],
  hasPrevious: false,
  hasMore: false,
  key: "",
  loadingCursor: null,
  pages: [],
  privateSources: 0,
};

function remoteStateOf(key: string, result: PageBacklinksLoadResult): RemoteBacklinksState {
  return {
    key,
    blocks: result.blocks,
    pages: result.pages ?? [],
    privateSources: result.privateSources ?? 0,
    hasPrevious: result.hasPrevious === true,
    ...(result.previousCursor ? { previousCursor: result.previousCursor } : {}),
    hasMore: result.hasMore === true,
    ...(result.nextCursor ? { nextCursor: result.nextCursor } : {}),
    loadingCursor: null,
  };
}

export function PageBacklinks({
  pageId,
  workspaceId,
  display = "default",
  localBlocksByPage,
  localPagesById,
  loadedSourcePageIds,
  listBacklinks,
  onApplyRemotePage,
  onSidebarClose,
}: {
  pageId: string;
  workspaceId?: string;
  display?: BacklinksDisplay;
  localBlocksByPage: Readonly<Record<string, Block[]>>;
  localPagesById: Readonly<Record<string, Page>>;
  loadedSourcePageIds: readonly string[];
  listBacklinks: PageBacklinksLoader;
  onApplyRemotePage: (page: Page) => void;
  onSidebarClose: () => void;
}) {
  const router = useRouter();
  // Its pages named as they are now, followed as they are renamed anywhere (lib/pageNames.ts).
  const { t } = useTranslation(["pageBacklinks", "updatesPanel"]), pageNames = usePageNames();
  const popoverId = useId();
  const wrapRef = useRef<HTMLDivElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const requestGenerationRef = useRef(0);
  const windowControllerRef = useRef<AbortController | null>(null);
  const [remoteState, setRemoteState] = useState<RemoteBacklinksState>(EMPTY_REMOTE_BACKLINKS);
  const [openKey, setOpenKey] = useState<string | null>(null);
  const open = openKey === pageId;
  const close = useCallback(() => setOpenKey(null), []);
  usePopoverAppear(open, popoverRef);
  const layer = useOverlayLayer(open, { kind: "picker", ref: popoverRef, inside: () => [wrapRef.current],
    onEscape: close, onOutsidePress: close, initialFocus: popoverRef });
  const remoteKey = `${pageId}:${workspaceId ?? ""}`;
  // A page opened again shows what was last read of it from its first paint,
  // as Notion's head, while it is read anew.
  const kept = remoteState.key === remoteKey || display === "off" || !workspaceId ? undefined
    : listBacklinks.last?.(pageId, BACKLINKS_PAGE_LIMIT, workspaceId);
  const currentRemote = remoteState.key === remoteKey ? remoteState
    : kept ? remoteStateOf(remoteKey, kept) : EMPTY_REMOTE_BACKLINKS;
  const read = remoteState.key === remoteKey || kept !== undefined;
  const shownBefore = useMemo(() => (display === "off" ? undefined : shownCounts()[remoteKey]), [display, remoteKey]);
  const localSourceBlocksByPage = useMemo(() => {
    if (display === "off") return {};
    const sources: Record<string, Block[]> = {};
    for (const sourcePageId of loadedSourcePageIds) {
      if (sourcePageId === pageId) continue;
      const blocks = localBlocksByPage[sourcePageId];
      if (blocks) sources[sourcePageId] = blocks;
    }
    return sources;
  }, [display, loadedSourcePageIds, localBlocksByPage, pageId]);
  const loadedSourcePages = useMemo(
    () => new Set(Object.keys(localSourceBlocksByPage)),
    [localSourceBlocksByPage],
  );
  const relevantPageSeedIds = useMemo(() => {
    if (display === "off") return [];
    const ids = new Set<string>([pageId, ...loadedSourcePageIds]);
    for (const block of currentRemote.blocks) ids.add(block.pageId);
    for (const page of currentRemote.pages) {
      ids.add(page.id);
      if (page.parentId) ids.add(page.parentId);
    }
    return Array.from(ids);
  }, [currentRemote.blocks, currentRemote.pages, display, loadedSourcePageIds, pageId]);
  const relevantLocalPagesById = useMemo(() => {
    if (display === "off") return {};
    const pages: Record<string, Page> = {};
    const pending = [...relevantPageSeedIds];
    const visited = new Set<string>();
    while (pending.length > 0) {
      const id = pending.pop();
      if (!id || visited.has(id)) continue;
      visited.add(id);
      const page = localPagesById[id];
      if (!page) continue;
      pages[id] = page;
      if (page.parentId) pending.push(page.parentId);
    }
    return pages;
  }, [display, localPagesById, relevantPageSeedIds]);

  // Read as the page opens, with its other reads: read seconds after it,
  // "N개의 백링크" came at the head's start and pushed its buttons over, and a
  // press meant for it added an icon (2026-11-11; Notion's is there from the
  // page's first paint).
  useEffect(() => {
    const generation = requestGenerationRef.current + 1;
    requestGenerationRef.current = generation;
    const controller = new AbortController();
    const isCurrent = () => (
      !controller.signal.aborted
      && generation === requestGenerationRef.current
    );
    const cleanup = () => {
      if (generation === requestGenerationRef.current) requestGenerationRef.current += 1;
      controller.abort();
      windowControllerRef.current?.abort();
      windowControllerRef.current = null;
    };

    if (display === "off" || !workspaceId) return cleanup;

    listBacklinks(pageId, BACKLINKS_PAGE_LIMIT, workspaceId, {
      signal: controller.signal,
    }).then((result) => {
      if (isCurrent()) setRemoteState(remoteStateOf(remoteKey, result));
    }).catch(() => {
      if (isCurrent()) setRemoteState({ ...EMPTY_REMOTE_BACKLINKS, key: remoteKey });
    });

    return cleanup;
  }, [display, listBacklinks, pageId, remoteKey, workspaceId]);

  const loadWindow = useCallback((sourceCursor?: string) => {
    if (
      !workspaceId
      || !sourceCursor
      || currentRemote.loadingCursor !== null
      || windowControllerRef.current !== null
    ) {
      return;
    }
    const generation = requestGenerationRef.current;
    const controller = new AbortController();
    windowControllerRef.current = controller;
    setRemoteState((state) => state.key === remoteKey
      ? { ...state, loadingCursor: sourceCursor }
      : state);
    listBacklinks(pageId, BACKLINKS_PAGE_LIMIT, workspaceId, {
      sourceCursor,
      signal: controller.signal,
    }).then((result) => {
      if (controller.signal.aborted || generation !== requestGenerationRef.current) return;
      setRemoteState((state) => {
        if (
          state.key !== remoteKey
          || (state.previousCursor !== sourceCursor && state.nextCursor !== sourceCursor)
        ) return state;
        return remoteStateOf(remoteKey, result);
      });
    }).catch(() => {
      if (controller.signal.aborted || generation !== requestGenerationRef.current) return;
      setRemoteState((state) => state.key === remoteKey
        ? { ...state, loadingCursor: null }
        : state);
    }).finally(() => {
      if (windowControllerRef.current === controller) windowControllerRef.current = null;
    });
  }, [currentRemote.loadingCursor, listBacklinks, pageId, remoteKey, workspaceId]);

  const effectivePagesById = useMemo(() => ({
    ...Object.fromEntries(currentRemote.pages.map((page) => [page.id, page])),
    ...relevantLocalPagesById,
  }), [currentRemote.pages, relevantLocalPagesById]);

  const backlinks = useMemo<PageReferenceHit[]>(() => {
    if (display === "off") return [];
    const allBlocks = mergedBlocks(
      currentRemote.blocks,
      localSourceBlocksByPage,
      loadedSourcePages,
    );
    return pageReferenceHits(
      allBlocks,
      effectivePagesById,
      { targetPageId: pageId },
    );
  }, [currentRemote.blocks, display, effectivePagesById, loadedSourcePages, localSourceBlocksByPage, pageId]);
  // As in Notion, private sources show beside readable backlinks and
  // nothing shows when every backlink is private.
  const privateCount = backlinks.length > 0 ? currentRemote.privateSources : 0;
  const backlinkCount = backlinks.length + privateCount;
  // Notion's popover lists each source page once, opening the block of it
  // that links here last edited (seen 2026-10-29); the count is every link.
  const sources = useMemo(() => {
    const byPage = new Map<string, PageReferenceHit>();
    for (const hit of backlinks) {
      const held = byPage.get(hit.page.id);
      if (!held || blockEdited(hit) > blockEdited(held)) byPage.set(hit.page.id, hit);
    }
    return [...byPage.values()];
  }, [backlinks]);
  const hasWindowNavigation = currentRemote.hasPrevious || currentRemote.hasMore;
  useEffect(() => {
    if (remoteState.key === remoteKey) keepShownCount(remoteKey, backlinkCount, hasWindowNavigation);
  }, [backlinkCount, hasWindowNavigation, remoteKey, remoteState.key]);
  const shownCount = read ? backlinkCount : shownBefore?.[0] ?? 0;
  const shownPartial = read ? hasWindowNavigation : shownBefore?.[1] === true;
  const windowNavigation = hasWindowNavigation ? (
    <div className={styles.backlinkWindow}>
      <button
        type="button"
        className={styles.backlinkMore}
        disabled={!currentRemote.previousCursor || currentRemote.loadingCursor !== null}
        onClick={() => loadWindow(currentRemote.previousCursor)}
      >
        {currentRemote.loadingCursor === currentRemote.previousCursor
          ? t("pageBacklinks:more.loading")
          : t("updatesPanel:versions.previous")}
      </button>
      <button
        type="button"
        className={styles.backlinkMore}
        disabled={!currentRemote.nextCursor || currentRemote.loadingCursor !== null}
        onClick={() => loadWindow(currentRemote.nextCursor)}
      >
        {currentRemote.loadingCursor === currentRemote.nextCursor
          ? t("pageBacklinks:more.loading")
          : t("updatesPanel:versions.next")}
      </button>
    </div>
  ) : null;

  const openBacklink = useCallback(
    (hit: PageReferenceHit, newTab = false) => {
      const href = backlinkHref(hit);
      if (newTab) {
        window.open(href, "_blank", "noopener,noreferrer");
        return;
      }
      const sourceRow = (hit.block as { sourceRow?: unknown }).sourceRow;
      if (!sourceRow && !localPagesById[hit.page.id]) onApplyRemotePage(hit.page);
      close();
      onSidebarClose();
      router.push(href);
    },
    [close, localPagesById, onApplyRemotePage, onSidebarClose, router],
  );

  if (display === "off" || (read ? backlinks.length === 0 && !hasWindowNavigation : !shownBefore)) return null;

  // As Notion's (seen 2026-10-29): "N개의 백링크" heads the options over the
  // title, and opens "이 페이지에 연결됨" under it — a line per source page
  // with its icon, its underlined title and, at the end, where it sits.
  // Notion keeps no other way to show them, so a stored "expanded" shows so too.
  return (
    <div ref={wrapRef} className={styles.backlinksWrap} data-backlinks-open={open ? "true" : undefined}>
      <button
        type="button"
        className={`${styles.ctrlBtn} ${styles.backlinkButton}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? popoverId : undefined}
        data-page-header-control
        data-page-backlinks
        onClick={() => setOpenKey(open ? null : pageId)}
      >
        <ArrowDownLeftIcon size="tiny" aria-hidden="true" />
        <span>
          {shownPartial
            ? t("pageBacklinks:toggle.partial", { count: shownCount })
            : t("pageBacklinks:toggle.count", { count: shownCount })}
        </span>
      </button>
      {open ? (
        <div
          ref={popoverRef}
          id={popoverId}
          className={styles.backlinkPopover}
          role="dialog"
          aria-label={t("pageBacklinks:popover.title")}
          tabIndex={-1}
          style={{ zIndex: layer.zIndex }}
          onKeyDown={layer.onKeyDown}
          data-page-backlinks-popover
        >
          <div className={styles.backlinkPopoverTitle}>{t("pageBacklinks:popover.title")}</div>
          <div className={styles.backlinkList}>
            {sources.map((hit) => (
              <a
                key={hit.page.id}
                href={backlinkHref(hit)}
                className={styles.backlinkRow}
                onClick={(event) => {
                  event.preventDefault();
                  openBacklink(hit, event.metaKey || event.ctrlKey);
                }}
                onAuxClick={(event) => {
                  if (event.button === 1) {
                    event.preventDefault();
                    openBacklink(hit, true);
                  }
                }}
              >
                <ArrowDownLeftIcon size="small" aria-hidden="true" />
                <span className={styles.backlinkIcon} aria-hidden="true">
                  <PageIconGlyph page={{ icon: hit.page.icon ?? "", iconType: hit.page.iconType ?? "none",
                    kind: hit.page.kind, hasContent: hit.page.hasContent }} size={18} />
                </span>
                <span className={styles.backlinkTitle}>{pageNames.title(hit.page)}</span>
                {hit.path.length > 0 ? <BacklinkPath titles={hit.path} /> : null}
              </a>
            ))}
            {Array.from({ length: privateCount }, (_, index) => (
              <div key={`private:${index}`} className={styles.backlinkRow} data-private="true">
                <ArrowDownLeftIcon size="small" aria-hidden="true" />
                <span className={styles.backlinkIcon} aria-hidden="true"><LinkIcon size="small" /></span>
                <span className={styles.backlinkTitle}>{t("pageBacklinks:private")}</span>
              </div>
            ))}
          </div>
          {windowNavigation}
        </div>
      ) : null}
    </div>
  );
}

/**
 * Where a backlink's page sits, as Notion's path (its code and its popover,
 * read 2026-10-31): one or two pages above it by name, more as the top one,
 * "..." and the parent, the ones between named in a tip on "...".
 */
function BacklinkPath({ titles }: { titles: string[] }) {
  const parts = titles.length > 2
    ? [titles[0], <HoverTip key="between" label={titles.slice(1, -1).join(" / ")}>...</HoverTip>, titles[titles.length - 1]]
    : titles;
  return (
    <span className={styles.backlinkPath}>
      {parts.map((part, index) => (
        <Fragment key={index}>
          {index > 0 ? <span className={styles.backlinkPathSlash}>/</span> : null}
          {part}
        </Fragment>
      ))}
    </span>
  );
}
