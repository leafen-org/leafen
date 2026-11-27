import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type MouseEvent, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type * as Y from "yjs";
import type { ContentGraph, GraphBlock, GraphView } from "@/contentGraph";
import { PhoneShareIcon } from "@/icons/leafen";
import { copyText } from "@/lib/copyText";
import { writeDatabaseViewChoice } from "@/lib/databaseViewChoice";
import { matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";
import { absolutePageUrl, pageHref } from "@/lib/navigation";
import { usePageNames } from "@/lib/pageNames";
import {
  PRESENT_EVENT, PRESENT_PARAM, pageSlides, presentationRoute, presentedSlide, registerPresentablePage, slideGraph, slideIndexOfBlock,
  type PageSlide, type PresentRequest,
} from "@/lib/pageSlides";
import { peekedPageId } from "@/lib/pagePeek";
import { TEMPLATE_PAGE_PARAM } from "@/components/database/useTemplateAddress";
import { pushRoute, replaceRoute, usePathname, useSearchParams } from "@/lib/router";
import { useProductToast } from "@/lib/productToast";
import { serverUrl } from "@/lib/serverOrigin";
import { StructuredEditor } from "@/StructuredEditor";
import type { Page, Session } from "@/types";
import { PageIconGlyph } from "./PageIcon";
import styles from "./PageSlideshow.module.css";
import { PresentationBar, presentationBarButton, usePresentationBar } from "./PresentationBar";
import { blockAnchorId } from "@/components/editor/blockHandleModel";
import { noteBlockAnchorShown } from "@/components/editor/useBlockAnchorScroll";
import { requestEditorBlockSelection } from "@/components/editor/editorCommands";

/** A slide is laid out 800px wide and scaled to the window, as Notion's. */
const SLIDE_WIDTH = 800;
const SLIDE_MARGIN = 72;

const ignore = () => {};
const ignoreText = (_block: GraphBlock, _text: string) => {};
const chooseView = (block: GraphBlock, view: GraphView) => writeDatabaseViewChoice(undefined, block.id, view.id);

/**
 * Presents the open page, as Notion's presentation mode (seen 2026-10-12):
 * the page menu's "프레젠테이션 모드" (⌘⌥P) starts at its title, a block
 * menu's "여기에서 발표" and ⌘⌥P in the page start at the slide holding that
 * block. The slide in the address (`?present=N`) shows over the window until
 * Escape or ✕, which leave the page at that slide's first block. A
 * database page opened over its database (`?p=`) presents itself instead,
 * as Notion's row page does, and so does a template open over it (`?tp=`)
 * and either opened whole at its own address.
 */
const NO_SLIDES: readonly PageSlide[] = [];

export function PagePresentationHost({ page, graph, collaborationDocument, session, opened = false }: {
  page: Page;
  graph: ContentGraph;
  collaborationDocument: Y.Doc | null;
  session: Session;
  /** The page is a database page opened over the window's page (`?p=` names it). */
  opened?: boolean;
}) {
  const search = useSearchParams(), pathname = usePathname();
  // A database's template open over it (its "tp") presents itself as a database page does, and either at its own address.
  const template = search.get(TEMPLATE_PAGE_PARAM), alone = !search.get("p") && !template;
  const active = opened ? peekedPageId(search.get("p")) === page.id || template === page.id || (alone && pathname === pageHref(page.id)) : alone;
  const index = active ? presentedSlide(search) : null;
  const text = useCallback((block: GraphBlock) => collaborationDocument
    ? collaborationDocument.getText(`block:${block.id}`).toString()
    : String(block.content.text ?? ""), [collaborationDocument]);
  // Its slides are worked out while it shows, not for each change of a page
  // not presented: a line made on a long page sorted all its blocks for them
  // in the press (2026-11-14, about 4 ms of 1,022 blocks at CPU ×2).
  const presenting = index !== null;
  const slides = useMemo(() => presenting ? pageSlides(graph, text) : NO_SLIDES, [graph, presenting, text]);

  useEffect(() => registerPresentablePage(page.id), [page.id]);

  useEffect(() => {
    if (!active) return undefined;
    const present = (blockId?: string) => {
      const start = slideIndexOfBlock(graph, pageSlides(graph, text), blockId);
      pushRoute(opened ? presentationRoute(start) : `${pageHref(page.id)}?${PRESENT_PARAM}=${start}`);
    };
    const onRequest = (event: Event) => {
      const detail = (event as CustomEvent<PresentRequest>).detail;
      if (detail?.pageId === page.id) present(detail.blockId);
    };
    // ⌘⌥P starts at the slide holding the caret, or the title.
    const onKey = (event: KeyboardEvent) => {
      if (presentedSlide(new URLSearchParams(window.location.search)) !== null || !matchesKeyboardShortcut("presentPage", event)) return;
      event.preventDefault();
      const anchor = window.getSelection()?.anchorNode;
      const element = anchor instanceof Element ? anchor : anchor?.parentElement;
      present(element?.closest<HTMLElement>("[data-block-id]")?.dataset.blockId);
    };
    window.addEventListener(PRESENT_EVENT, onRequest);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener(PRESENT_EVENT, onRequest);
      window.removeEventListener("keydown", onKey);
    };
  }, [active, graph, opened, page.id, text]);

  if (index === null) return null;
  const last = slides.length;
  return (
    <PagePresentation page={page} graph={graph} slides={slides} index={Math.min(index, last)}
      collaborationDocument={collaborationDocument} session={session}
      onIndex={(next) => replaceRoute(opened ? presentationRoute(next) : `${pageHref(page.id)}?${PRESENT_PARAM}=${next}`)}
      opened={opened}
      onExit={(blockId) => {
        // Leaving keeps the slide's block in the address, as Notion's (`#block-<id>`, the page's own block link).
        if (blockId && !opened) noteBlockAnchorShown(page.id, blockId);
        replaceRoute(opened ? presentationRoute(null) : `${pageHref(page.id)}${blockId ? `#${blockAnchorId(blockId)}` : ""}`);
        if (blockId) window.requestAnimationFrame(() => {
          // A block already in view stays where it is, as Notion's (seen 2026-10-30).
          document.getElementById(`block-${blockId}`)?.scrollIntoView({ block: "nearest" });
          requestEditorBlockSelection(blockId);
        });
      }} />
  );
}

function PagePresentation({ page, graph, slides, index, collaborationDocument, session, opened, onIndex, onExit }: {
  page: Page;
  opened: boolean;
  graph: ContentGraph;
  slides: readonly PageSlide[];
  index: number;
  collaborationDocument: Y.Doc | null;
  session: Session;
  onIndex: (index: number) => void;
  onExit: (blockId: string | null) => void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("presentation");
  const { notify } = useProductToast();
  const total = slides.length + 1;
  const slide = index > 0 ? slides[index - 1] : null;
  const stageRef = useRef<HTMLDivElement>(null);
  const slideRef = useRef<HTMLDivElement>(null);
  const scale = useSlideFit(slideRef, index);
  const bar = usePresentationBar();
  const { hide: hideBar, show: showBar } = bar;
  const current = useRef({ index, total, slide, onIndex, onExit });
  current.current = { index, total, slide, onIndex, onExit };

  const go = useCallback((next: number) => {
    const { index: now, total: count } = current.current;
    if (next < 0 || next >= count || next === now) return;
    hideBar();
    current.current.onIndex(next);
  }, [hideBar]);
  const exit = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined);
    current.current.onExit(current.current.slide?.rootIds[0] ?? null);
  }, []);

  // → Space PageDown go on, ← PageUp back, Escape ends, as Notion's.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const { index: now } = current.current;
      // The presentation keeps its keys: Escape ends it, not a database page open under it.
      if (event.key === "Escape") { event.preventDefault(); event.stopImmediatePropagation(); exit(); return; }
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      if (event.key === "ArrowRight" || event.key === "PageDown" || event.key === " " && !event.shiftKey) {
        event.preventDefault(); event.stopImmediatePropagation(); go(now + 1);
      } else if (event.key === "ArrowLeft" || event.key === "PageUp" || event.key === " " && event.shiftKey) {
        event.preventDefault(); event.stopImmediatePropagation(); go(now - 1);
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [exit, go]);

  // The controls show while the pointer moves and fade after it rests.
  useEffect(() => {
    window.addEventListener("mousemove", showBar);
    return () => window.removeEventListener("mousemove", showBar);
  }, [showBar]);

  useEffect(() => { stageRef.current?.focus(); }, []);

  // The presentation fills the screen as it starts, as Notion's does (seen
  // 2026-11-09; one opened by its address alone, with nothing pressed, can't).
  useEffect(() => {
    if (!document.fullscreenElement) void document.documentElement.requestFullscreen?.().catch(() => undefined);
  }, []);

  async function copySlideLink() {
    // An opened database page's slide is linked over its database, as the window shows it.
    const url = opened ? serverUrl(presentationRoute(index)) : absolutePageUrl(page.id);
    const ok = await copyText(opened ? url : `${url}${url.includes("?") ? "&" : "?"}${PRESENT_PARAM}=${index}`);
    notify(ok ? t("linkCopied") : t("linkCopyFailed"), ok ? "success" : "error");
  }

  // Links and mentions don't open while presenting, as Notion's.
  function holdLinks(event: MouseEvent) {
    if ((event.target as Element).closest("a, [data-mention]")) event.preventDefault();
  }

  return createPortal(
    <div ref={stageRef} className={styles.presentation} role="dialog" aria-modal="true" aria-label={t("label", { title: pageNames.title(page) })}
      tabIndex={-1} data-page-presentation={index} onClickCapture={holdLinks}>
      <div className={styles.stage}>
        <div key={index} ref={slideRef} className={styles.slide} style={{ transform: `scale(${scale})` }} data-presentation-slide={index}>
          {slide ? (
            <StructuredEditor pageId={page.id} workspaceId={page.workspaceId} session={session}
              collaborationDocument={collaborationDocument} graph={slideGraph(graph, slide)} loading={false}
              readOnly fixedBody showPageOutline={false}
              onAddParagraph={ignore} onAddDatabase={ignore} onChangeParagraph={ignoreText} onSelectView={chooseView} />
          ) : (
            <div className={styles.titleSlide} data-presentation-title>
              {page.icon ? <span className={styles.titleIcon}><PageIconGlyph page={page} size={64} fallback="none" /></span> : null}
              <h1 className={styles.title}>{pageNames.title(page)}</h1>
            </div>
          )}
        </div>
      </div>
      <PresentationBar shown={bar.shown} over={bar.over} onShow={showBar} index={index} total={total} onGo={go} onExit={exit}>
        <button type="button" className={presentationBarButton} aria-label={t("copyLink")} title={t("copyLink")}
          onClick={() => void copySlideLink()}><PhoneShareIcon size="small" aria-hidden="true" /></button>
      </PresentationBar>
    </div>,
    document.body,
  );
}

