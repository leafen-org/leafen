import { useEffect, useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { DotsHorizontal, InfoIcon, Search } from "@/icons/leafen";
import { HoverTip } from "../IconTipButton";
import { popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { RelationDatabaseOption } from "@/lib/relationPropertyCreationModel";
import { useProductPages } from "@/components/product/productPagesContext";
import { databaseSourcePlace, type Place } from "@/lib/pagePath";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { PropertyIcon } from "./PropertyIcon";
import { PlaceLine } from "../PlaceLine";
import { DataSourceMark } from "./DataSourceMark";
import { DataSourceListRow } from "./DataSourceListRow";
import { EXISTING_COUNTED, useSourceListLimit } from "./linkableSources";
import { pageLayoutTabSourceGroups, useDatabaseOwnTabSources } from "./pageLayoutTabSources";
import { ViewTypeIcon } from "./databaseViewShared";
import { PageLayoutTabOptions, tabSourceName, type PageLayoutTabNames } from "./PageLayoutTabOptions";
import styles from "./PageLayoutAddMenu.module.css";
import phoneStyles from "./PhonePageLayoutBuilder.module.css";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { pagesByIdOf } from "@/lib/pageIndex";
import type { Session } from "@/types";

/**
 * The "+" beside a tabbed layout's tabs, as Notion's (seen 2026-10-12):
 * "관계 대상: 〈database〉" with the database's two-way relations (a one-way
 * one isn't offered — seen 2026-11-12, both kinds side by side), each
 * making a tab of the pages it relates to, then under a line
 * "기존 데이터 소스 연결하기" with the table's mark — a search
 * of the workspace's data sources ("데이터 소스 연결", a database of several's
 * own sources under "추천" first, then "기존 데이터 소스" with "N개 더 보기"
 * counting the rest of the hundred read, twenty more a press, a search the
 * hundred found and its first twenty — the lists' rule in
 * pageLayoutTabSourceGroups, seen 2026-11-18) whose choice asks
 * "보기 옵션" (PageLayoutTabOptions): the tab's name, and "새 관계형 추가"
 * or not. The database's own source carries "ⓘ" at its
 * row's end, "이 데이터베이스" over it; each source names where it is under
 * its name, as every list of sources to link (Notion's, seen 2026-11-12),
 * an untitled one as its database calls it (tabSourceName).
 * On a phone the same as half sheets over "N개 보기", the first "새로운 보기"
 * with the table's mark on "기존 데이터 소스 연결하기" (seen 2026-10-25 at 375px).
 */
export function PageLayoutTabMenu({ button, session, databasePageId, databaseTitle, ownSourceId, relations, searchSources, canRelate, onRelationTab, onNewRelation, onViewTab, onClose, phone = false }: {
  button: RefObject<HTMLButtonElement | null>;
  /** Who reads the database's page, and that page, where its own sources are read (useDatabaseOwnTabSources). */
  session?: Session;
  databasePageId?: string;
  databaseTitle: string;
  /** The data source the laid out pages are of. */
  ownSourceId?: string;
  relations: readonly GraphProperty[];
  searchSources?: (query: string, signal: AbortSignal, upTo?: number) => Promise<RelationDatabaseOption[]>;
  canRelate: boolean;
  onRelationTab(property: GraphProperty): void;
  /** A relation to the source and its tab: named here on a desktop, in the relation's own window on a phone. */
  onNewRelation(source: RelationDatabaseOption, names?: PageLayoutTabNames): Promise<void> | void;
  onViewTab(source: RelationDatabaseOption, name?: string): void;
  onClose(): void;
  phone?: boolean;
}) {
  const { t } = useTranslation(["pageLayout", "databaseView"]);
  const [mode, setMode] = useState<"main" | "pick" | RelationDatabaseOption>("main");
  const [query, setQuery] = useState("");
  const [found, setFound] = useState<RelationDatabaseOption[] | null>(null);
  const { limit, more } = useSourceListLimit(query);
  // Read as the menu opens, before its "기존 데이터 소스 연결하기" is pressed.
  const own = useDatabaseOwnTabSources(session, databasePageId, ownSourceId);
  // Each source names where it is, "Top / Page", as the relation window's list (databaseSourcePlace).
  const pages = useProductPages()?.pages;
  const pagesById = pagesByIdOf(pages ?? []);
  const placeOf = (source: RelationDatabaseOption): Place =>
    databaseSourcePlace(source.pageId, pagesById, source.sourceTitles, source.databaseTitle) ?? (source.where ? [source.where] : []);
  const menu = useRef<HTMLDivElement>(null);
  // Centred under the "+", as Notion's (seen 2026-11-12): 270 wide right under it, the search 290 wide 4 under it.
  const place = usePopoverPlacement(menu, phone ? null : () => ({
    anchor: popoverRectOf(button.current) ?? { top: 0, bottom: 0, left: 0, right: 0 },
    placement: "bottom", gap: mode === "main" ? 0 : 4, options: { cap: 480 }, minHeight: 160,
  }), [mode], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose, inside: () => [button.current] });
  // The hundred, searched or not; unsearched besides its own sources, which it reads too.
  const ownCount = own?.length;
  useEffect(() => {
    if (mode !== "pick" || !searchSources || ownCount === undefined) return undefined;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      searchSources(query.trim(), controller.signal, EXISTING_COUNTED + (query.trim() ? 0 : ownCount)).then((next) => { if (!controller.signal.aborted) setFound(next); }, () => undefined);
    }, 150);
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [mode, ownCount, query, searchSources]);
  const groups = pageLayoutTabSourceGroups({ own: own ?? [], found: found ?? [], ownSourceId, query, limit });
  const searched = setQuery;

  if (phone) {
    if (mode === "main") {
      return (
        <PhoneSheet title={t("newTabTitle")} size="half" onClose={onClose} data={{ "data-layout-tab-menu": "" }}>
          {relations.length > 0 ? (
            <PhoneSheetGroup label={t("relationsOf", { name: databaseTitle || untitledPageDisplayTitle("database") })}>
              {relations.map((property) => (
                <PhoneSheetRow key={property.id} icon={<PropertyIcon property={property} size="regular" />} label={property.name} more={false}
                  onPress={() => onRelationTab(property)} data={{ "data-layout-tab-relation": property.id }} />
              ))}
            </PhoneSheetGroup>
          ) : null}
          {searchSources ? (
            <PhoneSheetGroup>
              <PhoneSheetRow icon={<ViewTypeIcon type="table" size="regular" />} label={t("connectSource")} more={false}
                onPress={() => setMode("pick")} data={{ "data-layout-tab-connect": "" }} />
            </PhoneSheetGroup>
          ) : null}
        </PhoneSheet>
      );
    }
    if (mode === "pick") {
      const sourceRow = (source: RelationDatabaseOption) => (
        <PhoneSheetRow key={source.id} icon={<DataSourceMark source={source.mark ?? {}} />} label={tabSourceName(source)}
          description={placeOf(source).length ? <PlaceLine place={placeOf(source)} /> : undefined} more={false} onPress={() => setMode(source)} data={{ "data-layout-tab-source": source.id }} />
      );
      return (
        <PhoneSheet title="" label={t("connectSourceSearch")} size="half" onClose={onClose} data={{ "data-layout-tab-menu": "" }}>
          <PhoneSheetGroup>
            <label className={phoneStyles.searchRow}>
              <Search size="page" aria-hidden="true" />
              <input className={phoneStyles.search} value={query} placeholder={t("connectSourceSearch")} aria-label={t("connectSourceSearch")}
                autoFocus={fieldFocusOnOpen()} onChange={(event) => searched(event.target.value)} data-layout-tab-search />
            </label>
          </PhoneSheetGroup>
          {groups.suggested.length > 0 ? (
            <PhoneSheetGroup label={t("databaseView:suggestedDataSources")}>{groups.suggested.map(sourceRow)}</PhoneSheetGroup>
          ) : null}
          <PhoneSheetGroup label={t("existingSources")}>
            {groups.existing.map(sourceRow)}
            {groups.hidden > 0 ? (
              <PhoneSheetRow label={t("showMore", { count: groups.hidden })} more={false} muted onPress={more} />
            ) : null}
          </PhoneSheetGroup>
          {found && groups.existing.length === 0 ? <p className={phoneStyles.empty}>{t("noResults")}</p> : null}
        </PhoneSheet>
      );
    }
    const source = mode;
    return (
      <PhoneSheet title={t("viewOptions")} size="half" onClose={onClose} onBack={() => setMode("pick")} data={{ "data-layout-tab-menu": "" }}>
        <PhoneSheetGroup label={tabSourceName(source)}>
          {canRelate ? <PhoneSheetRow label={t("newRelation")} more={false} onPress={() => onNewRelation(source)} data={{ "data-layout-tab-new-relation": "" }} /> : null}
          <PhoneSheetRow label={t("addView")} more={false} onPress={() => onViewTab(source)} data={{ "data-layout-tab-add-view": "" }} />
        </PhoneSheetGroup>
      </PhoneSheet>
    );
  }
  let body;
  if (mode === "main") {
    body = <>
      {relations.length > 0 ? <div className={styles.label}>{t("relationsOf", { name: databaseTitle || untitledPageDisplayTitle("database") })}</div> : null}
      {relations.map((property) => (
        <button key={property.id} type="button" className={styles.row} onClick={() => onRelationTab(property)} data-layout-tab-relation={property.id}>
          <span className={styles.icon}><PropertyIcon property={property} size="regular" /></span>
          <span className={styles.text}><span>{property.name}</span></span>
        </button>
      ))}
      {relations.length > 0 && searchSources ? <div className={styles.divider} role="separator" /> : null}
      {searchSources ? (
        <button type="button" className={styles.row} onClick={() => setMode("pick")} data-layout-tab-connect>
          <span className={styles.icon}><ViewTypeIcon type="table" size="regular" /></span>
          <span className={styles.text}><span>{t("connectSource")}</span></span>
        </button>
      ) : null}
    </>;
  } else if (mode === "pick") {
    const sourceRow = (source: RelationDatabaseOption) => (
      <DataSourceListRow key={source.id} className={styles.sourceRow} onClick={() => setMode(source)} data-layout-tab-source={source.id}
        source={source.mark ?? {}} name={tabSourceName(source)} place={placeOf(source)}
        end={source.id === ownSourceId ? (
          <HoverTip label={t("pageLayout:thisDatabase")} className={styles.ownSource}>
            <InfoIcon size="small" aria-label={t("pageLayout:thisDatabase")} />
          </HoverTip>
        ) : null} />
    );
    body = <>
      <input className={styles.search} value={query} placeholder={t("connectSourceSearch")} aria-label={t("connectSourceSearch")}
        autoFocus={fieldFocusOnOpen()} onChange={(event) => searched(event.target.value)} data-layout-tab-search />
      {groups.suggested.length > 0 ? <>
        <div className={styles.label}>{t("databaseView:suggestedDataSources")}</div>
        {groups.suggested.map(sourceRow)}
      </> : null}
      <div className={styles.label}>{t("existingSources")}</div>
      {groups.existing.map(sourceRow)}
      {groups.hidden > 0 ? (
        <button type="button" className={styles.more} onClick={more}>
          <span className={styles.icon}><DotsHorizontal size="regular" aria-hidden="true" /></span>{t("showMore", { count: groups.hidden })}</button>
      ) : null}
      {found && groups.existing.length === 0 ? <div className={styles.empty}>{t("noResults")}</div> : null}
    </>;
  } else {
    const source = mode;
    body = <PageLayoutTabOptions key={source.id} source={source} databaseTitle={databaseTitle} own={source.id === ownSourceId}
      canRelate={canRelate} onBack={() => setMode("pick")}
      onAdd={(names, relate) => relate ? onNewRelation(source, names) : onViewTab(source, names.name)} />;
  }
  return createPortal(
    <div ref={menu} className={`${styles.menu} ${mode === "main" ? styles.tabMenu : mode === "pick" ? styles.sourceMenu : ""}`} role="dialog" aria-label={t("addTab")}
      style={popoverPlacementStyle(place)} data-layout-tab-menu>
      {body}
    </div>,
    document.body,
  );
}
