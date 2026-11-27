export const CARD_PREVIEW_PAGE = "__page_cover";
export const CARD_PREVIEW_PAGE_CONTENT = "__page_content";
export const CARD_PREVIEW_NONE = "__none";

/**
 * A view's card preview when it names none: a board shows none, as a new
 * Notion board does (seen 2026-10-08, "카드 사용 안함"); a gallery the page's
 * cover.
 */
export function defaultCardPreview(viewType?: string) {
  return viewType === "board" ? CARD_PREVIEW_NONE : CARD_PREVIEW_PAGE;
}

export function cardPreviewMode(coverProperty?: string, viewType?: string) {
  return coverProperty ?? defaultCardPreview(viewType);
}

export function hasCardPreview(coverProperty?: string, viewType?: string) {
  return cardPreviewMode(coverProperty, viewType) !== CARD_PREVIEW_NONE;
}
