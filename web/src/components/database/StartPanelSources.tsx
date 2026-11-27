import { useContext, useEffect, useLayoutEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import type { EditorDatabaseStartActions } from "@/components/editor/editorRuntimeTypes";
import { dataSourceDisplayTitle } from "@/components/editor/databaseTitles";
import { useProductPages } from "@/components/product/productPagesContext";
import { foundDatabaseSourcePlace } from "@/lib/pagePath";
import { pagesByIdOf } from "@/lib/pageIndex";
import { DotsHorizontal, ImportIcon, Plus } from "@/icons/leafen";
import { DataSourceListRow } from "./DataSourceListRow";
import { LINK_FIRST_SHOWN, LINK_MOST_SHOWN, useLinkableSources } from "./linkableSources";
import { StartRowLit, sourceRowKey } from "./startPanelRows";
import styles from "./DatabaseStartPanel.module.css";

/**
 * "기존 데이터 소스", each by its mark and where it sits (DataSourceListRow), a few before "자세히 표시",
 * which shows `most` and stops there, searched or not — a waiting view's "새
 * 데이터베이스" and a new chart's alike (Notion's, seen 2026-11-18 twice from
 * "/차트": three, then five, no search of its own; the sources past them are
 * found under "기존 데이터 소스 연결하기", SourceList).
 */
export function ExistingSources({ query, first, most, busy, failed, load, own, foot, onChoose, onShown }: {
  query: string; first: number; busy: boolean; failed: boolean; own: string;
  /** All "자세히 표시" shows (LINK_MOST_SHOWN). */
  most: number;
  load: EditorDatabaseStartActions["loadDatabaseSources"]; onChoose(source: DatabaseContainerSource): void;
  /** Drawn after them, told whether a search found any; a search that found none shows no heading. */
  foot?(found: boolean): React.ReactNode;
  /** Told the key of the first source shown (`sourceRowKey`) as a search's sources come in, and that search. */
  onShown?(first: string | null, query: string): void;
}) {
  const { t } = useTranslation(["databaseView", "editor", "propertyTypeConfig"]);
  const { sources, loading, failed: loadFailed, shownFor } = useLinkableSources(load, query, own, most);
  const { lit, check } = useContext(StartRowLit);
  useLayoutEffect(check);
  useEffect(() => {
    if (shownFor !== undefined) onShown?.(sources[0] ? sourceRowKey(sources[0]) : null, shownFor);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shownFor, sources]);
  const [all, setAll] = useState(false);
  const pages = useProductPages()?.pages;
  const pagesById = pagesByIdOf(pages ?? []);
  // Searched, it still shows three, then five (Notion's, seen 2026-11-11).
  const shown = all ? sources : sources.slice(0, first);
  const more = shown.length < sources.length;
  const found = sources.length > 0;
  return <>
    {found || !foot ? <div className={`${styles.heading} ${styles.listHeading}`}>{t("propertyTypeConfig:existingDataSources")}</div> : null}
    {shown.map((source) => <DataSourceListRow key={`${source.id}:${source.dataSourceId}`} className={styles.source}
      disabled={busy} data-start-source={source.dataSourceId} data-key={sourceRowKey(source)}
      data-active={lit === sourceRowKey(source) ? "true" : undefined} onClick={() => onChoose(source)}
      source={source} name={dataSourceDisplayTitle(source.title, source.sourceTitles?.length ?? 1)} place={foundDatabaseSourcePlace(source, pagesById)} />)}
    {loading ? <p className={styles.note} role="status">{t("editor:starter.loading")}</p> : null}
    {loadFailed || failed ? <p className={styles.note} role="alert">{t("editor:starter.loadFailed")}</p> : null}
    {more && !loading ? <button type="button" className={`${styles.item} ${styles.more}`} disabled={busy}
      data-key="more" data-active={lit === "more" ? "true" : undefined} onClick={() => setAll(true)}>
      <DotsHorizontal size="regular" aria-hidden="true" />{t("startPanel.showMore")}</button> : null}
    {foot?.(found)}
  </>;
}

/**
 * What the search a database opened alone starts with finds, as Notion's
 * (seen 2026-11-17 at 1440, dark, on its own page): "기존 데이터 소스" with
 * three before "자세히 표시", then in a band of the menu's ground, 4px
 * under them or 8px under the search where none is found, "+ 새 '〈search〉'
 * 데이터 소스" and "CSV 가져오기" (Notion draws "{name}" there, its own slip;
 * the search is drawn here).
 */
export function StartSearchFound({ query, name, busy, failed, load, own, lit, onShown, onChoose, onNamed, onCsv }: {
  query: string; name: string; busy: boolean; failed: boolean; own: string;
  load: EditorDatabaseStartActions["loadDatabaseSources"];
  /** The row its list lights (`useStartPanelRows`). */
  lit: string | null;
  onShown(first: string | null, query: string): void;
  onChoose(source: DatabaseContainerSource): void; onNamed(): void; onCsv(): void;
}) {
  const { t } = useTranslation("databaseView");
  const row = (key: string) => ({ "data-key": key, "data-active": lit === key ? "true" : undefined });
  return <ExistingSources query={query} first={LINK_FIRST_SHOWN} most={LINK_MOST_SHOWN} busy={busy} failed={failed} load={load} own={own}
    onChoose={onChoose} onShown={onShown} foot={() => <div className={styles.foot}>
      <button type="button" className={`${styles.item} ${styles.start} ${styles.blank}`} disabled={busy} data-start-blank-named="" {...row("named")} onClick={onNamed}>
        <span className={styles.mark} aria-hidden="true"><Plus size="regular" /></span><span className={styles.blankName}>{t("startPanel.blankNamed", { name })}</span></button>
      <button type="button" className={`${styles.item} ${styles.start}`} disabled={busy} {...row("csv")} onClick={onCsv}>
        <ImportIcon size="regular" aria-hidden="true" />{t("startPanel.csv")}</button>
    </div>} />;
}
