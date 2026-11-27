import { useContext, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphView, GraphViewType } from "@/contentGraph";
import { Plus } from "@/icons/leafen";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { isDashboardWidget } from "@/lib/dashboardLayout";
import type { DatabaseDataSourcesSurface } from "./databaseDataSourcesContext";
import { databaseViewLabels } from "./databaseViewLabels";
import { dataSourceDisplayTitle } from "../editor/databaseTitles";
import { ViewIcon } from "./databaseViewShared";
import styles from "./DatabaseViewSourceScreen.module.css";
import { PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { PhoneSettingsListContext } from "./PropertySettingRow";
import { ReadFailed } from "@/components/ReadFailed";

/**
 * A new view's second step after it turned to another database's source,
 * as Notion's (seen 2026-10-11): "+ 새 〈유형〉 보기" keeps it a new view of
 * that kind, back at its settings; under "〈소스〉의 조회수" the source's own
 * views, one of which it can take after — its layout, name and settings —
 * ending the new view at the view's settings. A source linked from another
 * database lists this database's other views of it first, then that
 * database's, the new view itself in neither (Notion's, seen 2026-11-12:
 * "표", then "Default view").
 */
export function DatabaseViewSourceViews({ surface, viewId, viewType, candidate, onNew, onAdopted }: {
  surface: DatabaseDataSourcesSurface;
  viewId: string;
  viewType: GraphViewType;
  candidate: DatabaseContainerSource;
  onNew(): void;
  onAdopted(): void;
}) {
  const { t } = useTranslation(["databaseView", "editor", "formView"]);
  const phone = useContext(PhoneSettingsListContext);
  const labels = databaseViewLabels();
  const [views, setViews] = useState<GraphView[] | null>(null);
  const [cursor, setCursor] = useState<string>();
  const [busy, setBusy] = useState(false);
  // A window of views that could not be read: said where they show, with a
  // way to read it again — never as a source with none.
  const [failed, setFailed] = useState<{ after: string | undefined } | null>(null);
  const { loadSourceViews } = surface;
  const choosable = (view: GraphView) => view.id !== viewId && view.type !== "dashboard" && !isDashboardWidget(view);
  const blockViews = candidate.id === surface.databaseId ? []
    : (surface.views ?? []).filter((view) => view.dataSourceId === candidate.dataSourceId && choosable(view));
  const load = (after: string | undefined, signal: AbortSignal) => loadSourceViews(candidate, after, signal).then((page) => {
    if (signal.aborted) return;
    setViews((current) => [...(after ? current ?? [] : []), ...page.views.filter(choosable)]);
    setCursor(page.nextCursor);
    setFailed(null);
  }).catch(() => { if (!signal.aborted) setFailed({ after }); });
  const readAgain = () => { const after = failed?.after; setFailed(null); void load(after, new AbortController().signal); };
  const failure = failed ? <ReadFailed message={t("databaseView:sourceViewsLoadFailed")} onRetry={readAgain} /> : null;
  useEffect(() => {
    const controller = new AbortController();
    void load(undefined, controller.signal);
    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [candidate.id, candidate.dataSourceId]);
  async function adopt(saved: GraphView) {
    setBusy(true);
    const adopted = await surface.adoptView(viewId, saved);
    setBusy(false);
    if (adopted) onAdopted();
  }
  // An untitled source of a database of several reads "새 데이터 소스", as Notion's.
  const sourceName = dataSourceDisplayTitle(candidate.title, surface.own.some(({ id }) => id === candidate.dataSourceId)
    ? surface.naming + surface.linked.length : 1);
  const newLabel = t("databaseView:newTypedView", { type: labels.viewTypes[viewType].toLocaleLowerCase() });
  const shown = [...blockViews, ...(views ?? [])];
  // A phone's, as Notion's sheet (seen 2026-11-18 on a Galaxy A90 at 411): "+ 새 표 보기" a group of its own, its "+" grey, the
  // source's views a group under "〈소스〉의 조회수", as a linked view's "새 보기" (PhoneLinkedViewSheet).
  if (phone) return (
    <div className={styles.phone} data-view-source-views aria-busy={busy}>
      <PhoneSheetGroup>
        <PhoneSheetRow icon={<span className={styles.phoneMark}><Plus size="regular" /></span>} label={newLabel} more={false} disabled={busy} onPress={onNew} data={{ "data-view-source-new": "" }} />
      </PhoneSheetGroup>
      <PhoneSheetGroup label={t("databaseView:sourceViewsOf", { source: sourceName })}>
        {shown.map((view) => (
          <PhoneSheetRow key={view.id} icon={<ViewIcon view={view} size="regular" />} ownMark label={view.name || labels.viewTypes[view.type]} more={false}
            disabled={busy} onPress={() => void adopt(view)} data={{ "data-view-source-view": view.id }} />
        ))}
        {cursor ? <PhoneSheetRow label={t("formView:loadMore")} more={false} muted disabled={busy} onPress={() => void load(cursor, new AbortController().signal)} /> : null}
      </PhoneSheetGroup>
      {failure ?? (views === null ? <p className={styles.empty} role="status">{t("editor:starter.loading")}</p> : null)}
    </div>
  );
  return (
    <div className={styles.screen} data-view-source-views aria-busy={busy}>
      <div className={styles.group}><button type="button" className={styles.choice} disabled={busy} data-view-source-new onClick={onNew}>
        <Plus size="regular" aria-hidden="true" />
        <span>{newLabel}</span>
      </button></div>
      <div className={styles.group}>
      <div className={styles.section}>{t("databaseView:sourceViewsOf", { source: sourceName })}</div>
      <div role="listbox" aria-label={t("databaseView:sourceViewsOf", { source: sourceName })}>
        {shown.map((view) => (
          <button key={view.id} type="button" role="option" aria-selected={false} className={styles.choice} disabled={busy}
            data-view-source-view={view.id} onClick={() => void adopt(view)}>
            <ViewIcon view={view} size="regular" />
            <span>{view.name || labels.viewTypes[view.type]}</span>
          </button>
        ))}
        {failure ?? (views === null ? <p className={styles.empty} role="status">{t("editor:starter.loading")}</p> : null)}
        {cursor && !failed ? (
          <button type="button" className={styles.choice} disabled={busy}
            onClick={() => void load(cursor, new AbortController().signal)}>
            <span>{t("formView:loadMore")}</span>
          </button>
        ) : null}
      </div>
      </div>
    </div>
  );
}
