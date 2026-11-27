import type { ReactNode } from "react";
import type { GraphProperty, GraphView } from "@/contentGraph";
import { ChevronRight } from "@/icons/leafen";
import { DragHandleIcon, EyeIcon, EyeSlashIcon } from "../icons";
import { databaseViewLabels } from "./databaseViewLabels";
import { PropertyIcon } from "./PropertyIcon";
import {
  viewConfigAfterAllPropertiesShown,
  viewConfigAfterPropertyMoved,
  viewConfigAfterPropertyShown,
  viewPropertyVisibility,
  viewTitleFree,
  viewTitleMoves,
} from "./propertyVisibilityModel";
import { timelineTableAsView, timelineTableFromView } from "./timelineTableModel";
import { useDragReorder } from "@/lib/useDragReorder";
import styles from "./DatabasePropertyVisibilityList.module.css";

const PROPERTY_DRAG = "application/x-leafen-view-property";

/**
 * Notion's "속성 표시 여부" list — a database view's and the library's
 * alike: its groups (PropertyVisibilityGroup); while a search holds words a
 * phone's rows go without their drag handle, as Notion's (seen 2026-11-25
 * in iPhone Safari at 402).
 */
export function PropertyVisibilityListBox({ searching, opensProperties, children }: {
  searching: boolean;
  opensProperties?: boolean;
  children: ReactNode;
}) {
  return (
    <div className={styles.list} data-property-visibility data-searching={searching ? "true" : undefined}
      data-opens-properties={opensProperties ? "true" : undefined}>
      {children}
    </div>
  );
}

/**
 * One group of Notion's "속성 표시 여부" — a database view's and the
 * library's alike: its name with its "모두 숨기기" or "모두 표시하기" (left out
 * while searching), then its rows (styles.row), drawn by this list's rules,
 * the phone sheet's under [data-phone-settings].
 */
export function PropertyVisibilityGroup({ title, kind, action, children }: {
  title: string;
  kind: "shown" | "hidden";
  action?: { label: string; disabled?: boolean; data?: Record<string, string>; onPress(): void } | null;
  children: ReactNode;
}) {
  return (
    <div className={styles.group} role="group" aria-label={title} data-property-visibility-group={kind}>
      <div className={styles.groupHead}>
        <span>{title}</span>
        {action ? (
          <span className={styles.groupActionLine}>
            <button type="button" className={styles.groupAction} disabled={action.disabled} {...action.data} onClick={action.onPress}>
              {action.label}
            </button>
          </span>
        ) : null}
      </div>
      {children}
    </div>
  );
}

/**
 * A view's properties as Notion's "속성 표시 여부" lists them (seen
 * 2026-09-30): "표에 표시하기" with "모두 숨기기", then "표에서 숨기기" with
 * "모두 표시하기", each row a drag handle, the property's icon and name and
 * an eye that shows or hides it. Searching leaves out the "모두" links.
 * A timeline's "표 속성" adds "›" to each, which opens its settings
 * (Notion's, seen 2026-11-12).
 */
