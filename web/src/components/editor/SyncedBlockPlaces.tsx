import { blockAnchor } from "@/lib/blockAddress";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api, type SyncedBlockLocation, type SyncedBlockLocations } from "@/api";
import type { GraphBlock } from "@/contentGraph";
import { ArrowDownLeftIcon, ChevronDown } from "@/icons/leafen";
import { pagePath } from "@/lib/backlinks";
import { pageHref } from "@/lib/navigation";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { pushRoute } from "@/lib/router";
import { blockItemText } from "./blockItemLabels";
import { requestEditorBlockSelection } from "./editorCommands";
import { pageIconSource } from "./mentionMenuItems";
import { syncedBlockSource } from "./syncedBlockModel";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./editorStyles.generated.module.css";
import placeStyles from "./SyncedBlockPlaces.module.css";
import { PageIconGlyph } from "../page/PageIcon";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";

function readablePlace(value: unknown): value is SyncedBlockLocation {
  return typeof value === "object" && value !== null
    && typeof Reflect.get(value, "blockId") === "string" && typeof Reflect.get(value, "pageId") === "string";
}

function readablePlaces(value: SyncedBlockLocations): boolean {
  return readablePlace(value?.original) && Array.isArray(value.copies) && value.copies.every(readablePlace);
}

/**
 * The other pages a synced block is in, as Notion's "Editing in N other
 * pages": read each time the block is worked in, and again while it is when
 * a block of it comes or goes on this page, listed with the original's page
 * marked, each opening at the block.
 */
