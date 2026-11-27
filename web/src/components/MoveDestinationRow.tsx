import { type CSSProperties, type ReactNode, type RefObject } from "react";
import { useEdgeFade } from "@/lib/edgeFade";
import type { ListState } from "react-stately";
import { NativePickerOption } from "@/lib/nativeAutocomplete";
import { useTranslation } from "react-i18next";
import { ChevronRight, Home, LockIcon } from "@/icons/leafen";
import { PageIconGlyph } from "./page/PageIcon";
import { PlaceLine } from "./PlaceLine";
import { MiddleTitle } from "./MiddleTitle";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import type { MoveDestination, MoveGroup, MoveListRow } from "./pageMoveDialogModel";
import type { MoveListDatabaseRow } from "./moveSearchOrder";
import styles from "./MoveToDialog.module.css";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import { keysFocusHolder } from "@/lib/lastPress";

// A row whose place the keys' focus is on draws its ground (data-keys-within).
keysFocusHolder(styles.treeRow, { part: styles.result });

/**
 * One place in the move list. Among the suggestions it has a toggle that
 * opens the pages under it, each level 14px in, and shows its title alone,
 * as Notion's (seen 2026-10-01); a search result also says where it is,
 * draws a page's icon at 18px in place of 16, and a database with no icon of
 * its own as a 20px cylinder in place of the suggestions' 18px table (seen
 * 2026-11-11), and a long title cut in its middle (MiddleTitle), where the
 * suggestions cut it at its end (seen 2026-11-12).
 * The private pages show the person's own mark and a lock after their name,
 * as Notion's (seen 2026-10-31), a teamspace its mark as the sidebar draws it;
 * neither says where it is once searched (seen 2026-11-13). A full-page
 * database opens to nothing: its row keeps the toggle's room without one
 * (seen 2026-11-10).
 */
export function MoveDestinationRow<T>({
  destination, state, itemKey, depth = 0, expanded = false, onToggle, owner,
}: {
  /** The reader, whose mark the private pages show. */
  owner?: { id: string; name: string };
  destination: MoveDestination;
  state: ListState<T>; itemKey: string;
  depth?: number;
  expanded?: boolean;
  /** Opens or closes the pages under it; only suggestions have it. */
  onToggle?: () => void;
}) {
  const { t } = useTranslation("moveToDialog");
  // A search's page draws its place a title a piece (PlaceLine), named with
  // its title as Notion's row is (rowNameProps).
  const place = onToggle || destination.type !== "page" || !destination.where.length ? undefined : destination.where;
  const option = (
    <NativePickerOption state={state} itemKey={itemKey} className={styles.result}
      data-move-place={destination.type === "place" ? destination.place.teamspaceId || "private" : undefined}
      data-move-destination={destination.id} data-disabled={destination.disabled ? "true" : undefined} disabled={destination.disabled}
      data-placed={place || destination.disabledReason ? "true" : undefined}>
      {({ labelProps, placeProps, descriptionProps }) => <>
      <span className={styles.icon}>
        {destination.type === "page"
          ? <PageIconGlyph page={destination.page} size={onToggle ? 16 : 18} database={onToggle ? "table" : "cylinder"} />
          : destination.place.teamspaceId
            ? <MoveTeamspaceGlyph place={destination.place} />
            : owner ? <PersonAvatar userId={owner.id} name={owner.name} size={16} /> : <Home size="regular" aria-hidden="true" />}
      </span>
      <span className={styles.text}>
        <span {...labelProps}>{onToggle || destination.type !== "page" ? destination.label : <MiddleTitle title={destination.label} />}{destination.type === "place" && !destination.place.teamspaceId
          ? <LockIcon className={styles.privateLock} size="small" aria-hidden="true" /> : null}</span>
        {destination.disabledReason ? <span {...descriptionProps}>{destination.disabledReason}</span>
          : place ? <span {...placeProps}><PlaceLine place={place} /></span> : null}
      </span>
      {destination.disabledReason && <LockIcon className={styles.lock} size="tiny" aria-hidden="true" />}
      </>}
    </NativePickerOption>
  );
  if (!onToggle) return option;
  const database = destination.type === "page" && destination.page.kind === "database";
  return (
    <div className={styles.treeRow} role="presentation" style={depth ? { "--move-depth": depth } as CSSProperties : undefined}>
      {database ? <span className={styles.treeToggle} aria-hidden="true" /> : <button
        type="button"
        className={styles.treeToggle}
        aria-label={t(expanded ? "moveToDialog:tree.collapse" : "moveToDialog:tree.expand")}
        aria-expanded={expanded}
        data-move-tree-toggle={destination.id}
        data-expanded={expanded ? "true" : undefined}
        onClick={onToggle}
      >
        <ChevronRight size="micro" aria-hidden="true" />
      </button>}
      {option}
    </div>
  );
}

