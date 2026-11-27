import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { DotsHorizontal, PinIcon } from "../icons";
import type { PageIconSource } from "@/components/page/PageIcon";
import type { DbProperty, Page } from "@/lib/types";
import { BoardGroupPreview, type BoardGroupPreviewFilters } from "./BoardGroupPreview";
import { chipStyle } from "./colors";
import { GroupPageGlyph, type GroupPage } from "./GroupPageGlyph";
import { statusGroupTitle } from "./StatusGroupLabel";
import type { PropertyValueReadContext } from "./PropValue";
import type { StatusStage } from "./propertyStatusPolicy";
import styles from "./databaseSheet";
import { GroupAddKeepsFocus } from "./ViewGroupAdd";

type SideGroup = {
  key: string; label: string; color?: string; checked?: boolean; page?: GroupPage; statusStage?: StatusStage; statusOption?: boolean; rows: readonly { id: string }[];
};

/** What a pinned group's table preview shows and does. */
export type BoardGroupPreviewSource = {
  properties: readonly DbProperty[];
  context: PropertyValueReadContext;
  /** The row's page, as the board's cards show it. */
  pagesById: Readonly<Record<string, Page>>;
  databaseTitle: string;
  databaseSourceId: string;
  /** Its database's own icon, the preview's "⋯" drawing it by its name. */
  databaseIcon?: PageIconSource;
  databaseHref?: string | null;
  /** The property the board groups by, its column right after the title's. */
  groupPropertyId?: string;
  /** Its filter bar's start and what it narrows. */
  filters?: BoardGroupPreviewFilters;
  onOpenRow(rowId: string): void;
};

/**
 * What waits after a board's columns, as Notion's (seen 2026-10-11): "+ 신규
 * 그룹" and, once any is pinned, "고정된 그룹" — each pinned group, shown or
 * hidden, with how many cards it holds and its pin to let it go; its chip
 * opens its rows in a table over the board. Notion's board lists no hidden
 * group otherwise.
 */
export function BoardSideGroups({ addGroup, pinned, onUnpin, onMoreGroups, preview }: {
  addGroup: ReactNode;
  /** "⋯ 그룹 더 불러오기" over "+ 신규 그룹" while groups are left past the board's ten at a time. */
  onMoreGroups?: () => void;
  pinned: readonly SideGroup[];
  onUnpin?(key: string): void;
  preview?: BoardGroupPreviewSource;
}) {
  const { t } = useTranslation(["databaseView", "propValue", "boardView"]);
  const [previewKey, setPreviewKey] = useState<string | null>(null);
  const previewed = preview && previewKey !== null ? pinned.find((group) => group.key === previewKey) : undefined;
  return (
    <div className={styles.boardSide} data-board-side>
      {onMoreGroups ? <button type="button" className={styles.boardAddGroup} data-board-more-groups onClick={onMoreGroups}>
        <DotsHorizontal size="small" aria-hidden="true" />{t("boardView:moreGroups")}</button> : null}
      {/* Its "+ 신규 그룹" takes no focus when pressed, as Notion's board (ViewGroupAdd). */}
      <GroupAddKeepsFocus.Provider value>{addGroup}</GroupAddKeepsFocus.Provider>
      {pinned.length > 0 && <>
        <div className={styles.boardPinnedTitle}><PinIcon size="small" aria-hidden="true" />{t("databaseView:pinnedGroups")}</div>
        <ul className={styles.boardPinnedGroups} aria-label={t("databaseView:pinnedGroups")} data-board-pinned-groups>
          {pinned.map((group) => {
            const name = group.checked === undefined ? group.label
              : `${group.label} ${t(group.checked ? "propValue:checkbox.checked" : "propValue:checkbox.unchecked")}`;
            return (
              <li key={group.key} className={styles.boardPinnedGroup} data-board-pinned-group={group.key}>
                <button type="button" className={styles.boardPinnedName} disabled={!preview} aria-haspopup="dialog"
                  data-board-pinned-open={group.key} onClick={() => setPreviewKey(group.key)}>
                  {group.checked !== undefined ? (
                    <span className={styles.cardCheckbox} data-checked={group.checked ? "true" : undefined} aria-hidden="true">
                    </span>
                  ) : null}
                  <GroupPageGlyph page={group.page} pixels={18} />
                  {statusGroupTitle(group, group.label)
                    ?? (group.color ? <span className={styles.chip} style={chipStyle(group.color)}><span className={styles.chipText}>{group.label}</span></span> : <span>{group.label}</span>)}
                  <span className={styles.boardPinnedCount}>{group.rows.length}</span>
                </button>
                {onUnpin && <button type="button" className={styles.boardPinnedUnpin} aria-label={t("databaseView:unpinGroup", { name })}
                  onClick={() => onUnpin(group.key)}>
                  <PinIcon size="small" weight="fill" aria-hidden="true" />
                </button>}
              </li>
            );
          })}
        </ul>
      </>}
      {previewed && preview ? <BoardGroupPreview key={previewed.key} group={{ ...previewed, rows: previewed.rows.flatMap((row) => preview.pagesById[row.id] ?? []) }}
        properties={preview.properties} groupPropertyId={preview.groupPropertyId} context={preview.context} databaseTitle={preview.databaseTitle} databaseSourceId={preview.databaseSourceId} databaseIcon={preview.databaseIcon} databaseHref={preview.databaseHref}
        filters={preview.filters} onOpenRow={preview.onOpenRow} onClose={() => setPreviewKey(null)} /> : null}
    </div>
  );
}
