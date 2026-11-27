import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DatabaseContainerPickerResponse, DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { databaseStarterLabel as label, databaseStarterProperties, databaseStarterTemplates, type DatabaseStarterTemplate } from "@/lib/databaseStarterCatalog";
import { foundDatabaseSourcePlace } from "@/lib/pagePath";
import { useProductPages } from "@/components/product/productPagesContext";
import { DatabaseStarterPreview } from "./DatabaseStarterPreview";
import { PlaceLine } from "../PlaceLine";
import { dataSourceDisplayTitle } from "../editor/databaseTitles";
import styles from "./DatabaseStarterGallery.module.css";
import { pagesByIdOf } from "@/lib/pageIndex";

export type DatabaseStarterSourceLoader = (query: string, cursor: string | undefined, signal: AbortSignal) => Promise<DatabaseContainerPickerResponse>;

export function DatabaseStarterGallery({ query, busy, loadSources, onSource, onTemplate }: {
  query: string; busy: boolean; loadSources?: DatabaseStarterSourceLoader;
  onSource(source: DatabaseContainerSource): void; onTemplate(template: DatabaseStarterTemplate): void;
}) {
  const { t } = useTranslation(["sidebar", "editor", "formView"]);
  const pages = useProductPages()?.pages;
  const pagesById = pagesByIdOf(pages ?? []);
  const [sources, setSources] = useState<DatabaseContainerSource[]>([]);
  const [cursor, setCursor] = useState<string>();
  const [loading, setLoading] = useState(false), [failed, setFailed] = useState(false);
  const work = useRef<AbortController | null>(null);
  const failedCursor = useRef<string | undefined>(undefined);
  const freshQuery = useRef(query); freshQuery.current = query;
  async function load(after?: string) {
    work.current?.abort();
    if (!loadSources) return;
    const controller = new AbortController(); work.current = controller;
    const requestedQuery = query.trim();
    setLoading(true); setFailed(false); failedCursor.current = after;
    try {
      const result = await loadSources(requestedQuery, after, controller.signal);
      if (controller.signal.aborted || work.current !== controller || freshQuery.current.trim() !== requestedQuery) return;
      if (after && result.nextCursor === after) throw new Error("Invalid continuation");
      setSources(current => [...new Map([...(after ? current : []), ...result.items].map(source => [source.id + ":" + source.dataSourceId, source])).values()]);
      setCursor(result.nextCursor);
    } catch { if (!controller.signal.aborted) setFailed(true); }
    finally { if (work.current === controller && !controller.signal.aborted) setLoading(false); }
  }
  useEffect(() => {
    work.current?.abort(); setSources([]); setCursor(undefined); setFailed(false); setLoading(Boolean(loadSources));
    const timer = window.setTimeout(() => void load(), 120);
    return () => { window.clearTimeout(timer); work.current?.abort(); };
  }, [query, loadSources]);
  const visibleTemplates = databaseStarterTemplates.filter(template => !query.trim() || label(template.name).toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  return <div className={styles.gallery} data-database-starter-gallery>
    {(sources.length || loading || failed || cursor) ? <section>
      <h2>↗ {t("editor:starter.sources")}</h2>
      <div className={styles.grid}>{sources.map(source => <button key={source.id + ":" + source.dataSourceId} type="button" className={styles.card}
        disabled={busy || loading} data-starter-source={source.dataSourceId} onClick={() => onSource(source)}>
        <div className={styles.cardText}><strong>{dataSourceDisplayTitle(source.title, source.sourceTitles?.length ?? 1)}</strong><small><PlaceLine place={foundDatabaseSourcePlace(source, pagesById)} /></small></div>
        <DatabaseStarterPreview title={source.title} properties={source.previewProperties ?? []} thumbnail localize={false} />
      </button>)}</div>
      {loading ? <p role="status">{t("editor:starter.loading")}</p> : null}
      {failed ? <p role="alert">{t("editor:starter.loadFailed")} <button type="button" disabled={busy || loading} onClick={() => void load(failedCursor.current)}>{t("editor:starter.retry")}</button></p> : null}
      {cursor && !failed ? <button type="button" className={styles.more} disabled={busy || loading} onClick={() => void load(cursor)}>{t("formView:loadMore")}</button> : null}
    </section> : null}
    {visibleTemplates.length ? <section><h2>{t("sidebar:addWindow.templates")}</h2>
      <div className={styles.grid}>{visibleTemplates.map(template => <button key={template.key} type="button" className={styles.card} data-tone={template.tone}
        data-starter-template={template.key} disabled={busy} onClick={() => onTemplate(template)}>
        <div className={styles.cardText}><strong>{label(template.name)}</strong><small>{label(template.description)}</small></div>
        <DatabaseStarterPreview title={label(template.name)} icon={template.icon} view={template.views[0]} properties={template.previewPropertyIds
          ? template.previewPropertyIds.flatMap(id => template.properties.find(p => p.id === id) ?? [])
          : databaseStarterProperties(template, template.features.filter(f => f.defaultOn).map(f => f.key))} thumbnail />
      </button>)}</div>
    </section> : null}
  </div>;
}
