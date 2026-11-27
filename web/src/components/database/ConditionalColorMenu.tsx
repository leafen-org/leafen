import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { GraphProperty, GraphView } from "@/contentGraph";
import { Plus } from "@/icons/leafen";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { DbProperty, FilterGroup, ViewFilter } from "@/lib/types";
import { useDragReorder } from "@/lib/useDragReorder";
import {
  conditionalColorHasOneCondition,
  conditionalColorSettings,
  newConditionalColorSetting,
  type ConditionalColorSetting,
} from "./conditionalColorModel";
import { FilterGroupEditor, databasePropertyOptions, defaultOperator, defaultValue } from "./DatabaseFilterEditors";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { updateFilterGroupAt } from "./databaseToolbarModel";
import { databaseViewLabels } from "./databaseViewLabels";
import { ConditionalColorCard } from "./ConditionalColorCard";
import { ConditionalColorPreview } from "./ConditionalColorPreview";
import { ConditionalColorPropertySheet, ConditionalColorSheet } from "./ConditionalColorSheet";
import { DatabasePropertyPicker } from "./DatabasePropertyPicker";
import { withFilterAdded, withFilterRemoved, withGroupAdded, withGroupRemoved } from "./filterGroupTerms";
import cardStyles from "./ConditionalColorCard.module.css";
import filterStyles from "./filterGroups.module.css";
import styles from "./databaseSheet";
import { fieldEscape } from "@/lib/fieldKeyBoundary";

const COLOR_DRAG = "application/x-leafen-conditional-color";

function colorSettingId() {
  return `color-${Math.random().toString(36).slice(2, 10)}`;
}

const CONDITIONAL_COLOR_SKIPPED: ReadonlySet<string> = new Set(["unique_id", "url", "email", "phone", "files", "place", "button"]);

/** The properties a new setting colors by, under its button, as Notion's (seen 2026-10-24). Escape gives the
 * button the keys back, ringed, as Notion's (2026-11-15, Chrome, no script before the action; judged by the
 * screen and one typed letter: "+ 새로운 색상 설정하기" pressed, Escape — it ringed, the letter landing nowhere). */
function AddSetting({ label, first, properties, onPick }: {
  label: string; first: boolean; properties: DbProperty[]; onPick(property: DbProperty): void;
}) {
  const labels = databaseViewLabels();
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null), pickerRef = useRef<HTMLDivElement>(null);
  const close = (restore = false) => { setOpen(false); if (restore) buttonRef.current?.focus({ preventScroll: true }); };
  const place = usePopoverPlacement(pickerRef, open ? (size, viewport) => placeAround(popoverRectOf(buttonRef.current)!, size, viewport,
    { gap: 6, align: "start", alignFallback: true }) : null, [open], undefined,
  { kind: "menu", restoreFocus: false, onEscape: () => close(true), onOutsidePress: () => close(), inside: () => [buttonRef.current] });
  return <>
    <button ref={buttonRef} type="button" className={first ? cardStyles.addFirst : cardStyles.addMore} data-conditional-color-add
      data-first={first ? "true" : undefined} disabled={!properties.length} aria-haspopup="dialog" aria-expanded={open}
      onClick={() => setOpen(!open)}>
      <Plus size="tiny" aria-hidden="true" />{label}
    </button>
    {open && typeof document !== "undefined" && createPortal(
      <div ref={pickerRef} role="dialog" aria-label={labels.searchProperties} className={cardStyles.picker}
        style={{ position: "fixed", ...popoverPlacementStyle(place) }} onKeyDown={(event) => {
          if (fieldEscape(event)) { event.preventDefault(); event.stopPropagation(); close(true); }
        }}>
        <DatabasePropertyPicker placeholder={labels.searchProperties} properties={properties}
          onPick={(picked) => {
            const property = properties.find(({ id }) => id === picked.id);
            if (property) onPick(property);
            close(true);
          }} />
      </div>, document.body)}
  </>;
}

/**
 * The Conditional color menu, as Notion's (seen 2026-10-24): with no
 * settings, a preview, a line on what it does and a blue "새로운 색상
 * 설정하기"; else a card per setting and "+ 더 추가하기" under them, each
 * asking first for the property it colors by.
 */
