import { type CSSProperties, type ReactNode, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { DatabaseRowMoveDrop, DatabaseRowMoveTarget } from "@/components/database/DatabaseRowMenu";
import { DatabaseRowMoveSubmenu } from "@/components/database/DatabaseRowMoveSubmenu";
import { DatabaseRowMovePopover, databaseRowMoveAnchor, type DatabaseRowMoveSubject } from "@/components/database/DatabaseRowMovePopover";
import type { MovePopoverAnchor } from "@/components/movePopoverPlacement";
import type { DatabaseRowMoveTargetSearch } from "@/components/database/useDatabaseRowMoveTargets";
import styles from "@/components/RowMenu.module.css";
import { RowMenuEditedFoot } from "@/components/RowMenuEditedFoot";
import type { PageLastEditVia, EditedFootActivity } from "@/lib/pageEditedActivity";
import { ComposeIcon, Copy, LinkIcon, OpenInNew, SidePeekIcon, Star, StarFilled, Trash } from "@/icons/leafen";
import { placeAtPoint, popoverBackdropStyle, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { accessHeldStyle } from "@/lib/pageActionAccessContext";
import { Separator } from "react-aria-components";
import { menuKeyHint } from "@/lib/menuKeyHint";

/**
 * A view section's row's "삭제, 복제 등…", as Notion's (seen 2026-10-03):
 * where its button is, a sidebar page's 265px menu headed "페이지" —
 * favorite; copy link, duplicate, rename, move, trash; open in a new tab or
 * the side peek — over who edited the row last and when. It has no "다음에서
 * 열기", which the database's own row menu has.
 */
export function SidebarViewRowMenu({
  anchor,
  busy,
  held = false,
  edited,
  favorite,
  moveTargets,
  readOnly,
  searchMoveTargets,
  title,
  onClose,
  onCopyLink,
  onDuplicate,
  onFavorite,
  onMove,
  onOpenInNewTab,
  onOpenInSidePeek,
  onPreviewMove,
  moveSubject,
  onMoved,
  onRename,
  onTrash,
}: {
  anchor: { x: number; y: number };
  busy: boolean;
  /** Waiting for what its rows follow: open and placed as it stands, unseen and out of the pointer's way (useAccessHold). */
  held?: boolean;
  /** Who edited the row last, by name, and when, and its activity; unknown until its page is read. */
  edited: { name: string; at: string; editorId?: string | null; via?: PageLastEditVia; activity?: EditedFootActivity } | null;
  favorite: boolean;
  moveTargets: DatabaseRowMoveTarget[];
  readOnly: boolean;
  searchMoveTargets?: DatabaseRowMoveTargetSearch;
  title: string;
  onClose: () => void;
  onCopyLink: () => void;
  onDuplicate: () => void;
  onFavorite: (favorite: boolean) => void;
  onMove: (target: DatabaseRowMoveTarget, acceptDroppedPropertyIds: string[]) => Promise<void>;
  onOpenInNewTab: () => void;
  onOpenInSidePeek: () => void;
  onPreviewMove?: (target: DatabaseRowMoveTarget) => Promise<DatabaseRowMoveDrop[]>;
  /** The row's page and data source, which "옮기기" moves through the pages' move popover, as Notion's. */
  moveSubject?: DatabaseRowMoveSubject;
  /** The section reads its rows again once the row moved or came back. */
  onMoved?: () => void;
  onRename: () => void;
  onTrash: () => void;
}) {
  const { t } = useTranslation("rowMenu");
  const menuRef = useRef<HTMLDivElement>(null);
  const [moveAt, setMoveAt] = useState<MovePopoverAnchor | null | undefined>(undefined);
  const place = usePopoverPlacement(menuRef, moveAt !== undefined ? null : (size, viewport) => placeAtPoint(anchor, size, viewport), [anchor], undefined,
    { kind: "menu", onEscape: onClose, onOutsidePress: onClose });
  const menuStyle = {
    "--menu-x": `${anchor.x}px`,
    "--menu-y": `${anchor.y}px`,
    ...(place ? popoverPlacementStyle(place) : {}),
    ...(held ? accessHeldStyle : {}),
  } as CSSProperties;

  const item = (key: string, icon: ReactNode, label: string, onClick: () => void, hint?: string) => (
    <NativeActionMenuItem key={key} textValue={label} className={styles.item} data-menu-item
      data-sidebar-view-row-action={key} role="menuitem" disabled={busy} onClick={onClick}>
      {icon}
      <span>{label}</span>
      {hint ? <span className={styles.itemHint}>{hint}</span> : null}
    </NativeActionMenuItem>
  );
  if (moveAt !== undefined && moveSubject) {
    return <DatabaseRowMovePopover subject={moveSubject} anchor={moveAt} onClose={onClose} onListChanged={onMoved} />;
  }
  const surface = (
    <>
      <button type="button" className={styles.backdrop} style={popoverBackdropStyle(place)} tabIndex={-1}
        aria-label={t("aria.closePageActions")} onClick={onClose} />
      <div ref={menuRef} className={`${styles.menu} ${styles.contextMenu}`} style={menuStyle}
        data-row-menu-variant="sidebar" data-sidebar-view-row-menu onClick={(event) => event.stopPropagation()}>
        <div className={styles.sectionLabel}>{t("sections.page")}</div>
        <NativeActionMenu aria-label={t("aria.pageActionsFor", { title })} autoFocus>
        {item("favorite", favorite ? <StarFilled size="regular" aria-hidden="true" /> : <Star size="regular" aria-hidden="true" />,
          favorite ? t("actions.removeFromFavorites") : t("actions.addToFavorites"), () => onFavorite(!favorite))}
        <Separator className={styles.divider} />
        {item("copy-link", <LinkIcon size="regular" aria-hidden="true" />, t("actions.copyLink"), onCopyLink)}
        {readOnly ? null : (
          <>
            {item("duplicate", <Copy size="regular" aria-hidden="true" />, t("actions.duplicate"), onDuplicate, menuKeyHint("⌘D"))}
            {item("rename", <ComposeIcon size="regular" aria-hidden="true" />, t("actions.rename"), onRename, menuKeyHint("⌘⇧R"))}
            <DatabaseRowMoveSubmenu busy={busy} disabled={false} hint={menuKeyHint("⌘⇧P")} moveTargets={moveTargets}
              searchMoveTargets={searchMoveTargets} onMove={onMove} onPreviewMove={onPreviewMove}
              onOpen={moveSubject ? () => setMoveAt(databaseRowMoveAnchor(menuRef.current)) : undefined} />
            {item("trash", <Trash size="regular" aria-hidden="true" />, t("actions.moveToTrash"), onTrash)}
          </>
        )}
        <Separator className={styles.divider} />
        {item("new-tab", <OpenInNew size="regular" aria-hidden="true" />, t("actions.openInNewTab"), onOpenInNewTab, menuKeyHint("⌘⇧↵"))}
        {item("side-peek", <SidePeekIcon size="regular" aria-hidden="true" />, t("actions.openInSidePeek"), onOpenInSidePeek, menuKeyHint("⌥Click"))}
        </NativeActionMenu>
        {edited ? <RowMenuEditedFoot name={edited.name} editorId={edited.editorId} via={edited.via} at={edited.at} activity={edited.activity}
          marker="sidebar-view-row-edited" /> : null}
      </div>
    </>
  );
  return createPortal(surface, document.body);
}