export function DatabasePropertyVisibilityList({ view, properties, search, writable, onUpdateView, timelineTable, other, onOpenProperty }: {
  view?: GraphView;
  properties: readonly GraphProperty[];
  search: string;
  writable: boolean;
  onUpdateView?(view: GraphView): void;
  /** Lists a timeline's table columns rather than what its bars show. */
  timelineTable?: boolean;
  /** Lists what something else shows, in a view's terms (timelineTableAsView, relationPickerAsView). */
  other?: { asView: { type: string; config: Record<string, unknown>; titleFirst?: boolean }; shownIn: string; hiddenIn: string; onChange(config: Record<string, unknown>): void };
  onOpenProperty?(propertyId: string): void;
}) {
  const labels = databaseViewLabels();
  const listed = other?.asView ?? (timelineTable && view ? timelineTableAsView(properties, view.config) : view!);
  const shownIn = other?.shownIn ?? labels.propertiesShownIn(timelineTable ? "table" : listed.type);
  const hiddenIn = other?.hiddenIn ?? labels.propertiesHiddenIn(timelineTable ? "table" : listed.type);
  const { shown, hidden } = viewPropertyVisibility(listed, properties);
  const query = search.trim().toLocaleLowerCase();
  const matches = (property: GraphProperty) => !query || property.name.toLocaleLowerCase().includes(query);
  const update = (config: Record<string, unknown> | null) => {
    if (!config) return;
    if (other) return other.onChange(config);
    if (!view) return;
    onUpdateView?.({ ...view, config: timelineTable
      ? { ...view.config, timelineTableProperties: timelineTableFromView(properties, view.config, config) } : config });
  };
  // The title stays shown, but in a timeline's table panel (timelineTableAsView) and on its bars beside its table
  // (viewTitleFree), and in its place, but there, in a table, a list and a timeline, where it moves as any property (viewTitleMoves).
  const keptTitle = (property: GraphProperty) => property.type === "title" && !viewTitleFree(listed);
  const pinnedTitle = (property: GraphProperty) => keptTitle(property) && !viewTitleMoves(listed);
  const reorder = useDragReorder(PROPERTY_DRAG, (sourceId, targetId, side) =>
    update(viewConfigAfterPropertyMoved(listed, properties, sourceId, targetId, side)), { live: true });

  const group = (list: readonly GraphProperty[], isShown: boolean) => {
    const rows = list.filter(matches);
    if (!rows.length) return null;
    // "모두 숨기기" stays even over the title alone, which it leaves shown, as Notion's (seen 2026-11-06).
    const allAction = isShown
      ? { label: labels.hideAll, show: false }
      : { label: labels.showAll, show: true };
    return (
      <PropertyVisibilityGroup title={isShown ? shownIn : hiddenIn} kind={isShown ? "shown" : "hidden"}
        action={!query && !other && writable
          ? { label: allAction.label, onPress: () => update(viewConfigAfterAllPropertiesShown(listed, properties, allAction.show)) } : null}>
        {rows.map((property) => {
          const title = pinnedTitle(property);
          const index = list.indexOf(property);
          // A row moves only within its own list, and never before a title that stays first;
          // past one that the view put further down (propertyVisibilityModel.ts) it does.
          const titleFirst = (at: number) => at === 0 && !!list[0] && pinnedTitle(list[0]);
          const inList = (sourceId: string) => !titleFirst(index) && list.some(({ id }) => id === sourceId);
          return (
            <div key={property.id} className={styles.row} data-view-property={property.id} {...reorder.row(property.id, inList)}>
              <button type="button" className={styles.handle} disabled={!writable || title}
                aria-label={labels.dragToMoveProperty(property.name)}
                {...reorder.handle(property.id, writable && !title, {
                  above: titleFirst(index - 1) ? undefined : list[index - 1]?.id, below: list[index + 1]?.id,
                })}>
                <DragHandleIcon size="regular" aria-hidden="true" />
              </button>
              <span className={styles.icon}><PropertyIcon property={property} size="regular" /></span>
              <span className={styles.name}>{property.name}</span>
              <button type="button" className={styles.eye} aria-pressed={isShown} disabled={!writable || keptTitle(property)}
                aria-label={labels.togglePropertyShown(property.name)}
                onClick={() => update(viewConfigAfterPropertyShown(listed, properties, property.id, !isShown))}>
                {isShown ? <EyeIcon size="regular" aria-hidden="true" /> : <EyeSlashIcon size="regular" aria-hidden="true" />}
              </button>
              {onOpenProperty ? (
                <button type="button" className={styles.open} aria-label={labels.propertySettingsOf(property.name)}
                  onClick={() => onOpenProperty(property.id)}>
                  <ChevronRight size="small" aria-hidden="true" />
                </button>
              ) : null}
            </div>
          );
        })}
      </PropertyVisibilityGroup>
    );
  };

  const shownGroup = group(shown, true);
  const hiddenGroup = group(hidden, false);
  return (
    <PropertyVisibilityListBox searching={Boolean(query)} opensProperties={Boolean(onOpenProperty)}>
      {shownGroup}
      {hiddenGroup}
      {!shownGroup && !hiddenGroup && <div className={styles.empty}>{labels.noSearchResults}</div>}
    </PropertyVisibilityListBox>
  );
}
