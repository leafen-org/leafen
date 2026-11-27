import type { GraphView } from "@/contentGraph";
import { FileText } from "@/icons/leafen";
import type { DatabaseSubtaskViewConfig } from "@/lib/types";
import { databaseViewLabels } from "./databaseViewLabels";
import { NotionSelect } from "./NotionSelect";
import styles from "./DatabaseTaskSettings.module.css";
import { subitemFilterScope } from "./subitemFilterScope";
import { SubitemsCardPicture, SubitemsPicture, type SubitemsPictureRow } from "./SubitemsPicture";

type DisplayMode = "show" | "flattened" | "disabled";
type FilterScope = DatabaseSubtaskViewConfig["filterScope"];

/**
 * How the view shows sub-items and which of them its filters keep, as
 * Notion's sub-items screen sets them once they are on (seen 2026-10-11):
 * "표시 옵션" nested in toggles — on a board, gallery or calendar as a card
 * property — a flattened list or disabled; then "필터 옵션", or "포함" for
 * the flattened list, parents only, parents and sub-items or sub-items only
 * (a card property keeps the parents), and a line saying what the choice
 * shows. A sub-item display only the import knows ("hidden") shows as
 * nested parents only, which is how the views treat it. Each name has its
 * picture under it and its choice under that, the line saying what it shows
 * between the last picture and its choice (seen 2026-11-11); the filter's
 * picture shows which rows each choice keeps, nested in toggles (seen
 * 2026-11-03) or flattened, a parent with its sub-items' count and a
 * sub-item with the parent's property (seen 2026-11-11). A task database
 * names its rows tasks.
 */
export function DatabaseSubitemViewOptions({ view, writable, parent, child, childNumbered, onUpdateView }: {
  view: GraphView; writable: boolean; parent: string; child: string; childNumbered(number: number): string;
  onUpdateView(view: GraphView): void;
}) {
  const labels = databaseViewLabels();
  const names = labels.subitemPicture;
  const cards = view.type === "board" || view.type === "gallery" || view.type === "calendar";
  const subtasks = (view.config?.subtasks ?? {}) as Partial<DatabaseSubtaskViewConfig>;
  const mode: DisplayMode = subtasks.displayMode === "flattened" || subtasks.displayMode === "disabled" ? subtasks.displayMode : "show";
  // "상위 항목만" until another is chosen, as Notion's (seen 2026-10-11).
  const scope: FilterScope = subtasks.displayMode === "hidden" ? "parents" : subitemFilterScope(view.config);
  const set = (patch: Partial<DatabaseSubtaskViewConfig>) => onUpdateView({ ...view, config: { ...view.config,
    subtasks: { propertyId: "", toggleColumnId: "", ...subtasks, displayMode: mode, filterScope: scope, ...patch } } });
  const help = mode === "disabled" ? labels.subitemHelpDisabled
    : mode === "flattened" ? labels.subitemHelpFlattened[scope]
      : cards ? labels.subitemHelpCardProperty : labels.subitemHelpNested[scope];
  const scoped = mode === "flattened" || (mode === "show" && !cards);
  // Which rows a filter for "리펜" keeps under each choice, the rest faint.
  const filterRows: Record<FilterScope, SubitemsPictureRow[]> = mode === "flattened" ? {
    parents: [{ label: names.first, count: 5 }, { label: names.second, count: 12 }, { label: names.third, count: 8 }],
    parents_and_subitems: [{ label: names.first }, { label: names.spring, faded: true },
      { label: names.calendar, sub: true, parent }, { label: names.third }],
    subitems: [{ label: names.spring, faded: true }, { label: names.calendar, sub: true, parent },
      { label: names.summer, faded: true }, { label: names.mail, sub: true, parent }],
  } : {
    parents: [{ label: names.first, open: true }, { label: child, sub: true, faded: true }, { label: child, sub: true, faded: true },
      { label: names.second }, { label: names.third }],
    parents_and_subitems: [{ label: names.first }, { label: names.spring, open: true, faded: true },
      { label: names.calendar, sub: true }, { label: names.third }],
    subitems: [{ label: names.spring, open: true, faded: true }, { label: names.calendar, sub: true },
      { label: names.summer, open: true, faded: true }, { label: names.mail, sub: true }],
  };
  const displaySelect = (
    <NotionSelect ariaLabel={labels.subitemDisplayOptions} value={mode} disabled={!writable}
      options={[
        { value: "show", label: cards ? labels.subitemDisplayCardProperty : labels.subitemDisplayNested },
        { value: "flattened", label: labels.subitemDisplayFlattened },
        { value: "disabled", label: labels.subitemDisplayDisabled },
      ]}
      onChange={(value) => set({ displayMode: value as DisplayMode })} />
  );
  return (
    <div className={styles.subitemViewOptions} data-subitem-view-options>
      <span className={styles.fieldTitle}>{labels.subitemDisplayOptions}</span>
      {cards && mode === "show" ? (
        <SubitemsCardPicture parent={parent} child={childNumbered}
          person={labels.subitemPreviewPerson} status={labels.subitemPreviewStatus} />
      ) : cards ? (
        // A card for each row, a sub-item as one of them.
        <div className={styles.subitemsPreview} data-mode={mode} data-cards aria-hidden="true">
          <div><span>{parent}</span></div>
          <div><FileText size="tiny" /><span>{child}</span></div>
          <div><FileText size="tiny" /><span>{child}</span></div>
        </div>
      ) : (
        <SubitemsPicture display toggles={mode === "show"}
          rows={[{ label: parent, open: true }, { label: child, sub: true }, { label: child, sub: true }]} />
      )}
      {scoped ? displaySelect : <><p data-display-help>{help}</p>{displaySelect}</>}
      {scoped ? <>
        <span className={styles.fieldTitle}>{mode === "flattened" ? labels.subitemInclude : labels.subitemFilterOptions}</span>
        {cards ? null : <SubitemsPicture pill={names.pill} toggles={mode === "show"} rows={filterRows[scope]}
          slots={Math.max(...Object.values(filterRows).map((rows) => rows.length))} />}
        <p data-scope-help>{help}</p>
        <NotionSelect ariaLabel={mode === "flattened" ? labels.subitemInclude : labels.subitemFilterOptions} value={scope} disabled={!writable}
          options={[
            { value: "parents", label: labels.parentsOnly },
            { value: "parents_and_subitems", label: labels.parentsAndSubitems },
            { value: "subitems", label: labels.subitemsOnly },
          ]}
          onChange={(value) => set({ filterScope: value as FilterScope })} />
      </> : null}
    </div>
  );
}
