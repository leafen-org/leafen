import { useRef, useState } from "react";
import { i18next } from "@/i18n";
import { Plus, X } from "@/icons/leafen";
import type { DbProperty, ViewFilter } from "@/lib/types";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { PopoverLayer } from "@/lib/popoverLayer";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { AnchoredIconPicker } from "../page/AnchoredEmojiPicker";
import { BuiltInIconGlyph } from "../page/PageIcon";
import { parseNativeIconValue } from "../page/nativeIcons";
import { DashboardFilterPropertyPicker } from "./DashboardFilterPropertyPicker";
import { MultiSourceCard, MultiSourceHead, MultiSourceList } from "./DashboardMultiSourceCard";
import { DataSourceMark } from "./DataSourceMark";
import type { DashboardFilterSource } from "./dashboardFilterSources";
import { defaultOperator, defaultValue } from "./databaseFilterPolicy";
import { multiSourceKind, multiSourceMoreSources, multiSourceProperties, withMultiSourceProperties } from "./dashboardMultiSourceFilter";
import { PropertyIcon } from "./PropertyIcon";
import { propertyTypeLabel } from "./propertyTypes";
import menuStyles from "./TablePropertyMenu.module.css";
import styles from "./DashboardMultiSourceFilter.module.css";

const t = (key: string, options?: Record<string, unknown>) => i18next.t(`databaseView:${key}`, options);
const NO_RECT = { top: 0, bottom: 0, left: 0, right: 0 };
// Marks where the kind goes in its line, to set it apart in 500 (Notion's).
const KIND_MARK = "\u2063";

/**
 * A dashboard filter's "다중 출처 필터링" window under its pill, as Notion's
 * (seen twice on 2026-11-17 at 1440): its head; its icon, a button that opens
 * the icon list, and its name, a field that takes a name only once it names
 * two or more properties, its first property's name standing in it until
 * then; a line saying the filter applies to the properties chosen below,
 * named by their kind; a row for each
 * property it names — the property's mark and name over its source's mark
 * and name, and "×" ("출처 제거하기"); "+ 더 추가하기" while another widget
 * source holds a property of its kind, which lists those; and "완료하기",
 * which, as the head's "×", hands its place to the filter's condition. With
 * its last property taken away it is the card of "+ 필터에 추가하기" again,
 * whose closing takes the filter away.
 */
