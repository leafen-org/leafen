import type { Page } from "@/types";

/**
 * A row icon that stands for a person (`person:〈user〉`, as People's rows
 * have, people_row_pictures.go), drawn as their picture where the list of
 * people provides it, and elsewhere as Notion draws a People row's icon: the
 * person's photo as a page's picture, the page icon without one (PageIcon).
 */
export const PERSON_ICON_PREFIX = "person:";

/**
 * Whether a page's icon is a picture (uploaded or linked), which a page's
 * head gives a bigger box — read here, apart from the icon's drawing, so the
 * screen a public page shows while it comes (lib/publicOpeningShape) reads
 * it too.
 */
export function isPagePictureIcon(page: Pick<Page, "icon" | "iconType">) {
  return page.iconType === "image" && Boolean(page.icon) && !page.icon?.startsWith(PERSON_ICON_PREFIX);
}
