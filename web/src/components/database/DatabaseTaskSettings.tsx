import { SwitchInput } from "@/components/Switch";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { useContext, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DatabaseToolbarTaskFeatureWire } from "@/databaseToolbarConfigWire.generated";
import type { GraphProperty, GraphView } from "@/contentGraph";
import { CaretRightFill, CheckboxIcon, ChevronRight, ClockIcon, DoubleChevronRight, EyeIcon, EyeSlashIcon, LinkIcon, RelationIcon, Settings, Trash } from "@/icons/leafen";
import { confirmChoice } from "@/components/ConfirmActionDialog";
import { databaseViewLabels } from "./databaseViewLabels";
import { toolbarTaskFeaturePatch } from "./databaseToolbarModel";
import { DatabaseSprintContext } from "./databaseSprintContext";
import { DatabaseSprintScreen } from "./DatabaseSprintScreen";
import { DatabaseTaskDatabaseScreen } from "./DatabaseTaskDatabaseScreen";
import { DatabaseTaskSettingsHeader } from "./DatabaseTaskSettingsHeader";
import { DatabasePanelMenu } from "./DatabasePanelMenu";
import { DatabaseSubitemViewOptions } from "./DatabaseSubitemViewOptions";
import { DependencyShiftPicture } from "./DependencyShiftPicture";
import { SubitemsCardPicture, SubitemsPicture } from "./SubitemsPicture";
import { NotionSelect } from "./NotionSelect";
import { PropertyIcon } from "./PropertyIcon";
import { useWikiSource } from "./wikiSourceShape";
import { rowWordOf, taskSettingsWords } from "./taskSettingsWords";
import styles from "./DatabaseTaskSettings.module.css";

type TaskFeature = "subitems" | "dependencies";
type TaskScreen = "overview" | "taskDatabase" | "sprints" | "subitems" | "subitemsAdvanced" | "dependencies" | "dependenciesAdvanced";
type DependencyDraft = {
  shiftMode: string; avoidWeekends: boolean; dateMode: "single" | "separate";
  datePropertyId: string; startDatePropertyId: string; endDatePropertyId: string;
};

function featureConfig(value: Record<string, unknown> | null) {
  return value ?? { enabled: false };
}

function stringValue(value: unknown, fallback = "") {
  return typeof value === "string" ? value : fallback;
}

function booleanValue(value: unknown, fallback: boolean) {
  return typeof value === "boolean" ? value : fallback;
}

