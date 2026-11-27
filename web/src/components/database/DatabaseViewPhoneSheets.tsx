"use client";

import { Fragment, useState, type ReactNode } from "react";
import type { GraphView, GraphViewType } from "@/contentGraph";
import { AddCircleFillIcon, CheckIcon, DotsHorizontal, SquareOnSquarePlusIcon } from "@/icons/leafen";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { databaseViewLabels } from "./databaseViewLabels";
import { ViewIcon, ViewTypeIcon, viewDisplayName } from "./databaseViewShared";
import { useViewTabDisplay, VIEW_TAB_DISPLAYS } from "./databaseViewTabDisplay";
import { DatabaseAddViewChoices, type DatabaseViewAction } from "./DatabaseTabMenus";
import type { ViewListLead } from "./DatabaseViewList";
import styles from "./DatabaseViewPhoneSheets.module.css";
import tabStyles from "./DatabaseViewTabs.module.css";

/** Actions in white groups, a new group at each one that starts one, as Notion's sheets part them. */
function ActionGroups({ actions }: { actions: DatabaseViewAction[] }) {
  const groups: DatabaseViewAction[][] = [];
  for (const action of actions) {
    if (!groups.length || action.grouped) groups.push([action]);
    else groups[groups.length - 1].push(action);
  }
  return <>{groups.map((group) => (
    <PhoneSheetGroup key={group[0].id}>
      {group.map((action) => (
        <PhoneSheetRow key={action.id} icon={action.icon} label={action.label} value={action.meta} more={Boolean(action.next)}
          danger={action.danger} disabled={action.disabled} onPress={action.act} data={{ "data-view-action": action.id }} />
      ))}
    </PhoneSheetGroup>
  ))}</>;
}

/**
 * The "⌄" beside a database's title on a phone, as Notion's half sheet
 * "데이터 소스 작업" (seen 2026-10-24 at 375px): [새 보기 추가, 보기 링크 복사,
 * 보기 복제] [데이터베이스 보기, 제목 편집, 아이콘 편집, 레이아웃 편집]
 * [제목 숨기기], and "완료".
 */
function PhoneDatabaseActionsSheet({ actions, onClose }: { actions: DatabaseViewAction[]; onClose(): void }) {
  const labels = databaseViewLabels();
  return (
    <PhoneSheet size="half" title={labels.dataSourceActions} onClose={onClose} action={{ label: labels.sheetDone, onPress: onClose }}
      tone="views" data={{ "data-phone-database-actions": "" }}>
      <ActionGroups actions={actions} />
    </PhoneSheet>
  );
}

/**
 * A view's "⋯" on a phone, as Notion's sheet "보기 옵션": the tab menu's rows
 * in its groups, deleting red, but no "사이드바에 보기 추가", which Notion's
 * phone leaves out though its desktop's tab menu has it (Galaxy A90 Chrome
 * 411, 2026-11-15: a shared and a private database page alike).
 */
function PhoneViewOptionsSheet({ actions, onClose }: { actions: DatabaseViewAction[]; onClose(): void }) {
  const labels = databaseViewLabels();
  // A row left out that started a group hands its start to the next row.
  const shown = actions.flatMap((action, index) => action.id === "sidebar" ? []
    : actions[index - 1]?.id === "sidebar" && actions[index - 1]!.grouped ? [{ ...action, grouped: true }] : [action]);
  return (
    <PhoneSheet title={labels.viewOptions} onClose={onClose} tone="views" data={{ "data-phone-view-options": "" }}>
      <ActionGroups actions={shown.map((action) => action.id === "delete" ? { ...action, danger: true } : action)} />
    </PhoneSheet>
  );
}

/**
 * "새 보기 추가" on a phone, as Notion's sheet (seen 2026-10-24): a row per
 * layout — its 20px icon, its name, no "›" — and "새 데이터 소스" apart.
 * Opened from "N개 보기" › "새 보기" it is a whole screen of the layouts
 * alone, flat rows across it and no "새 데이터 소스", its "완료" closing it
 * with the list under it (Notion's, seen 2026-11-18 in iPhone 17 Safari at
 * 402 and on a Galaxy A90 at 411; from "데이터 소스 작업" the sheet above).
 */
