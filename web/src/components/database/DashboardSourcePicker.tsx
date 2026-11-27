import { useContext, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { placeInWindow, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { readsRightToLeft } from "@/lib/readingDirection";
import { keepLayerKeys } from "@/lib/overlayLayer";
import { DatabaseDataSourcesContext } from "./databaseDataSourcesContext";
import { SourceList } from "./DatabaseStartPanel";
import styles from "./DatabaseStartPanel.module.css";

const PANEL_WIDTH = 290;

/**
 * Notion's "기존 데이터 소스 연결하기" beside a dashboard's widget (seen
 * 2026-11-12 at 1440): a dashboard of a view block, or of a database of more
 * than one source, asks a new widget's source first — its search "연결하기…"
 * taking the keys, every source under "기존 데이터 소스", the database's own
 * among them, five before "N개 더 보기", no new source. "←" from the
 * widget's "새 보기" comes back here, its source under "추천", to give it
 * another. Escape, a press elsewhere and × close it.
 */
export function DashboardSourcePicker({ widgetId, current, onChoose, onClose }: {
  /** The widget it stands level with: the one being made, or the one whose source it changes. */
  widgetId: string;
  /** The widget's source, where it has one. */
  current?: string;
  onChoose(source: DatabaseContainerSource): Promise<boolean>;
  onClose(): void;
}) {
  const { t } = useTranslation("databaseView");
  const surface = useContext(DatabaseDataSourcesContext);
  const panelRef = useRef<HTMLDivElement>(null), searchRef = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false), [failed, setFailed] = useState(false);
  // At the window's end edge (its left read from the right), level with the widget, as "새 보기".
  const place = usePopoverPlacement(panelRef, (size, viewport) => {
    const frame = popoverRectOf(document.querySelector(`[data-dashboard-widget="${CSS.escape(widgetId)}"]`));
    return placeInWindow({ left: readsRightToLeft() ? 12 : viewport.width - PANEL_WIDTH - 12, top: frame?.top ?? 12 }, size, viewport);
  }, [widgetId], undefined, { kind: "picker", onEscape: onClose, onOutsidePress: onClose });
  const session = surface?.session;
  if (!surface || !session) return null;
  const load = (query: string, cursor: string | undefined, signal: AbortSignal, limit?: number, first?: readonly string[]) =>
    api.databaseContainerSources(session, surface.workspaceId, query, { cursor, limit: limit ?? 25, signal, first });
  async function choose(source: DatabaseContainerSource) {
    if (busy) return;
    setBusy(true); setFailed(false);
    const chosen = await onChoose(source).catch(() => false);
    setBusy(false);
    if (!chosen) setFailed(true);
  }
  return createPortal(
    <div ref={panelRef} role="dialog" aria-label={t("linkExistingDataSource")} aria-busy={busy || undefined} className={styles.panel} onKeyDown={keepLayerKeys}
      style={{ position: "fixed", ...popoverPlacementStyle(place) }} data-dashboard-source-picker={widgetId}>
      <SourceList searchRef={searchRef} busy={busy} failed={failed} load={load} own="" suggest={current ? [current] : undefined}
        onClose={onClose} onChoose={(source) => void choose(source)} />
    </div>,
    document.body,
  );
}
