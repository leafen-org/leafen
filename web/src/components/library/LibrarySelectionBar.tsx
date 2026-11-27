import { Suspense, useEffect, useState, useSyncExternalStore } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { DotsHorizontal, MoveToTopIcon, StarSlashIcon, Trash } from "@/icons/leafen";
import { AnchoredIconPicker } from "@/components/page/AnchoredEmojiPicker";
import { confirmDatabasePagesTrash, confirmDatabasePageTrash, confirmDatabaseSprintsTrash } from "@/components/editor/databaseBlockRemoval";
import { hasExplicitPageIcon } from "@/components/page/PageIcon";
import { PageRenamePopover } from "@/components/page/PageRenamePopover";
import type { TextSpan } from "@/lib/types";
import type { PagePresentationChange } from "@/components/page/pagePresentation";
import type { MovePopoverAnchor } from "@/components/movePopoverPlacement";
import { copyText } from "@/lib/copyText";
import type { NativePageMetadataChange } from "@/lib/nativePageMetadata";
import { usePageActionAccessReader } from "@/lib/pageActionAccessContext";
import { openPagePeek } from "@/lib/pagePeek";
import type { ProductStatusSink } from "@/lib/productStatus";
import { useProductToast } from "@/lib/productToast";
import { rowMenuDocumentStats } from "@/lib/rowMenuDocumentStats";
import { serverUrl } from "@/lib/serverOrigin";
import type { Page, Session } from "@/types";
import { LibrarySelectionMenu, type LibrarySelectionKind, type LibrarySelectionMenuAction } from "./LibrarySelectionMenu";
import { announcePageVisitsChanged } from "./useLibraryData";
import styles from "./LibrarySelection.module.css";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { POPOVER_MARGIN } from "@/lib/popoverPlacement";
import { inlineStartX } from "@/lib/readingDirection";

const MoveToDialog = lazyScreen(() => (
  import("@/components/MoveToDialog").then(({ MoveToDialog: Dialog }) => ({ default: Dialog }))
), { typedOnOpen: true });

/** Notion's icon picker is this wide, under the bar's "…" (seen 2026-10-07). */
const PICKER_WIDTH = 408;
const RENAME_WIDTH = 380;

/** What the library's chosen pages can have done to them, as their sidebar rows' menus do. */
export type LibraryPageActions = {
  /** Every page the reader has, where "옮기기" may put them. */
  pages: Page[];
  setFavorite: (pageId: string, favorite: boolean) => Promise<unknown>;
  changePageMetadata: (page: Page, change: NativePageMetadataChange, signal?: AbortSignal) => Promise<boolean | Page | void>;
  /** Renames a page; spans keep its mentions and formatting (PageRenamePopover). */
  changePageTitle: (page: Page, title: string, base?: undefined, titleSpans?: TextSpan[]) => unknown;
  onPageChange: (page: Page) => void;
  onStatus?: ProductStatusSink;
};

/**
 * The chosen pages' bar, as Notion's library (seen 2026-10-07): "N개 선택됨",
 * which clears the choice, in the favorites and the private pages "즐겨찾기
 * 상단으로 이동" or "개인 페이지 상단으로 이동" and, among the favorites, a
 * struck star that takes them out (seen 2026-11-16), "휴지통으로 이동" and "…", whose menu
 * (`LibrarySelectionMenu`) opens the icon picker, the rename box and "옮기기"
 * where it was. Delete moves the chosen pages to the trash, a database among
 * them asking first, and Escape clears the choice, as Notion's.
 */
