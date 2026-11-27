import { type CSSProperties, Fragment, type ReactNode, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ComposeIcon, EyeSlashIcon, LinkIcon, MoveIcon, MoveToTopIcon, OpenInNew, SidePeekIcon, SmileIcon, Star, StarSlashIcon, Trash,
} from "@/icons/leafen";
import { NativeActionMenu, NativeActionMenuItem, NativeMenuSeparator } from "@/lib/nativeActionMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeAtPoint, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PopoverLayer } from "@/lib/popoverLayer";
import { RowMenuEditedFoot } from "@/components/RowMenuEditedFoot";
import rowMenuStyles from "@/components/RowMenu.module.css";
import styles from "./LibrarySelection.module.css";
import { menuKeyHint } from "@/lib/menuKeyHint";
import { pageEditedActivity, usePageLastEdit, usePageLastEditVia } from "@/lib/pageEditedActivity";
import { editorName } from "@/lib/peopleDirectory";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import type { Page, Session } from "@/types";

export type LibrarySelectionMenuAction =
  | "favorite" | "moveTop" | "removeRecent" | "icon" | "rename" | "newTab" | "sidePeek" | "copyLink" | "move" | "trash";

type Entry = { key: LibrarySelectionMenuAction; icon: ReactNode; label: string; hint?: string };

/** What the chosen rows are: pages, databases, or both. */
export type LibrarySelectionKind = "page" | "database" | "mixed";

const NO_PAGE = { id: "", updatedAt: "", lastEditedBy: "" };

/**
 * The chosen rows' "…", as Notion's library (seen 2026-10-07): a search
 * field, then under "페이지" — for one page: "즐겨찾기에 추가" or, a
 * favorite, "즐겨찾기 해제" under a struck star, "즐겨찾기 상단으로 이동" or
 * "개인 페이지 상단으로 이동" where the menu was opened from the row itself
 * (its handle or a right click — the bar's "…" leaves it to the bar's own
 * button; seen 2026-11-16 in both tabs), "최근 항목에서 제거" (in 최근), edit icon, rename | open in a new tab, in side peek | copy link,
 * move, trash, over the foot a sidebar row's menu has — "최종 편집 일시", the
 * editor's face and name, when — and its title's words and characters; for
 * several: "최근 항목에서 제거", edit icon | copy every link, move, trash,
 * over their titles' words (LibrarySelectionBar counts them). A database
 * chosen alone is headed "데이터베이스" — favorite | the two opens | copy
 * link, over the same foot without words; several databases only copy every
 * link, and pages and databases together, headed "블록 N개",
 * copy every link over their words (Notion, seen 2026-11-16 at 1440 in the
 * private pages, the recents and from the bar alike; its "데이터베이스 사용법"
 * at the foot opens Notion's help center and is left out). It opens
 * at the pointer, or beside a row's handle, its rows as a page's own "…"
 * (28px).
 */
