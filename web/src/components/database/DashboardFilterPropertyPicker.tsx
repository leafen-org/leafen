import { Fragment, useMemo, useRef, useState } from "react";
import { i18next } from "@/i18n";
import { useEdgeFade } from "@/lib/edgeFade";
import { DotsHorizontal, Settings } from "@/icons/leafen";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import type { GraphProperty } from "@/contentGraph";
import { PhoneSheetGroup, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import type { DashboardFilterSource } from "./dashboardFilterSources";
import { PropertyIcon } from "./PropertyIcon";
import { MultiSourceCard, MultiSourceFooter, closeHoldingWindow } from "./DashboardMultiSourceCard";
import { pickForMultiSource } from "./dashboardMultiSourceFilter";
import { databaseViewLabels } from "./databaseViewLabels";
import { SearchClearButton } from "@/components/PageActionSearch";
import pickerStyles from "./DatabasePropertyPicker.module.css";
import styles from "./DashboardFilterPropertyPicker.module.css";

/** How many of a source's properties show before its "N개 더 보기". */
const SHOWN = 4;

type Row = { key: string; label: string; property?: GraphProperty; more?: string };

/**
 * A dashboard's "필터 기준" while its widgets show more than one source, as
 * Notion's (seen twice on 2026-11-17 at 1440 in the Business trial's synthetic
 * dashboard: "AB03c 1105 · 1개 보기", "작업 트래커 · 3개 보기"): its widget
 * sources by name, each under its name in 12px grey with how many widgets
 * show it in 10px after it, then four of its properties by name — its name
 * property among them — and "⋯ N개 더 보기", which shows the rest of that
 * source's; a search lists every source's matches under their names. The
 * search stays put while the sources scroll under it, their ends fading as
 * the property pickers' do (useEdgeFade). A dashboard of one source lists
 * its properties as any view's picker does (DatabaseFilterMenu). A phone lays
 * them out as its sheet groups, the source's name over each (w1554, iPhone 17
 * 402). A toolbar's and a bar's list — a phone's "속성 필터링" too, in a group
 * of its own — end in "⚙ 다중 출처 필터링", whose card
 * takes the list's place (DashboardMultiSourceCard); a property picked from
 * the list the card opens starts a filter over several sources' properties
 * (dashboardMultiSourceFilter.ts).
 */
export function DashboardFilterPropertyPicker({ sources, exclude = [], sheet = false, multiSource = false, onClose, onPick }: {
  sources: readonly DashboardFilterSource[];
  /** Properties it already filters, which a phone's "+ 필터 추가하기" leaves out. */
  exclude?: readonly string[];
  sheet?: boolean;
  /** Ends in "⚙ 다중 출처 필터링", as a toolbar's "필터" and a bar's "+ 필터" (Notion's, seen 2026-11-17). */
  multiSource?: boolean;
  /** Closes the window holding it, from the card's "×"; else as its Escape does. */
  onClose?(): void;
  onPick(propertyId: string): void;
}) {
  const labels = databaseViewLabels();
  const [search, setSearch] = useState("");
  const [opened, setOpened] = useState<ReadonlySet<string>>(() => new Set());
  const [card, setCard] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null), listRef = useRef<HTMLDivElement>(null), popoverRef = useRef<HTMLDivElement>(null);
  const query = search.trim().toLocaleLowerCase();
  const groups = useMemo(() => sources.flatMap(({ source, name, properties, views }) => {
    const found = properties.filter(({ id, name }) => !exclude.includes(id) && (!query || name.toLocaleLowerCase().includes(query)));
    if (!found.length) return [];
    const whole = Boolean(query) || opened.has(source.id) || found.length <= SHOWN;
    const rows: Row[] = (whole ? found : found.slice(0, SHOWN)).map((property) => ({ key: property.id, label: property.name, property }));
    if (!whole) rows.push({ key: `more:${source.id}`, label: i18next.t("databaseView:showMoreDataSources", { count: found.length - SHOWN }), more: source.id });
    return [{ id: source.id, title: name, views, rows }];
  }), [exclude, opened, query, sources]);
  const items = useMemo(() => groups.flatMap(({ rows }) => rows), [groups]);
  // Its ends fade 1.7px a pixel scrolled, up to 34px, as Notion's (`[data-edge-fade]`).
  useEdgeFade(listRef, items, 1.7);
  const pick = (row: Row) => {
    if (row.property) onPick(row.property.id);
    else if (row.more) setOpened((current) => new Set([...current, row.more!]));
  };
  const picker = useNativeAutocomplete({ items, query: search, onQuery: setSearch, inputRef, listRef, popoverRef,
    keyOf: (row) => row.key, textOf: (row) => row.label, onPick: pick, label: labels.filterBy, commitTab: false });
  const head = (group: (typeof groups)[number]) => (
    <div className={sheet ? styles.sheetHead : styles.head} role="presentation" data-dashboard-filter-source={group.id}>
      <span className={styles.title}>{group.title}</span>
      <span className={styles.views}><span>{databaseViewLabels().viewsCount(group.views)}</span></span>
    </div>
  );
  if (card) {
    return <MultiSourceCard sheet={sheet} onClose={(button) => (onClose ? onClose() : closeHoldingWindow(button))}
      list={(close) => <DashboardFilterPropertyPicker sources={sources} exclude={exclude} sheet={sheet}
        onPick={(id) => { close(); pickForMultiSource(id); onPick(id); }} />} />;
  }
  if (sheet) {
    return (
      <div ref={popoverRef} className={`${sheetStyles.stack} ${sheetStyles.propertyRows}`} data-property-picker data-property-picker-sheet data-dashboard-filter-picker>
        <PhoneSheetGroup>
          <label className={`${sheetStyles.row} ${pickerStyles.sheetSearchBare}`}>
            <input ref={inputRef} className={pickerStyles.sheetSearch} placeholder={labels.filterBy} aria-label={labels.filterBy}
              value={search} onChange={(event) => setSearch(event.currentTarget.value)} />
            <SearchClearButton value={search} inputRef={inputRef} onClear={() => setSearch("")} />
          </label>
        </PhoneSheetGroup>
        {groups.map((group) => (
          <Fragment key={group.id}>
            {head(group)}
            <PhoneSheetGroup>
              {group.rows.map((row) => (
                <PhoneSheetRow key={row.key} more={false} muted={Boolean(row.more)} onPress={() => pick(row)}
                  icon={row.property ? <PropertyIcon property={row.property} size="wide" /> : <DotsHorizontal size="regular" />}
                  label={row.label} data={{ "data-picker-property": row.property?.id, "data-dashboard-filter-more": row.more }} />
              ))}
            </PhoneSheetGroup>
          </Fragment>
        ))}
        {!groups.length && <PhoneSheetGroup><div className={sheetStyles.row}>{labels.noSearchResults}</div></PhoneSheetGroup>}
        {multiSource ? <PhoneSheetGroup><PhoneSheetRow icon={<Settings size="regular" />} label={i18next.t("databaseView:multiSourceFiltering")}
          more={false} onPress={() => setCard(true)} data={{ "data-multi-source-filtering": "" }} /></PhoneSheetGroup> : null}
      </div>
    );
  }
  return (
    <div ref={popoverRef} className={styles.picker} data-property-picker data-dashboard-filter-picker>
      <input {...picker.inputProps} ref={inputRef} className={pickerStyles.search} placeholder={labels.filterBy} autoFocus={fieldFocusOnOpen()} />
      <div {...picker.listBoxProps} ref={listRef} className={styles.list} data-edge-fade="">
        {groups.map((group) => (
          <Fragment key={group.id}>
            {head(group)}
            {group.rows.map((row) => (
              <NativePickerOption key={row.key} itemKey={row.key} state={picker.state} className={`${pickerStyles.option} ${row.more ? styles.more : ""}`}
                data-picker-property={row.property?.id} data-dashboard-filter-more={row.more}>{({ labelProps }) => <>
                <span className={pickerStyles.icon} aria-hidden="true">{row.property ? <PropertyIcon property={row.property} size="regular" /> : <DotsHorizontal size="regular" />}</span>
                <span {...labelProps} className={pickerStyles.name}>{row.label}</span>
              </>}</NativePickerOption>
            ))}
          </Fragment>
        ))}
        {!groups.length && <div className={pickerStyles.empty}>{labels.noSearchResults}</div>}
      </div>
      {multiSource ? <MultiSourceFooter onOpen={() => setCard(true)} /> : null}
    </div>
  );
}