function PhoneAddViewSheet({ types, screen, onCreate, onAddSource, onClose }: {
  types: readonly GraphViewType[]; screen?: boolean; onCreate(type: GraphViewType): void; onAddSource?(): void; onClose(): void;
}) {
  const labels = databaseViewLabels();
  return (
    <PhoneSheet title={labels.addNewView} size={screen ? "screen" : "full"} onClose={onClose} action={{ label: labels.sheetDone, onPress: onClose }}
      data={{ "data-phone-add-view": "", "data-from-views": screen ? "true" : undefined }} bodyClassName={screen ? styles.addViewScreen : undefined}>
      <PhoneSheetGroup>
        {types.map((type) => (
          <PhoneSheetRow key={type} icon={<ViewTypeIcon type={type} size="regular" />} label={labels.viewTypes[type]} more={false}
            onPress={() => onCreate(type)} data={{ "data-phone-add-view-type": type }} />
        ))}
      </PhoneSheetGroup>
      {onAddSource && !screen ? (
        <PhoneSheetGroup>
          <PhoneSheetRow icon={<SquareOnSquarePlusIcon size="regular" />} label={labels.newDataSource} more={false} onPress={onAddSource} data={{ "data-phone-add-source": "" }} />
        </PhoneSheetGroup>
      ) : null}
    </PhoneSheet>
  );
}

/**
 * "새 보기 추가" on a touch screen wider than a phone, as Notion's iPad (seen
 * 2026-11-14 at 834): a whole sheet with no head, the desktop's ten layouts
 * five to a row under its small heading, then "새 데이터 소스", laid across
 * the sheet (DatabaseViewTabs .titleAddViewSheetPanel).
 */
function WideAddViewSheet({ types, onCreate, onAddSource, onClose }: {
  types: readonly GraphViewType[]; onCreate(type: GraphViewType): void; onAddSource?(): void; onClose(): void;
}) {
  const labels = databaseViewLabels();
  return (
    <PhoneSheet title={labels.addNewView} head={false} onClose={onClose} data={{ "data-phone-add-view": "", "data-wide": "true" }}
      bodyClassName={tabStyles.titleAddViewSheetBody}>
      <div className={`${tabStyles.titleAddViewMenuPanel} ${tabStyles.titleAddViewSheetPanel}`}>
        <DatabaseAddViewChoices types={[...types]} heading={labels.addNewView} open onCreate={onCreate} onAddSource={onAddSource} sheet
          className={tabStyles.titleAddViewGrid} itemClassName={tabStyles.inlineAddViewType} />
      </div>
    </PhoneSheet>
  );
}

/**
 * A row of Notion's sheet "N개 보기" (seen 2026-10-24 for a database's views,
 * 2026-11-14 at 402pt for the library's): the view's icon and name, the
 * one shown ticked before its "⋯" (alone, the tick takes the "⋯"'s place).
 */
export function PhoneViewListRow({ icon, name, current, onPick, more, data }: {
  icon: ReactNode;
  name: ReactNode;
  current: boolean;
  onPick(): void;
  /** The row's "⋯"; left out, the row has none. */
  more?: { label: string; onPress(): void };
  data?: Record<`data-${string}`, string | undefined>;
}) {
  return (
    <div className={styles.viewRow} data-active={current ? "true" : undefined} {...data}>
      <button type="button" className={styles.viewPick} aria-current={current ? "true" : undefined} onClick={onPick}>
        <span className={styles.viewIcon} aria-hidden="true">{icon}</span>
        <span className={styles.viewName}>{name}</span>
      </button>
      {current ? <CheckIcon size="page" aria-hidden="true" className={styles.viewTick} data-alone={more ? undefined : "true"} /> : null}
      {more ? (
        <button type="button" className={styles.viewMore} aria-label={more.label} onClick={more.onPress}>
          <DotsHorizontal size="small" aria-hidden="true" />
        </button>
      ) : null}
    </div>
  );
}