export function LibrarySelectionBar({
  session, workspaceId, chosen, recents, favorites = false, toTop, favoritePageIds, rowPageIds, actions, hrefOf, onOpenPage, onClear, menuRequest,
  onMenuRequestTaken,
}: {
  session: Session;
  workspaceId: string;
  /** The chosen pages, in the order the rows show them. */
  chosen: readonly Page[];
  recents: boolean;
  /** The view is the favorites, whose chosen pages may leave them here. */
  favorites?: boolean;
  /** The view's "… 상단으로 이동", where it is put in order by hand (useLibraryRowOrder). */
  toTop?: { label: string; move(chosen: readonly Page[]): void };
  favoritePageIds: readonly string[];
  /** The chosen pages that are a database's rows, which open, copy and leave "최근" but take no other change here. */
  rowPageIds: ReadonlySet<string>;
  actions: LibraryPageActions;
  /** Where a page opens, a database row's page in its database. */
  hrefOf: (page: Page) => string;
  onOpenPage: (page: Page, newTab: boolean) => void;
  onClear: () => void;
  /** A row's right click: the menu opens at that point. */
  menuRequest?: { x: number; y: number; at: number; pressed?: boolean; beside?: { left: number; right: number; top: number; bottom: number } } | null;
  onMenuRequestTaken?: () => void;
}) {
  const { t } = useTranslation(["library", "rowMenu", "common", "blockHandle"]);
  const { notify } = useProductToast();
  const access = usePageActionAccessReader();
  // Opened from the row itself (its handle or a right click), the menu also
  // carries the view's "… 상단으로 이동", which the bar has as its own button.
  const [menu, setMenu] = useState<{ x: number; y: number; button: DOMRect; beside?: { left: number; right: number; top: number; bottom: number };
    fromRow?: boolean; pressed?: boolean } | null>(null);
  useEffect(() => {
    if (!menuRequest) return;
    setMenu({ x: menuRequest.x, y: menuRequest.y, button: new DOMRect(menuRequest.x, menuRequest.y, 0, 0), beside: menuRequest.beside, fromRow: true,
      pressed: menuRequest.pressed });
    onMenuRequestTaken?.();
  }, [menuRequest, onMenuRequestTaken]);
  const [icon, setIcon] = useState<{ top: number; left: number } | null>(null);
  const [rename, setRename] = useState<{ top: number; left: number } | null>(null);
  const [move, setMove] = useState<MovePopoverAnchor | null>(null);
  const [busy, setBusy] = useState(false);
  const ids = chosen.map((page) => page.id);
  const idsKey = ids.join("\u0000");
  const single = chosen.length === 1 ? chosen[0] : null;
  const editable = chosen.every((page) => !rowPageIds.has(page.id));
  const databases = chosen.filter((page) => page.kind === "database").length;
  const kind: LibrarySelectionKind = databases === 0 ? "page" : databases === chosen.length ? "database" : "mixed";
  const canEdit = useSyncExternalStore(access.subscribe, () => ids.every((id) => access.canEdit(id)), () => false);
  const changes = canEdit && editable;

  const snapshot = useSyncExternalStore(access.subscribe, access.snapshot, () => 0);
  useEffect(() => {
    for (const id of idsKey.split("\u0000")) if (id) void access.read(id).catch(() => undefined);
  }, [access, idsKey, snapshot]);

  async function eachPage(change: NativePageMetadataChange, pages: readonly Page[] = chosen) {
    const signal = new AbortController().signal;
    const changed: Page[] = [];
    for (const page of pages) {
      const result = await actions.changePageMetadata(page, change, signal);
      if (result === false) throw new Error("Page change failed.");
      if (typeof result === "object") actions.onPageChange(result);
      changed.push(typeof result === "object" ? result : { ...page, ...change } as Page);
    }
    return changed;
  }

  async function trash() {
    if (busy || !editable || chosen.length === 0) return;
    setBusy(true);
    try {
      // Delete pressed right after choosing waits for the access read the bar's button waits for.
      const allowed = await Promise.all(ids.map((id) => access.read(id).catch(() => false)));
      if (!allowed.every(Boolean)) return;
      // Databases ask first, as Notion's library does (seen 2026-11-16, two ways): one as its
      // sidebar row's "휴지통으로 이동" asks (with its sprints), several once for their sources.
      const databases = chosen.filter((page) => page.kind === "database");
      let withIt: Page[] = [];
      if (databases.length === 1) {
        if (!await confirmDatabasePageTrash(session, databases[0].id)) return;
        const sprints = await confirmDatabaseSprintsTrash(session, databases[0].id, databases[0].title);
        if (!sprints) return;
        withIt = sprints.flatMap((id) => actions.pages.find((page) => page.id === id) ?? []);
      } else if (databases.length > 1 && !await confirmDatabasePagesTrash(session, databases.map((page) => page.id))) return;
      const trashed = await eachPage({ inTrash: true }, [...chosen, ...withIt]);
      onClear();
      notify(t("rowMenu:toast.movedToTrash"), "success", {
        label: t("rowMenu:trashDatabase.restore"),
        onClick: async () => {
          try {
            await eachPage({ inTrash: false }, trashed);
          } catch {
            notify(t("rowMenu:toast.couldntRestorePage"), "error");
          }
        },
      });
    } catch {
      notify(t("rowMenu:toast.couldntMoveToTrash"), "error");
    } finally {
      setBusy(false);
    }
  }

  // Delete takes the chosen pages to the trash and Escape lets them go, as
  // Notion's, unless a field, a menu or a window has the key.
  const keysOpen = !menu && !icon && !rename && !move;
  useEffect(() => {
    if (!keysOpen) return undefined;
    const keydown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      // A row's own box keeps the keys working; a text field doesn't.
      if (event.defaultPrevented || target?.closest("input:not([type='checkbox']), textarea, [contenteditable='true'], [role='dialog'], [role='menu']")) return;
      if (event.key === "Escape") {
        event.preventDefault();
        onClear();
      } else if ((event.key === "Delete" || event.key === "Backspace") && !event.metaKey && !event.ctrlKey) {
        event.preventDefault();
        void trash();
      }
    };
    document.addEventListener("keydown", keydown);
    return () => document.removeEventListener("keydown", keydown);
  });

  function changeIcon(change: PagePresentationChange, pages: readonly Page[] = chosen) {
    setIcon(null);
    void eachPage(change, pages).then(() => onClear()).catch(() => notify(t("rowMenu:toast.couldntChangeIcon"), "error"));
  }

  function act(action: LibrarySelectionMenuAction, menuRect: DOMRect | null) {
    const button = menu?.button;
    setMenu(null);
    const top = (menuRect?.top ?? button?.bottom ?? 0) + 4;
    if (action === "moveTop") {
      toTop?.move(chosen);
      onClear();
    } else if (action === "favorite" && single) {
      const favorite = favoritePageIds.includes(single.id);
      void actions.setFavorite(single.id, !favorite).then(() => {
        onClear();
      }).catch(() => notify(t("rowMenu:toast.couldntUpdateFavorites"), "error"));
    } else if (action === "removeRecent") {
      api.removePageVisits(session, workspaceId, ids)
        .then(() => { announcePageVisitsChanged(); onClear(); })
        .catch(() => notify(t("library:selection.couldntRemoveRecent"), "error"));
    } else if (action === "icon") {
      const center = button ? button.left + button.width / 2 : (menuRect?.left ?? 0);
      setIcon({ top, left: Math.max(POPOVER_MARGIN, center - PICKER_WIDTH / 2) });
    } else if (action === "rename") {
      setRename({ top, left: Math.max(POPOVER_MARGIN, Math.min(menuRect?.left ?? POPOVER_MARGIN, window.innerWidth - RENAME_WIDTH - POPOVER_MARGIN)) });
    } else if (action === "newTab" && single) {
      onOpenPage(single, true);
      onClear();
    } else if (action === "sidePeek" && single) {
      onClear();
      openPagePeek(single.id);
    } else if (action === "copyLink") {
      void copyText(chosen.map((page) => serverUrl(hrefOf(page))).join("\n")).then((copied) => {
        if (!copied) throw new Error("Copy failed.");
        notify(t("rowMenu:toast.copiedLink"), "success");
        onClear();
      }).catch(() => notify(t("rowMenu:toast.couldntCopyLink"), "error"));
    } else if (action === "move" && menuRect) {
      setMove({ top: menuRect.top - 4, bottom: menuRect.top - 4, left: menuRect.left, right: menuRect.right });
    } else if (action === "trash") {
      void trash();
    }
  }

  function unfavorite() {
    void Promise.all(chosen.map((page) => actions.setFavorite(page.id, false))).then(() => onClear())
      .catch(() => notify(t("rowMenu:toast.couldntUpdateFavorites"), "error"));
  }

  // The menu's foot counts the chosen rows' titles, not their bodies, as a
  // database view's row menu does (rowMenuFoot) — Notion's library, seen
  // 2026-11-16 at 1440: "w1393 셈 하나 둘" over seven words of body is
  // "4개의 단어, 12자", it and "w1387 resize probe" "7개의 단어, 30자".
  const stats = rowMenuDocumentStats(chosen.map((page) => page.title), chosen.length);
  const counts = t("blockHandle:footer.counts", { words: stats.words, chars: stats.characters });
  return (
    <div className={styles.selectionAnchor}>
      <div className={styles.selectionBar} role="toolbar" aria-label={t("library:selection.actions", { count: chosen.length })}
        aria-busy={busy || undefined} data-library-selection-bar>
        <button type="button" className={styles.selectionCount} aria-label={t("library:selection.clear", { count: chosen.length })}
          data-library-selection-count onClick={onClear}>
          {t("library:selection.count", { count: chosen.length })}
        </button>
        {toTop && changes ? (
          <button type="button" className={styles.selectionAction} data-library-selection-top onClick={() => act("moveTop", null)}>
            <MoveToTopIcon size="regular" trim aria-hidden="true" />{toTop.label}
          </button>
        ) : null}
        {favorites && editable ? (
          <button type="button" className={styles.selectionIcon} aria-label={t("rowMenu:actions.removeFromFavorites")}
            title={t("rowMenu:actions.removeFromFavorites")} data-library-selection-unfavorite onClick={unfavorite}>
            <StarSlashIcon size="regular" aria-hidden="true" />
          </button>
        ) : null}
        {changes ? (
          <button type="button" className={styles.selectionIcon} aria-label={t("rowMenu:actions.moveToTrash")}
            title={t("rowMenu:actions.moveToTrash")} aria-keyshortcuts="Delete" disabled={busy} data-library-selection-trash
            onClick={() => void trash()}>
            <Trash size="regular" aria-hidden="true" />
          </button>
        ) : null}
        <button type="button" className={styles.selectionIcon} aria-label={t("library:selection.more")} aria-haspopup="menu"
          aria-expanded={menu !== null} data-library-selection-more
          onClick={(event) => {
            const button = event.currentTarget.getBoundingClientRect();
            // Opened at the pointer, as Notion's; from the keyboard, under the button.
            setMenu({ x: inlineStartX(button, 0, event.currentTarget), y: event.clientY || button.bottom, button });
          }}>
          <DotsHorizontal size="regular" aria-hidden="true" />
        </button>
      </div>
      {menu ? (
        <LibrarySelectionMenu anchor={menu} count={chosen.length} kind={kind} page={single} session={session}
          favorite={single ? favoritePageIds.includes(single.id) : false}
          recents={recents} topLabel={menu.fromRow && changes ? toTop?.label : undefined} canEdit={canEdit} editable={editable} counts={counts}
          onAction={act} onClose={() => setMenu(null)} />
      ) : null}
      {icon ? (
        <AnchoredIconPicker anchor={{ top: icon.top - 4, bottom: icon.top - 4, left: icon.left }} marker="library-selection-icon"
          onClose={() => setIcon(null)}
          picker={{
            onPick: (emoji) => changeIcon({ icon: emoji, iconType: "emoji" }),
            onPickCustomEmoji: (emoji) => changeIcon({ icon: emoji.id, iconType: "custom_emoji" }),
            onPickBuiltInIcon: (value) => changeIcon({ icon: value, iconType: "icon" }),
            // A workspace emoji added here is kept with the first page.
            uploadTarget: { workspaceId, pageId: chosen[0]?.id ?? "" },
            // One page may take an uploaded image or lose its icon; several only take a new one, as Notion's.
            ...(single ? {
              current: single,
              onPickImage: (url: string) => changeIcon({ icon: url, iconType: "image" }),
              onRemove: hasExplicitPageIcon(single) ? () => changeIcon({ icon: "", iconType: "none" }) : undefined,
            } : {}),
          }} />
      ) : null}
      {rename && single ? (
        <PageRenamePopover page={single} title={single.title} titleSpans={single.titleSpans} untitled={untitledPageDisplayTitle(single)} at={rename}
          onTitleChange={(title, spans) => void actions.changePageTitle(single, title, undefined, spans)}
          onPresentationChange={(change) => changeIcon(change, [single])}
          onClose={() => { setRename(null); onClear(); }} />
      ) : null}
      {move && chosen.length > 0 ? (
        <Suspense fallback={null}>
          <MoveToDialog pageId={chosen[0].id} alsoPageIds={ids.slice(1)} pages={actions.pages} session={session} anchor={move}
            onClose={() => setMove(null)} onMoved={onClear} onPageChange={actions.onPageChange} onStatus={actions.onStatus} />
        </Suspense>
      ) : null}
    </div>
  );
}
