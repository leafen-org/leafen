import { Fragment, useContext, useRef } from "react";
import { useTranslation } from "react-i18next";
import { CheckboxIcon, ChevronRight, DependencyIcon, PageLayoutIcon, SprintIcon, SubitemsIcon } from "@/icons/leafen";
import { placeAround, popoverBackdropStyle, popoverPlacementStyle, usePopoverPlacement, type PopoverRect, type PopoverSize, type PopoverViewport } from "@/lib/popoverPlacement";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { DatabaseSprintContext } from "./databaseSprintContext";
import styles from "./DatabaseMoreSettingsMenu.module.css";

/** The settings "추가 설정" opens, in the view settings' panel. */
export type DatabaseSettingsStart = "subitems" | "dependencies" | "taskDatabase" | "sprints";

// Both lists are 250px, their right edge at their row's and under it, over
// the row where the room under it is short (Notion's, seen 2026-11-03: an
// inline database's row at 768 opened it above, a page's at 709 under it).
const MENU_WIDTH = 250;
const placeAtRow = (anchor: PopoverRect) => (size: PopoverSize, viewport: PopoverViewport) => placeAround(anchor, size, viewport, { gap: 0, align: "end" });

/**
 * "추가 설정" in a database's view settings, as Notion's (seen 2026-10-01 and
 * 2026-10-11, at 375px 2026-10-30): a 240px menu right under its row with
 * "하위 항목" (on the first screen once on), "종속성" and "페이지 레이아웃
 * 사용자 지정", which opens the pages' layout editor; Leafen adds its sprints
 * after them. A setting that is on says "켜짐" beside it (Notion's 종속성 and
 * 스프린트, seen 2026-11-09 on a phone and a desktop). The phone's sheet is the
 * same list. "유형 전환" and "보관된 페이지 보기" are on the settings' first
 * screen, as Notion's (seen 2026-10-11, `DatabaseTypeSwitchMenu`). Notion's
 * "연결" — other tools' connected properties, such as Google Drive files,
 * GitHub pull requests, Figma pages and Zendesk tickets (seen 2026-10-01 and
 * 2026-10-30) — is left out.
 */
function useMoreSettingsItems(subitemsOn: boolean, layout: boolean, taskDatabase = false, dependenciesOn = false, wiki = false) {
  const { t } = useTranslation(["databaseView", "databaseTasks"]);
  const sprintsOn = useContext(DatabaseSprintContext)?.state?.enabled === true;
  const sprints = { id: "sprints", label: t("databaseTasks:sprints"), icon: SprintIcon, on: sprintsOn };
  // A task database's: "하위 작업 · 종속성 · 스프린트 · 페이지 레이아웃 사용자
  // 지정", its sprints in Notion's place (seen 2026-11-02).
  return [
    // A wiki's are its "하위 페이지" (seen 2026-11-11).
    ...(subitemsOn ? [] : [{ id: "subitems", label: t(taskDatabase ? "databaseView:subtasks" : wiki ? "databaseView:subpages" : "databaseView:subitems"), icon: SubitemsIcon }]),
    { id: "dependencies", label: t("databaseView:dependencies"), icon: DependencyIcon, on: dependenciesOn },
    ...(taskDatabase ? [sprints] : []),
    ...(layout ? [{ id: "pageLayout", label: t("databaseView:customizePageLayout"), icon: PageLayoutIcon }] : []),
  ];
}

export function DatabaseMoreSettingsMenu({ anchor, subitemsOn = false, taskDatabase = false, dependenciesOn = false, wiki = false, onLayout, onPick, onClose }: {
  anchor: DOMRect; subitemsOn?: boolean; taskDatabase?: boolean; dependenciesOn?: boolean; wiki?: boolean; onLayout?(): void; onPick(start: DatabaseSettingsStart): void; onClose(): void;
}) {
  const { t } = useTranslation(["databaseView", "databaseTasks"]);
  const menuRef = useRef<HTMLDivElement>(null);
  const items = useMoreSettingsItems(subitemsOn, Boolean(onLayout), taskDatabase, dependenciesOn, wiki);
  const picker = useNativeMenu({ items, open: true, menuRef, label: t("databaseView:moreSettings"), onClose,
    keyOf: item => item.id, textOf: item => item.label,
    onAction: item => item.id === "pageLayout" ? onLayout?.() : onPick(item.id as DatabaseSettingsStart) });
  const place = usePopoverPlacement(menuRef, placeAtRow(anchor),
    [anchor], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose });
  return <>
    <button type="button" className={styles.backdrop} style={popoverBackdropStyle(place)} tabIndex={-1}
      aria-label={t("databaseView:closeMoreSettings")} onClick={onClose} />
    <div {...picker.menuProps} ref={menuRef} className={styles.menu} data-more-settings-menu
      style={{ width: MENU_WIDTH, ...popoverPlacementStyle(place) }}>
      {items.map(item => <Fragment key={item.id}>
        {item.id === "sprints" && !taskDatabase ? <div className={styles.divider} /> : null}
        <NativeMenuItem state={picker.state} itemKey={item.id} className={styles.item} data-more-setting={item.id}>
          {({ labelProps }) => <><item.icon size="regular" aria-hidden="true" /><span {...labelProps}>{item.label}</span>
            {"on" in item && item.on ? <small>{t("databaseView:taskFeatureOn")}</small> : null}
            <ChevronRight size="small" aria-hidden="true" /></>}
        </NativeMenuItem></Fragment>)}
    </div>
  </>;
}