/**
 * A phone's view picker opens Notion's sheet "N개 보기" (seen 2026-10-24): a
 * row per view with its icon and "⋯", then "새 보기" ("표, 차트, 폼 등")
 * and "새 데이터 소스".
 */
function withLead(rows: ReactNode[], index: number, lead: ReactNode) {
  rows.splice(index, 0, lead);
  return rows;
}

export function PhoneViewListSheet({ views, activeId, lead, onSelect, onActions, onNewView, onAddSource, addDisabled, onClose }: {
  views: readonly GraphView[];
  /** "새 보기" and "새 데이터 소스" dim: the database holds as many views as it may. */
  addDisabled?: boolean;
  activeId: string;
  lead?: ViewListLead;
  onSelect(view: GraphView): void;
  onActions?(view: GraphView): void;
  onNewView?(): void;
  onAddSource?(): void;
  onClose(): void;
}) {
  const labels = databaseViewLabels();
  const row = (view: GraphView): ReactNode => {
    const name = viewDisplayName(view);
    return (
      <PhoneViewListRow key={view.id} icon={<ViewIcon view={view} size="regular" />} name={name} current={view.id === activeId && !lead?.current}
        onPick={() => onSelect(view)} more={onActions ? { label: labels.openActions(name), onPress: () => onActions(view) } : undefined}
        data={{ "data-phone-view": view.id }} />
    );
  };
  return (
    <PhoneSheet title={labels.viewsCount(views.length)} onClose={onClose} action={{ label: labels.sheetDone, onPress: onClose }}
      data={{ "data-phone-view-list": "" }}>
      <PhoneSheetGroup>
        {/* A wiki's Home at its place among the views. */}
        {withLead(views.map((view) => <Fragment key={view.id}>{row(view)}</Fragment>), lead?.index ?? 0, lead ? (
          <PhoneViewListRow key="lead" icon={lead.icon} name={lead.label} current={Boolean(lead.current)} onPick={lead.onSelect}
            data={{ "data-phone-view-lead": "" }} />
        ) : null)}
      </PhoneSheetGroup>
      {onNewView || onAddSource ? (
        <PhoneSheetGroup>
          {onNewView ? (
            <PhoneSheetRow icon={<AddCircleFillIcon size="regular" className={styles.newViewIcon} />} label={labels.newView}
              description={labels.newViewKinds} more={false} disabled={addDisabled} onPress={onNewView} data={{ "data-phone-new-view": "" }} />
          ) : null}
          {onAddSource ? (
            <PhoneSheetRow icon={<SquareOnSquarePlusIcon size="regular" />} label={labels.newDataSource} more={false} disabled={addDisabled}
              onPress={onAddSource} data={{ "data-phone-add-source": "" }} />
          ) : null}
        </PhoneSheetGroup>
      ) : null}
    </PhoneSheet>
  );
}


type PhoneMenu = "title" | "add" | "titleAdd" | "overflow" | "view";
type TabDisplay = "both" | "text" | "icon";

/**
 * A phone's view menus as sheets in place of the tab row's popovers: the
 * title's "⌄", "새 보기 추가", the picker's "N개 보기" and a view's "⋯"
 * ("다음과 같이 표시 ›" a choice over it).
 */