/** The slide fills the window's width, or its height when it is tall. */
function useSlideFit(slideRef: RefObject<HTMLDivElement | null>, index: number) {
  const [scale, setScale] = useState(1);
  useLayoutEffect(() => {
    const element = slideRef.current;
    if (!element) return;
    const fit = () => {
      const height = element.offsetHeight || 1;
      setScale(Math.max(0.1, Math.min(window.innerWidth / SLIDE_WIDTH, (window.innerHeight - SLIDE_MARGIN * 2) / height)));
    };
    fit();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(fit);
    observer?.observe(element);
    window.addEventListener("resize", fit);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", fit);
    };
  }, [index, slideRef]);
  return scale;
}

/**
 * One slide of a page read without a session — a view link opened with
 * `present=N` in its address (after "#"), for an agent to see a slide as the
 * page's presentation shows it: the title slide first, each slide
 * 800px wide and fitted to the window, without the bar.
 */
export function PageSlideView({ page, graph, index }: { page: Pick<Page, "id" | "workspaceId" | "title" | "icon" | "iconType" | "kind">; graph: ContentGraph; index: number }) {
  const pageNames = usePageNames();
  const { t } = useTranslation("presentation");
  const slides = useMemo(() => pageSlides(graph, (block) => String(block.content.text ?? "")), [graph]);
  const shown = Math.min(index, slides.length);
  const slide = shown > 0 ? slides[shown - 1] : null;
  const slideRef = useRef<HTMLDivElement>(null);
  const scale = useSlideFit(slideRef, shown);
  return (
    <div className={styles.presentation} role="region" aria-label={t("count", { current: shown + 1, total: slides.length + 1 })}
      data-page-presentation={shown} data-view-link-slide>
      <div className={styles.stage}>
        <div ref={slideRef} className={styles.slide} style={{ transform: `scale(${scale})` }} data-presentation-slide={shown}>
          {slide ? (
            <StructuredEditor pageId={page.id} workspaceId={page.workspaceId} session={null} collaborationDocument={null}
              graph={slideGraph(graph, slide)} loading={false} readOnly fixedBody showPageOutline={false}
              onAddParagraph={ignore} onAddDatabase={ignore} onChangeParagraph={ignoreText} onSelectView={chooseView} />
          ) : (
            <div className={styles.titleSlide} data-presentation-title>
              {page.icon ? <span className={styles.titleIcon}><PageIconGlyph page={page} size={64} fallback="none" /></span> : null}
              <h1 className={styles.title}>{pageNames.title(page as Page)}</h1>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
