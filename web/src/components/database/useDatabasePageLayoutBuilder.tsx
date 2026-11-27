import { useEffect, useRef, useState } from "react";
import { OPEN_PAGE_LAYOUT_EVENT, type OpenPageLayoutDetail } from "@/lib/databasePageLayout";
import { focusAfterPaint } from "@/components/editor/focus";
import { touchScreen } from "@/lib/screenKind";
import type { Page } from "@/lib/types";
import { DatabasePageLayoutBuilder, type DatabasePageLayoutBuilderProps } from "./DatabasePageLayoutBuilder";

/**
 * The pages the builder's "미리보기" offers, by when they were last edited,
 * the latest first, the order given where those tie: its database's pages
 * alike whether it opened from a view or from one of them, as Notion's (seen
 * 2026-11-14: 둘째·첫·넷째·셋째 from a view showing 넷째·셋째·둘째·첫, and
 * from 셋째's own page, 셋째 chosen in its place).
 */
function lastEditedFirst(pages: readonly Page[]) {
  return pages.map((page, index) => ({ page, index, edited: page.updatedAt ? Date.parse(page.updatedAt) || 0 : 0 }))
    .sort((left, right) => right.edited - left.edited || left.index - right.index)
    .map(({ page }) => page);
}

/**
 * A database's view opens the page layout builder when "레이아웃 사용자 지정"
 * names its database page (and, from a database page, its data source),
 * the view holding the properties and pages the builder lays out.
 */
export function useDatabasePageLayoutBuilder(props: Omit<DatabasePageLayoutBuilderProps, "onClose" | "previewRows"> & {
  /** Where the caller has not the name: read as the builder opens. */
  resolveTitle?: () => Promise<string | undefined>;
  /** The pages it previews, or how to list them once it opens (a database's of 100,000 rows). */
  previewRows: DatabasePageLayoutBuilderProps["previewRows"] | (() => DatabasePageLayoutBuilderProps["previewRows"]);
  /** Asks for the pages to preview where the caller holds only some as the builder opens (a row's own page). */
  onOpen?: () => void;
  /** The page its view shows first, none (a new page) for a view in groups; else the first listed. */
  firstShown?: () => string | null;
} | null) {
  const [open, setOpen] = useState<OpenPageLayoutDetail | null>(null);
  const opened = useRef(props?.onOpen);
  opened.current = props?.onOpen;
  // The page previewed first, read as it opens: the one it was opened from, else its view's first shown.
  const shownFirst = useRef(props?.firstShown);
  shownFirst.current = props?.firstShown;
  const [first, setFirst] = useState<string | null>();
  const [resolved, setResolved] = useState<string>();
  const pageId = props?.pageId, dataSourceId = props?.dataSourceId;
  const resolveTitle = open ? props?.resolveTitle : undefined;
  useEffect(() => {
    setResolved(undefined);
    if (!resolveTitle) return undefined;
    let live = true;
    void resolveTitle().then((title) => { if (live) setResolved(title); }, () => undefined);
    return () => { live = false; };
    // Read once for each opening.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  useEffect(() => {
    if (!pageId) return undefined;
    const listener = (event: Event) => {
      const detail = (event as CustomEvent<OpenPageLayoutDetail>).detail;
      if (event.defaultPrevented || detail?.pageId !== pageId || detail.dataSourceId && detail.dataSourceId !== dataSourceId) return;
      event.preventDefault();
      setOpen(detail);
      setFirst(detail.rowId ?? shownFirst.current?.());
      opened.current?.();
    };
    window.addEventListener(OPEN_PAGE_LAYOUT_EVENT, listener);
    return () => window.removeEventListener(OPEN_PAGE_LAYOUT_EVENT, listener);
  }, [dataSourceId, pageId]);
  if (!open || !props) return null;
  const { resolveTitle: _resolve, previewRows, onOpen: _onOpen, firstShown: _first, ...builder } = props;
  const listed = typeof previewRows === "function" ? previewRows() : previewRows;
  // Opened from a template, the template is previewed, first in the list before its database's pages, as
  // Notion's (seen 2026-11-16 at 1440, twice: "미리보기: 〈template〉", it ✓ over the pages by when they were edited).
  const own = open.preview;
  const rows = own ? [own, ...lastEditedFirst(listed.filter(({ id }) => id !== own.id))] : lastEditedFirst(listed);
  // Left — "취소" or "모든 페이지에 적용" — it goes back to the page it was opened from, a row's page or a
  // template's, under it all along, the caret at the end of that page's title wherever it was before, once
  // the builder is gone, as Notion's (seen 2026-11-16 at 1440: a template's, from its title and from its
  // body, and a row's side peek; a letter typed next landed at the title's end), what is typed meanwhile
  // waiting for it there (focusAfterPaint — called off by a key, it went nowhere, 2026-11-21). A new press
  // first keeps its own focus. A touch screen's is not measured, and keeps the focus where the builder leaves it.
  const from = open.rowId;
  const close = () => {
    setOpen(null);
    if (from && !touchScreen()) focusAfterPaint(() => ({ key: `title:${from}`, position: "end" }));
  };
  return <DatabasePageLayoutBuilder {...builder} title={resolved ?? props.title} previewRows={rows}
    previewRowId={own ? own.id : first === undefined ? listed[0]?.id : first} onClose={close} />;
}
