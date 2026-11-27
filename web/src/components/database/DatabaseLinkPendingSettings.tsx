import { useRef, useState } from "react";
import { i18next } from "@/i18n";
import type { GraphView, GraphViewType } from "@/contentGraph";
import {
  ArchiveIcon, BulletedListIcon, CheckIcon, ConvertIcon, DatabaseCylinderIcon, DotsHorizontal, ExpandDiagonalIcon, EyeIcon, FilterIcon,
  GroupIcon, LinkIcon, LockIcon, PaintRollerIcon, SortIcon, UnlockIcon,
} from "@/icons/leafen";
import { copyText } from "@/lib/copyText";
import { NativeChoiceGrid } from "@/lib/nativeChoiceGrid";
import { useOptionalProductToast } from "@/lib/productToast";
import { useRouter } from "@/lib/router";
import { PhoneSheet } from "../shell/PhoneSheet";
import { DatabaseViewNameField } from "./DatabaseViewNameField";
import { DatabaseViewSettingsItem as Item } from "./DatabaseViewSettingsPanel";
import { databaseViewLabels } from "./databaseViewLabels";
import { LAYOUT_VIEW_TYPES } from "./databaseToolbarModel";
import { databaseViewLink, databaseViewLinkId, ViewTypeIcon } from "./databaseViewShared";
import { PhoneSettingsListContext } from "./PropertySettingRow";
import sheet from "./databaseSheet";
import styles from "./DatabaseViewSettingsPanel.module.css";

// Its "레이아웃" tiles, as a view's settings' (dashboard last; no form, as its "새 보기 추가").
const types: readonly GraphViewType[] = [...LAYOUT_VIEW_TYPES, "dashboard"];

/**
 * A linked view's "설정" while it waits for its source, on a phone, as
 * Notion's (seen 2026-11-18 on a Galaxy A90's Chrome at 411, in two tabs and
 * one tab reloaded): the view's settings sheet with the keyboard down and
 * nothing focused — its unnamed name over "보기 이름", "레이아웃", "속성 표시
 * 여부", "필터", "정렬", "그룹화", "조건부 색상", "보기 링크 복사"; "데이터
 * 소스 설정" with "속성 편집", "작업 데이터베이스로 전환" and "추가 설정"
 * faded and "보관된 페이지 보기"; "전체 페이지로 열기"; "데이터 소스 관리"
 * and "보기 잠금". It has no "데이터베이스" row. Its name, icon, description
 * and layout are the view's own. What it would show of a source — its
 * properties, filters, sorts, groups, colours, archived pages and sources —
 * Notion shows of a stand-in source none of the person's (링크, 설명, 언어,
 * 제목, 체크 표시됨, 출처, 캡션, 크기); here those rows choose its source, as
 * its "데이터 소스 연결" does (approved difference: a source not theirs is
 * nothing to set).
 */
