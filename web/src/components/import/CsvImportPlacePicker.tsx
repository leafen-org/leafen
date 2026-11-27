import { CheckIcon, DatabaseCylinderIcon, LockIcon, Plus, TableIcon } from "@/icons/leafen";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { usePageVisits } from "@/components/library/useLibraryData";
import { pagePlace, placeUnder } from "@/lib/pagePath";
import { PlaceLine } from "@/components/PlaceLine";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import glyphStyles from "@/components/teamspace/TeamspaceGlyph.module.css";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { usePageNames } from "@/lib/pageNames";
import { PageTitleText } from "@/components/page/TitleWithCustomEmoji";
import { type ReactNode, type RefObject, useMemo, useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { POPOVER_MARGIN, placeAround, placeBeside, popoverAnchorOf, popoverPlacementStyle, usePopoverPlacement,
  type PopoverRect, type PopoverSize, type PopoverViewport } from "@/lib/popoverPlacement";

import type { Page } from "../../types";
import type { WorkspaceCSVMergeTargetSummary } from "../../workspaceImportWire.generated";
import styles from "./CsvImportPlacePicker.module.css";
import { usePathsAboveShown } from "@/lib/usePagePathsAbove";
import { touchScreen } from "@/lib/screenKind";
import { pagesByIdOf } from "@/lib/pageIndex";
import { pageTrashRoot } from "@/lib/pageTrash";
import { useImportPlaceDatabases, type ImportPlaceDatabaseSource } from "./useImportPlaceDatabases";
import { useImportPlaceSearch } from "./useImportPlaceSearch";

/**
 * Where a new database or page goes: the reader's private pages, under a
 * page, at a teamspace's top, or, for a file window's files, into a database
 * as its rows.
 */
export type CsvImportPlace =
  | { kind: "private" }
  | { kind: "page"; pageId: string }
  | { kind: "teamspace"; teamspaceId: string; label: string; icon?: string }
  | { kind: "database"; dataSourceId: string; label: string };

/** A teamspace the reader may add pages to. */
export type CsvImportTeamspace = { teamspaceId: string; label: string; icon?: string };

/** The most pages or databases the list shows, searched or not, as Notion's (seen 2026-11-15: 5 pages, 5 databases); a search's pages visited go before its five found (useImportPlaceSearch). */
const GROUP_LIMIT = 5;

/**
 * What a database is called in an import window's lists: its name, or while
 * it has none what an untitled database is called, "새 데이터베이스", as
 * Notion's "위치 가져오기" and "데이터베이스 선택" name one (seen 2026-11-19 in
 * two Chrome tabs, its table icon beside it; its sidebar and path too).
 */
export function importDatabaseLabel(title: string) {
  return title.trim() || untitledPageDisplayTitle("database");
}

/** The key of the row that makes a new teamspace. */
const NEW_TEAMSPACE = "new-teamspace";

/**
 * The pages the list may offer: not a database's, not in the trash, itself
 * or under a page that is (pageTrashRoot) — an import refuses a place in
 * the trash, as the page it adds to (2026-11-20: a page under a trashed
 * page was offered and its import refused).
 */
function listedAmong(pagesById: Readonly<Record<string, Page>>) {
  const pageById = (id: string) => pagesById[id];
  return (page: Page | undefined): page is Page =>
    page !== undefined && page.kind !== "database" && !pageTrashRoot(page, pageById);
}

/**
 * A picker an import window's field opens, as Notion's "위치 가져오기" and
 * "데이터베이스 선택" (seen 2026-11-08 at 1303×873): over the window rather
 * than inside its scroll, as wide as the field, under it where it has room
 * and over it where it hasn't, `gap` from it, moved and cut to stay on the
 * screen. Its list ends its own scroll at either end, not the window's
 * under it.
 */
function FieldPicker({ popoverRef: ref, field, gap, height, label, data, onClose, children }: {
  popoverRef: RefObject<HTMLDivElement | null>;
  field: HTMLElement;
  gap: number;
  /** Its height while the screen has room for it; drawn to its rows otherwise. */
  height?: number;
  label: string;
  data: Record<`data-${string}`, string>;
  onClose(): void;
  children: ReactNode;
}) {
  const layer = useOverlayLayer(true, { kind: "picker", ref, onEscape: onClose, onOutsidePress: onClose });
  const placement = usePopoverPlacement(ref, (measured, viewport) => placeAround(popoverAnchorOf(field), measured, viewport, { gap }),
    [field, gap], undefined, false);
  return createPortal(
    <div ref={ref} className={styles.popover} role="dialog" aria-label={label} {...data} onKeyDown={layer.onKeyDown}
      style={{ ...popoverPlacementStyle(placement), width: field.getBoundingClientRect().width, height, zIndex: layer.zIndex }}>
      {children}
    </div>,
    document.body,
  );
}

/**
 * Where a CSV column's type menu opens, as Notion's (seen 2026-11-08 at
 * 1303×873): beside the type box, its top on the box's, moved up to stay on
 * the screen; with room for it on neither side (a phone), under the box, or
 * over it where the screen has no room under it.
 */
export function placeBesideField(anchor: PopoverRect, measured: PopoverSize, viewport: PopoverViewport) {
  const room = Math.max(anchor.left, viewport.width - anchor.right) - POPOVER_MARGIN;
  return room >= measured.width ? placeBeside(anchor, measured, viewport)
    : placeAround(anchor, measured, viewport, { gap: 4, align: "end" });
}

/** Whether a listed place is the one chosen now. */
function samePlace(place: CsvImportPlace, current: CsvImportPlace | undefined) {
  if (!current || current.kind !== place.kind) return false;
  if (place.kind === "page") return current.kind === "page" && current.pageId === place.pageId;
  if (place.kind === "teamspace") return current.kind === "teamspace" && current.teamspaceId === place.teamspaceId;
  if (place.kind === "database") return current.kind === "database" && current.dataSourceId === place.dataSourceId;
  return true;
}

/** The mark at the end of the place chosen now, as Notion's: a 16px check, its row unlit (seen 2026-11-15). */
function ChosenMark({ place, current }: { place: CsvImportPlace; current?: CsvImportPlace }) {
  return samePlace(place, current) ? <CheckIcon size="small" aria-hidden="true" className={styles.chosen} /> : null;
}

/**
 * The place a new database or page goes, as Notion's "위치 가져오기" lists it
 * (seen 2026-11-08 and 2026-11-15): the field's width and 400px high, 6px
 * from it, with "페이지 검색…" in the menus' search field, ringed blue while
 * it has the keys, "개인 페이지", under "페이지" the pages found, each with
 * where it sits, and under "팀스페이스" the teamspaces; the place chosen now
 * checked at its end. The pages and the databases list up to five each:
 * before a search the pages the reader visited last, newest first, the open
 * one too, as the sidebar's recent pages (seen 2026-11-19 in two Chrome tabs);
 * searched, those of them it names, then five the workspace's search finds
 * (useImportPlaceSearch), "개인 페이지" kept over them. With `onNewTeamspace`
 * "팀스페이스" ends in "+ 새 팀스페이스", searched or not, its head kept for it
 * when no teamspace's name holds the search (seen 2026-11-19 in two Chrome
 * tabs); a teamspace's mark is the sidebar's, its icon or its name's first
 * letter. A page's row is a page picker's,
 * one line or two with its place (`data-placed`, the CSS). A file window's list (`databases`) searches "페이지 또는
 * 데이터베이스" and has "데이터베이스" after the pages, whose rows its files
 * become, as Notion's Text & Markdown, HTML, PDF, Word and Zip windows (seen
 * 2026-11-15; its CSV window lists no databases). A search that finds no
 * page keeps "페이지" as it was before the search, and one that finds no
 * database "데이터베이스" (useImportPlaceDatabases), as Notion's ("Qzxvw",
 * "HQ", "abc def", 2026-11-19); the teamspaces stay filtered. The window
 * keeps the words searched (`query`): closed by Esc, a press outside it or
 * "+ 새 팀스페이스", the list opens again on them, the caret at their end;
 * a place chosen clears them, as Notion's (seen 2026-11-19), and so does a
 * teamspace made from "+ 새 팀스페이스", which becomes the place.
 */
export function CsvLocationPicker({ pages, reader, teamspaces = [], databases: databaseSource, current, field, query, onQuery,
  onChoose, onNewTeamspace, onClose }: {
  pages: readonly Page[];
  /** Whose visits order the pages before a search. */
  reader: ImportPlaceDatabaseSource;
  teamspaces?: readonly CsvImportTeamspace[];
  /** Lists the databases the reader may add rows to, read from here. */
  databases?: ImportPlaceDatabaseSource;
  /** The place chosen now. */
  current?: CsvImportPlace;
  /** The field that opened it. */
  field: HTMLElement;
  /** The words searched, which the window keeps while the list is closed. */
  query: string;
  onQuery(query: string): void;
  onChoose(place: CsvImportPlace): void;
  /** Opens the window that makes a teamspace, for a reader who may make one. */
  onNewTeamspace?(): void;
  onClose(): void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["importDialog", "moveToDialog", "common"]);
  const ref = useRef<HTMLDivElement>(null), inputRef = useRef<HTMLInputElement>(null), listRef = useRef<HTMLDivElement>(null);
  const search = t(databaseSource ? "importDialog:csvWindow.searchPagesOrDatabases" : "importDialog:csvWindow.searchPages");
  const pagesById = pagesByIdOf(pages);
  const listed = listedAmong(pagesById);
  const visits = usePageVisits(reader.session, reader.workspaceId);
  const visited = visits.map((visit) => pagesById[visit.pageId]).filter(listed);
  const searched = useImportPlaceSearch(reader, query, visited, listed, GROUP_LIMIT);
  const found = searched.length ? searched : visited.slice(0, GROUP_LIMIT);
  const databases = useImportPlaceDatabases(databaseSource, query).databases.slice(0, GROUP_LIMIT);
  usePathsAboveShown(found, pagesById);
  const foundTeamspaces = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase();
    return teamspaces.filter((teamspace) => !needle || teamspace.label.toLocaleLowerCase().includes(needle));
  }, [query, teamspaces]);
  const choices: Array<{ id: string; label: string; place?: CsvImportPlace }> = [
    { id: "private", label: t("importDialog:csvWindow.privatePage"), place: { kind: "private" } as const },
    ...found.map(page => ({ id: `page:${page.id}`, label: pageNames.title(page), place: { kind: "page", pageId: page.id } as const })),
    ...databases.map(source => ({ id: `database:${source.dataSourceId}`, label: importDatabaseLabel(source.title),
      place: { kind: "database", dataSourceId: source.dataSourceId, label: importDatabaseLabel(source.title) } as const })),
    ...foundTeamspaces.map(space => ({ id: `teamspace:${space.teamspaceId}`, label: space.label,
      place: { kind: "teamspace", teamspaceId: space.teamspaceId, label: space.label, icon: space.icon } as const })),
    ...(onNewTeamspace ? [{ id: NEW_TEAMSPACE, label: t("importDialog:csvWindow.newTeamspace") }] : []),
  ];
  const choose = (place: CsvImportPlace) => {
    onQuery("");
    onChoose(place);
  };
  const picker = useNativeAutocomplete({ items: choices, query, onQuery, inputRef, listRef, popoverRef: ref,
    keyOf: item => item.id, textOf: item => item.label, onPick: item => item.place ? choose(item.place) : onNewTeamspace?.(), onClose,
    label: search, commitTab: false,
    // Opened, no place is lit until the arrows light one, as Notion's (seen
    // 2026-11-14); Enter with none lit chooses nothing.
    highlightFirst: false, onEnterNone: (event) => event.preventDefault() });
  return (
    <FieldPicker popoverRef={ref} field={field} gap={6} height={400} label={t("importDialog:csvWindow.location")}
      data={{ "data-csv-location-picker": "true" }} onClose={onClose}>
      {/* A touch screen's picker leaves its search to a tap: focused as it
          opened, the soft keyboard covered most of the list (2026-11-08). */}
      <label className={styles.search}>
        <input {...picker.inputProps} ref={inputRef} autoFocus={!touchScreen()} placeholder={search} />
      </label>
      <div {...picker.listBoxProps} ref={listRef} className={styles.popoverList}>
        {/* "개인 페이지" stays first while searching, as Notion's (seen 2026-11-19 in two Chrome tabs). */}
        <NativePickerOption state={picker.state} itemKey="private" className={styles.popoverItem}>
          {({ labelProps }) => <>
          <LockIcon size="regular" aria-hidden="true" />
          <span><span {...labelProps}>{t("importDialog:csvWindow.privatePage")}</span></span>
          <ChosenMark place={{ kind: "private" }} current={current} />
          </>}
        </NativePickerOption>
        <div className={styles.popoverGroup}>{t("importDialog:csvWindow.pages")}</div>
        {found.map((page) => {
          const location = pagePlace(page, pagesById);
          return (
            <NativePickerOption key={page.id} state={picker.state} itemKey={`page:${page.id}`} className={styles.popoverItem}
              data-csv-location={page.id} data-placed={location.length ? "true" : undefined}>{({ labelProps, placeProps }) => <>
              <span className={styles.glyph} aria-hidden="true"><PageIconGlyph page={page} size={18} /></span>
              <span>
                <span {...labelProps}><PageTitleText page={page} /></span>
                {location.length ? <small {...placeProps}><PlaceLine place={location} /></small> : null}
              </span>
              <ChosenMark place={{ kind: "page", pageId: page.id }} current={current} />
            </>}</NativePickerOption>
          );
        })}
        {found.length === 0 ? <div className={styles.popoverEmpty}>{t("importDialog:csvWindow.noPages")}</div> : null}
        {databases.length > 0 ? <div className={styles.popoverGroup}>{t("importDialog:csvWindow.databases")}</div> : null}
        {databases.map((source) => {
          // Where it sits, as Notion's list names it: an inline database's
          // page holding it, a full-page database's own place; with neither
          // among the reader's pages, the page holding it unless that is
          // the database itself, whose name its place would only repeat.
          const holder = pagesById[source.pageId];
          const location = holder ? (holder.kind === "database" ? pagePlace(holder, pagesById) : placeUnder(holder.id, pagesById))
            : source.pageTitle && source.pageTitle !== source.title ? [source.pageTitle] : [];
          const label = importDatabaseLabel(source.title);
          return (
            <NativePickerOption key={source.dataSourceId} state={picker.state} itemKey={`database:${source.dataSourceId}`} className={styles.popoverItem}
              data-csv-location-database={source.dataSourceId} data-placed={location.length ? "true" : undefined}>{({ labelProps, placeProps }) => <>
              <span className={styles.glyph} aria-hidden="true">{holder?.kind === "database" && holder.icon && holder.iconType === "emoji"
                ? holder.icon : <TableIcon size="regular" />}</span>
              <span>
                <span {...labelProps}>{label}</span>
                {location.length ? <small {...placeProps}><PlaceLine place={location} /></small> : null}
              </span>
              <ChosenMark place={{ kind: "database", dataSourceId: source.dataSourceId, label }} current={current} />
            </>}</NativePickerOption>
          );
        })}
        {foundTeamspaces.length > 0 || onNewTeamspace ? <div className={styles.popoverGroup}>{t("importDialog:csvWindow.teamspaces")}</div> : null}
        {foundTeamspaces.map((teamspace) => (
          <NativePickerOption key={teamspace.teamspaceId} state={picker.state} itemKey={`teamspace:${teamspace.teamspaceId}`}
            className={styles.popoverItem} data-csv-location-teamspace={teamspace.teamspaceId}>
            {({ labelProps }) => <>
            <span aria-hidden="true" className={`${glyphStyles.glyph} ${styles.teamspaceMark}`}>{teamspaceGlyph({ icon: teamspace.icon ?? "", name: teamspace.label })}</span>
            <span><span {...labelProps}>{teamspace.label}</span></span>
            <ChosenMark place={{ kind: "teamspace", teamspaceId: teamspace.teamspaceId, label: teamspace.label }} current={current} />
            </>}
          </NativePickerOption>
        ))}
        {/* Notion's "+ 새 팀스페이스": a 16px plus, gray, centered where a row's icon stands (seen 2026-11-19). */}
        {onNewTeamspace ? (
          <NativePickerOption state={picker.state} itemKey={NEW_TEAMSPACE} className={styles.popoverItem} data-csv-location-new-teamspace="true">
            {({ labelProps }) => <>
            <span aria-hidden="true" className={styles.newMark}><Plus size="small" /></span>
            <span><span {...labelProps}>{t("importDialog:csvWindow.newTeamspace")}</span></span>
            </>}
          </NativePickerOption>
        ) : null}
      </div>
    </FieldPicker>
  );
}

