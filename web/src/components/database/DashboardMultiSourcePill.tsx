import { useCallback, useEffect, useMemo, useRef, useState, type ComponentProps, type ComponentType, type CSSProperties } from "react";
import { i18next } from "@/i18n";
import type { ViewFilter } from "@/lib/types";
import type { QuickFilterPill } from "./DatabaseQuickFilterBar";
import { MultiSourceWindow } from "./DashboardMultiSourceWindow";
import { MultiSourceEditContext } from "./dashboardMultiSourceEdit";
import { groupedSources, useDashboardFilterSources, type DashboardFilterSource } from "./dashboardFilterSources";
import { isMultiSourceFilter, multiSourceProperties, multiSourceShownProperty, takeMultiSourcePick, withMultiSourceProperties } from "./dashboardMultiSourceFilter";
import styles from "./DashboardMultiSourceFilter.module.css";

type PillProps = ComponentProps<typeof QuickFilterPill>;

/**
 * A filter bar's pill: the bar's own (`Pill`), but on a dashboard whose
 * widgets show more than one source each filter can name a property of each
 * of them, from its condition's "⋯" › "⚙ 다중 출처 필터링" or from the lists'
 * "⚙ 다중 출처 필터링" card (MultiSourcePill).
 */
export function FilterBarPill({ Pill, ...props }: PillProps & { Pill: ComponentType<PillProps> }) {
  const sources = groupedSources(useDashboardFilterSources());
  return sources ? <MultiSourcePill Pill={Pill} sources={sources} {...props} /> : <Pill {...props} />;
}

/**
 * A dashboard filter's pill, as Notion's (seen twice on 2026-11-17 at 1440):
 * named and marked as the filter is — its given name once it names two or
 * more properties, how many in a tag on its mark, "필터" with none — and,
 * from its condition's "⋯" › "⚙ 다중 출처 필터링" or as a property picked from
 * the card's list comes in, its window (MultiSourceWindow) in the condition's
 * place under it, "완료하기" bringing the condition back.
 */
function MultiSourcePill({ Pill, sources, filter, properties, open, onOpenChange, onChange, onRemove, ...rest }: PillProps & {
  Pill: ComponentType<PillProps>;
  sources: readonly DashboardFilterSource[];
}) {
  // A property picked from the card's list comes in as any filter does, then opens this window.
  const [editing, setEditing] = useState(() => takeMultiSourcePick(filter.propertyId));
  const multi = isMultiSourceFilter(filter);
  const shownFilter: ViewFilter = !multi && editing ? withMultiSourceProperties(filter, [filter.propertyId]) : filter;
  useEffect(() => {
    if (editing && !multi) onChange(shownFilter);
    // Once, as it comes in.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const named = isMultiSourceFilter(shownFilter) ? multiSourceProperties(shownFilter, properties) : [];
  const empty = isMultiSourceFilter(shownFilter) && !named.length;
  // With none of its properties left its pill says "필터" alone, no condition (Notion's).
  const pillFilter = empty ? { ...shownFilter, value: undefined } : shownFilter;
  const shown = useMemo(() => isMultiSourceFilter(shownFilter) ? properties.map((property) => property.id !== shownFilter.propertyId ? property
    : multiSourceShownProperty(shownFilter, property, properties, i18next.t("databaseView:multiSourceEmptyPill"))) : properties, [properties, shownFilter]);
  const hostRef = useRef<HTMLSpanElement>(null);
  const edit = useCallback(() => {
    if (!isMultiSourceFilter(filter)) onChange(withMultiSourceProperties(filter, [filter.propertyId]));
    setEditing(true);
  }, [filter, onChange]);
  const count = named.length > 1 ? named.length : 0;
  const style = { display: "contents", ...(count ? { "--multi-source-count": `"${count}"` } : {}) } as CSSProperties;
  return (
    <span ref={hostRef} className={styles.pill} style={style} data-multi-source-count={count || undefined} data-multi-source-empty={empty ? "" : undefined}>
      <MultiSourceEditContext.Provider value={edit}>
        <Pill {...rest} filter={pillFilter} properties={shown} open={open && !editing} onChange={onChange} onRemove={onRemove}
          onOpenChange={(next) => {
            // With none of its properties left, it opens on its card again.
            if (empty) { setEditing(next); return; }
            setEditing(false);
            onOpenChange(next);
          }} />
      </MultiSourceEditContext.Provider>
      {editing ? (
        <MultiSourceWindow anchor={() => hostRef.current?.querySelector<HTMLElement>("[data-quick-filter-box]") ?? null} filter={shownFilter}
          properties={properties} sources={sources} exclude={[]} onChange={onChange}
          onRemove={() => { setEditing(false); onRemove(); }}
          onDone={() => { setEditing(false); onOpenChange(true); }}
          onClose={() => { setEditing(false); onOpenChange(false); }} />
      ) : null}
    </span>
  );
}