export function ConditionalColorMenu({
  view,
  properties,
  dbProperties,
  filterSnapshot,
  heading = true,
  sheet,
  onUpdateView,
}: {
  view: GraphView;
  properties: readonly GraphProperty[];
  dbProperties: DbProperty[];
  filterSnapshot: DatabaseFilterSnapshot;
  /** Whether it names itself; the view settings panel's heading does it there. */
  heading?: boolean;
  /** In a phone's settings sheet, its settings as Notion's sheet groups (ConditionalColorSheet). */
  sheet?: boolean;
  onUpdateView(view: GraphView): void;
}) {
  const labels = databaseViewLabels();
  const [adding, setAdding] = useState(false);
  const settings = conditionalColorSettings(view.config);
  const commit = (next: ConditionalColorSetting[]) => onUpdateView({
    ...view, config: { ...view.config, conditionalColors: next.length ? next : undefined },
  });
  const change = (id: string, patch: (setting: ConditionalColorSetting) => ConditionalColorSetting) =>
    commit(settings.map((setting) => setting.id === id ? patch(setting) : setting));
  const keys = settings.map(({ id }) => id);
  const reorder = useDragReorder(COLOR_DRAG, (sourceId, targetId, side) => {
    const order = keys.filter((key) => key !== sourceId);
    order.splice(order.indexOf(targetId) + (side === "after" ? 1 : 0), 0, sourceId);
    commit(order.map((key) => settings[keys.indexOf(key)]));
  }, { live: true });
  // An ID, a link, an email, a phone number, files, a place or a button colors nothing (Notion's list, seen 2026-11-02).
  const shown = dbProperties.filter(({ id, type }) => !CONDITIONAL_COLOR_SKIPPED.has(type) && properties.some((property) => property.id === id));
  const addable = shown.length ? shown : dbProperties;
  const addFor = (property: DbProperty) => commit([...settings, newConditionalColorSetting(property, colorSettingId(), dbProperties)]);
  if (sheet && settings.length > 0) {
    return <ConditionalColorSheet view={view} settings={settings} dbProperties={dbProperties} addable={addable} filterSnapshot={filterSnapshot}
      onCommit={commit} onAdd={addFor} />;
  }
  return (
    <div className={sheet ? cardStyles.sheet : cardStyles.screen} data-heading={heading ? "" : undefined}>
      {heading && <div className={styles.toolbarMenuLabel}>{labels.conditionalColor}</div>}
      {settings.length === 0 && <ConditionalColorPreview className={sheet ? styles.conditionalColorPreview : cardStyles.preview} layout={view.type} />}
      {settings.length === 0 && (
        <div className={sheet ? styles.layoutGroupLabel : cardStyles.emptyText} data-conditional-color-empty>{labels.conditionalColorEmpty}</div>
      )}
      {settings.length > 0 && (
        <div className={cardStyles.cards} data-conditional-colors>
          {settings.map((setting, index) => {
            const tree = setting.filterGroup;
            const edit = (next: FilterGroup) => change(setting.id, (current) => ({ ...current, filterGroup: next }));
            const target = dbProperties.find(({ id }) => id === setting.propertyId) ?? dbProperties[0];
            const conditions = conditionalColorHasOneCondition(setting) ? undefined : (
              <FilterGroupEditor group={tree} path={[]} props={dbProperties} snapshot={filterSnapshot}
                propertyOptions={databasePropertyOptions(dbProperties)}
                onSetConjunction={(path, conjunction) => edit(updateFilterGroupAt(tree, path, (group) => ({ ...group, conjunction })))}
                onUpdateFilter={(path, filterIndex, patch) => edit(updateFilterGroupAt(tree, path, (group) => ({
                  ...group, filters: group.filters.map((filter, current) => current === filterIndex ? { ...filter, ...patch } as ViewFilter : filter),
                })))}
                onRemoveFilter={(path, filterIndex) => edit(updateFilterGroupAt(tree, path, (group) => withFilterRemoved(group, filterIndex)))}
                onRemoveGroup={(path, groupIndex) => edit(updateFilterGroupAt(tree, path, (group) => withGroupRemoved(group, groupIndex)))}
                onEdit={(path, next) => edit(updateFilterGroupAt(tree, path, next))}
                onAddFilter={(path) => {
                  if (target) {
                    edit(updateFilterGroupAt(tree, path, (group) => withFilterAdded(group,
                      { propertyId: target.id, operator: defaultOperator(target, dbProperties), value: defaultValue(target, dbProperties) })));
                  }
                }}
                onAddGroup={(path) => edit(updateFilterGroupAt(tree, path, (group) => withGroupAdded(group, { conjunction: "and", filters: [] })))} />
            );
            return (
              <ConditionalColorCard key={setting.id} view={view} setting={setting} index={index} dbProperties={dbProperties}
                filterSnapshot={filterSnapshot} reorder={reorder} handle={{ above: keys[index - 1], below: keys[index + 1] }}
                conditions={conditions} onChange={(patch) => change(setting.id, patch)}
                onDelete={() => commit(settings.filter((current) => current.id !== setting.id))} />
            );
          })}
        </div>
      )}
      {sheet ? (
        <button type="button" className={filterStyles.addBtn} data-conditional-color-add data-first="true" disabled={!addable.length}
          onClick={() => setAdding(true)}>
          <Plus size="tiny" aria-hidden="true" />{labels.conditionalColorNew}
        </button>
      ) : (
        <AddSetting label={settings.length ? labels.conditionalColorAddMore : labels.conditionalColorNew} first={settings.length === 0}
          properties={addable} onPick={addFor} />
      )}
      {adding && <ConditionalColorPropertySheet properties={addable} onPick={addFor} onClose={() => setAdding(false)} />}
    </div>
  );
}