export function DatabaseTaskSettings({
  properties,
  taskConfiguration,
  writable,
  onChange,
  onBack,
  initialScreen,
  view,
  onUpdateView,
}: {
  properties: readonly GraphProperty[];
  /** The view the settings opened from, whose sub-item display and filters the sub-items screen sets. */
  view?: GraphView;
  onUpdateView?(view: GraphView): void;
  taskConfiguration: DatabaseToolbarTaskFeatureWire;
  writable: boolean;
  /** Saves the settings, with the properties a task database's change makes or retypes in the same request. */
  onChange(next: DatabaseToolbarTaskFeatureWire, madeProperties?: readonly GraphProperty[]): Promise<boolean>;
  /** Not used by these screens: what they make goes with the settings (madeProperties). */
  onCreateProperty?(type: GraphProperty["type"], name: string, config?: Record<string, unknown>): Promise<GraphProperty | null>;
  /** Back to the view settings the panel opened these from. */
  onBack?(): void;
  /** The setting "추가 설정" chose, which goes back to the view settings. */
  initialScreen?: "subitems" | "dependencies" | "taskDatabase" | "sprints";
}) {
  const labels = databaseViewLabels();
  const { t, i18n } = useTranslation(["databaseTasks", "pageArchive"]);
  // A row's value stands in plain words on a desktop (seen 2026-11-04), with its icon in a phone's sheet.
  const { phone } = useSidebarLayout(true);
  const sprints = useContext(DatabaseSprintContext);
  const taskDatabase = taskConfiguration.tasks?.enabled === true;
  const sprintsOn = sprints?.state?.enabled === true;
  // A wiki says "페이지" where another says "항목", a task database "작업", as Notion's (taskSettingsWords.ts).
  const wiki = useWikiSource(view?.dataSourceId ?? properties[0]?.dataSourceId);
  const words = taskSettingsWords(rowWordOf(taskDatabase, wiki));
  const dates = properties.filter((property) => property.type === "date");
  const titleProperties = properties.filter((property) => property.type === "title");
  const named = (id: unknown) => properties.find((property) => property.id === id)?.name.trim() ?? "";
  // A change shows as it is sent, as Notion's, and goes back if it is refused.
  const [applying, setApplying] = useState<{ feature: TaskFeature; configuration: Record<string, unknown> } | null>(null);
  const shownFeature = (feature: TaskFeature) => featureConfig(applying?.feature === feature
    ? { ...taskConfiguration[feature], ...applying.configuration } : taskConfiguration[feature]);
  const subitems = shownFeature("subitems");
  const dependencies = shownFeature("dependencies");
  // Once on, the pictures name the pair's own properties, renamed or made in
  // another language, where they are off read the rows' words (Notion's, seen 2026-11-11).
  const subitemsOn = subitems.enabled === true;
  const pictureParent = (subitemsOn && named(subitems.parentPropertyId)) || words.parent;
  const pictureChild = (subitemsOn && named(subitems.subitemsPropertyId)) || words.child;
  const [screen, setScreen] = useState<TaskScreen>(initialScreen ?? "overview");
  const [pendingFeature, setPendingFeature] = useState<TaskFeature | null>(null);
  // The settings shown before dependencies are on; once on, each change
  // applies at once, as Notion's (seen 2026-10-10).
  const dependencySettings = (config: Record<string, unknown>): DependencyDraft => ({
    shiftMode: stringValue(config.shiftMode, "overlap"),
    avoidWeekends: booleanValue(config.avoidWeekends, true),
    dateMode: config.dateMode === "separate" ? "separate" : "single",
    datePropertyId: stringValue(config.datePropertyId, dates[0]?.id),
    startDatePropertyId: stringValue(config.startDatePropertyId, stringValue(config.datePropertyId, dates[0]?.id)),
    endDatePropertyId: stringValue(config.endDatePropertyId, dates.find((date) => date.id !== stringValue(config.datePropertyId, dates[0]?.id))?.id ?? dates[0]?.id),
  });
  const [dependencyDraft, setDependencyDraft] = useState(() => dependencySettings(dependencies));
  // A setting "추가 설정" opened goes back to the view settings.
  const toStart = () => (initialScreen && onBack ? onBack() : setScreen("overview"));
  const startLabel = initialScreen ? labels.backToViewSettings : labels.additionalSettings;

  const currentSubitemTitlePropertyId = stringValue(
    subitems.titlePropertyId,
    titleProperties[0]?.id,
  );
  // Off unless set: turned on, the toggle stands in the table's first column, as Notion's (seen 2026-11-22, three times).
  const currentShowToggleOnTitle = booleanValue(subitems.showToggleOnTitle, false);

  async function update(feature: TaskFeature, configuration: Record<string, unknown>) {
    if (!writable || pendingFeature) return false;
    setPendingFeature(feature);
    setApplying({ feature, configuration });
    try {
      return await onChange({
        ...taskConfiguration,
        ...toolbarTaskFeaturePatch(feature, configuration.enabled !== false, configuration),
      });
    } finally {
      setPendingFeature(null);
      setApplying(null);
    }
  }

  function openDependencies() {
    setDependencyDraft(dependencySettings(dependencies));
    setScreen("dependencies");
  }

  // The pair a binding keeps is the server's; the settings store only what
  // the screen sets.
  const storedDependencies = () => {
    const { successorPropertyId: _successor, predecessorPropertyId: _predecessor, ...stored } = dependencies;
    return stored;
  };
  const dependencyConfig = (settings: DependencyDraft) => ({
    shiftMode: settings.shiftMode,
    avoidWeekends: settings.avoidWeekends,
    dateMode: settings.dateMode,
    datePropertyId: settings.datePropertyId,
    ...(settings.dateMode === "separate"
      ? { startDatePropertyId: settings.startDatePropertyId, endDatePropertyId: settings.endDatePropertyId } : {}),
  });

  function changeDependencies(patch: Partial<DependencyDraft>) {
    if (dependencies.enabled !== true) {
      setDependencyDraft((draft) => ({ ...draft, ...patch }));
      return;
    }
    void update("dependencies", {
      ...storedDependencies(), ...dependencyConfig({ ...dependencySettings(dependencies), ...patch }), enabled: true,
    });
  }

  // Turned on, the view it was turned on from keeps its parents only under its filters, as Notion's says so
  // in its views API there alone (seen 2026-11-18 in three databases: filter_scope "parents").
  async function activateSubitems() {
    const kept = subitems.propertyDisposition === "keep";
    const on = await update("subitems", {
      ...(kept ? subitems : {}),
      enabled: true,
      titlePropertyId: kept ? currentSubitemTitlePropertyId : titleProperties[0]?.id,
      showToggleOnTitle: kept ? currentShowToggleOnTitle : false,
    });
    const shown = (view?.config?.subtasks ?? {}) as Record<string, unknown>;
    if (on && view && onUpdateView && typeof shown.filterScope !== "string") {
      onUpdateView({ ...view, config: { ...view.config, subtasks: { ...shown, filterScope: "parents" } } });
    }
  }

  // Turned on, dependencies go back to the view settings, as Notion's do. A
  // database with no date gets one, 날짜, with their pair: the server makes
  // both (completeDatabaseTaskFeatures), as Notion's (seen 2026-11-18).
  async function activateDependencies() {
    const settings = dependencyDraft;
    if (dates.length && !settings.datePropertyId && settings.dateMode !== "separate") return;
    if (settings.dateMode === "separate" && (!settings.startDatePropertyId || !settings.endDatePropertyId)) return;
    if (await update("dependencies", { ...dependencyConfig(settings), enabled: true })) toStart();
  }

  // Turning a feature off asks in the middle of the window whether its
  // relations go too, naming them, as Notion's does (seen 2026-10-10).
  async function turnOff(feature: TaskFeature) {
    const current = feature === "subitems" ? subitems : dependencies;
    const pair = feature === "subitems" ? [current.subitemsPropertyId, current.parentPropertyId]
      : [current.successorPropertyId, current.predecessorPropertyId];
    const names = pair.map((id) => properties.find((property) => property.id === id)?.name.trim()).filter((name): name is string => Boolean(name));
    const disposition = await confirmChoice({
      message: feature === "subitems" ? labels.turnOffSubitemsQuestion : labels.turnOffDependenciesQuestion,
      choices: [
        { value: "remove", label: labels.removeProperties, description: names.length
          ? labels.removesProperties(new Intl.ListFormat(i18n.language, { type: "conjunction" }).format(names)) : undefined },
        { value: "keep", label: labels.keepProperties },
      ],
      confirmLabel: labels.turnOff,
      danger: true,
    });
    if (disposition !== "remove" && disposition !== "keep") return;
    const kept = feature === "subitems" ? subitems : storedDependencies();
    const next = disposition === "keep"
      ? { ...kept, enabled: false, propertyDisposition: disposition }
      : { enabled: false, propertyDisposition: disposition };
    if (!await update(feature, next)) return;
    if (feature === "dependencies") setDependencyDraft(dependencySettings(disposition === "keep" ? kept : {}));
  }

  if (screen === "overview") {
    return (
      <div className={styles.panel} data-database-task-settings data-task-screen="overview">
        <DatabaseTaskSettingsHeader title={labels.additionalSettings} backLabel={labels.backToViewSettings} onBack={onBack} />
        {/* Notion's order (seen 2026-09-30): sub-items, dependencies and the
            task database it turns into; Leafen's sprints follow. */}
        <DatabasePanelMenu label={labels.additionalSettings} actions={[
          { key: "subitems", label: words.subitems, icon: <DoubleChevronRight size="regular" />, tail: <ChevronRight size="small" />,
            note: subitems.enabled === true ? labels.taskFeatureOn : "", run: () => setScreen("subitems") },
          { key: "dependencies", label: labels.dependencies, icon: <LinkIcon size="regular" />, tail: <ChevronRight size="small" />,
            note: dependencies.enabled === true ? labels.taskFeatureOn : "", run: openDependencies },
          { key: "taskDatabase", label: t("taskDatabase"), icon: <CheckboxIcon size="regular" />, tail: <ChevronRight size="small" />,
            note: taskDatabase ? labels.taskFeatureOn : "", data: { "data-task-database": "" }, run: () => setScreen("taskDatabase") },
          { key: "sprints", label: t("sprints"), icon: <ClockIcon size="regular" />, tail: <ChevronRight size="small" />,
            note: sprintsOn ? labels.taskFeatureOn : "", data: { "data-sprints": "" }, run: () => setScreen("sprints") },
        ]} />
      </div>
    );
  }

  if (screen === "taskDatabase") {
    return <DatabaseTaskDatabaseScreen backLabel={startLabel} properties={properties}
      sprintsOn={sprintsOn} taskConfiguration={taskConfiguration} writable={writable}
      onBack={toStart} onChange={onChange} />;
  }

  if (screen === "sprints") {
    return <DatabaseSprintScreen backLabel={startLabel} taskDatabase={taskDatabase}
      writable={writable} onBack={toStart} onOpenTaskDatabase={() => setScreen("taskDatabase")} />;
  }

  if (screen === "subitems") {
    const busy = pendingFeature === "subitems";
    return (
      <div className={styles.panel} data-database-task-settings data-task-screen="subitems" aria-busy={busy}>
        <DatabaseTaskSettingsHeader title={words.subitems} backLabel={startLabel} onBack={toStart} />
        <div className={styles.body}>
          {/* Turned on, the screen sets how this view shows sub-items, as Notion's (seen 2026-10-11);
              a reader reads only what they are, "고급 설정" dimmed (seen 2026-10-12). */}
          {subitems.enabled === true && view && onUpdateView && writable
            ? <DatabaseSubitemViewOptions view={view} writable={writable} parent={pictureParent} child={pictureChild}
              childNumbered={words.childNumbered} onUpdateView={onUpdateView} />
            : <>
              {/* Off, it shows how the view will show them, as Notion's (seen 2026-11-03): a board's card and what it lists,
                  flattened without toggles where the view was made a chart (seen 2026-11-18 in two databases). */}
              <p>{words.subitemsDescription}</p>
              {view?.type === "board" ? <>
                <SubitemsCardPicture parent={words.parent} child={words.childNumbered}
                  person={labels.subitemPreviewPerson} status={labels.subitemPreviewStatus} />
                <p className={styles.pictureNote}>{labels.subitemHelpCardProperty}</p>
              </> : <SubitemsPicture toggles={(view?.config?.subtasks as { displayMode?: unknown } | undefined)?.displayMode !== "flattened"}
                rows={[{ label: words.parent, open: true }, { label: words.child, sub: true }, { label: words.child, sub: true }]} />}
            </>}
          {subitems.enabled === true && <hr className={styles.divider} />}
          {subitems.enabled === true && (
            <button type="button" className={styles.turnOffRow} data-subitem-advanced disabled={!writable} onClick={() => setScreen("subitemsAdvanced")}>
              <Settings size="regular" aria-hidden="true" /><span>{labels.advancedSettings}</span>
              <ChevronRight size="small" aria-hidden="true" className={styles.rowChevron} />
            </button>
          )}
          {!writable ? null : subitems.enabled === true ? (
            <button type="button" className={styles.turnOffRow} disabled={!writable || busy} onClick={() => void turnOff("subitems")}>
              <Trash size="regular" aria-hidden="true" /><span>{words.turnOff}</span>
            </button>
          ) : (
            <button type="button" className={styles.primaryAction} disabled={!writable || busy} onClick={() => void activateSubitems()}>
              {words.turnOn}
            </button>
          )}
        </div>
      </div>
    );
  }

  if (screen === "subitemsAdvanced") {
    const busy = pendingFeature === "subitems";
    // The relation rows nest by, the pair turning sub-items on made; a row's
    // place follows that pair, so it is the one to choose.
    const relation = properties.find((property) => property.id === subitems.subitemsPropertyId);
    // Any two-way relation of the database to itself may name the sub-items, its other side their parent (Notion's "속성").
    // Notion's list (seen 2026-11-06, with the sub-items' pair and the dependencies' chosen in turn): the newest
    // relation first, whichever is chosen — "하위 작업 · 상위 작업", a line, "후속 작업 · 선행 작업" — a line between pairs.
    const sides = [...selfRelationSides(properties)].reverse();
    return (
      <div className={styles.panel} data-database-task-settings data-task-screen="subitems-advanced" aria-busy={busy}>
        <DatabaseTaskSettingsHeader title={labels.advancedSettings} backLabel={words.subitems} onBack={() => setScreen("subitems")} />
        <div className={`${styles.body} ${styles.advancedBody} ${styles.tightFoot}`}>
          {/* The picture and what the property is for, over it, as Notion's (seen 2026-11-03);
              each change applies at once, as Notion's (seen 2026-11-04). */}
          <SubitemsPicture rows={[{ label: pictureParent, open: true }, { label: pictureChild, sub: true }, { label: pictureChild, sub: true }]} />
          <p>{words.subitemsPropertyHint}</p>
          <div className={`${styles.settingRows} ${styles.wideRows}`}>
            <NotionSelect ariaLabel={labels.property} settingLabel={labels.property} settingIcon={<RelationIcon size="small" />} settingValueIcon={phone}
              propertyChoice settingMenuWidth={300} value={relation?.id ?? ""} disabled={!writable || !relation || busy}
              options={sides.map((side, index) => ({ value: side.id, label: side.name, icon: <PropertyIcon property={side} size="regular" />,
                divided: index > 0 && otherSide(sides[index - 1]!) !== side.id }))}
              search={{ ariaLabel: labels.searchProperties, placeholder: labels.searchProperties }}
              onChange={(id) => {
                const side = sides.find((candidate) => candidate.id === id);
                if (side && id !== relation?.id) void update("subitems", { ...subitems, enabled: true, subitemsPropertyId: id, parentPropertyId: otherSide(side) });
              }} />
          </div>
          <label className={styles.switchRow}>
            <span><strong><span className={styles.switchRowIcon}><CaretRightFill aria-hidden="true" /></span>{labels.showNestingToggleOnTitle}</strong></span>
            <SwitchInput aria-label={labels.showNestingToggleOnTitle} disabled={!writable}
              checked={currentShowToggleOnTitle}
              onChange={(event) => void update("subitems", { ...subitems, enabled: true, showToggleOnTitle: event.currentTarget.checked })} />
          </label>
        </div>
      </div>
    );
  }

  if (screen === "dependenciesAdvanced") {
    return <DependencyAdvancedScreen properties={properties} dependencies={dependencies} writable={writable} help={words.successorHelp} valueIcon={phone}
      busy={pendingFeature === "dependencies"} onBack={() => setScreen("dependencies")}
      onChoose={(successorPropertyId) => void update("dependencies", {
        ...storedDependencies(), ...dependencyConfig(dependencySettings(dependencies)), enabled: true, successorPropertyId,
      })} />;
  }

  const busy = pendingFeature === "dependencies";
  const on = dependencies.enabled === true;
  const hiddenInView = view?.config?.timelineHideDependencies === true;
  const settings = on ? dependencySettings(dependencies) : dependencyDraft;
  // The lists name properties by their marks — a date's and a relation's type mark at 19.2, Notion's calendar and
  // arrow-northeast masks — in a 300 wide list searched for them, its end on the row's, while their rows keep their
  // own ↗ (arrowDiagonalUpRight) (seen 2026-11-22 at 1440 dark, dpr 1).
  const dateOptions = dates.length
    ? dates.map((date) => ({ value: date.id, label: date.name, icon: <PropertyIcon property={date} size="regular" /> }))
    : [{ value: "", label: labels.noDateProperty, disabled: true }];
  // A date the shifts follow, chosen as Notion's: a row naming it, opening
  // a search of the database's dates.
  const dateRow = (label: string, key: "datePropertyId" | "startDatePropertyId" | "endDatePropertyId") => (
    <NotionSelect ariaLabel={label} settingLabel={label} settingValueIcon={phone} propertyChoice settingMenuWidth={300} value={settings[key]} options={dateOptions}
      disabled={!writable || !dates.length}
      search={{ ariaLabel: labels.searchProperties, placeholder: labels.searchProperties }}
      onChange={(value) => changeDependencies({ [key]: value })} />
  );
  return (
    <div className={styles.panel} data-database-task-settings data-task-screen="dependencies" aria-busy={busy}>
      <DatabaseTaskSettingsHeader title={labels.dependencies} backLabel={startLabel} onBack={toStart} />
      <div className={`${styles.body} ${styles.dependencyBody} ${on ? styles.tightFoot : ""}`}>
        <p>{words.dependenciesDescription}</p>
        <span className={styles.fieldTitle}>{labels.automaticDateShifting}</span>
        <DependencyShiftPicture className={styles.shiftPicture} mode={settings.shiftMode} />
        <div role="radiogroup" aria-label={labels.automaticDateShifting} className={styles.shiftChoices}>
          {([
            ["overlap", labels.dependencyShiftOverlap],
            ["maintain_spacing", words.shiftMaintain],
            ["none", labels.dependencyShiftNone],
          ] as const).map(([value, label]) => (
            <button key={value} type="button" role="radio" aria-checked={settings.shiftMode === value}
              className={styles.shiftChoice} disabled={!writable}
              onClick={() => changeDependencies({ shiftMode: value })}>{label}</button>
          ))}
        </div>
        <label className={styles.switchRow}>
          <span><strong>{labels.avoidWeekends}</strong><small>{words.avoidWeekendsDescription}</small></span>
          <SwitchInput aria-label={labels.avoidWeekends} disabled={!writable}
            checked={settings.avoidWeekends}
            onChange={(event) => changeDependencies({ avoidWeekends: event.currentTarget.checked })} />
        </label>
        {/* Which date moves, asked only where there is more than one, as
            Notion's with a single date asks nothing (seen 2026-11-02). */}
        {dates.length > 1 ? <>
          <div className={`${styles.settingRows} ${styles.wideRows}`}>
            {settings.dateMode === "separate"
              ? <>{dateRow(labels.startDate, "startDatePropertyId")}{dateRow(labels.endDate, "endDatePropertyId")}</>
              : dateRow(labels.moveBasedOn, "datePropertyId")}
          </div>
          <label className={styles.switchRow}>
            <span><strong>{labels.separateStartEndDates}</strong></span>
            <SwitchInput aria-label={labels.separateStartEndDates} disabled={!writable}
              checked={settings.dateMode === "separate"}
              onChange={(event) => changeDependencies({ dateMode: event.currentTarget.checked ? "separate" : "single" })} />
          </label>
        </> : null}
        {on ? (
          <>
            <hr className={styles.divider} />
            <button type="button" className={styles.turnOffRow} data-dependency-advanced onClick={() => setScreen("dependenciesAdvanced")}>
              <Settings size="regular" aria-hidden="true" /><span>{labels.advancedSettings}</span>
              <ChevronRight size="small" aria-hidden="true" className={styles.rowChevron} />
            </button>
            {/* A timeline view hides or shows its arrows, as Notion's (seen 2026-10-11); other views draw none. */}
            {view?.type === "timeline" && onUpdateView ? (
              <button type="button" className={styles.turnOffRow} data-dependency-view-visibility={hiddenInView ? "hidden" : "shown"} disabled={!writable}
                onClick={() => onUpdateView({ ...view, config: { ...view.config, timelineHideDependencies: hiddenInView ? undefined : true } })}>
                {hiddenInView ? <EyeIcon size="regular" aria-hidden="true" /> : <EyeSlashIcon size="regular" aria-hidden="true" />}
                <span>{hiddenInView ? labels.showDependenciesInView : labels.hideDependenciesInView}</span>
              </button>
            ) : null}
            <button type="button" className={styles.turnOffRow} disabled={!writable || busy} onClick={() => void turnOff("dependencies")}>
              <Trash size="regular" aria-hidden="true" /><span>{labels.turnOffDependencies}</span>
            </button>
          </>
        ) : (
          <button type="button" className={styles.primaryAction} disabled={!writable || busy}
            onClick={() => void activateDependencies()}>{labels.turnOnDependencies}</button>
        )}
      </div>
    </div>
  );
}

