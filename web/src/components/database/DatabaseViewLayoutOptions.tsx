import { SwitchInput } from "@/components/Switch";
import type { ReactNode } from "react";
import type { GraphProperty, GraphView } from "@/contentGraph";
import { ChevronRight } from "@/icons/leafen";
import { CARD_PREVIEW_NONE, cardPreviewMode, defaultCardPreview } from "./cardPreviewModePolicy";
import { CardPreviewChoice } from "./CardPreviewChoice";
import { useDatabasePageTitleNamesSource } from "./databaseFullPageLink";
import { usePageLayoutTabMode } from "./pageLayoutTabEnvironment";
import { databaseViewLabels } from "./databaseViewLabels";
import { DatabaseCardLayoutTiles } from "./DatabaseCardLayoutTiles";
import { NotionSelect } from "./NotionSelect";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import { firstViewDateSource, viewDateSources } from "./viewDateSource";
import { timelineTableColumns } from "./timelineTableModel";
import { viewShowsPageIcons } from "./CardTitleIcon";
import { tableConfigAfterPageIcons, tableConfigAfterWrapAll, tableShowsPageIcons, tableWrapsAllColumns } from "./tablePropertyMenuModel";
import layoutStyles from "./databaseSheet";
import { boardGroupPropertyId, regroupedViewConfig } from "./viewLayoutDefaults";
import { isBoardValueGroupProperty } from "./boardValueGrouping";
import { offeredGroupProperty } from "./arrangeChoices";
import { useOpenSettingsScreen } from "./settingsScreenHead";

const CARD_SIZES = ["small", "medium", "large"] as const;

type LayoutOptionsProps = {
  view: GraphView;
  properties: readonly GraphProperty[];
  onUpdateView: (view: GraphView) => void;
  disabled?: boolean;
  /** "페이지 보기 선택", which each layout places where Notion's does. */
  openPagesIn?: ReactNode;
  /** "처음 불러오기", where the layout has it. */
  initialLoad?: ReactNode;
  /** Makes a property of the view's source, as a card preview's "새 파일과 미디어 속성". */
  onCreateProperty?(type: GraphProperty["type"], name: string): Promise<GraphProperty | null>;
};

const CALENDAR_LAYOUTS = ["month", "week"] as const;

/**
 * The Layout menu's options, as Notion's (seen 2026-09-30, each layout again
 * 2026-10-05): whether the data source's title shows — not on the
 * database's own page, whose title is the database's, nor in a page
 * layout's tab, whose tabs name it (seen 2026-11-12: in every layout), nor
 * for a dashboard, whose layout screen is its tiles alone, the switch in its
 * settings' list (seen 2026-11-15 twice: a "/대시보드" view) — then in each
 * layout's order: page icons; a table's column lines and wrapping; a board's
 * wrapping, grouping and column colors; a gallery's wrapping; a timeline's
 * dates, table, its properties and first rows; a calendar's title wrapping, date, month or week and
 * weekends; a feed's wrapping and author; a map's place property and
 * height; where pages open and the first cards; then a board's or gallery's
 * card preview, size, media fit (with a preview) and layout's tiles.
 */
export function DatabaseViewLayoutOptions(props: LayoutOptionsProps) {
  const { view, onUpdateView } = props;
  const ownPage = useDatabasePageTitleNamesSource();
  const layoutTab = usePageLayoutTabMode() !== null;
  return (
    <>
      {!ownPage && !layoutTab && view.type !== "dashboard" && view.config.tasksAcrossSources !== true && <label className={`${layoutStyles.layoutRow} ${layoutStyles.layoutSwitchRow}`}>
        <span>{databaseViewLabels().showDataSourceTitle}</span>
        <SwitchInput data-layout-option="showDataSourceTitle"
          checked={view.config.showDataSourceTitle !== false}
          onChange={(event) => onUpdateView({
            ...view, config: { ...view.config, showDataSourceTitle: event.currentTarget.checked ? undefined : false },
          })} />
      </label>}
      <ViewTypeLayoutOptions {...props} />
    </>
  );
}

