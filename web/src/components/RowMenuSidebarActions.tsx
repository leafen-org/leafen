import { NativeActionMenu, NativeActionMenuItem, NativeMenuSeparator } from "@/lib/nativeActionMenu";
import { useSubmenuHover } from "./PageMenu";
import { RowMenuBesideSubmenu, RowMenuSubmenuRow } from "./RowMenuSubmenus";
import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { ArchiveIcon, ComposeIcon, Copy, LinkIcon, LogOutIcon, MoveIcon, OpenInNew, RepeatIcon, SidePeekIcon, Star, StarFilled, Trash } from "@/icons/leafen";
import type { Page } from "@/types";
import type { GuestSidebarRows } from "./useGuestRowMenu";
import { RowMenuEditedFoot } from "./RowMenuEditedFoot";
import type { PageLastEditVia, EditedFootActivity } from "@/lib/pageEditedActivity";
import styles from "./RowMenu.module.css";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { menuKeyHint } from "@/lib/menuKeyHint";
import { keepLayerKeys } from "@/lib/overlayLayer";

/**
 * A sidebar page's own short menu, as Notion's "삭제, 복제 등…" (seen
 * 2026-10-01 and 2026-10-04): headed "페이지" — favorite; copy link,
 * duplicate, rename, move, archive (2026-10-11), trash; wiki; open in a new
 * tab and open in side peek (⌥Click) — over who edited the page last and when, with the keys
 * Leafen has: ⌘⇧R renames from a page's title and ⌘⇧↵ opens a focused row in
 * a new tab, as Notion's (seen 2026-10-02). A favorite's row in the
 * favorites lists leaves out duplicate and trash but keeps archive. Someone who can't edit the
 * page sees only favorite, copy link and the two opens, over a line saying so
 * (Notion's for comment and read access, seen 2026-10-08). A locked page's
 * keeps favorite, copy link, a dimmed trash and the opens (Notion, seen
 * 2026-10-31); Leafen's archive dims with the trash. A database's is headed
 * "데이터베이스", its "복제 ›" opening "콘텐츠 포함 복제 ⌘D" and "콘텐츠 제외
 * 복제 ⌘⇧D" beside it, with no wiki (Notion, seen 2026-11-10). A guest's
 * follows the page above as their page menu (useGuestEditsPageAbove): "복제"
 * and a lit trash only under a page above they edit, "옮기기" always dimmed,
 * no archive nor wiki (Notion, seen 2026-11-16 in two tabs by a guest who
 * edits: under a page they edit, and the page topping what is shared). The
 * page topping what is shared with a guest has "나가기" under favorite
 * (useGuestRowMenu; Notion, seen 2026-11-16 in two tabs). A guest's other
 * rows follow their access (GuestSidebarRows): with full access, "위키로
 * 전환", a lit "옮기기" under a page they edit, and over none they edit
 * "복제 대상 ›" beside its row, their own workspaces, 220 wide.
 */