export function SyncedBlockPlaces({ adapter, block, active }: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  /** Whether the block has been pointed at or focused. */
  active: boolean;
}) {
  const [places, setPlaces] = useState<SyncedBlockLocations | null>(null);
  const [open, setOpen] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const rootRef = useRef<HTMLSpanElement>(null);

  // The blocks of its original on this page, itself among them: a copy
  // pasted or removed here changes where it is, as Notion's toolbar counts
  // it at once (seen 2026-11-16: a copy pasted beside an original's 1 made
  // it 2, a copy deleted made it 1 again, with no reload). Read once per
  // change of the page's blocks, by content alone.
  const original = syncedBlockSource(block, adapter.pageId).blockId;
  const here = useMemo(() => adapter.graph.blocks.filter((candidate) => candidate.type === "synced_block"
    && syncedBlockSource(candidate, adapter.pageId).blockId === original).length, [adapter.graph.blocks, adapter.pageId, original]);
  const graphSync = useRef(adapter.graphSync);
  graphSync.current = adapter.graphSync;
  useEffect(() => {
    const session = adapter.session;
    if (!active || !session) return;
    let cancelled = false;
    void (async () => {
      // The page's own changes reach the server first, a copy just pasted among them.
      await graphSync.current?.flush().catch(() => false);
      if (cancelled) return;
      const read = await api.syncedBlockLocations(session, adapter.pageId, block.id);
      if (!cancelled && readablePlaces(read)) setPlaces(read);
    })().catch(() => {
      // What was read last shows till it can be read again.
    });
    return () => { cancelled = true; };
  }, [active, adapter.pageId, adapter.session, block.id, here]);

  // However many pages it is in, the next ones load when asked for.
  async function loadMore() {
    const session = adapter.session;
    if (!session || !places?.hasMore || !places.nextAfter || loadingMore) return;
    setLoadingMore(true);
    try {
      const next = await api.syncedBlockLocations(session, adapter.pageId, block.id, places.nextAfter);
      if (readablePlaces(next)) {
        setPlaces((current) => current ? { ...next, original: current.original, copies: [...current.copies, ...next.copies] } : next);
      }
    } catch {
      // The row stays for another try.
    } finally {
      setLoadingMore(false);
    }
  }

  // The open list is a layer over the page: Escape or a press outside it
  // closes the list alone and Escape returns to its button. It floats, the
  // press outside taken by the page's backdrop and doing nothing more: the
  // block it chose stays chosen, as Notion's (seen 2026-11-16 in two tabs).
  const buttonRef = useRef<HTMLButtonElement>(null), menuRef = useRef<HTMLDivElement>(null), boxRef = useRef<HTMLDivElement>(null);
  usePopoverAppear(open, boxRef, () => buttonRef.current);
  useOverlayLayer(open, { kind: "menu", ref: rootRef, onEscape: () => setOpen(false), onOutsidePress: () => setOpen(false),
    returnFocusRef: buttonRef });

  // One entry for each page it is in, this one too, at the original where it
  // is on that page, as Notion's list (seen 2026-11-15: the original's page
  // first marked "원본", this page "이 페이지"). The count is every other
  // block of it, a copy on this page too, as Notion's "N개의 다른 페이지에
  // 동기화" (seen 2026-11-14·15: an original and its copy on one page say 1
  // each, four copies there 4, a copy beside two of another page's 2).
  const all = places ? [places.original, ...places.copies] : [];
  const byPage = new Map<string, { place: SyncedBlockLocation; original: boolean }>();
  for (const place of all) if (!byPage.has(place.pageId)) byPage.set(place.pageId, { place, original: place === places?.original });
  const others = Math.max(0, all.length - 1);
  // A synced block sits only in a page's body, so an untitled one is a page.
  const pagesById = adapter.mentionSnapshot?.pagesById;
  const title = (pageId: string) => pagesById?.[pageId]?.title?.trim() || untitledPageDisplayTitle("page");
  // The pages above it, as Notion's list names them on the right ("한지테스트페이지 / …").
  const above = (pageId: string) => {
    const page = pagesById?.[pageId];
    return page && pagesById ? pagePath(page, pagesById).join(" / ") : "";
  };
  const entries = [...byPage.values()];
  // Opened from the pointer it lights no row, as Notion's (seen 2026-11-15 and 2026-11-16).
  const menu = useNativeMenu({ items: entries, open: open && entries.length > 0, menuRef, label: blockItemText("synced.places"), quietOnPointer: true,
    keyOf: entry => entry.place.pageId, textOf: entry => title(entry.place.pageId), onAction: ({ place }) => {
      setOpen(false);
      pushRoute(pageHref(place.pageId) + blockAnchor(place.pageId, place.blockId));
    } });
  // The open list stays on the screen, moved back from the edge it would pass.
  useLayoutEffect(() => {
    const box = boxRef.current;
    if (!open || !box) return;
    box.style.translate = "";
    const edge = parseFloat(getComputedStyle(box).getPropertyValue("--popover-edge")) || 12;
    const { left, right } = box.getBoundingClientRect();
    const past = right - (window.innerWidth - edge);
    const shift = past > 0 ? -Math.min(past, left - edge) : left < edge ? edge - left : 0;
    if (shift) box.style.translate = `${shift}px 0`;
  }, [open, places]);
  // Alone, the original says it is being edited, as Notion's does.
  if (!places || others === 0) {
    return <span className={styles.syncedPlaces}><span className={styles.syncedPlacesLabel} data-synced-places-alone>
      <ArrowDownLeftIcon size="small" aria-hidden="true" />{blockItemText("synced.editingOriginal")}</span></span>;
  }

  return (
    <span ref={rootRef} className={styles.syncedPlaces}>
      <button ref={buttonRef} type="button" aria-haspopup="menu" aria-expanded={open} data-synced-places
        aria-label={blockItemText("synced.openPlaces")} onClick={() => {
          // Opening it chooses the block, the caret leaving it: chosen it
          // stays once the list is closed, by Escape (the focus back on this
          // button, the next letter going nowhere) or by a press outside, as
          // Notion's (seen 2026-11-16 in two tabs, keys and a letter after).
          if (!open) requestEditorBlockSelection(block.id, false, buttonRef.current ?? undefined);
          setOpen((current) => !current);
        }}>
        <ArrowDownLeftIcon size="small" aria-hidden="true" />
        {blockItemText(places.hasMore ? "synced.otherPagesAtLeast" : "synced.otherPages", { count: others })}
        <ChevronDown size="tiny" aria-hidden="true" />
      </button>
      {open ? (
        <div ref={boxRef} className={`${styles.syncedPlacesMenu} ${placeStyles.menu}`}>
          <div className={placeStyles.head}>{blockItemText("synced.placesHead")}</div>
          <div {...menu.menuProps} ref={menuRef} className={placeStyles.rows}>
            {entries.map(({ place, original }) => {
              const path = above(place.pageId), here = place.pageId === adapter.pageId, page = pagesById?.[place.pageId];
              return (
                <NativeMenuItem state={menu.state} itemKey={place.pageId} key={place.pageId} data-synced-place={place.pageId}
                  className={placeStyles.row}>
                  {({ labelProps }) => <>
                    <span className={placeStyles.icon} aria-hidden="true">
                      <PageIconGlyph page={page ? pageIconSource(page) : { icon: "", iconType: "none" }} size={18} /></span>
                    <span className={placeStyles.name}><span {...labelProps} className={placeStyles.title}>{title(place.pageId)}</span></span>
                    {path ? <span className={placeStyles.place}>{path}</span> : null}
                    {original ? <span className={placeStyles.pill}>{blockItemText("synced.original")}</span> : null}
                    {here ? <span className={placeStyles.pill}>{blockItemText("synced.thisPage")}</span> : null}
                  </>}
                </NativeMenuItem>
              );
            })}
          </div>
          {places.hasMore ? (
            <button type="button" className={placeStyles.more} disabled={loadingMore} data-synced-places-more
              onClick={() => void loadMore()}>{blockItemText("synced.placesMore")}</button>
          ) : null}
        </div>
      ) : null}
    </span>
  );
}
