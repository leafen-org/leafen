import { useContext, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { foundDatabaseSourcePlace } from "@/lib/pagePath";
import { useProductPages } from "@/components/product/productPagesContext";
import { DotsHorizontal } from "@/icons/leafen";
import { DataSourceMark } from "./DataSourceMark";
import { DataSourceListRow } from "./DataSourceListRow";
import type { DatabaseDataSourcesSurface } from "./databaseDataSourcesContext";
import styles from "./DatabaseViewSourceScreen.module.css";
import { dataSourceDisplayTitle } from "../editor/databaseTitles";
import { EXISTING_COUNTED, existingSourcesShown, useSourceListLimit } from "./linkableSources";
import { PlaceLine } from "../PlaceLine";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { pagesByIdOf } from "@/lib/pageIndex";
import { PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { SearchClearButton } from "@/components/PageActionSearch";
import { PhoneSettingsListContext } from "./PropertySettingRow";
import linkStyles from "./DatabaseLinkPhoneSheets.module.css";

/**
 * The view settings' "데이터베이스" screen, as Notion's (seen 2026-10-11): a
 * search "데이터 소스 연결" — a new view's, "기존 데이터 소스 연결하기"'s,
 * "연결하기…" (seen 2026-11-15) — "추천" — the database's other sources, its
 * own and those it links from other databases (seen 2026-11-18 in Chrome 1440
 * and a phone: a table of the database's own source suggests the one it links,
 * a table of that one the database's own) — then "기존 데이터 소스", each
 * with where it is and its source's mark (DataSourceMark), the view's own
 * source left out. The first five of the
 * hundred it reads count the database's own sources first, the only one or the
 * view's own too though not listed, so "기존 데이터 소스" shows five less
 * them (seen 2026-11-15: 4 by one source, 3 by two, 2 by three, none by five,
 * the switch and the new view alike), then "N개 더 보기", which counts the
 * rest of the hundred and shows twenty more a press (seen 2026-11-11), a
 * search the first twenty found — the reach kept through searches
 * (useSourceListLimit, existingSourcesShown). Choosing one
 * turns the view to it; a new view goes on to that source's views
 * (`onSwitched`), its "추천" every source of its database, the one it shows
 * too and those it links from other databases, as Notion's (seen 2026-11-02;
 * a linked database's, 2026-11-12). Under each name, the pages it sits
 * under, then its database's name while that holds several, the
 * database's own sources under "추천" too ("… / 사과 DB 및 사과 둘째", seen 2026-11-15).
 */
export function DatabaseViewSourceScreen({ surface, viewId, currentSourceId, newView = false, onDone, onSwitched }: {
  surface: DatabaseDataSourcesSurface;
  viewId: string;
  currentSourceId: string;
  newView?: boolean;
  onDone(): void;
  onSwitched?(candidate: DatabaseContainerSource): void;
}) {
  const { t } = useTranslation(["propertyTypeConfig", "databaseView"]);
  const phone = useContext(PhoneSettingsListContext);
  const searchRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  const [candidates, setCandidates] = useState<DatabaseContainerSource[] | null>(null);
  const { limit, more } = useSourceListLimit(query);
  const [busy, setBusy] = useState(false);
  const { loadCandidates } = surface;
  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void loadCandidates(query.trim(), controller.signal, EXISTING_COUNTED).then(setCandidates).catch(() => {
        if (!controller.signal.aborted) setCandidates([]);
      });
    }, query ? 120 : 0);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [loadCandidates, query]);
  const ownIds = new Set(surface.own.map(({ id }) => id));
  // The sources it links from other databases are among its "추천", not again below.
  const linkedIds = new Set(surface.linked.map(({ dataSourceId }) => dataSourceId));
  // The view's own source isn't among the others to turn to, as Notion's
  // (seen 2026-11-11: 4 + "95개 더 보기" of a hundred); a new view's is, to go on to its views.
  const sources = (candidates ?? []).filter((candidate, index, list) => !ownIds.has(candidate.dataSourceId)
    && !linkedIds.has(candidate.dataSourceId) && (newView || candidate.dataSourceId !== currentSourceId)
    && list.findIndex((other) => other.dataSourceId === candidate.dataSourceId) === index);
  const needle = query.trim().toLocaleLowerCase();
  const pages = useProductPages()?.pages;
  const pagesById = pagesByIdOf(pages ?? []);
  // A source in the trash isn't one to turn to, as Notion's (seen 2026-11-11); a database's
  // only source isn't suggested to its new view, which lists "기존 데이터 소스" alone (seen 2026-11-15),
  // nor one left alone by the trash, nor counted (seen 2026-11-19, twice: no "추천").
  const several = surface.own.filter((own) => !own.sourceInTrash).length + surface.linked.length > 1;
  const suggested = surface.own.filter((own) => several && !own.sourceInTrash && (newView || own.id !== currentSourceId)
    && (!needle || own.title.toLocaleLowerCase().includes(needle)))
    .map((own): DatabaseContainerSource => ({ id: surface.databaseId, pageId: own.pageId ?? "", pageTitle: "", dataSourceId: own.id,
      viewId: "", viewType: "table", viewName: "", viewConfig: {}, title: own.title, icon: own.icon, iconType: own.iconType,
      sourceTitles: surface.sourceTitles && [...surface.sourceTitles], databaseTitle: surface.databaseTitle }))
    .concat(surface.linked.filter((linked) => (newView || linked.dataSourceId !== currentSourceId)
      && (!needle || linked.title.toLocaleLowerCase().includes(needle)))
      .map((linked): DatabaseContainerSource => (candidates ?? []).find((candidate) => candidate.dataSourceId === linked.dataSourceId)
        ?? { id: linked.sourceContainerId ?? "", pageId: linked.pageId ?? "", pageTitle: "", dataSourceId: linked.dataSourceId,
          viewId: "", viewType: "table", viewName: "", viewConfig: {}, title: linked.title }));
  const members = surface.own.filter((own) => !own.sourceInTrash).length + surface.linked.length;
  // A search reads up to a hundred found, past the hundred listed unsearched, and shows those among the first
  // twenty it found — the database's own, those it links and the view's own count there though listed above or
  // not at all — then "N개 더 보기" for the rest, as Notion's (seen 2026-11-18 in Chrome 1440: 24 sources found,
  // 11 past the hundred, 20 shown and "38개 더 보기"; "DB", which the view's own and its linked source answer
  // too, 18 and "64개 더 보기"; unsearched 2 + 2 and "95개 더 보기").
  const { shown, hidden: moreCount } = existingSourcesShown((candidates ?? []).map(({ dataSourceId }) => dataSourceId), sources,
    { searched: query.trim() !== "", limit, ahead: members, key: ({ dataSourceId }) => dataSourceId });
  const search = setQuery;
  async function choose(candidate: DatabaseContainerSource) {
    if (candidate.dataSourceId !== currentSourceId) {
      setBusy(true);
      const switched = await surface.switchView(viewId, candidate);
      setBusy(false);
      if (!switched) return;
    }
    if (onSwitched) {
      onSwitched(candidate);
      return;
    }
    onDone();
  }
  const row = (candidate: DatabaseContainerSource) => {
    const where = foundDatabaseSourcePlace(candidate, pagesById);
    return (
      <DataSourceListRow key={candidate.dataSourceId} role="option" disabled={busy} source={candidate} place={where}
        name={dataSourceDisplayTitle(candidate.title, ownIds.has(candidate.dataSourceId) ? surface.naming + surface.linked.length : candidate.sourceTitles?.length ?? 1)}
        aria-selected={candidate.dataSourceId === currentSourceId} data-view-source-candidate={candidate.dataSourceId}
        onClick={() => void choose(candidate)} />
    );
  };
  const label = newView ? t("databaseView:startPanel.linkSearch") : t("propertyTypeConfig:connectDataSource");
  if (phone) {
    // A phone's, as Notion's sheet (seen 2026-11-18 on a Galaxy A90 at 411, a new view's "기존 데이터 소스
    // 연결하기"): the search a white round row (DatabaseLinkPhoneSheets' own), taking the keys as the screen
    // opens, the keyboard up (judged from the screen after the press and one letter typed); its groups under
    // their grey heads, a source's grey mark, its name and where it is.
    // A source with nowhere to name has no second line, as on a wide screen.
    const placeLine = (place: ReturnType<typeof foundDatabaseSourcePlace>) => place.length ? <PlaceLine place={place} /> : undefined;
    const phoneRow = (candidate: DatabaseContainerSource) => (
      <PhoneSheetRow key={candidate.dataSourceId} icon={<span className={styles.phoneMark}><DataSourceMark source={candidate} size="regular" /></span>} more={false} disabled={busy}
        label={dataSourceDisplayTitle(candidate.title, ownIds.has(candidate.dataSourceId) ? surface.naming + surface.linked.length : candidate.sourceTitles?.length ?? 1)}
        description={placeLine(foundDatabaseSourcePlace(candidate, pagesById))}
        onPress={() => void choose(candidate)} data={{ "data-view-source-candidate": candidate.dataSourceId }} />
    );
    const none = candidates !== null && sources.length === 0;
    return (
      <div className={styles.phone} data-view-source-screen aria-busy={busy}>
        <label className={linkStyles.search}>
          <input ref={searchRef} autoFocus={newView || fieldFocusOnOpen()} value={query} placeholder={label} aria-label={label}
            onChange={(event) => search(event.currentTarget.value)} />
          <SearchClearButton value={query} inputRef={searchRef} onClear={() => search("")} />
        </label>
        {suggested.length > 0 ? <PhoneSheetGroup label={t("databaseView:suggestedDataSources")}>{suggested.map(phoneRow)}</PhoneSheetGroup> : null}
        <PhoneSheetGroup label={t("propertyTypeConfig:existingDataSources")} className={none ? linkStyles.bare : undefined}>
          {shown.map(phoneRow)}
          {none ? <p className={linkStyles.empty}>{t("databaseView:noSearchResults")}</p> : null}
          {moreCount > 0 ? <PhoneSheetRow icon={<DotsHorizontal size="regular" />} more={false} muted disabled={busy}
            label={t("databaseView:showMoreDataSources", { count: moreCount })} onPress={more}
            data={{ "data-view-source-more": "" }} /> : null}
        </PhoneSheetGroup>
      </div>
    );
  }
  return (
    <div className={styles.screen} data-view-source-screen aria-busy={busy}>
      <input className={styles.search} autoFocus={fieldFocusOnOpen()} value={query} placeholder={label} aria-label={label}
        onChange={(event) => search(event.currentTarget.value)} />
      {suggested.length > 0 ? <div className={styles.group}>
        <div className={styles.section}>{t("databaseView:suggestedDataSources")}</div>
        <div role="listbox" aria-label={t("databaseView:suggestedDataSources")} data-view-source-suggested>{suggested.map(row)}</div>
      </div> : null}
      <div className={styles.group}>
      <div className={styles.section}>{t("propertyTypeConfig:existingDataSources")}</div>
      <div role="listbox" aria-label={t("propertyTypeConfig:existingDataSources")}>
        {shown.map(row)}
        {moreCount > 0 ? (
          <button type="button" className={styles.source} data-view-source-more onClick={more}>
            <DotsHorizontal size="regular" aria-hidden="true" />
            <span className={styles.sourceText}><span>{t("databaseView:showMoreDataSources", { count: moreCount })}</span></span>
          </button>
        ) : null}
        {candidates && sources.length === 0 ? <p className={styles.empty}>{t("databaseView:noDataSourcesFound")}</p> : null}
      </div>
      </div>
    </div>
  );
}