export function DatabaseLinkPendingSettings({ view, pageId, blockId, locked, fullPageHref, onUpdateView, onConnect, onLock, onClose }: {
  view: GraphView;
  pageId: string;
  blockId: string;
  /** Its block's lock, which "보기 잠금" turns. */
  locked: boolean;
  fullPageHref?: string;
  onUpdateView(view: GraphView): void;
  onConnect(): void;
  onLock(locked: boolean): void;
  onClose(): void;
}) {
  const labels = databaseViewLabels();
  const toast = useOptionalProductToast();
  const router = useRouter();
  const [screen, setScreen] = useState<"layout" | null>(null);
  const layoutGrid = useRef<HTMLDivElement>(null);
  const connect = () => { onClose(); onConnect(); };
  const updateConfig = (patch: Record<string, unknown>) => {
    const config: Record<string, unknown> = { ...view.config, ...patch };
    for (const key of Object.keys(patch)) if (patch[key] === undefined) delete config[key];
    onUpdateView({ ...view, config });
  };
  const copyLink = async () => {
    onClose();
    const copied = await copyText(databaseViewLink(databaseViewLinkId({ pageId, blockId, ownPage: !fullPageHref }), view.id));
    toast?.notify(i18next.t(copied ? "databaseView:copiedViewLink" : "databaseView:copyViewLinkFailed"), copied ? "success" : "error");
  };
  const main = <>
    <DatabaseViewNameField view={view} newView={false} locked={false} onRename={(name) => onUpdateView({ ...view, name })} onUpdateConfig={updateConfig} />
    <Item icon={<ViewTypeIcon type={view.type} size="regular" />} label={labels.layout} meta={labels.viewTypes[view.type]} marker="layout"
      onClick={() => setScreen("layout")} />
    <Item icon={<EyeIcon size="regular" weight="regular" />} label={labels.propertyVisibility} marker="properties" onClick={connect} />
    <Item icon={<FilterIcon size="regular" />} label={labels.filter} marker="filter" onClick={connect} />
    <Item icon={<SortIcon size="regular" />} label={labels.sort} marker="sort" onClick={connect} />
    <Item icon={<GroupIcon size="regular" />} label={labels.group} marker="group" onClick={connect} />
    <Item icon={<PaintRollerIcon size="regular" />} label={labels.conditionalColor} marker="color" onClick={connect} />
    <Item icon={<LinkIcon size="regular" />} label={labels.copyViewLink} next={false} marker="link" onClick={() => void copyLink()} />
    <div className={styles.divider} />
    <div className={styles.section}>{labels.dataSourceSettings}</div>
    <Item icon={<BulletedListIcon size="regular" />} label={labels.editProperties} marker="editProperties" disabled />
    <Item icon={<ConvertIcon size="regular" />} label={i18next.t("databaseView:convertToTaskDatabase")} marker="convertType" disabled />
    <Item icon={<ArchiveIcon size="regular" />} label={labels.viewArchivedPages} next={false} marker="archived" onClick={connect} />
    <Item icon={<DotsHorizontal size="regular" />} label={labels.moreSettings} marker="settings" disabled />
    {fullPageHref ? <>
      <div className={styles.divider} />
      <Item icon={<ExpandDiagonalIcon size="regular" />} label={labels.openAsFullPage} next={false} marker="fullPage"
        onClick={() => { onClose(); router.push(fullPageHref); }} />
    </> : null}
    <div className={styles.divider} />
    <Item icon={<DatabaseCylinderIcon size="regular" />} label={labels.manageDataSources} marker="dataSources" onClick={connect} />
    <Item icon={locked ? <UnlockIcon size="regular" /> : <LockIcon size="regular" />} label={locked ? labels.unlockViews : labels.lockViews}
      next={false} marker="lock" onClick={() => { onClose(); onLock(!locked); }} />
  </>;
  // "레이아웃" turns its layout among the waiting views' (the tiles of Notion's screen; its options are a source's).
  const layout = (
    <div className={styles.screen} data-view-settings-screen="layout">
      <fieldset className={sheet.layoutFieldset}>
        <NativeChoiceGrid items={types} keyOf={(type) => type} textOf={(type) => labels.viewTypes[type]} checkedKey={view.type}
          disabledKeys={[view.type]} onAction={(type) => onUpdateView({ ...view, type })} listRef={layoutGrid} autoFocus={false}
          label={labels.databaseViewType} className={`${sheet.layoutTypeGrid} ${sheet.layoutTypeGridScreen}`} itemClassName={sheet.layoutTypeButton}>
          {(type) => <><ViewTypeIcon type={type} size="regular" /><span>{labels.viewTypes[type]}</span>
            {view.type === type && <CheckIcon size="small" className={sheet.check} aria-hidden="true" />}</>}
        </NativeChoiceGrid>
      </fieldset>
    </div>
  );
  return (
    <PhoneSheet title={screen === "layout" ? labels.layout : labels.settings} label={labels.viewSettingsLabel} tone="settings" onClose={onClose}
      onBack={screen ? () => setScreen(null) : undefined}
      data={{ "data-database-view-settings": screen ?? "main", "data-link-pending-settings": blockId, "data-popover": "true" }}>
      <div className={styles.phoneList} data-phone-settings>
        <PhoneSettingsListContext.Provider value={true}>{screen === "layout" ? layout : main}</PhoneSettingsListContext.Provider>
      </div>
    </PhoneSheet>
  );
}
