import type { NativePageMetadataChange } from "@/lib/nativePageMetadata";
import { tellPageEditedHere } from "@/lib/pageEditedHere";
import type { Page } from "@/types";

type HeldPlace = Pick<Page, "parentId" | "inTrash">;

/**
 * The pages a person's change of a page's place edits, as the server notes
 * them (page_place_parent_edits.go) and Notion's shows them (seen
 * 2026-11-27, two tabs at two times, a section sorted by last edit): the
 * page it leaves and the page it goes into, dragged or moved with the move
 * window; its parent as it goes to the trash, or as it takes another place
 * among its siblings, unless only to keep an order shown. A page coming back
 * from the trash edits none, nor does its title changed.
 */
export function placeEditedParents(page: HeldPlace, change: NativePageMetadataChange): string[] {
  if (page.inTrash) return [];
  const from = page.parentId ?? "";
  if (change.inTrash === true) return from ? [from] : [];
  const to = change.parentId ?? from;
  if (to !== from) return [from, to].filter(Boolean);
  // A position is sent only for a new place among its siblings.
  return change.position !== undefined && !change.keepLastEdit && to ? [to] : [];
}

/** Shows the pages a place change saved from here edits as edited now, by this person (pageEditedHere.ts). */
export function tellPlaceEditedHere(page: HeldPlace, change: NativePageMetadataChange, userId: string) {
  const now = new Date().toISOString();
  for (const id of placeEditedParents(page, change)) tellPageEditedHere({ id, updatedAt: now, lastEditedBy: userId });
}
