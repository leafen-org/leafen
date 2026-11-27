import type { GraphView } from "@/contentGraph";
import { SwitchInput } from "@/components/Switch";
import { databaseViewLabels } from "./databaseViewLabels";
import { ViewTypeIcon } from "./databaseViewShared";
import layoutStyles from "./databaseSheet";
import styles from "./DatabaseTimelineVisibilityTabs.module.css";

/**
 * A timeline's "속성 표시 여부" lists what its bars show and what its table
 * shows under two tabs, the table's opening on its own switch, as Notion's
 * (seen 2026-11-04).
 */
export function DatabaseTimelineVisibilityTabs({ view, tab, onTab, writable, onUpdateView }: {
  view: GraphView;
  tab: "bars" | "table";
  onTab(tab: "bars" | "table"): void;
  writable: boolean;
  onUpdateView(view: GraphView): void;
}) {
  const labels = databaseViewLabels();
  const choice = (value: "bars" | "table", label: string) => (
    <button type="button" role="tab" className={styles.tab} aria-selected={tab === value} onClick={() => onTab(value)}>
      <span>{label}</span>
    </button>
  );
  return (
    <>
      <div className={styles.tabs} role="tablist">
        {choice("bars", labels.viewTypes.timeline)}
        {choice("table", labels.viewTypes.table)}
      </div>
      {tab === "table" ? (
        <label className={`${layoutStyles.layoutRow} ${layoutStyles.layoutSwitchRow} ${styles.showTable}`}>
          <span className={styles.showTableName}><ViewTypeIcon type="table" size="regular" />{labels.showTable}</span>
          <SwitchInput data-layout-option="timelineShowTable" checked={view.config?.timelineShowTable === true} disabled={!writable}
            onChange={(event) => onUpdateView({ ...view, config: { ...view.config, timelineShowTable: event.currentTarget.checked || undefined } })} />
        </label>
      ) : null}
    </>
  );
}
