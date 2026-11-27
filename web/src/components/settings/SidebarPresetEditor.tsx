import { useState, type ReactNode, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { AnchoredIconPicker, PICKER_WIDTH } from "@/components/page/AnchoredEmojiPicker";
import { useProductPages } from "@/components/product/productPagesContext";
import { SidebarCustomizeList } from "@/components/sidebar/SidebarCustomizeList";
import { SidebarDatabaseShortcutMenu } from "@/components/sidebar/SidebarDatabaseShortcut";
import { SidebarViewSectionName } from "@/components/sidebar/SidebarDatabaseViewSection";
import { SidebarSectionMenu } from "@/components/sidebar/SidebarSectionMenu";
import { SidebarShortcutMenu } from "@/components/sidebar/SidebarShortcutMenu";
import { SidebarShortcutIcon, SidebarShortcutsEditor, sidebarShortcutLabel, type SidebarShortcutMenuRender } from "@/components/sidebar/SidebarShortcuts";
import { SidebarViewPicker } from "@/components/sidebar/SidebarViewPicker";
import { SidebarViewSectionMenu } from "@/components/sidebar/SidebarViewSectionMenu";
import {
  addSidebarShortcut, addSidebarView, canAddSidebarShortcut, canAddSidebarView, changeSidebarSection, changeSidebarShortcut, changeSidebarView,
  knownSidebarSection, moveSidebarSection, moveSidebarSectionBefore, moveSidebarShortcut, moveSidebarShortcutBefore, newSidebarTabId,
  removeSidebarShortcut, setSidebarSectionHidden, sidebarAddableSections, sidebarSection, sidebarSectionTeamspace, sidebarShownSections,
  sidebarTabShortcuts,
} from "@/components/sidebar/sidebarLayoutModel";
import { Copy, UndoIcon, WarningTriangleIcon } from "@/icons/leafen";
import { SidebarSectionGlyph } from "@/components/sidebar/SidebarSectionGlyph";
import { SIDEBAR_PRESET_TAB, sidebarPresetFromLayout, sidebarPresetLayout, type SidebarPreset } from "@/lib/sidebarPresets";
import { knownTeamspaceName } from "@/lib/teamspaceNames";
import type { Session } from "@/types";
import { workspaceMembershipBounds, type SidebarLayout, type SidebarLayoutSection } from "@/workspaceMembershipWire.generated";
import { SidebarPresetPreview, SidebarPresetTab } from "./SidebarPresetPreview";
import styles from "./SidebarPresetsSection.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

const TAB = SIDEBAR_PRESET_TAB;

/**
 * The editor beside the presets, as Notion's (seen 2026-11-09): the chosen
 * preset's icon ("아이콘 바꾸기", Notion's icons alone with "제거"), its name
 * ("새 탭" until named) and "변경 사항 초기화" ↩ once it changed — Leafen's
 * own list has "복제" there and can't be edited —; then what it holds as the
 * sidebar's customize mode lists it: its shortcuts and "단축키 추가", a line,
 * its sections and "섹션 추가", each dragged by ⠿, with its ⚙ menu and ×,
 * and ⚠ on one naming a page not every member opens. "미리보기" at the
 * bottom turns the list under the head into the sidebar it makes, the head
 * showing the preset as its tab reads, "미리보기 종료" back.
 */
export function SidebarPresetEditor({ session, workspaceId, preset, homeSections, icon, untitled, changed, editable, canDuplicate, warned,
  nameRef, onChange, onReset, onDuplicate }: {
  session: Session;
  workspaceId: string;
  /** The preset, or none for Leafen's own list. */
  preset: SidebarPreset | null;
  homeSections: readonly SidebarLayoutSection[];
  icon: ReactNode;
  untitled: string;
  changed: boolean;
  editable: boolean;
  canDuplicate: boolean;
  /** The items naming a page not every member opens, by key. */
  warned: ReadonlySet<string>;
  nameRef: RefObject<HTMLInputElement | null>;
  onChange: (preset: SidebarPreset) => void;
  onReset: () => void;
  onDuplicate: () => void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "sidebar"]);
  const pages = useProductPages()?.pages ?? [];
  const [previewing, setPreviewing] = useState(false);
  const [iconAnchor, setIconAnchor] = useState<{ top: number; bottom: number; left: number } | null>(null);
  const layout = preset ? sidebarPresetLayout(preset) : null;
  const change = (next: SidebarLayout) => preset && onChange(sidebarPresetFromLayout(preset, next));
  const shown = layout ? sidebarShownSections(layout, TAB) : homeSections.map((section) => section.id);
  // One teamspace's own section goes by the teamspace's name, as the sidebar's does.
  const sectionName = (key: string) => (knownSidebarSection(key) ? t(`sidebar:${key === "private" ? "privatePages" : key}`)
    : sidebarSectionTeamspace(key) ? knownTeamspaceName(sidebarSectionTeamspace(key)) || t("sidebar:teamspaces")
      : (layout && sidebarSection(layout, TAB, key).view?.name.trim()) || t("sidebar:databaseView"));
  const presetView = (key: string) => (layout && !knownSidebarSection(key) ? sidebarSection(layout, TAB, key).view : undefined);
  const warning = (key: string) => (warned.has(key) ? (
    <span className={styles.warn} title={t("sidebarPresetPageWarning")} aria-label={t("sidebarPresetPageWarning")} role="img"
      data-sidebar-preset-warning={key}><WarningTriangleIcon size="small" aria-hidden="true" /></span>
  ) : null);

  function sectionMenu(key: string) {
    if (!layout) return null;
    const section = sidebarSection(layout, TAB, key), at = shown.indexOf(key);
    const moves = { canMoveUp: at > 0, canMoveDown: at >= 0 && at < shown.length - 1,
      onMove: (step: -1 | 1) => change(moveSidebarSection(layout, TAB, key, step, shown)),
      onRemove: () => change(setSidebarSectionHidden(layout, TAB, key, true)) };
    return section.view ? (
      <SidebarViewSectionMenu name={sectionName(key)} view={section.view} limit={section.limit} graph={null} session={session}
        workspaceId={workspaceId} {...moves} onChange={(next) => change(changeSidebarView(layout, TAB, key, next))}
        onLimit={(limit) => change(changeSidebarSection(layout, TAB, key, { limit }))} />
    ) : <SidebarSectionMenu name={sectionName(key)} section={section} {...moves} onChange={(next) => change(changeSidebarSection(layout, TAB, key, next))} />;
  }

  const shortcutMenu: SidebarShortcutMenuRender = (shortcut, label, rename, databaseTitle) => {
    if (!layout) return null;
    const all = sidebarTabShortcuts(layout, TAB), at = all.findIndex((candidate) => candidate.key === shortcut.key);
    const step = (by: -1 | 1) => () => change(moveSidebarShortcut(layout, TAB, shortcut.key, by));
    const props = { label, shortcut, onRename: rename, onMove: { up: at > 0 ? step(-1) : undefined, down: at < all.length - 1 ? step(1) : undefined },
      onRemove: () => change(removeSidebarShortcut(layout, TAB, shortcut.key)) };
    return shortcut.kind === "new_database_page" ? (
      <SidebarDatabaseShortcutMenu {...props} databaseTitle={databaseTitle ?? "…"} session={session}
        holder={pages.find((page) => page.id === shortcut.pageId)}
        onChange={(next) => change(changeSidebarShortcut(layout, TAB, shortcut.key, next))} />
    ) : <SidebarShortcutMenu {...props} onOpenIn={(openIn) => change(changeSidebarShortcut(layout, TAB, shortcut.key, { openIn }))} />;
  };

  const readOnlyItems = (
    <div className={styles.items} role="group" aria-label={t("sidebarPresetItems")}>
      {(preset?.shortcuts ?? []).map((shortcut) => {
        const page = pages.find((candidate) => candidate.id === shortcut.pageId);
        return (
          <div key={shortcut.key} className={styles.itemRow} data-sidebar-preset-shortcut={shortcut.kind}>
            <span className={styles.itemIcon}><SidebarShortcutIcon shortcut={shortcut} page={page} /></span>
            <span>{sidebarShortcutLabel(shortcut, page, (key, options) => t(`sidebar:${key}`, options))}</span>
          </div>
        );
      })}
      {preset?.shortcuts?.length ? <div className={styles.divider} /> : null}
      {shown.map((key) => (
        <div key={key} className={styles.itemRow} data-sidebar-preset-section={key}>
          <span className={styles.itemIcon}><SidebarSectionGlyph id={key} view={presetView(key)} session={session} size="wide" /></span>
          <span>{sectionName(key)}</span>
        </div>
      ))}
    </div>
  );

  const editor = layout && editable ? (
    <SidebarCustomizeList sections={shown} name={sectionName}
      label={(key) => {
        const view = knownSidebarSection(key) ? undefined : sidebarSection(layout, TAB, key).view;
        return view ? <SidebarViewSectionName view={view} session={session} fallback={t("sidebar:databaseView")} /> : sectionName(key);
      }}
      mark={warning}
      icon={(key, size) => <SidebarSectionGlyph id={key} view={presetView(key)} session={session} size={size} />}
      addable={sidebarAddableSections(layout, TAB)}
      menu={sectionMenu}
      shortcuts={(
        <div className={styles.shortcutsBlock}>
          <SidebarShortcutsEditor shortcuts={sidebarTabShortcuts(layout, TAB)} pages={pages} session={session} workspaceId={workspaceId}
            canAdd={canAddSidebarShortcut(layout, TAB)} mark={(shortcut) => warning(shortcut.key)}
            onAdd={(kind, pageId, name) => change(addSidebarShortcut(layout, TAB, { key: newSidebarTabId(), kind, pageId, name }))}
            onRemove={(key) => change(removeSidebarShortcut(layout, TAB, key))}
            onMove={(key, before) => change(moveSidebarShortcutBefore(layout, TAB, key, before))}
            menu={shortcutMenu} onRename={(key, name) => change(changeSidebarShortcut(layout, TAB, key, { name }))} />
        </div>
      )}
      viewsFull={canAddSidebarView(layout, TAB) ? undefined : t("sidebar:viewsFull", { limit: workspaceMembershipBounds.sidebarViewsPerTab })}
      viewPicker={canAddSidebarView(layout, TAB) ? (close, anchor, back, byKeys) => (
        <SidebarViewPicker anchor={anchor} session={session} workspaceId={workspaceId} searchFirst={byKeys} onClose={back} onChoose={(source) => {
          close();
          change(addSidebarView(layout, TAB, { key: newSidebarTabId(), pageId: source.pageId, dataSourceId: source.dataSourceId, viewId: source.viewId }));
        }} />
      ) : undefined}
      onMove={(key, before) => change(moveSidebarSectionBefore(layout, TAB, key, before))}
      onRemove={(key) => change(setSidebarSectionHidden(layout, TAB, key, true))}
      onAdd={(id) => change(setSidebarSectionHidden(layout, TAB, id, false))} />
  ) : readOnlyItems;

  const name = preset ? preset.name : t("sidebar:home");
  const duplicateButton = (
    <button type="button" className={styles.headAction} aria-label={t("sidebarPresetDuplicate")} title={t("sidebarPresetDuplicate")}
      disabled={!canDuplicate} onClick={onDuplicate} data-sidebar-preset-duplicate><Copy size="regular" aria-hidden="true" /></button>
  );
  const locked = preset ? null : <div className={styles.locked}>{t("sidebarPresetLocked")}</div>;
  return (
    <div className={styles.panel} data-sidebar-preset-panel={preset?.id ?? "home"} data-previewing={previewing ? "true" : undefined}>
      {previewing ? (
        // The head keeps its place as Notion's preview does (seen 2026-11-18): the preset as its tab reads, 복제 beside
        // Leafen's own list and its note under it; a preset of the workspace's shows its tab alone, no ↩.
        <>
          <div className={styles.panelHead}>
            <SidebarPresetTab icon={icon} name={name || untitled} />
            {preset ? null : duplicateButton}
          </div>
          {locked}
          <SidebarPresetPreview session={session} workspaceId={workspaceId}
            sections={layout ? layout.tabs[0].sections : [...homeSections]} shortcuts={layout ? sidebarTabShortcuts(layout, TAB) : []} />
        </>
      ) : (
        <>
          <div className={styles.panelHead}>
            <span className={styles.panelIconLine}><button type="button" className={styles.panelIcon} aria-label={t("sidebarPresetChangeIcon")}
              title={t("sidebarPresetChangeIcon")} aria-haspopup="dialog" aria-expanded={iconAnchor !== null} disabled={!preset || !editable}
              data-sidebar-preset-icon onClick={(event) => {
                // Its middle under the icon's, as Notion's (seen 2026-11-09), kept in the window.
                const rect = event.currentTarget.getBoundingClientRect();
                setIconAnchor({ top: rect.top, bottom: rect.bottom, left: rect.left + (rect.width - PICKER_WIDTH) / 2 });
              }}><span className={styles.panelIconBox}>{icon}</span></button></span>
            <input ref={nameRef} className={styles.name} aria-label={t("sidebarPresetName")} readOnly={!preset || !editable} maxLength={40}
              value={name} placeholder={untitled} data-sidebar-preset-name
              onKeyDown={(event) => { if (fieldEnter(event)) event.currentTarget.blur(); }}
              onChange={(event) => preset && onChange({ ...preset, name: event.currentTarget.value })} />
            {preset ? (changed && editable ? (
              <button type="button" className={styles.headAction} aria-label={t("sidebarPresetResetChanges")} title={t("sidebarPresetResetChanges")}
                onClick={onReset} data-sidebar-preset-reset><UndoIcon size="regular" aria-hidden="true" /></button>
            ) : null) : duplicateButton}
          </div>
          {locked}
          <div className={styles.panelBody}>{editor}</div>
        </>
      )}
      <button type="button" className={styles.previewToggle} aria-pressed={previewing} data-sidebar-preset-preview-toggle
        onClick={() => setPreviewing((current) => !current)}>
        {t(previewing ? "sidebarPresetPreviewEnd" : "sidebarPresetPreview")}
      </button>
      {iconAnchor && preset ? (
        <AnchoredIconPicker anchor={iconAnchor} marker="sidebar-preset-icon-picker" onClose={() => setIconAnchor(null)}
          picker={{
            label: t("sidebarPresetChangeIcon"),
            onPickBuiltInIcon: (value) => { onChange({ ...preset, icon: value }); setIconAnchor(null); },
            onRemove: () => { const { icon: _removed, ...rest } = preset; onChange(rest); setIconAnchor(null); },
          }} />
      ) : null}
    </div>
  );
}
