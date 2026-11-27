import { Fragment, useContext, useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  ArchiveIcon, BulletedListIcon, CardLayoutIcon, ClockIcon, CommentIcon, Copy, Download, EyeIcon, FullPagePeekIcon, LinkIcon, LockIcon, NewWindowIcon, OpenInNew,
  Pencil as SuggestIcon, PresentIcon, RenameInPlaceIcon, SidePeekIcon, SmileIcon, Star, StarFilled, SuggestEditIcon, Trash, UnlockIcon, Upload,
} from "@/icons/leafen";
import { matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";
import type { DatabaseRowPageOpenMode } from "@/lib/databaseRowPageService";
import { NativeActionMenuItem, NativeMenuSeparator } from "@/lib/nativeActionMenu";
import type { PageSuggestingControl } from "../RowMenu";
import { RowMenuBesideSubmenu, RowMenuSubmenuRow } from "../RowMenuSubmenus";
import { useSubmenuHover } from "../PageMenu";
import styles from "../RowMenu.module.css";
import type { DatabaseRowMoveDrop, DatabaseRowMoveTarget, DatabaseRowPageLockControl } from "./DatabaseRowMenu";
import { DatabaseRowMoveSubmenu } from "./DatabaseRowMoveSubmenu";
import { PropertyIcon } from "./PropertyIcon";
import { SidePeekContext } from "./databaseRowOpeners";
import type { DatabaseRowMoveTargetSearch } from "./useDatabaseRowMoveTargets";
import { menuKeyHint } from "@/lib/menuKeyHint";

/** A row's ways to open, from its menu's "다음에서 열기 ›". */
export type DatabaseRowMenuOpenMode = DatabaseRowPageOpenMode | "new_tab";

/** What a row's menu in its view offers beyond the page's actions, each by the row it was opened from. */
export type DatabaseRowMenuLines = {
  /** "아이콘 편집": the row page's icon picker. */
  onEditIcon?: () => void;
  /** "속성 편집 ›": the database's properties, each opening the row's value of it. */
  editProperties?: {
    properties: ReadonlyArray<{ id: string; name: string; type: string; config?: Record<string, unknown> }>;
    onEdit(propertyId: string): void;
  };
  /** A board's or gallery's "레이아웃" and "속성 표시 여부": the view settings at either. */
  onViewSettings?: (screen: "layout" | "properties") => void;
  /** "댓글": a new comment's field under the row. */
  onComment?: () => void;
  /** A timeline's: "새 탭에서 열기" and "사이드 보기에서 열기" as lines of their own, as Notion's timeline bar menu has them. */
  openInline?: boolean;
  /** Opened by ⌘/ over the chosen rows: Notion's then shows no "Del" by "휴지통으로 이동" (seen 2026-11-07). */
  byKey?: boolean;
  /** Opened by ⌘/ over the row or card alone: Notion's page item menu, with its name, suggesting and presenting here. */
  pageItem?: { onRename?: () => void; onSuggest?: () => void; onPresent?: () => void };
};

/**
 * A row's "⋯" in its view, as Notion's table row, board, gallery, list and
 * calendar card menus (seen 2026-10-11): favorite, icon, properties › |
 * layout, property visibility (a board's and a gallery's) | open in ›,
 * comment | link, duplicate, move, archive, trash. A timeline bar's opens in
 * a new tab or the side peek at its own lines and has no comment. Notion's "알림 받기" and
 * "업데이트와 애널리틱스" are its row page's own "…". Rows the open page lends
 * (lock, suggest edits, import, export, history) follow the move. ⌘/ over
 * one row or card opens Notion's page item menu instead (`lines.pageItem`).
 * The rows go in the menu, the choices beside it outside it.
 */
export function useDatabaseRowViewMenu({
  query, busy, readOnly, archived, favorite, lines, lock, suggesting, moveTargets, searchMoveTargets,
  onFavorite, onOpenMode, onOpenWindow, onCopyLink, copyLinkLabel, onDuplicate, onMove, onPreviewMove, onOpenMove, onImport, onExport, onHistory,
  onArchive, onTrash, onClose,
}: {
  query: string;
  busy: boolean;
  readOnly: boolean;
  archived: boolean;
  favorite: boolean;
  lines?: DatabaseRowMenuLines;
  lock?: DatabaseRowPageLockControl;
  suggesting?: PageSuggestingControl;
  moveTargets: DatabaseRowMoveTarget[];
  searchMoveTargets?: DatabaseRowMoveTargetSearch;
  onFavorite?: (favorite: boolean) => Promise<void>;
  onOpenMode?: (mode: DatabaseRowMenuOpenMode) => void;
  /** A computer's app's "새 창에서 열기", under the new tab in ⌘/'s page item menu, as Notion's app's (seen 2026-11-16). */
  onOpenWindow?: () => void;
  onCopyLink: () => Promise<void>;
  copyLinkLabel?: string;
  onDuplicate?: () => Promise<void>;
  onMove?: (target: DatabaseRowMoveTarget, acceptDroppedPropertyIds: string[]) => Promise<void>;
  onPreviewMove?: (target: DatabaseRowMoveTarget) => Promise<DatabaseRowMoveDrop[]>;
  /** Opens the pages' move popover for "옮기기" instead of the list under it. */
  onOpenMove?: () => void;
  onImport?: () => void;
  onExport?: () => void;
  onHistory?: () => void;
  onArchive?: (archived: boolean) => Promise<void>;
  onTrash?: () => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useTranslation(["rowMenu", "databaseView", "topBar", "blockHandle"]);
  const [submenu, setSubmenu] = useState<{ kind: "properties" | "openIn"; row: HTMLElement } | null>(null);
  // In a side peek "다음에서 열기 ›" also opens the row as a full page, between the new tab and the side peek, as
  // Notion's (seen 2026-11-25 in Chrome, two tabs: an inline database opened alone in a side peek and a database in
  // a row page there listed 새 탭 · 전체 페이지 보기 · 사이드 보기, its press opening the row's own page; on a page,
  // a database's own page, a row page opened whole and a center peek, 새 탭 · 사이드 보기).
  const inSidePeek = useContext(SidePeekContext);
  // "속성 편집 ›" and "다음에서 열기 ›" open beside the menu under the pointer
  // too, by the rule every page menu's › rows keep (useSubmenuHover), as Notion's.
  const hover = useSubmenuHover(submenu?.kind ?? null,
    (kind, row) => setSubmenu({ kind: kind === "properties" ? "properties" : "openIn", row }), () => setSubmenu(null));
  // With the menu open ⌘⇧↵ opens the row in a new tab, whether "새 탭에서 열기"
  // shows or waits under "다음에서 열기", as Notion's (seen 2026-11-03).
  useEffect(() => {
    if (!onOpenMode) return undefined;
    const keys = (event: KeyboardEvent) => {
      if (!matchesKeyboardShortcut("openInNewTab", event)) return;
      event.preventDefault();
      event.stopPropagation();
      onClose();
      onOpenMode("new_tab");
    };
    window.addEventListener("keydown", keys, true);
    return () => window.removeEventListener("keydown", keys, true);
  }, [onClose, onOpenMode]);
  const needle = query.trim().toLocaleLowerCase();
  const visible = (label: string) => !needle || label.toLocaleLowerCase().includes(needle);
  const item = (key: string, label: string, icon: ReactNode, onClick: (() => void) | undefined, options: {
    hint?: string; disabled?: boolean; data?: Record<string, string>; role?: "menuitem" | "menuitemcheckbox"; checked?: boolean;
    className?: string; badge?: string;
  } = {}) => (onClick && visible(label) ? (
    <NativeActionMenuItem key={key} type="button" className={`${styles.item} ${options.className ?? ""}`} data-menu-item role={options.role ?? "menuitem"}
      aria-checked={options.checked} disabled={busy || options.disabled} onClick={onClick} {...options.data}>
      {icon}
      <span>{label}{options.badge ? <span className={styles.itemBadge}>{options.badge}</span> : null}</span>
      {options.hint ? <span className={styles.itemHint}>{options.hint}</span> : null}
    </NativeActionMenuItem>
  ) : null);
  const besideRow = (kind: "properties" | "openIn", label: string, icon: ReactNode, disabled: boolean) => (visible(label) ? (
    <RowMenuSubmenuRow key={kind} kind={kind} icon={icon} label={label} value="" open={submenu?.kind === kind} disabled={busy || disabled}
      onOpen={(row) => setSubmenu({ kind, row })} data={{ "data-row-menu-beside": kind }} />
  ) : null);
  const later = (run: () => void) => () => { onClose(); run(); };
  const { onEditIcon, editProperties, onViewSettings, onComment, openInline, pageItem } = lines ?? {};
  const icon = onEditIcon && !readOnly ? item("icon", t("rowMenu:actions.editIcon"), <SmileIcon size="regular" aria-hidden="true" />,
    later(onEditIcon), { data: { "data-row-edit-icon": "" } }) : null;
  const favoriteLine = onFavorite ? item("favorite", favorite ? t("rowMenu:actions.removeFromFavorites") : t("rowMenu:actions.addToFavorites"),
    favorite ? <StarFilled size="regular" aria-hidden="true" /> : <Star size="regular" aria-hidden="true" />,
    () => void onFavorite(!favorite), { disabled: readOnly }) : null;
  const properties = editProperties && editProperties.properties.length > 0
    ? besideRow("properties", t("rowMenu:actions.editProperties"), <BulletedListIcon size="regular" aria-hidden="true" />, readOnly) : null;
  const opens = onOpenMode ? [
    item("new-tab", t("rowMenu:actions.openInNewTab"), <OpenInNew size="regular" aria-hidden="true" />, later(() => onOpenMode("new_tab")),
      { hint: menuKeyHint("⌘⇧↵"), data: { "data-row-open-in": "new_tab" } }),
    pageItem && onOpenWindow ? item("new-window", t("rowMenu:actions.openInNewWindow"), <NewWindowIcon size="regular" aria-hidden="true" />,
      later(onOpenWindow), { data: { "data-row-open-in": "new_window" } }) : null,
    item("side", t("rowMenu:actions.openInSidePeek"), <SidePeekIcon size="regular" aria-hidden="true" />, later(() => onOpenMode("side")),
      { hint: menuKeyHint("⌥Click"), data: { "data-row-open-in": "side" } }),
  ] : [];
  const comment = onComment ? item("comment", t("topBar:comment"), <CommentIcon size="regular" aria-hidden="true" />, later(onComment),
    { hint: menuKeyHint("⌘⇧M"), data: { "data-row-comment-action": "" } }) : null;

  // The page's own lines: link, duplicate, move, those the open page lends (lock, suggest edits, import, export, history), archive, trash.
  const pageActions = () => [
    item("link", copyLinkLabel ?? t("rowMenu:actions.copyLink"), <LinkIcon size="regular" aria-hidden="true" />, () => void onCopyLink()),
    onDuplicate ? item("duplicate", t("rowMenu:actions.duplicate"), <Copy size="regular" aria-hidden="true" />, () => void onDuplicate(),
      { hint: menuKeyHint("⌘D"), disabled: readOnly }) : null,
    onMove && visible(t("rowMenu:actions.moveTo")) ? (
      <DatabaseRowMoveSubmenu key="move" busy={busy} disabled={readOnly} hint={menuKeyHint("⌘⇧P")} moveTargets={moveTargets}
        searchMoveTargets={searchMoveTargets} onMove={onMove} onPreviewMove={onPreviewMove} onOpen={onOpenMove} />
    ) : null,
    lock ? item("lock", t(lock.locked ? "rowMenu:actions.unlockPage" : "rowMenu:actions.lockPage"),
      lock.locked ? <UnlockIcon size="regular" aria-hidden="true" /> : <LockIcon size="regular" aria-hidden="true" />,
      () => { lock.onLock(!lock.locked); onClose(); },
      { disabled: readOnly, role: "menuitemcheckbox", checked: lock.locked, data: { "data-row-page-lock": "" } }) : null,
    // Given to whoever may suggest on the row's body, one who may only comment too: read-only for them, it stays pressable.
    suggesting ? item("suggest", t("rowMenu:actions.suggestEdits"), <SuggestIcon size="regular" aria-hidden="true" />,
      () => { suggesting.onChange(!suggesting.active); onClose(); },
      { role: "menuitemcheckbox", checked: suggesting.active, data: { "data-page-suggest-action": "" } }) : null,
    onImport ? item("import", t("rowMenu:actions.import"), <Upload size="regular" aria-hidden="true" />, later(onImport),
      { disabled: readOnly, data: { "data-row-page-import": "" } }) : null,
    onExport ? item("export", t("rowMenu:actions.export"), <Download size="regular" aria-hidden="true" />, later(onExport),
      { data: { "data-page-export-action": "" } }) : null,
    onHistory ? item("history", t("rowMenu:actions.pageHistory"), <ClockIcon size="regular" aria-hidden="true" />, later(onHistory),
      { data: { "data-row-page-history": "" } }) : null,
    onArchive ? item("archive", t(archived ? "rowMenu:actions.unarchive" : "rowMenu:actions.archive"),
      <ArchiveIcon size="regular" aria-hidden="true" />, () => void onArchive(!archived),
      { disabled: readOnly, data: { "data-row-archive-action": archived ? "archived" : "active" } }) : null,
    onTrash ? item("trash", t("rowMenu:actions.moveToTrash"), <Trash size="regular" aria-hidden="true" />, () => void onTrash(),
      { hint: lines?.byKey ? undefined : "Del", disabled: readOnly, className: styles.dangerOnHover, data: { "data-row-trash-action": "" } }) : null,
  ];

  const groups: ReactNode[][] = pageItem ? [
    // ⌘/ over the row or card alone: Notion's page item menu (seen 2026-11-12) — icon, favorite, name,
    // properties | the opens | link, duplicate, move, archive, trash | comment, suggesting | presenting here.
    [icon, favoriteLine,
      pageItem.onRename && !readOnly ? item("rename", t("rowMenu:actions.rename"), <RenameInPlaceIcon size="regular" aria-hidden="true" />,
        later(pageItem.onRename), { hint: menuKeyHint("⌘⇧R"), data: { "data-row-rename-action": "" } }) : null,
      properties],
    opens,
    pageActions(),
    [comment,
      pageItem.onSuggest ? item("suggest", t("blockHandle:menu.suggestEdits"), <SuggestEditIcon size="regular" aria-hidden="true" />,
        later(pageItem.onSuggest), { hint: menuKeyHint("⌘⇧⌥X"), data: { "data-page-suggest-action": "" } }) : null],
    [pageItem.onPresent ? item("present", t("blockHandle:menu.presentHere"), <PresentIcon size="regular" aria-hidden="true" />,
      later(pageItem.onPresent), { hint: menuKeyHint("⌘⌥P"), badge: t("blockHandle:menu.beta"), data: { "data-row-present-action": "" } }) : null],
  ] : [
    [favoriteLine, icon, properties],
    onViewSettings ? [
      // A card over a tilted one and an open eye, as Notion's (seen 2026-10-24).
      item("layout", t("databaseView:layout"), <CardLayoutIcon size="regular" aria-hidden="true" />, later(() => onViewSettings("layout")),
        { data: { "data-row-view-settings": "layout" } }),
      item("visibility", t("databaseView:propertyVisibility"), <EyeIcon size="regular" weight="regular" aria-hidden="true" />,
        later(() => onViewSettings("properties")), { data: { "data-row-view-settings": "properties" } }),
    ] : [],
    openInline ? opens : [
      onOpenMode ? besideRow("openIn", t("rowMenu:actions.openIn"), <OpenInNew size="regular" aria-hidden="true" />, false) : null,
      comment,
    ],
    pageActions(),
  ];
  // A divider between groups that show anything, none while searching.
  const shown = groups.map((group) => group.filter(Boolean)).filter((group) => group.length > 0);
  const items = shown.map((group, index) => (
    <Fragment key={index}>
      {index > 0 && !needle ? <NativeMenuSeparator className={styles.divider} /> : null}
      {group}
    </Fragment>
  ));
  const choose = (run: () => void) => () => { setSubmenu(null); onClose(); run(); };
  const list = submenu?.kind === "properties" && editProperties ? (
    <RowMenuBesideSubmenu row={submenu.row} width={220} label={t("rowMenu:actions.editProperties")} onClose={() => setSubmenu(null)}>
      {editProperties.properties.map((property) => (
        <button key={property.id} type="button" role="menuitem" className={styles.submenuItem} data-row-edit-property={property.id}
          onClick={choose(() => editProperties.onEdit(property.id))}>
          <PropertyIcon property={property} size="regular" />
          <span>{property.name}</span>
        </button>
      ))}
    </RowMenuBesideSubmenu>
  ) : submenu?.kind === "openIn" && onOpenMode ? (
    <RowMenuBesideSubmenu row={submenu.row} width={220} label={t("rowMenu:actions.openIn")} onClose={() => setSubmenu(null)}>
      <button type="button" role="menuitem" className={styles.submenuItem} data-row-open-in="new_tab" onClick={choose(() => onOpenMode("new_tab"))}>
        <OpenInNew size="regular" aria-hidden="true" />
        <span>{t("rowMenu:actions.newTab")}</span>
        <span className={styles.itemHint}>{menuKeyHint("⌘⇧↵")}</span>
      </button>
      {inSidePeek ? (
        <button type="button" role="menuitem" className={styles.submenuItem} data-row-open-in="full" onClick={choose(() => onOpenMode("full"))}>
          <FullPagePeekIcon size="regular" aria-hidden="true" />
          <span>{t("databaseView:peekModes.full")}</span>
        </button>
      ) : null}
      <button type="button" role="menuitem" className={styles.submenuItem} data-row-open-in="side" onClick={choose(() => onOpenMode("side"))}>
        <SidePeekIcon size="regular" aria-hidden="true" />
        <span>{t("databaseView:peekModes.side")}</span>
        <span className={styles.itemHint}>{menuKeyHint("⌥Click")}</span>
      </button>
    </RowMenuBesideSubmenu>
  ) : null;
  // The pointer in the list beside the menu holds it (useSubmenuHover's `hold`).
  const overlay = list ? <span className={styles.submenuHost} onPointerMove={hover.hold}>{list}</span> : null;
  return { items, overlay, onPointerMove: hover.onPointerMove };
}