export function MultiSourceWindow({ anchor, filter, properties, sources, exclude, onChange, onRemove, onDone, onClose }: {
  anchor(): HTMLElement | null;
  filter: ViewFilter;
  /** Every widget source's properties, as the bar names them. */
  properties: DbProperty[];
  sources: readonly DashboardFilterSource[];
  /** The properties the dashboard's other filters name, which a new first property is not. */
  exclude: readonly string[];
  onChange(filter: ViewFilter): void;
  onRemove(): void;
  /** "완료하기" and "×": the filter's condition in its place. */
  onDone(): void;
  /** Escape or a press outside it. */
  onClose(): void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const empty = multiSourceProperties(filter, properties).length === 0;
  const leave = empty ? onRemove : onClose;
  const layer = useOverlayLayer(true, { kind: "popover", ref, restoreFocus: false, onEscape: leave, onOutsidePress: leave, inside: () => [anchor()] });
  const place = usePopoverPlacement(ref, (size, viewport) => placeAround(popoverRectOf(anchor()) ?? NO_RECT, size, viewport,
    { gap: 4, align: "start" }), [], undefined, false);
  return (
    <PopoverLayer>
      <div ref={ref} className={`${styles.window} ${empty ? styles.windowCard : ""}`} role="dialog" aria-label={t("multiSourceFiltering")}
        tabIndex={-1} data-multi-source-window style={{ ...popoverPlacementStyle(place), position: "fixed", zIndex: layer.zIndex }} onKeyDown={layer.onKeyDown}>
        <MultiSourceBody filter={filter} properties={properties} sources={sources} exclude={exclude}
          onChange={onChange} onRemove={onRemove} onDone={onDone} />
      </div>
    </PopoverLayer>
  );
}

/**
 * What a filter's "다중 출처 필터링" holds, in its window or, on a phone, in
 * a sheet whose head is its own (`sheet`): the filter's editor, or with none
 * of its properties left, the card that picks one again.
 */
export function MultiSourceBody({ filter, properties, sources, exclude, sheet = false, onChange, onRemove, onDone }: {
  filter: ViewFilter;
  properties: DbProperty[];
  sources: readonly DashboardFilterSource[];
  exclude: readonly string[];
  sheet?: boolean;
  onChange(filter: ViewFilter): void;
  onRemove(): void;
  onDone(): void;
}) {
  const named = multiSourceProperties(filter, properties);
  const start = (propertyId: string) => {
    const property = properties.find(({ id }) => id === propertyId);
    if (!property) return;
    onChange(withMultiSourceProperties({ ...filter, operator: defaultOperator(property, properties), value: defaultValue(property, properties) }, [propertyId]));
  };
  if (!named.length) {
    return <MultiSourceCard sheet={sheet} onClose={onRemove} list={(close) => <DashboardFilterPropertyPicker sources={sources} exclude={exclude}
      sheet={sheet} onPick={(id) => { close(); start(id); }} />} />;
  }
  return <MultiSourceEditor filter={filter} named={named} sources={sources} sheet={sheet} onChange={onChange} onDone={onDone} />;
}

function MultiSourceEditor({ filter, named, sources, sheet, onChange, onDone }: {
  filter: ViewFilter;
  named: DbProperty[];
  sources: readonly DashboardFilterSource[];
  sheet: boolean;
  onChange(filter: ViewFilter): void;
  onDone(): void;
}) {
  const [iconAnchor, setIconAnchor] = useState<{ top: number; bottom: number; left: number; right: number } | null>(null);
  const [adding, setAdding] = useState(false);
  const addRef = useRef<HTMLButtonElement>(null);
  const first = named[0]!;
  const multiSource = filter.multiSource ?? { propertyIds: named.map(({ id }) => id) };
  const ids = named.map(({ id }) => id);
  const icon = multiSource.icon ? parseNativeIconValue(multiSource.icon) : undefined;
  const more = multiSourceMoreSources(filter, sources);
  const [before, after = ""] = t("multiSourceAppliesTo", { kind: KIND_MARK }).split(KIND_MARK);
  const sourceOf = (property: DbProperty) => sources.find(({ source }) => source.id === property.databaseId);
  const set = (patch: Partial<NonNullable<ViewFilter["multiSource"]>>) => onChange({ ...filter, multiSource: { ...multiSource, ...patch } });
  return (
    <>
      {sheet ? null : <MultiSourceHead onClose={onDone} />}
      <div className={styles.nameRow}>
        <button type="button" className={menuStyles.headIcon} aria-label={t("multiSourceChangeIcon")} aria-haspopup="dialog"
          aria-expanded={iconAnchor !== null} data-multi-source-icon onClick={(event) => {
            const rect = event.currentTarget.getBoundingClientRect();
            setIconAnchor({ top: rect.top, bottom: rect.bottom, left: rect.left, right: rect.right });
          }}>
          {icon ? <BuiltInIconGlyph icon={icon} size={18} /> : <PropertyIcon property={first} size="regular" />}
        </button>
        {/* Its first property's name stands in it, the field shut, until it names two or more (Notion's). */}
        <input className={menuStyles.name} value={named.length > 1 ? multiSource.name ?? "" : ""} placeholder={first.name}
          aria-label={t("multiSourceName")} disabled={named.length < 2} data-multi-source-name
          onChange={(event) => set({ name: event.currentTarget.value })} />
      </div>
      <p className={styles.kind}>{before}<strong>{propertyTypeLabel(multiSourceKind(first.type) as DbProperty["type"])}</strong>{after}</p>
      <div className={styles.rows} data-multi-source-rows>
        {named.map((property) => {
          const source = sourceOf(property);
          return (
            <div key={property.id} className={styles.row} data-multi-source-row={property.id}>
              <span className={styles.rowIcon} aria-hidden="true"><PropertyIcon property={property} size="regular" /></span>
              <span className={styles.rowText}>
                <span className={styles.rowName}>{property.name}</span>
                {source ? <span className={styles.rowSource}><DataSourceMark source={source.source} size="small" /><span>{source.name}</span></span> : null}
              </span>
              <button type="button" className={styles.rowRemove} aria-label={t("multiSourceRemoveSource")} data-multi-source-remove={property.id}
                onClick={() => onChange(withMultiSourceProperties(filter, ids.filter((id) => id !== property.id)))}>
                <X size="tiny" aria-hidden="true" />
              </button>
            </div>
          );
        })}
      </div>
      <div className={styles.actions}>
        {more.length ? (
          <button ref={addRef} type="button" className={styles.more} aria-haspopup="dialog" aria-expanded={adding}
            data-multi-source-add-more onClick={() => setAdding(!adding)}>
            <Plus size="tiny" aria-hidden="true" /><span>{t("multiSourceAddMore")}</span>
          </button>
        ) : null}
        <button type="button" className={styles.primary} data-multi-source-done onClick={onDone}>{t("multiSourceDone")}</button>
      </div>
      {adding && more.length ? (
        <MultiSourceList sheet={sheet} anchor={addRef} onClose={() => setAdding(false)}>
          <DashboardFilterPropertyPicker sources={more} sheet={sheet}
            onPick={(id) => { setAdding(false); onChange(withMultiSourceProperties(filter, [...ids, id])); }} />
        </MultiSourceList>
      ) : null}
      {iconAnchor ? (
        <AnchoredIconPicker anchor={iconAnchor} marker="multi-source-icon-picker" onClose={() => setIconAnchor(null)}
          picker={{
            label: t("multiSourceChangeIcon"),
            onPickBuiltInIcon: (picked) => { set({ icon: picked }); setIconAnchor(null); },
            onRemove: () => { set({ icon: undefined }); setIconAnchor(null); },
          }} />
      ) : null}
    </>
  );
}