export function RowMenuSidebarActions({
  archiveState = "active",
  busy,
  canEdit,
  edited,
  favorite,
  favoritesList = false,
  guest = false,
  guestRows,
  heldBy = "",
  lockedForMe,
  page,
  onCopyLink,
  onDuplicate,
  onDuplicateWithoutContent,
  onFavorite,
  onLeave,
  onArchive,
  onMove,
  onOpenInNewTab,
  onOpenInSidePeek,
  onRename,
  onTrash,
  onWiki,
  restricted = false,
}: {
  /** "inherited" when the page is archived with an archived parent page, which has no archive item. */
  archiveState?: "active" | "archived" | "inherited";
  busy: boolean;
  canEdit: boolean;
  /** The person's access is known and doesn't let them edit the page. */
  restricted?: boolean;
  /** Who edited the page last, and when, for the menu's foot. */
  edited?: { name: string; at?: string | null; editorId?: string | null; via?: PageLastEditVia; activity?: EditedFootActivity };
  favorite: boolean;
  /** The row is in the favorites, whose menu has no duplicate or trash. */
  favoritesList?: boolean;
  /** The reader is a guest here: no archive of theirs. */
  guest?: boolean;
  /** A guest's rows by their access (useGuestRowMenu). */
  guestRows?: GuestSidebarRows;
  /** Why the page takes no changes for now, which is why this menu is short. */
  heldBy?: string;
  lockedForMe: boolean;
  page: Page;
  onCopyLink(): void;
  onDuplicate(): void;
  /** A database's copy without its rows. */
  onDuplicateWithoutContent?: () => void;
  onFavorite(): void;
  /** A guest's leaving the page topping what is shared with them (useGuestRowMenu). */
  onLeave?: () => void;
  onArchive?: () => void;
  /** Moving it; a favorite database page's row, which moves in its database, has none here. */
  onMove?: () => void;
  onOpenInNewTab(): void;
  onOpenInSidePeek?: () => void;
  onRename?: () => void;
  onTrash(): void;
  onWiki?: () => void;
}) {
  const { t } = useTranslation(["rowMenu", "topBar", "common", "blockHandle"]);
  const wikiRoot = page.isWiki && page.wikiRootId === page.id;
  const database = page.kind === "database";
  // A database's "복제 ›" row, its two rows open beside it, or a guest's "복제 대상 ›" and their workspaces.
  const [beside, setBeside] = useState<{ kind: string; row: HTMLElement } | null>(null);
  const duplicateRow = beside?.kind === "duplicate" ? beside.row : null;
  const duplicateToRow = beside?.kind === "duplicate-to" ? beside.row : null;
  const besideHover = useSubmenuHover(beside?.kind ?? null, (kind, row) => setBeside({ kind, row }), () => setBeside(null));
  const duplicateTo = canEdit && !favoritesList && !lockedForMe ? guestRows?.duplicateTo : undefined;
  const item = (key: string, icon: ReactNode, label: string, onClick: () => void, hint?: string, disabled = false) => (
    <NativeActionMenuItem key={key} type="button" className={styles.item} data-menu-item
      data-sidebar-page-action={key} role="menuitem" disabled={busy || disabled} onClick={onClick}>
      {icon}
      <span>{label}</span>
      {hint ? <span className={styles.itemHint}>{hint}</span> : null}
    </NativeActionMenuItem>
  );
  const divider = <NativeMenuSeparator className={styles.divider} />;
  return (
    <>
      <div className={styles.sectionLabel}>{database ? t("blockHandle:database.title") : t("rowMenu:sections.page")}</div>
      {heldBy ? <div className={styles.submenuNote} data-row-menu-held>{heldBy}</div> : null}
      <span className={styles.submenuHost} onPointerMove={database || duplicateTo ? besideHover.onPointerMove : undefined}>
      <NativeActionMenu aria-label={t("rowMenu:aria.pageActionsFor", { title: titleWithClosedMentions(page.title, page.titleSpans) || untitledPageDisplayTitle(page) })} autoFocus>
      {item("favorite", favorite ? <StarFilled size="regular" aria-hidden="true" /> : <Star size="regular" aria-hidden="true" />,
        favorite ? t("rowMenu:actions.unfavorite") : t("rowMenu:actions.addToFavorites"), onFavorite)}
      {onLeave ? item("leave", <LogOutIcon size="regular" aria-hidden="true" />, t("rowMenu:actions.leave"), onLeave) : null}
      {divider}
      {item("copy-link", <LinkIcon size="regular" aria-hidden="true" />, t("rowMenu:actions.copyLink"), onCopyLink)}
      {canEdit && !favoritesList && !lockedForMe && (guestRows?.duplicate ?? true)
        ? database && onDuplicateWithoutContent
          ? <RowMenuSubmenuRow key="duplicate-menu" kind="duplicate" icon={<Copy size="regular" aria-hidden="true" />} label={t("rowMenu:actions.duplicate")}
            value="" open={duplicateRow !== null} disabled={busy} onOpen={(row) => setBeside({ kind: "duplicate", row })} data={{ "data-sidebar-page-action": "duplicate-menu" }} />
          : item("duplicate", <Copy size="regular" aria-hidden="true" />, t("rowMenu:actions.duplicate"), onDuplicate, menuKeyHint("⌘D"))
        : duplicateTo ? <RowMenuSubmenuRow key="duplicate-to" kind="duplicate-to" icon={<Copy size="regular" aria-hidden="true" />} label={t("rowMenu:actions.duplicateTo")}
          value="" open={duplicateToRow !== null} disabled={busy} onOpen={(row) => setBeside({ kind: "duplicate-to", row })} data={{ "data-sidebar-page-action": "duplicate-to" }} />
          : null}
      {onRename && canEdit && !lockedForMe
        ? item("rename", <ComposeIcon size="regular" aria-hidden="true" />, t("rowMenu:actions.rename"), onRename, menuKeyHint("⌘⇧R"))
        : null}
      {canEdit && onMove && !lockedForMe ? item("move", <MoveIcon size="regular" aria-hidden="true" />, t("rowMenu:actions.moveTo"), onMove, menuKeyHint("⌘⇧P"), guestRows ? !guestRows.move : false) : null}
      {canEdit && onArchive && archiveState !== "inherited" && !guest
        ? item("archive", <ArchiveIcon size="regular" aria-hidden="true" />,
          t(archiveState === "archived" ? "rowMenu:actions.unarchive" : "rowMenu:actions.archive"), onArchive, undefined, lockedForMe)
        : null}
      {canEdit && !favoritesList
        ? item("trash", <Trash size="regular" aria-hidden="true" />, t("rowMenu:actions.moveToTrash"), onTrash, undefined, lockedForMe || (guestRows ? !guestRows.trash : false))
        : null}
      {onWiki && canEdit && !lockedForMe && !database && (guestRows?.wiki ?? true) ? (
        <>
          {divider}
          {/* Undoing a wiki asks over the closed menu (confirmWikiUndo). */}
          {item("wiki", <RepeatIcon size="regular" aria-hidden="true" />, wikiRoot ? t("topBar:undoWiki") : t("topBar:turnIntoWiki"), onWiki)}
        </>
      ) : null}
      {divider}
      {item("new-tab", <OpenInNew size="regular" aria-hidden="true" />, t("rowMenu:actions.openInNewTab"), onOpenInNewTab, menuKeyHint("⌘⇧↵"))}
      {onOpenInSidePeek
        ? item("side-peek", <SidePeekIcon size="regular" aria-hidden="true" />, t("rowMenu:actions.openInSidePeek"), onOpenInSidePeek, menuKeyHint("⌥Click"))
        : null}
      </NativeActionMenu>
      </span>
      {duplicateRow && onDuplicateWithoutContent ? (
        <span className={styles.submenuHost} onKeyDown={keepLayerKeys} onPointerMove={besideHover.hold}>
          <RowMenuBesideSubmenu row={duplicateRow} width={275} label={t("rowMenu:actions.duplicate")} onClose={() => setBeside(null)} ownMenu>
            <NativeActionMenu aria-label={t("rowMenu:actions.duplicate")}>
              {/* Its two rows have no icon, each with its key, as the page menu's. */}
              {item("duplicate", null, t("rowMenu:actions.duplicateWithContent"), onDuplicate, menuKeyHint("⌘D"))}
              {item("duplicate-empty", null, t("rowMenu:actions.duplicateWithoutContent"), onDuplicateWithoutContent, menuKeyHint("⌘⇧D"))}
            </NativeActionMenu>
          </RowMenuBesideSubmenu>
        </span>
      ) : null}
      {duplicateToRow && duplicateTo ? (
        <span className={styles.submenuHost} onKeyDown={keepLayerKeys} onPointerMove={besideHover.hold}>
          <RowMenuBesideSubmenu row={duplicateToRow} width={220} label={t("rowMenu:actions.duplicateTo")} onClose={() => setBeside(null)} ownMenu>
            <NativeActionMenu aria-label={t("rowMenu:actions.duplicateTo")}>
              {duplicateTo.map((choice) => (
                <NativeActionMenuItem key={choice.key} type="button" className={styles.item} data-menu-item role="menuitem" disabled={busy}
                  onClick={choice.run} {...choice.data}>
                  {choice.icon}
                  <span>{choice.label}</span>
                </NativeActionMenuItem>
              ))}
            </NativeActionMenu>
          </RowMenuBesideSubmenu>
        </span>
      ) : null}
      {restricted && (guestRows?.restrictedFoot ?? true) ? (
        <>
          {divider}
          <div className={styles.menuFoot} data-sidebar-page-restricted="">{t("rowMenu:meta.cannotEdit")}</div>
        </>
      ) : canEdit && edited ? <RowMenuEditedFoot name={edited.name} editorId={edited.editorId} via={edited.via} at={edited.at} activity={edited.activity}
        marker="sidebar-page-edited" /> : null}
    </>
  );
}