/**
 * The database rows join, as Notion's "데이터베이스 선택" lists them (seen
 * 2026-11-08): the field's width, 4px from it, its list of databases, each
 * with where it sits, as long as its rows up to 300px.
 */
export function CsvDatabasePicker({ sources, query, field, onQuery, onChoose, onClose }: {
  sources: readonly WorkspaceCSVMergeTargetSummary[] | null;
  query: string;
  /** The field that opened it. */
  field: HTMLElement;
  onQuery(query: string): void;
  onChoose(source: WorkspaceCSVMergeTargetSummary): void;
  onClose(): void;
}) {
  const { t } = useTranslation(["importDialog", "common"]);
  const ref = useRef<HTMLDivElement>(null), inputRef = useRef<HTMLInputElement>(null), listRef = useRef<HTMLDivElement>(null);
  const picker = useNativeAutocomplete({ items: [...(sources ?? [])], query, onQuery, inputRef, listRef, popoverRef: ref,
    keyOf: source => source.dataSourceId, textOf: source => importDatabaseLabel(source.title), onPick: onChoose,
    onClose, label: t("importDialog:csvMerge.searchDatabase"), commitTab: false });
  return (
    <FieldPicker popoverRef={ref} field={field} gap={4} label={t("importDialog:csvMerge.target")}
      data={{ "data-csv-database-picker": "true" }} onClose={onClose}>
      <label className={styles.search}>
        <input {...picker.inputProps} ref={inputRef} autoFocus={!touchScreen()} placeholder={t("importDialog:csvMerge.searchDatabase")} />
      </label>
      <div {...picker.listBoxProps} ref={listRef} className={`${styles.popoverList} ${styles.databaseList}`}>
        {sources?.map((source) => (
          <NativePickerOption key={source.dataSourceId} state={picker.state} itemKey={source.dataSourceId}
            className={styles.popoverItem} data-csv-merge-source={source.dataSourceId}>
            {({ labelProps, descriptionProps }) => <>
            <DatabaseCylinderIcon size="regular" aria-hidden="true" />
            <span>
              <span {...labelProps}>{importDatabaseLabel(source.title)}</span>
              {source.pageTitle && source.pageTitle !== source.title ? <small {...descriptionProps}>{source.pageTitle}</small> : null}
            </span>
            </>}
          </NativePickerOption>
        ))}
        {sources?.length === 0 ? <div className={styles.popoverEmpty}>{t("importDialog:csvMerge.noDatabases")}</div> : null}
      </div>
    </FieldPicker>
  );
}