function ViewTypeLayoutOptions({ view, properties, onUpdateView, disabled, openPagesIn, initialLoad, onCreateProperty }: LayoutOptionsProps) {
  const labels = databaseViewLabels();
  const layoutTab = usePageLayoutTabMode() !== null;
  const openScreen = useOpenSettingsScreen();
  const update = (patch: Record<string, unknown>) => onUpdateView({ ...view, config: { ...view.config, ...patch } });
  const dateProperties = viewDateSources(properties);
  const toggle = (name: string, label: string, checked: boolean, change: (checked: boolean) => void) => (
    <label className={`${layoutStyles.layoutRow} ${layoutStyles.layoutSwitchRow}`}>
      <span>{label}</span>
      <SwitchInput data-layout-option={name} checked={checked}
        onChange={(event) => change(event.currentTarget.checked)} />
    </label>
  );
  // A row naming what is chosen, whose choices open under it, as "페이지 보기 선택".
  const choice = (name: string, label: string, value: string, options: Array<{ value: string; label: string }>,
    change: (value: string) => void) => (
    <label className={`${layoutStyles.layoutRow} ${layoutStyles.layoutChoiceRow}`} data-layout-option={name}>
      <span>{label}</span>
      <NotionSelect ariaLabel={label} rowChoice className={layoutStyles.layoutChoice} buttonClassName={layoutStyles.layoutChoiceButton}
        value={value} options={options} onChange={change} disabled={disabled} />
    </label>
  );
  // A row choosing one of the properties: their list searched, each by its
  // icon, under the row at its end (Notion's 표시 기준, seen 2026-11-06).
  const propertyChoice = (name: string, label: string, value: string, list: readonly GraphProperty[],
    change: (value: string) => void) => (
    <label className={`${layoutStyles.layoutRow} ${layoutStyles.layoutChoiceRow}`} data-layout-option={name}>
      <span data-view-setting-name="one-line">{label}</span>
      <NotionSelect ariaLabel={label} rowChoice propertyChoice className={layoutStyles.layoutChoice} buttonClassName={layoutStyles.layoutChoiceButton}
        value={value} options={list.map((property) => ({ value: property.id, label: property.name || labels.untitled, icon: <PropertyTypeIcon type={property.type} size="regular" /> }))}
        search={{ ariaLabel: labels.searchProperties, placeholder: labels.searchProperties }} onChange={change} disabled={disabled} />
    </label>
  );
  const pageIcons = toggle("showPageIcons", labels.showPageIcons, viewShowsPageIcons(view.config),
    (checked) => update({ showPageIcons: checked ? undefined : false }));
  if (view.type === "table") {
    // Freezing columns lives in the column menu, as in Notion.
    const propertyIds = properties.map((property) => property.id);
    return (
      <>
        {/* Turned back on, the lines stay a setting of the view, as Notion's API then gives them (seen 2026-11-10). */}
        {toggle("showVerticalLines", labels.showVerticalLines, view.config.showVerticalLines !== false,
          (checked) => update({ showVerticalLines: checked }))}
        {toggle("showPageIcons", labels.showPageIcons, tableShowsPageIcons(view.config),
          (checked) => onUpdateView({ ...view, config: tableConfigAfterPageIcons(view.config, checked) }))}
        {toggle("wrapAllColumns", labels.wrapAllContent, tableWrapsAllColumns(view.config),
          (checked) => onUpdateView({ ...view, config: tableConfigAfterWrapAll(view.config, checked, propertyIds) }))}
        {openPagesIn}
        {initialLoad}
      </>
    );
  }
  if (view.type === "board" || view.type === "gallery") {
    const preview = cardPreviewMode(typeof view.config.coverProperty === "string" ? view.config.coverProperty : undefined, view.type);
    const size = CARD_SIZES.find((option) => option === view.config.cardSize) ?? "medium";
    const layout = view.config.cardLayout === "compact" ? "compact" : "list";
    // A board groups by what a table can, a title and relations too (Notion's, seen 2026-10-11): what a grouping list offers, and its own.
    const groupId = boardGroupPropertyId(view.config, properties);
    const groupable = properties.filter((property) => offeredGroupProperty(property, groupId));
    return (
      <>
        {pageIcons}
        {toggle("wrap", labels.wrapAllContent, view.config.wrap === true, (checked) => update({ wrap: checked || undefined }))}
        {/* "그룹화 기준" opens the grouping screen, coming back here (Notion's, seen 2026-11-06). */}
        {view.type === "board" && groupable.length > 0 && (openScreen ? (
          <button type="button" className={`${layoutStyles.layoutRow} ${layoutStyles.layoutChoiceRow}`} data-layout-option="groupBy"
            disabled={disabled} onClick={() => openScreen("group")}>
            <span>{labels.groupBy}</span>
            <span className={layoutStyles.layoutChoiceValue}>
              <span data-sheet-value>{groupable.find((property) => property.id === groupId)?.name ?? ""}</span><ChevronRight size="small" aria-hidden="true" />
            </span>
          </button>
        ) : propertyChoice("groupBy", labels.groupBy, groupId ?? "", groupable,
          (groupBy) => onUpdateView({ ...view, config: regroupedViewConfig(view.config, groupId, groupBy, properties, "board") })))}
        {/* "열 배경색": each column tinted by its option's color (on, seen 2026-10-05); groups of what cards hold have no color. */}
        {view.type === "board" && !isBoardValueGroupProperty(properties.find((property) => property.id === groupId)) && toggle("columnColors", labels.columnColors, view.config.boardColumnColors !== false,
          (checked) => update({ boardColumnColors: checked ? undefined : false }))}
        {openPagesIn}
        {initialLoad}
        <div className={layoutStyles.layoutDivider} role="separator" />
        <CardPreviewChoice label={labels.cardPreview} value={preview} properties={properties} disabled={disabled}
          onChange={(value) => update({ coverProperty: value === defaultCardPreview(view.type) ? undefined : value })}
          onCreateFilesProperty={onCreateProperty ? (name) => onCreateProperty("files", name) : undefined} />
        {choice("cardSize", labels.cardSize, size, CARD_SIZES.map((option) => ({
          value: option, label: option === "small" ? labels.small : option === "medium" ? labels.medium : labels.large,
        })), (option) => update({ cardSize: option === "medium" ? undefined : option }))}
        {/* "미디어 맞춤" fits the preview's media, so a card without one has none (Notion's, seen 2026-10-11). */}
        {preview !== CARD_PREVIEW_NONE && toggle("fitImage", labels.fitImage, view.config.fitImage === true,
          (checked) => update({ fitImage: checked || undefined }))}
        <DatabaseCardLayoutTiles value={layout} label={labels.cardLayout} disabled={disabled}
          names={{ compact: labels.cardLayoutCompact, list: labels.cardLayoutList }}
          onChange={(option) => update({ cardLayout: option === "list" ? undefined : option })}>
          {/* "작게" opens its own settings from its box, as Notion's (seen 2026-11-03). */}
          {layout === "compact" && openScreen && (
            <button type="button" className={layoutStyles.compactCardsRow} data-layout-option="compactCards" onClick={() => openScreen("compactCards")}>
              <span>{labels.compactCardSettings}</span>
              <ChevronRight size="small" aria-hidden="true" />
            </button>
          )}
        </DatabaseCardLayoutTiles>
      </>
    );
  }
  // A list's "Directly edit page name": a press on a name edits it instead of opening the page (Notion's,
  // seen 2026-11-07, off at first, after the icons; its Korean screen leaves the words in English).
  if (view.type === "list") return <>{pageIcons}{toggle("directEditPageName", labels.directEditPageName, view.config.directEditPageName === true,
    (checked) => update({ directEditPageName: checked || undefined }))}{openPagesIn}{initialLoad}</>;
  if (view.type === "calendar") {
    const calendarBy = typeof view.config.calendarBy === "string" ? view.config.calendarBy : firstViewDateSource(dateProperties)?.id ?? "";
    const calendarLayout = view.config.calendarLayout === "week" ? "week" : "month";
    return (
      <>
        {pageIcons}
        {toggle("wrapPageTitles", labels.wrapPageTitles, view.config.calendarWrapTitles === true,
          (checked) => update({ calendarWrapTitles: checked || undefined }))}
        {dateProperties.length > 0 && propertyChoice("calendarBy", labels.calendarBy, calendarBy, dateProperties,
          (value) => update({ calendarBy: value }))}
        {choice("calendarLayout", labels.calendarLayout, calendarLayout,
          CALENDAR_LAYOUTS.map((option) => ({ value: option, label: option === "month" ? labels.month : labels.week })),
          (value) => update({ calendarLayout: value === "month" ? undefined : value }))}
        {toggle("showWeekends", labels.showWeekends, view.config.showWeekends !== false,
          (checked) => update({ showWeekends: checked ? undefined : false }))}
        {openPagesIn}
      </>
    );
  }
  if (view.type === "feed") {
    return (
      <>
        {pageIcons}
        {toggle("wrap", labels.wrapProperties, view.config.wrap === true, (checked) => update({ wrap: checked || undefined }))}
        {toggle("showAuthor", labels.showAuthor, view.config.feedShowAuthor !== false,
          (checked) => update({ feedShowAuthor: checked ? undefined : false }))}
        {openPagesIn}
        {initialLoad}
      </>
    );
  }
  // A chart's settings are in its own panel (ChartSettingsPanel), as Notion's.
  if (view.type === "timeline") {
    const start = typeof view.config.timelineBy === "string" ? view.config.timelineBy : firstViewDateSource(dateProperties)?.id ?? "";
    // With two dates or more a bar may run from one to another: "별도의 시작일과 종료일 사용" shows
    // "시작일" and "종료일", the end at first the start's, as Notion's (seen 2026-10-11).
    const separate = dateProperties.length > 1 && typeof view.config.timelineEndBy === "string" && view.config.timelineEndBy !== "";
    return (
      <>
        {pageIcons}
        {propertyChoice("timelineBy", separate ? labels.startDate : labels.timelineBy, start, dateProperties,
          (value) => update({ timelineBy: value || undefined }))}
        {separate && propertyChoice("timelineEndBy", labels.timelineEndDate, String(view.config.timelineEndBy), dateProperties,
          (value) => update({ timelineEndBy: value }))}
        {dateProperties.length > 1 && toggle("timelineSeparateDates", labels.timelineSeparateDates, separate,
          (checked) => update({ timelineEndBy: checked ? start : undefined }))}
        {/* A layout tab's table is opened by its "≫" in the builder, its switch listed only while it is
            open (Notion's builder, seen 2026-11-12). */}
        {(!layoutTab || view.config.timelineShowTable === true) && toggle("timelineShowTable", labels.showTable, view.config.timelineShowTable === true,
          (checked) => update({ timelineShowTable: checked || undefined }))}
        {/* Under it while the table shows, "표 속성 · N개 표시됨 ›" opens the table's list (Notion's, seen 2026-11-12). */}
        {view.config.timelineShowTable === true && openScreen && (
          <button type="button" className={`${layoutStyles.layoutRow} ${layoutStyles.layoutChoiceRow}`} data-layout-option="timelineTableProperties"
            disabled={disabled} onClick={() => openScreen("timelineTable")}>
            <span>{labels.tableProperties}</span>
            <span className={layoutStyles.layoutChoiceValue}>
              <span data-sheet-value>{labels.shownCount(timelineTableColumns(properties, view.config).length)}</span><ChevronRight size="small" aria-hidden="true" />
            </span>
          </button>
        )}
        {openPagesIn}
        {initialLoad}
      </>
    );
  }
  if (view.type === "map") {
    const places = properties.filter((property) => property.type === "place");
    return (
      <>
        {pageIcons}
        {places.length > 0 && propertyChoice("mapBy", labels.mapBy,
          typeof view.config.mapBy === "string" && places.some((property) => property.id === view.config.mapBy) ? view.config.mapBy : places[0].id,
          places, (value) => update({ mapBy: value }))}
        {openPagesIn}
        {initialLoad}
      </>
    );
  }
  // A chart's "처음 불러오기" is all its layout keeps beside the data source title (Notion's, seen 2026-11-03).
  // A dashboard's layout screen is its tiles alone, its pages opening as each widget's do (Notion's, seen 2026-11-07).
  return view.type === "chart" ? <>{initialLoad}</> : view.type === "dashboard" ? null : <>{openPagesIn}</>;
}