export function PhoneViewMenus({ kind, view, views, activeId, lead, addTypes, wide = false, titleActions, viewActions, onDisplay, onSelect, onCreate,
  onAddSource, addDisabled, onOpen, onClose }: {
  kind: PhoneMenu;
  /** On a touch screen wider than a phone, "새 보기 추가" holds the desktop's layouts (WideAddViewSheet). */
  wide?: boolean;
  /** The view a "title" or "view" menu is for. */
  view?: GraphView;
  views: readonly GraphView[];
  activeId: string;
  /** A wiki's Home, over its views. */
  lead?: ViewListLead;
  addTypes: readonly GraphViewType[];
  /** The title's actions, or none for someone who can only read. */
  titleActions(view: GraphView): DatabaseViewAction[];
  viewActions(view: GraphView): DatabaseViewAction[];
  onDisplay(display: TabDisplay): void;
  onSelect(view: GraphView): void;
  onCreate?(type: GraphViewType): void;
  onAddSource?(): void;
  /** At the database's views' limit (PhoneViewListSheet). */
  addDisabled?: boolean;
  onOpen(kind: PhoneMenu, viewId?: string): void;
  onClose(): void;
}) {
  const [choosingDisplay, setChoosingDisplay] = useState(false);
  const withDisplay = (actions: DatabaseViewAction[]) => actions.map((action) => action.id === "display"
    ? { ...action, act: () => setChoosingDisplay(true) } : action);
  if (kind === "title" && view) return <PhoneDatabaseActionsSheet actions={titleActions(view)} onClose={onClose} />;
  if ((kind === "add" || kind === "titleAdd") && onCreate) {
    if (wide) return <WideAddViewSheet types={addTypes} onCreate={onCreate} onAddSource={onAddSource} onClose={onClose} />;
    return <PhoneAddViewSheet types={addTypes} screen={kind === "add"} onCreate={onCreate} onAddSource={onAddSource} onClose={onClose} />;
  }
  if (kind === "overflow") {
    return <PhoneViewListSheet views={views} activeId={activeId} lead={lead} onSelect={onSelect}
      // A list whose views have no actions (a visitor's) draws no "⋯".
      onActions={views.some((candidate) => viewActions(candidate).length) ? (candidate) => viewActions(candidate).length ? onOpen("view", candidate.id) : undefined : undefined}
      onNewView={onCreate ? () => onOpen("add") : undefined} onAddSource={onAddSource} addDisabled={addDisabled} onClose={onClose} />;
  }
  if (kind === "view" && view) {
    return <>
      <PhoneViewOptionsSheet actions={withDisplay(viewActions(view))} onClose={onClose} />
      {choosingDisplay ? (
        // The tab row shows the active view's data source's choice (useViewTabDisplay).
        <PhoneTabDisplaySheet dataSourceId={(views.find(({ id }) => id === activeId) ?? view).dataSourceId}
          onPick={onDisplay} onDone={onClose} onClose={() => setChoosingDisplay(false)} />
      ) : null}
    </>;
  }
  return null;
}

/**
 * "다음과 같이 표시" over a view's options, as Notion's phone (seen 2026-11-15
 * at 402pt): a whole sheet without a title or "완료", its three choices in a
 * group, the one the tabs show now ticked at its end, and "나에게만 적용됨"
 * grey under them; a pick, the one shown too, closes the view's sheets
 * (`onDone`), while pulling it down goes back to the view's options.
 */
function PhoneTabDisplaySheet({ dataSourceId, onPick, onDone, onClose }: {
  dataSourceId: string;
  onPick(display: TabDisplay): void;
  onDone(): void;
  onClose(): void;
}) {
  const labels = databaseViewLabels();
  const [display] = useViewTabDisplay(dataSourceId);
  return (
    <PhoneSheet title="" label={labels.showTabsAs} onClose={onClose} data={{ "data-view-tab-display-sheet": "" }}>
      <PhoneSheetGroup>
        {VIEW_TAB_DISPLAYS.map((key) => (
          <PhoneSheetRow key={key} label={labels.tabDisplay(key)} more={false}
            value={key === display ? <CheckIcon size="regular" className={styles.displayTick} aria-hidden="true" /> : undefined}
            data={{ "data-phone-choice": key, "data-chosen": key === display ? "true" : undefined }}
            onPress={() => { onDone(); if (key !== display) onPick(key); }} />
        ))}
      </PhoneSheetGroup>
      <p className={styles.displayNote}>{labels.tabDisplayOnlyYou}</p>
    </PhoneSheet>
  );
}
