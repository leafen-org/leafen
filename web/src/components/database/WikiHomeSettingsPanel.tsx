"use client";

import { type RefObject, useEffect, useRef, useState } from "react";
import { FileText, X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { databaseViewLabels } from "./databaseViewLabels";
import { PanelHeadBox, PanelHeading } from "./panelHead";
import { useDockedPanelTop } from "./useDockedPanelTop";
import dockColumn from "./dockedPanelColumn.module.css";
import styles from "./DatabaseViewSettingsPanel.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * A wiki's Home's settings, which its "⋯" "이름 바꾸기" opens, as Notion's
 * (seen 2026-11-11): the view settings' panel hung under the views' row,
 * "설정 보기" over Home's page icon and its name, selected. Enter or leaving
 * the field keeps the name, for everyone; emptied, Home is "홈" again.
 */
export function WikiHomeSettingsPanel({ name, placeholder, dockRef, onRename, onClose }: {
  /** Home's own name, empty while it has none. */
  name: string;
  placeholder: string;
  /** The views' row the panel hangs under. */
  dockRef: RefObject<HTMLElement | null>;
  onRename(name: string): void;
  onClose(): void;
}) {
  const labels = databaseViewLabels();
  const [value, setValue] = useState(name || placeholder);
  const panelRef = useRef<HTMLDivElement>(null);
  const dockTop = useDockedPanelTop(dockRef, true, panelRef);
  // The page narrows beside it, as beside a view's settings.
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.databaseViewSettingsOpen = "true";
    return () => { delete root.dataset.databaseViewSettingsOpen; };
  }, []);
  // Docked beside the page as a view's settings are, a press beside it closes it and goes on to what it pressed,
  // as theirs does (useDatabaseToolbarController's docked panel).
  useOverlayLayer(true, { kind: "side-peek", closesAfterClick: false, ref: panelRef, onEscape: onClose, onOutsidePress: onClose });
  const keep = (next: string) => {
    const kept = next.trim() === placeholder ? "" : next.trim();
    if (kept !== name) onRename(kept);
  };
  return (
    <div ref={panelRef} className={`${styles.panel} ${dockColumn.column}`} role="dialog" aria-label={labels.viewSettingsLabel}
      data-docked-panel="true" data-wiki-home-settings style={dockTop === null ? undefined
        : { top: `max(var(--page-panel-top, var(--topbar-height)), ${dockTop}px)` }}>
      <button type="button" className={styles.close} aria-label={labels.closeViewSettings} onClick={onClose}>
        <X size="tiny" aria-hidden="true" />
      </button>
      <PanelHeadBox>
        <div className={styles.panelBody} data-panel-body data-edge-fade=""><div className={styles.panelContent}>
          <PanelHeading><div className={`${styles.heading} ${styles.mainHeading}`}><span className={styles.label}>{labels.viewSettingsLabel}</span></div></PanelHeading>
          <div className={styles.viewName}>
            <span className={styles.viewIconButton} aria-hidden="true"><FileText size="regular" /></span>
            <span className={styles.viewNameBox}>
              <input autoFocus data-view-settings-name placeholder={placeholder} value={value} aria-label={labels.viewName}
                onChange={(event) => setValue(event.currentTarget.value)} onFocus={(event) => event.currentTarget.select()}
                onKeyDown={(event) => {
                  if (!fieldEnter(event)) return;
                  event.preventDefault();
                  keep(event.currentTarget.value);
                }}
                onBlur={(event) => keep(event.target.value)} />
            </span>
          </div>
        </div></div>
      </PanelHeadBox>
    </div>
  );
}
