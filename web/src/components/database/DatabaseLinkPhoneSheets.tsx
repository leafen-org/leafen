import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphView } from "@/contentGraph";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { foundDatabaseSourcePlace } from "@/lib/pagePath";
import { DotsHorizontal, Plus } from "@/icons/leafen";
import { useProductPages } from "@/components/product/productPagesContext";
import type { EditorDatabaseStartActions } from "@/components/editor/editorRuntimeTypes";
import { dataSourceDisplayTitle, inlineDatabasePlaceholderTitle } from "@/components/editor/databaseTitles";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { ViewIcon, viewDisplayName } from "./databaseViewShared";
import { PlaceLine } from "../PlaceLine";
import { DataSourceMark } from "./DataSourceMark";
import styles from "./DatabaseLinkPhoneSheets.module.css";
import { SearchClearButton } from "@/components/PageActionSearch";
import { EXISTING_COUNTED, existingSourcesShown, LINK_FIRST_SHOWN, LINK_MOST_SHOWN, useLinkableSources, useSourceListLimit } from "./linkableSources";
import { pagesByIdOf } from "@/lib/pageIndex";

/**
 * A waiting linked view's "데이터 소스 연결" on a phone, as Notion's whole
 * sheet "새 데이터베이스" (seen 2026-11-11 at 402pt in iOS Safari): its
 * search "데이터 소스 추가 또는 연결하기…" in a white row, not focused; "+
 * 비어 있는 새 데이터 소스" with its "+" in a grey tile; "기존 데이터 소스",
 * each with where it sits under its name, three before "⋯ 자세히 표시", which
 * shows five. A search names the new source after it, as the desktop panel's.
 * One among other views is Notion's "기존 데이터 소스 연결하기" (seen
 * 2026-11-11): its search "연결하기…" focused, the sources the block links
 * under "추천", then the others, five in all before "⋯ N개 더 보기", which
 * counts the rest of the hundred it holds and shows twenty more a press, a
 * search's first twenty, the reach kept through searches, and no empty
 * source; the desktop panel's counts (useSourceListLimit, existingSourcesShown).
 */
export function PhoneLinkSourceSheet({ busy, failed, load, own, blank = true, existing, onBlank, onChoose, onClose }: {
  busy: boolean; failed: boolean; own: string;
  /** Whether it offers the view's own empty source (not once a source was linked). */
  blank?: boolean;
  /** One among other views: the sources the block links, first under "추천". */
  existing?: readonly string[];
  load: EditorDatabaseStartActions["loadDatabaseSources"]; onBlank(name?: string): void; onChoose(source: DatabaseContainerSource): void; onClose(): void;
}) {
  const { t } = useTranslation(["databaseView", "editor", "propertyTypeConfig"]);
  const [query, setQuery] = useState("");
  const name = query.trim();
  const { sources, cursor, loading, failed: loadFailed, more: readMore } = useLinkableSources(load, query, own, existing ? EXISTING_COUNTED : LINK_MOST_SHOWN, existing);
  // "새 데이터베이스" shows three, then five, a search three again (Notion's, seen 2026-11-11); "기존 데이터 소스
  // 연결하기" every source window's reach (useSourceListLimit).
  const [all, setAll] = useState(false), reach = useSourceListLimit(query);
  const searchRef = useRef<HTMLInputElement>(null);
  const search = (value: string) => { setQuery(value); setAll(false); };
  const pages = useProductPages()?.pages;
  const pagesById = pagesByIdOf(pages ?? []);
  const suggested = existing ? sources.filter((source) => existing.includes(source.dataSourceId)) : [];
  const others = existing ? sources.filter((source) => !existing.includes(source.dataSourceId)) : sources;
  const ordered = [...suggested, ...others];
  const shownAll = existing ? existingSourcesShown(sources.map(({ dataSourceId }) => dataSourceId), ordered,
    { searched: name !== "", limit: reach.limit, key: ({ dataSourceId }) => dataSourceId }).shown
    : all ? ordered : ordered.slice(0, LINK_FIRST_SHOWN);
  const shown = others.filter((source) => shownAll.includes(source));
  const more = shownAll.length < ordered.length || (all && cursor);
  const none = Boolean(existing) && !loading && !others.length;
  const offersBlank = blank && !existing;
  // A source with nowhere to name has no second line, as on a wide screen.
  const placeLine = (place: ReturnType<typeof foundDatabaseSourcePlace>) => place.length ? <PlaceLine place={place} /> : undefined;
  const sourceRow = (source: DatabaseContainerSource) => (
    <PhoneSheetRow key={`${source.id}:${source.dataSourceId}`} icon={<span className={styles.mark}><DataSourceMark source={source} /></span>} more={false} disabled={busy}
      label={dataSourceDisplayTitle(source.title, source.sourceTitles?.length ?? 1)} description={placeLine(foundDatabaseSourcePlace(source, pagesById))}
      onPress={() => onChoose(source)} data={{ "data-start-source": source.dataSourceId }} />
  );
  const blankRow = (label: string, named?: string) => (
    <PhoneSheetGroup>
      <PhoneSheetRow label={<span className={styles.blank}><span className={styles.tile} aria-hidden="true"><Plus size="regular" /></span>{label}</span>}
        more={false} disabled={busy}
        onPress={() => onBlank(named)} data={{ "data-start-link-blank": named === undefined ? "" : "named" }} />
    </PhoneSheetGroup>
  );
  return (
    <PhoneSheet title={existing ? t("linkExistingDataSource") : inlineDatabasePlaceholderTitle()} onClose={onClose} data={{ "data-database-start-panel": "link", "data-phone-link-sources": "" }}>
      <label className={styles.search}>
        <input ref={searchRef} value={query} placeholder={t(existing ? "startPanel.linkSearch" : "startPanel.linkAddOrConnect")}
          aria-label={t(existing ? "startPanel.linkSearch" : "startPanel.linkAddOrConnect")} autoFocus={Boolean(existing)}
          disabled={busy} onChange={(event) => search(event.target.value)} />
        <SearchClearButton value={query} inputRef={searchRef} onClear={() => search("")} />
      </label>
      {offersBlank && !name ? blankRow(t("startPanel.blank")) : null}
      {suggested.some((source) => shownAll.includes(source)) ? (
        <PhoneSheetGroup label={t("startPanel.suggested")}>
          {suggested.filter((source) => shownAll.includes(source)).map(sourceRow)}
        </PhoneSheetGroup>
      ) : null}
      {others.length || !name || existing ? (
        // Holding none, searched in vain or with all under "추천", it says
        // "결과 없음" in grey under its heading, without the group's card
        // where nothing else stands in it (Notion's, seen 2026-11-11 at 402pt).
        <PhoneSheetGroup label={t("propertyTypeConfig:existingDataSources")} className={none && !(more && !loading) ? styles.bare : undefined}>
          {shown.map(sourceRow)}
          {none ? <p className={styles.empty}>{t("noSearchResults")}</p> : null}
          {more && !loading ? (
            <PhoneSheetRow icon={<DotsHorizontal size="regular" />} more={false} muted disabled={busy}
              label={existing && !all ? t("showMoreDataSources", { count: ordered.length - shownAll.length }) : t("startPanel.showMore")}
              onPress={() => {
                if (all) readMore();
                else if (existing) reach.more();
                else setAll(true);
              }} />
          ) : null}
        </PhoneSheetGroup>
      ) : null}
      {offersBlank && name ? blankRow(t("startPanel.blankNamed", { name }), name) : null}
      {loading ? <p className={styles.note} role="status">{t("editor:starter.loading")}</p> : null}
      {loadFailed || failed ? <p className={styles.note} role="alert">{t("editor:starter.loadFailed")}</p> : null}
    </PhoneSheet>
  );
}

