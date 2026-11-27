import { useRef, useState, type ReactNode } from "react";
import type { GraphView } from "@/contentGraph";
import type { DbProperty, FilterGroup, ViewFilter } from "@/lib/types";
import { DatabaseFilterMenu } from "./DatabaseFilterSettings";
import { defaultOperator, defaultValue } from "./DatabaseFilterEditors";
import { QuickFilterSheet } from "./DatabaseQuickFilterSheet";
import { useDatabaseFilterBar } from "./databaseFilterBar";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { viewWithQuickFilterInAdvanced } from "./databaseToolbarModel";

type QuickFilter = ViewFilter | FilterGroup;

/**
 * The "필터 추가" window of a touch screen wider than a phone
 * (DatabaseTouchMenuWindow), as Notion's iPad (seen 2026-11-14 and
 * 2026-11-15 in iPad Safari at 834): the desktop's "필터 기준" list; a
 * property picked goes on in the same place to its value — the phone's
 * filter screen (QuickFilterSheet), there a window of its own over this one
 * (sheetWindow.ts): "⋯" at its left, its condition in the middle (its
 * choices rising from the bottom), "완료" at its right and the value under
 * them — while its pill joins the bar. "완료" closes both, a filter left
 * without its value gone, as the phone's.
 */
export function DatabaseTouchFilterMenu({ view, properties, snapshot, writable, advanced, onAdvancedAdded, onUpdateView, onClose }: {
  view: GraphView;
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  writable: boolean;
  advanced: { count: number; editor: ReactNode; add(): void };
  onAdvancedAdded?(): void;
  onUpdateView(view: GraphView): void;
  onClose(): void;
}) {
  const [editing, setEditing] = useState<string | null>(null);
  const bar = useDatabaseFilterBar();
  const editorRef = useRef<HTMLDivElement>(null);
  const quick = (Array.isArray(view.config.quickFilters) ? view.config.quickFilters : []) as QuickFilter[];
  const commit = (next: QuickFilter[]) => onUpdateView({ ...view, config: { ...view.config, quickFilters: next.length ? next : undefined } });
  const filterOf = (propertyId: string) => quick.find((filter): filter is ViewFilter => (filter as ViewFilter).propertyId === propertyId);
  const filter = editing ? filterOf(editing) : undefined;
  const property = filter ? properties.find(({ id }) => id === filter.propertyId) : undefined;
  return (
    <>
      <DatabaseFilterMenu properties={properties} writable={writable} advancedCount={advanced.count}
        advancedEditor={advanced.editor} onAddAdvanced={advanced.add} onAdvancedAdded={onAdvancedAdded}
        onPick={(propertyId) => {
          const picked = properties.find(({ id }) => id === propertyId);
          if (!picked) return;
          if (!filterOf(propertyId)) commit([...quick, { propertyId, operator: defaultOperator(picked, properties), value: defaultValue(picked, properties) }]);
          // Its pill shows on the bar behind the window at once, as Notion's iPad.
          if (bar && !bar.shown) bar.toggle();
          setEditing(propertyId);
        }} />
      {filter && property ? (
        <QuickFilterSheet filter={filter} property={property} properties={properties} snapshot={snapshot} sheetRef={editorRef}
          onChange={(patch) => commit(quick.map((current) => current === filter ? { ...filter, ...patch } : current))}
          onRemove={() => { commit(quick.filter((current) => current !== filter)); onClose(); }}
          onToAdvanced={() => { onUpdateView(viewWithQuickFilterInAdvanced(view, quick.indexOf(filter))); onClose(); }}
          onClose={onClose} />
      ) : null}
    </>
  );
}