export function LibrarySelectionMenu({
  anchor, count, kind, page, session, favorite, recents, topLabel, canEdit, editable, counts, onAction, onClose,
}: {
  /**
   * Where it opens: at the point, or beside a row's handle — to its left, 5
   * from it, its middle level with the handle's, slid into the window where
   * it would cross it, as Notion's (seen 2026-11-16 at 1440: a handle at
   * 312..330 centred at 306.4 put the menu's 42..307 centred at 306.4, one
   * near the top at 12 and one near the foot 11.65 from it).
   */
  anchor: { x: number; y: number; beside?: { left: number; right: number; top: number; bottom: number } };
  count: number;
  kind: LibrarySelectionKind;
  /** The one page or database chosen, whose last edit the foot names. */
  page: Page | null;
  session: Session;
  /** The one page chosen is a favorite. */
  favorite: boolean;
  /** The view is "최근", whose rows may leave it. */
  recents: boolean;
  /** The view's "… 상단으로 이동", where the menu was opened from the row. */
  topLabel?: string;
  /** The reader may change every page chosen. */
  canEdit: boolean;
  /** The pages chosen are the workspace's own, not a database's rows, which these actions don't reach. */
  editable: boolean;
  counts: string;
  onAction: (action: LibrarySelectionMenuAction, menu: DOMRect | null) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation(["library", "rowMenu", "blockHandle"]);
  // The foot names who edited last once the people directory knows them.
  usePeopleDirectoryRevision();
  const menuRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  const place = usePopoverPlacement(menuRef, (size, viewport) => (anchor.beside
    ? { anchor: anchor.beside, placement: "left", gap: 5, options: {} } : placeAtPoint(anchor, size, viewport)), [anchor], undefined, false);
  const { zIndex } = useOverlayLayer(true, { kind: "menu", ref: menuRef, onEscape: onClose, onOutsidePress: onClose });

  useEffect(() => {
    searchRef.current?.focus({ preventScroll: true });
  }, []);

  const single = count === 1;
  const changes = canEdit && editable;
  const database = kind === "database";
  // The last edit as read again when the menu opens, as a sidebar row's menu reads it (RowMenu).
  const edited = usePageLastEdit(session, page ?? NO_PAGE, Boolean(page));
  const editorId = page ? edited.lastEditedBy || page.createdBy || null : null;
  const via = usePageLastEditVia(session, page?.id ?? "", editorId, Boolean(page));
  const favoriteEntry: Entry[] = editable ? [{ key: "favorite", icon: favorite ? <StarSlashIcon size="regular" aria-hidden="true" /> : <Star size="regular" aria-hidden="true" />,
    label: t(favorite ? "rowMenu:actions.unfavorite" : "rowMenu:actions.addToFavorites") }] : [];
  const opens: Entry[] = [
    { key: "newTab", icon: <OpenInNew size="regular" aria-hidden="true" />, label: t("rowMenu:actions.openInNewTab"), hint: menuKeyHint("⌘⇧↵") },
    { key: "sidePeek", icon: <SidePeekIcon size="regular" aria-hidden="true" />, label: t("rowMenu:actions.openInSidePeek"), hint: menuKeyHint("⌥Click") },
  ];
  const copyLinks: Entry = { key: "copyLink", icon: <LinkIcon size="regular" aria-hidden="true" />, label: t("library:selection.copyLinks") };
  const groups: Entry[][] = kind !== "page" ? (single && database ? [
    favoriteEntry, opens, [{ key: "copyLink", icon: <LinkIcon size="regular" aria-hidden="true" />, label: t("rowMenu:actions.copyLink") }],
  ] : [[copyLinks]]) : single ? [
    [
      ...favoriteEntry,
      ...(topLabel ? [{ key: "moveTop" as const, icon: <MoveToTopIcon size="regular" aria-hidden="true" />, label: topLabel }] : []),
      ...(recents ? [{ key: "removeRecent" as const, icon: <EyeSlashIcon size="regular" weight="regular" aria-hidden="true" />, label: t("library:selection.removeRecent") }] : []),
      ...(changes ? [
        { key: "icon" as const, icon: <SmileIcon size="regular" aria-hidden="true" />, label: t("rowMenu:actions.editIcon") },
        { key: "rename" as const, icon: <ComposeIcon size="regular" aria-hidden="true" />, label: t("rowMenu:actions.rename"), hint: menuKeyHint("⌘⇧R") },
      ] : []),
    ],
    opens,
    [
      { key: "copyLink", icon: <LinkIcon size="regular" aria-hidden="true" />, label: t("rowMenu:actions.copyLink") },
      ...(changes ? [
        { key: "move" as const, icon: <MoveIcon size="regular" aria-hidden="true" />, label: t("rowMenu:actions.moveTo"), hint: menuKeyHint("⌘⇧P") },
        { key: "trash" as const, icon: <Trash size="regular" aria-hidden="true" />, label: t("rowMenu:actions.moveToTrash"), hint: "Del" },
      ] : []),
    ],
  ] : [
    [
      ...(recents ? [{ key: "removeRecent" as const, icon: <EyeSlashIcon size="regular" weight="regular" aria-hidden="true" />, label: t("library:selection.removeRecent") }] : []),
      ...(changes ? [{ key: "icon" as const, icon: <SmileIcon size="regular" aria-hidden="true" />, label: t("rowMenu:actions.editIcon") }] : []),
    ],
    [
      copyLinks,
      ...(changes ? [
        { key: "move" as const, icon: <MoveIcon size="regular" aria-hidden="true" />, label: t("rowMenu:actions.moveTo"), hint: menuKeyHint("⌘⇧P") },
        { key: "trash" as const, icon: <Trash size="regular" aria-hidden="true" />, label: t("rowMenu:actions.moveToTrash"), hint: "Del" },
      ] : []),
    ],
  ];
  const needle = query.trim().toLowerCase();
  const shown = groups.map((group) => group.filter((entry) => !needle || entry.label.toLowerCase().includes(needle)))
    .filter((group) => group.length > 0);
  const style = { ...popoverPlacementStyle(place), zIndex, "--menu-x": `${anchor.x}px`, "--menu-y": `${anchor.y}px` } as CSSProperties;

  return (
    <PopoverLayer><div ref={menuRef} className={`${rowMenuStyles.menu} ${rowMenuStyles.contextMenu} ${styles.selectionMenu}`} style={style}
      data-row-menu-variant="page" data-library-selection-menu={single ? "one" : "many"}>
      <label className={rowMenuStyles.actionSearch}>
        <input ref={searchRef} value={query} placeholder={t("library:selection.searchActions")} aria-label={t("library:selection.searchActions")}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== "ArrowDown") return;
            event.preventDefault();
            menuRef.current?.querySelector<HTMLElement>("[data-native-menu-item]:not([aria-disabled='true'])")?.focus();
          }} />
      </label>
      <div className={styles.menuSection} data-library-selection-head={kind}>
        {kind === "mixed" ? t("blockHandle:menu.blockCount", { count }) : database ? t("blockHandle:database.title") : t("rowMenu:sections.page")}
      </div>
      <NativeActionMenu aria-label={t("library:selection.actions", { count })}>
        {shown.map((group, index) => (
          <Fragment key={group[0].key}>
            {index > 0 ? <NativeMenuSeparator className={rowMenuStyles.divider} /> : null}
            {group.map((entry) => (
              <NativeActionMenuItem key={entry.key} type="button" className={rowMenuStyles.item} data-menu-item
                data-library-selection-action={entry.key} role="menuitem"
                onClick={() => onAction(entry.key, menuRef.current?.getBoundingClientRect() ?? null)}>
                {entry.icon}
                <span>{entry.label}</span>
                {entry.hint ? <span className={rowMenuStyles.itemHint}>{entry.hint}</span> : null}
              </NativeActionMenuItem>
            ))}
          </Fragment>
        ))}
      </NativeActionMenu>
      {single && page ? (
        <RowMenuEditedFoot name={editorName(editorId, session.user.id)} editorId={editorId} via={via} at={edited.updatedAt}
          counts={database ? undefined : counts} activity={pageEditedActivity({ ...page, ...edited }, session.user.id)} marker="library-selection-edited" />
      ) : database ? null : (
        <>
          <div className={rowMenuStyles.divider} role="separator" />
          <div className={styles.menuCounts} data-library-selection-counts>{counts}</div>
        </>
      )}
    </div></PopoverLayer>
  );
}