/**
 * The settings' "유형 전환 ›", as Notion's (seen 2026-10-11): a 250px menu
 * beside its row with "작업 데이터베이스로 전환" ("켜짐" once it is one).
 * Notion's "스킬 데이터베이스로 전환" isn't: Leafen keeps no skills (the
 * agents-outside decision of 2026-10-29, 3).
 */
export function DatabaseTypeSwitchMenu({ anchor, taskDatabase, onPick, onClose }: {
  anchor: DOMRect; taskDatabase: boolean; onPick(start: DatabaseSettingsStart): void; onClose(): void;
}) {
  const { t } = useTranslation("databaseView");
  const menuRef = useRef<HTMLDivElement>(null);
  const picker = useNativeMenu({ items: ["taskDatabase"], open: true, menuRef, label: t("convertType"), onClose,
    keyOf: key => key, textOf: () => t("convertToTaskDatabase"), onAction: () => onPick("taskDatabase") });
  const place = usePopoverPlacement(menuRef, placeAtRow(anchor),
    [anchor], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose });
  return <>
    <button type="button" className={styles.backdrop} style={popoverBackdropStyle(place)} tabIndex={-1}
      aria-label={t("closeMoreSettings")} onClick={onClose} />
    <div {...picker.menuProps} ref={menuRef} className={styles.menu} data-convert-type-menu
      style={{ width: MENU_WIDTH, ...popoverPlacementStyle(place) }}>
      <NativeMenuItem state={picker.state} itemKey="taskDatabase" className={styles.item} data-more-setting="taskDatabase">
        {({ labelProps }) => <><CheckboxIcon size="regular" aria-hidden="true" /><span {...labelProps}>{t("convertToTaskDatabase")}</span>
          {taskDatabase ? <small>{t("taskFeatureOn")}</small> : null}</>}
      </NativeMenuItem>
    </div>
  </>;
}

/**
 * On a phone, "추가 설정" and "유형 전환" open over the settings sheet as half
 * sheets with "완료", their choices in one group, as Notion's (seen
 * 2026-10-21 at 375px): "추가 설정"'s each "›", "유형 전환"'s without.
 */
export function DatabaseMoreSettingsSheet({ subitemsOn = false, taskDatabase = false, dependenciesOn = false, wiki = false, zIndex, onLayout, onPick, onClose }: {
  subitemsOn?: boolean; taskDatabase?: boolean; dependenciesOn?: boolean; wiki?: boolean; zIndex?: number; onLayout?(): void; onPick(start: DatabaseSettingsStart): void; onClose(): void;
}) {
  const { t } = useTranslation("databaseView");
  const items = useMoreSettingsItems(subitemsOn, Boolean(onLayout), taskDatabase, dependenciesOn, wiki);
  return (
    <PhoneSheet title={t("moreSettings")} size="half" zIndex={zIndex} onClose={onClose}
      action={{ label: t("done"), onPress: onClose }} data={{ "data-more-settings-menu": "" }}>
      <PhoneSheetGroup>
        {items.map((item) => <PhoneSheetRow key={item.id} icon={<item.icon size="regular" />} label={item.label}
          value={"on" in item && item.on ? t("taskFeatureOn") : undefined}
          onPress={() => item.id === "pageLayout" ? onLayout?.() : onPick(item.id as DatabaseSettingsStart)} data={{ "data-more-setting": item.id }} />)}
      </PhoneSheetGroup>
    </PhoneSheet>
  );
}

export function DatabaseTypeSwitchSheet({ taskDatabase, zIndex, onPick, onClose }: {
  taskDatabase: boolean; zIndex?: number; onPick(start: DatabaseSettingsStart): void; onClose(): void;
}) {
  const { t } = useTranslation("databaseView");
  return (
    <PhoneSheet title={t("convertType")} size="half" zIndex={zIndex} onClose={onClose}
      action={{ label: t("done"), onPress: onClose }} data={{ "data-convert-type-menu": "" }}>
      <PhoneSheetGroup>
        <PhoneSheetRow icon={<CheckboxIcon size="regular" />} label={t("convertToTaskDatabase")} more={false}
          value={taskDatabase ? t("taskFeatureOn") : undefined} onPress={() => onPick("taskDatabase")}
          data={{ "data-more-setting": "taskDatabase" }} />
      </PhoneSheetGroup>
    </PhoneSheet>
  );
}