/**
 * Dependencies' "고급 설정", as Notion's (seen 2026-10-11): a picture of a row
 * and the one after it named by the pair, and the relation naming a row's
 * successors — any two-way relation of the database to itself, others first,
 * then the bound pair; the other side names the predecessors.
 */
function DependencyAdvancedScreen({ properties, dependencies, writable, help, valueIcon, busy, onBack, onChoose }: {
  properties: readonly GraphProperty[]; dependencies: Record<string, unknown>; writable: boolean; help: string; valueIcon: boolean; busy: boolean;
  onBack(): void; onChoose(successorPropertyId: string): void;
}) {
  const labels = databaseViewLabels();
  const successorId = stringValue(dependencies.successorPropertyId), predecessorId = stringValue(dependencies.predecessorPropertyId);
  const sides = selfRelationSides(properties);
  const bound = new Set([successorId, predecessorId]);
  const ordered = [...sides.filter((side) => !bound.has(side.id)), ...[successorId, predecessorId]
    .flatMap((id) => sides.filter((side) => side.id === id))];
  const name = (id: string) => properties.find((property) => property.id === id)?.name ?? "";
  return (
    <div className={styles.panel} data-database-task-settings data-task-screen="dependencies-advanced" aria-busy={busy}>
      <DatabaseTaskSettingsHeader title={labels.advancedSettings} backLabel={labels.dependencies} onBack={onBack} />
      <div className={`${styles.body} ${styles.advancedBody} ${styles.tightFoot}`}>
        <div className={styles.pairPicture} aria-hidden="true">
          <span data-part="row" /><span data-part="name">{name(successorId)}</span>
          <span data-part="name">{name(predecessorId)}</span><span data-part="row" />
          <svg viewBox="0 0 100 40" preserveAspectRatio="none"><path d="M18 12 C 44 12, 30 30, 58 30" /></svg>
        </div>
        <p>{help}</p>
        <div className={`${styles.settingRows} ${styles.wideRows}`}>
          <NotionSelect ariaLabel={labels.property} settingLabel={labels.property} settingIcon={<RelationIcon size="small" />} settingValueIcon={valueIcon}
            propertyChoice settingMenuWidth={300} value={successorId} disabled={!writable || !ordered.length}
            options={ordered.map((side) => ({ value: side.id, label: side.name, icon: <PropertyIcon property={side} size="regular" /> }))}
            search={{ ariaLabel: labels.searchProperties, placeholder: labels.searchProperties }}
            onChange={(value) => { if (value && value !== successorId) onChoose(value); }} />
        </div>
      </div>
    </div>
  );
}

const otherSide = (property: GraphProperty) => stringValue(property.config?.relatedPropertyId);

/** The database's two-way relations to itself, each side: what sub-items and dependencies may be bound to. */
function selfRelationSides(properties: readonly GraphProperty[]) {
  const sourceId = properties[0]?.dataSourceId;
  return properties.filter((property) => property.type === "relation" && property.config?.relationDatabaseId === sourceId
    && otherSide(property) && otherSide(property) !== property.id
    && properties.some((other) => other.id === otherSide(property) && otherSide(other) === property.id));
}
