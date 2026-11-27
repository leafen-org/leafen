import { useTranslation } from "react-i18next";
import type { DatabaseRowPageRow, DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { pageLayoutCommentsChange, pageLayoutCommentsStyle, saveDatabasePageLayoutComments, useDatabasePageLayout } from "@/lib/databasePageLayout";
import { useProductToast } from "@/lib/productToast";
import { sendRowPresentation } from "@/lib/rowValuesKept";
import type { Page, Session } from "@/types";
import { notePageChangeStep } from "../editor/pageUndo";
import { presentationBefore, type PagePresentationChange } from "../page/pagePresentation";
import type { PageStyle, PageStyleChange } from "../PageStyleControls";
import { rowPagePresentationInput } from "./RowPageHeader";

/** The font, small text, full width and table of contents a database page is drawn with. */
export function rowPageStyle(row: DatabaseRowPageRow): Required<PageStyle> {
  return { font: row.font ?? "default", smallText: Boolean(row.smallText), fullWidth: Boolean(row.fullWidth),
    tableOfContentsDisplay: row.tableOfContentsDisplay ?? "default" };
}

/** A page of the list drawn with its database page's style, as a wiki's page's menu shows it (WikiPageView). */
export function pageWithRowStyle(page: Page, style: PageStyle): Page {
  return { ...page, font: style.font as Page["font"], smallText: style.smallText, fullWidth: style.fullWidth,
    ...(style.tableOfContentsDisplay ? { tableOfContentsDisplay: style.tableOfContentsDisplay as Page["tableOfContentsDisplay"] } : {}) };
}

const STYLE_KEYS = ["font", "smallText", "fullWidth", "tableOfContentsDisplay"] as const;
const LOOK_KEYS = [...STYLE_KEYS, "icon", "iconType", "cover", "coverPosition"] as const;
const COMMENT_KEYS = ["pageCommentsDisplay", "inlineCommentsDisplay"] as const;
const onlyKeys = (change: object, keys: readonly string[]) => Object.keys(change).length > 0 && Object.keys(change).every((key) => keys.includes(key));

/** Whether a page menu change is a database page's style alone, which its own page saves (rowPagePresentationSaver). */
export function isRowPageStyleChange(change: object): change is PageStyleChange {
  return onlyKeys(change, STYLE_KEYS);
}

/** Whether a page menu change is the comments switch or choice alone, which a wiki's pages share (useWikiPageComments). */
export function isRowPageCommentsChange(change: object): change is Pick<PagePresentationChange, typeof COMMENT_KEYS[number]> {
  return onlyKeys(change, COMMENT_KEYS);
}

/**
 * A wiki's page comments and inline comments, as its pages' "테마와
 * 레이아웃" shows and changes them: the wiki's, every page of it showing
 * what one page's menu sets — its database's, kept as a database's page
 * layout keeps them — while its font, small text, full width and table of
 * contents stay the page's own, as Notion's (seen 2026-11-16 twice, Chrome
 * 1440: page comments off and inline comments minimal on one wiki page
 * showed on the other, its table of contents did not, each kept after a
 * reload). A change shows on every open page of the wiki at the press;
 * a refusal reads the wiki's layout again, which puts it back.
 */
export function useWikiPageComments(session: Session, page: Pick<Page, "wikiRootId" | "wikiDataSourceId">) {
  const { t } = useTranslation("databaseView");
  const { notify } = useProductToast();
  const rootId = page.wikiRootId ?? undefined, dataSourceId = page.wikiDataSourceId ?? undefined;
  const layout = useDatabasePageLayout(session, rootId, dataSourceId).layout;
  return {
    value: pageLayoutCommentsStyle(layout),
    onChange: (change: PagePresentationChange) => !rootId || !dataSourceId ? Promise.resolve(false)
      : saveDatabasePageLayoutComments(session, rootId, dataSourceId, pageLayoutCommentsChange(change))
        .then(() => true, () => { notify(t("toast.couldntSaveChanges"), "error"); return false; }),
  };
}

// The page as drawn once the look `change` holds — its icon, cover or style — is saved; the same page for a change of none.
function withLook(snapshot: DatabaseRowPageSnapshot | null, change: PagePresentationChange) {
  const style = Object.fromEntries(LOOK_KEYS.filter((key) => change[key] !== undefined).map((key) => [key, change[key]]));
  return snapshot && Object.keys(style).length ? { ...snapshot, row: { ...snapshot.row, ...style } as DatabaseRowPageRow } : snapshot;
}

/**
 * Saves a database page's icon, cover or style, from its head and its menu —
 * a wiki's page's page menu too (WikiPageView) — as one step of the page's
 * ⌘Z. It shows on the page at the press (user decision 2026-11-12: a press
 * answers on the next frame) and is kept in the outbox behind the row's
 * other changes, its making among them (rowValuesKept.ts); the page is read
 * again once it went, which puts it back where the server refused it
 * (2026-11-25: sent straight to the server, an icon picked on a new task
 * whose making waited for the server showed nothing and went at a reload).
 */
export function rowPagePresentationSaver({ session, rowId, shown, show, load }: {
  session: Session;
  rowId: string;
  shown(): DatabaseRowPageSnapshot | null;
  show(next: (current: DatabaseRowPageSnapshot | null) => DatabaseRowPageSnapshot | null): void;
  load(): Promise<boolean>;
}) {
  const save = (change: PagePresentationChange) => {
    const pageId = shown()?.row.containerPageId;
    show((current) => withLook(current, change));
    return sendRowPresentation(session, rowId, rowPagePresentationInput(change), pageId)
      .then(() => load().then(() => true), () => load().then(() => false));
  };
  return async (change: PagePresentationChange) => {
    const before = presentationBefore((shown()?.row ?? {}) as PagePresentationChange, change);
    const saved = await save(change);
    if (saved) notePageChangeStep(rowId, before, change, save);
    return saved;
  };
}
