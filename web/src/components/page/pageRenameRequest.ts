/**
 * The top bar asks the open page's header to open its rename popover under
 * the path's last piece, as Notion's click on that piece does (seen
 * 2026-10-27); the header owns the title and its saving: the page's, or the
 * inline database's opened alone.
 */
export type PageRenameRequest = { pageId: string; at: { top: number; left: number } };

const PAGE_RENAME_EVENT = "leafen:page-rename";

export function requestPageRename(request: PageRenameRequest) {
  window.dispatchEvent(new CustomEvent<PageRenameRequest>(PAGE_RENAME_EVENT, { detail: request }));
}

export function onPageRenameRequest(listener: (request: PageRenameRequest) => void): () => void {
  const handle = (event: Event) => listener((event as CustomEvent<PageRenameRequest>).detail);
  window.addEventListener(PAGE_RENAME_EVENT, handle);
  return () => window.removeEventListener(PAGE_RENAME_EVENT, handle);
}
