import type { ComponentProps } from "react";
import { RowMenu, type PageSuggestingControl } from "@/components/RowMenu";
import type { RowPageMenuExtras } from "@/components/database/DatabaseRowMenu";
import type { Page, Session } from "@/types";
import { isRowPageCommentsChange, isRowPageStyleChange, pageWithRowStyle, useWikiPageComments } from "../database/rowPageStyleSave";

type PageMenuProps = ComponentProps<typeof RowMenu>;

/** The product's page menu operations for one wiki page. */
export type WikiPageMenuActions = Pick<PageMenuProps, "onPageChange" | "onFavoriteChange" | "onPresentationChange" | "onPageMetadataChange"
  | "onDuplicatePage" | "onTrashChange" | "onImport" | "readDocumentStats" | "onStatus">;

/**
 * A page of a wiki's "⋯": the page menu, not a database row's, as Notion's
 * wiki pages — duplicate, "개인 페이지로 복제", move, archive, trash and
 * updates — opened whole (WikiPageView) and in a peek alike (seen 2026-11-17
 * at 1416: a wiki page in the center peek its sidebar "+" opened, and in the
 * side peek its wiki's "모든 페이지" opened, the same menu as the page opened
 * whole). Its "테마와 레이아웃" — font, small text, full width and table of
 * contents — is the database page's style (`style`), saved and shown as the
 * database page's own menu saves it (rowPageStyleSave.ts): the wiki's change
 * of the page leaves a page's style as it is. Its page comments and inline
 * comments are the wiki's, shared by all its pages (useWikiPageComments).
 */
export function WikiPageMenu({ page, pages, session, favorite, menu, anchor, canEdit, suggesting, style, onOpenUpdatesPanel, onClose }: {
  page: Page;
  pages: Page[];
  session: Session;
  favorite: boolean;
  menu: WikiPageMenuActions;
  anchor: { x: number; y: number };
  canEdit: boolean;
  suggesting?: PageSuggestingControl;
  /** The database page's style as shown, and how it is saved. */
  style?: RowPageMenuExtras["style"];
  onOpenUpdatesPanel?: () => void;
  onClose: () => void;
}) {
  const comments = useWikiPageComments(session, page);
  const shown = { ...page, ...comments.value };
  const { onPageMetadataChange } = menu;
  return (
    <RowMenu {...menu} anchor={anchor} page={style ? pageWithRowStyle(shown, style.value) : shown} pages={pages} session={session} favorite={favorite}
      onOpenUpdatesPanel={onOpenUpdatesPanel}
      onPageMetadataChange={style && onPageMetadataChange ? (target, change, signal) => isRowPageStyleChange(change)
        ? Promise.resolve(style.onChange(change)).then(() => true) : isRowPageCommentsChange(change) ? comments.onChange(change)
          : onPageMetadataChange(target, change, signal) : onPageMetadataChange}
      canEdit={canEdit} suggesting={suggesting} onClose={onClose} />
  );
}
