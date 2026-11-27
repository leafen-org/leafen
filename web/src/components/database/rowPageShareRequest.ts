import { OPEN_PAGE_SHARE_EVENT, takePendingPageShare, type OpenPageShareDetail } from "../page/pageShareEvents";

/**
 * A phone's "작업" sheet asks the open database page to open its share, as
 * Notion's sheet's "공유" does (seen 2026-10-27); the page owns its share.
 */
const ROW_PAGE_SHARE_EVENT = "leafen:row-page-share";

export function requestRowPageShare(rowId: string) {
  window.dispatchEvent(new CustomEvent<string>(ROW_PAGE_SHARE_EVENT, { detail: rowId }));
}

/**
 * Tells a row's page when its share is asked for: by the sheet, or as any
 * page's (`openPageShare`) by an "@" invite in its text or properties, with
 * the name to invite. The listener says whether the row is its own.
 */
export function onRowPageShareRequest(listener: (rowId: string, invite?: string) => boolean | void): () => void {
  const handle = (event: Event) => listener((event as CustomEvent<string>).detail);
  const asPage = (event: Event) => {
    const detail = (event as CustomEvent<OpenPageShareDetail>).detail;
    if (detail?.pageId && detail.tab !== "publish" && listener(detail.pageId, detail.invite)) takePendingPageShare(detail.pageId);
  };
  window.addEventListener(ROW_PAGE_SHARE_EVENT, handle);
  window.addEventListener(OPEN_PAGE_SHARE_EVENT, asPage);
  return () => { window.removeEventListener(ROW_PAGE_SHARE_EVENT, handle); window.removeEventListener(OPEN_PAGE_SHARE_EVENT, asPage); };
}