/** A teamspace's mark in a move list, as the sidebar draws it: its icon, or its name's first letter. */
export function MoveTeamspaceGlyph({ place }: { place: { icon?: string; label: string } }) {
  return <span className={styles.teamspaceGlyph} aria-hidden="true">{teamspaceGlyph({ icon: place.icon ?? "", name: place.label })}</span>;
}

/**
 * The move list's rows (moveListRows), a page's window's and a block's
 * alike: the suggestions with their toggles, or a search's results, which
 * have none (onToggle left out), the databases it found among its pages
 * (rankedAmongPages, drawn by `database`); each group under its heading, as
 * Notion's (seen 2026-11-13: "추천", then "팀스페이스"; searched,
 * "팀스페이스" over "페이지").
 */
export function MoveListRows<T, D = never>({ rows, state, owner, onToggle, heading, database }: {
  rows: readonly (MoveListRow | MoveListDatabaseRow<D>)[];
  state: ListState<T>;
  owner?: { id: string; name: string };
  onToggle?: (key: string) => void;
  /** A group's heading. */
  heading: (group: MoveGroup) => string;
  /** A database the search found, among the pages. */
  database?: (row: MoveListDatabaseRow<D>) => ReactNode;
}) {
  return rows.flatMap((row, index) => {
    const item = row.kind === "database" ? database?.(row) ?? null : row.kind === "empty" ? <MoveTreeEmptyRow key={row.key} depth={row.depth} /> : (
      <MoveDestinationRow key={row.key} owner={owner} destination={row.destination} state={state} itemKey={row.key}
        depth={row.depth} expanded={row.expanded}
        onToggle={onToggle ? () => onToggle(row.key) : undefined} />
    );
    return index > 0 && rows[index - 1].group === row.group ? [item]
      : [<MoveGroupHeading key={`group:${row.group}`} group={row.group}>{heading(row.group)}</MoveGroupHeading>, item];
  });
}

/**
 * How the keys go through a move list, a page's window and a block's alike:
 * a dimmed row — the moving page and the pages under it, a locked page, a
 * database a database can't go into — takes the arrows and a search's first
 * place as any row does, lit by nothing, and Enter there does nothing; the
 * pointer lights it never. As Notion's (seen 2026-11-18 in Chrome at 1440,
 * judged by the screen after each key with no script before it, in two tabs
 * by "•••" › "옮기기": searched "W1851" while "W1851 Mover" moves, its row
 * first and dimmed, nothing lit, Enter left the window as it was, ↓ lit
 * "W1851 Other", ↓↓ over the two dimmed pages under the moving one lit
 * nothing, ↓ lit the next; by ⌘⇧P nothing lit too; a block's window the
 * same with a locked page first, ↓ then ↑ back on it lighting nothing).
 */
export const MOVE_LIST_KEYS = { dimmedRowsTakeKeys: true } as const;

/** A group's heading in a move list (MoveListRows). */
export function MoveGroupHeading({ group, children }: { group: MoveGroup; children: string }) {
  return <div className={styles.groupHeading} aria-hidden="true" data-move-group={group}>{children}</div>;
}

/**
 * Under an opened place with no pages, as Notion's "하위 페이지 없음": its
 * words 4px past where the opened row's mark starts (seen 2026-11-10).
 */
export function MoveTreeEmptyRow({ depth }: { depth: number }) {
  const { t } = useTranslation("moveToDialog");
  return (
    <div className={styles.treeEmpty} role="presentation" style={{ paddingInlineStart: depth * 14 + 24 }} data-move-tree-empty>
      {t("moveToDialog:tree.empty")}
    </div>
  );
}

/**
 * The move list's fade at the edges it runs on past, as Notion's list fades
 * there (seen 2026-11-10): the one rule every scrolling menu list follows
 * (lib/edgeFade.ts), measured again as what it shows (contentKey) changes.
 */
export function useMoveListEdges(listRef: RefObject<HTMLElement | null>, contentKey: unknown) {
  useEdgeFade(listRef, contentKey);
  return { "data-edge-fade": "" };
}