/**
 * A linked view's "새 보기" on a phone, as Notion's whole sheet (seen
 * 2026-11-11): "‹" back to its sources, "+ 새 표 보기" ("+ 새 보드 보기" for a
 * board), then the source's views under its name; a view chosen opens the
 * view's settings sheet.
 */
export function PhoneLinkedViewSheet({ sourceTitle, views, loading, failed, busy, more, newLabel, onNewTable, onPick, onMore, onBack, onClose }: {
  sourceTitle: string; views: readonly GraphView[]; loading: boolean; failed: boolean; busy: boolean; newLabel: string;
  /** The source has more views to read. */
  more: boolean;
  onNewTable(): void; onPick(view: GraphView): void; onMore(): void; onBack?(): void; onClose(): void;
}) {
  const { t } = useTranslation(["databaseView", "editor", "formView"]);
  return (
    <PhoneSheet title={t("databaseView:starterNewView")} onBack={onBack} onClose={onClose} data={{ "data-linked-database-starter": "choose", "data-phone-linked-starter": "" }}>
      <PhoneSheetGroup>
        <PhoneSheetRow icon={<Plus size="regular" />} label={newLabel} more={false} disabled={busy} onPress={onNewTable}
          data={{ "data-linked-starter-new-table": "" }} />
      </PhoneSheetGroup>
      {views.length ? (
        <PhoneSheetGroup label={t("databaseView:starterSourceViews", { source: sourceTitle || inlineDatabasePlaceholderTitle() })}>
          {views.map((view) => (
            <PhoneSheetRow key={view.id} icon={<ViewIcon view={view} size="regular" />} ownMark label={viewDisplayName(view)} more={false} disabled={busy || loading}
              onPress={() => onPick(view)} data={{ "data-linked-starter-view": view.id }} />
          ))}
          {more && !loading ? <PhoneSheetRow label={t("formView:loadMore")} more={false} muted disabled={busy} onPress={onMore} /> : null}
        </PhoneSheetGroup>
      ) : null}
      {loading ? <p className={styles.note} role="status">{t("editor:starter.loading")}</p> : null}
      {failed ? <p className={styles.note} role="alert">{t("editor:starter.loadFailed")}</p> : null}
    </PhoneSheet>
  );
}
