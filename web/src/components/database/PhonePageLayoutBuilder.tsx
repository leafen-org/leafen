import { useEffect, useRef, useState, type MutableRefObject, type ReactNode, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import {
  AddCircleFillIcon, ArrowDown, ArrowLeft, ArrowRight, ArrowUp, CheckIcon, ChevronDown, DatabaseCylinderIcon, DotsHorizontal, EyeSlashIcon, Trash, X,
} from "@/icons/leafen";
import type { DatabasePageLayout, PageLayoutTab } from "@/lib/databasePageLayout";
import type { PropertyType } from "@/lib/types";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import type { DatabasePropertyEditing } from "./DatabasePropertyEditPanel";
import { PageLayoutAddMenu } from "./PageLayoutAddMenu";
import { CONTENT_TAB_SELECTION, PageLayoutCanvas, TAB_SELECTION, type PageLayoutSelection } from "./PageLayoutCanvas";
import { PageLayoutSidebar } from "./PageLayoutSidebar";
import { ContentTabIcon, PageLayoutContentTabFields } from "./PageLayoutContentTabFields";
import { addPropertyModule, canMoveModule, canRemoveModule, moduleLocation, moveModule, moveModuleTo, removeModule, type PageLayoutPlace } from "./pageLayoutModel";
import { layoutTabKeys } from "./pageLayoutTabOrder";
import { databaseViewLabels } from "./databaseViewLabels";
import { ViewTypeIcon } from "./databaseViewShared";
import styles from "./PhonePageLayoutBuilder.module.css";
import viewStyles from "./DatabaseViewPhoneSheets.module.css";

/** What the canvas shows whichever form the builder takes. */
export type PageLayoutCanvasShared = Omit<Parameters<typeof PageLayoutCanvas>[0],
  "layout" | "selected" | "onSelect" | "onModuleMenu" | "onAdd" | "onPanel" | "phoneArea">;

type Sheet = { kind: "area" } | { kind: "more" } | { kind: "settings" } | { kind: "add"; place: PageLayoutPlace; index: number }
  | { kind: "tabs" } | { kind: "contentTab" } | null;

/**
 * The page layout builder on a phone, as Notion's (seen 2026-10-24 at
 * 375px): a 45px bar of "페이지 ⌄" (the page or its details panel), a blue
 * "적용" and "⋯" (페이지 설정 · 취소 · a red 레이아웃 초기화); under it that
 * area's modules as cards. A card pressed opens its sheet — the title's
 * pinned properties, the property group's list, or a module's moves — and
 * "+" the properties to add.
 */
export function PhonePageLayoutBuilder({ rootRef, escapeRef, title, layout, onLayout, canvas, properties, editing, onAddProperty, onNewProperty, onNewRelation,
  candidates, busy, canReset, onApply, onReset, onLeave, wide }: {
  rootRef: RefObject<HTMLDivElement | null>;
  /** Escape closes an open sheet first: true when it did. */
  escapeRef: MutableRefObject<(() => boolean) | null>;
  title: string;
  layout: DatabasePageLayout;
  onLayout(next: DatabasePageLayout): void;
  canvas: PageLayoutCanvasShared;
  /** The properties as their settings list them. */
  properties: readonly GraphProperty[];
  editing?: DatabasePropertyEditing;
  onAddProperty?(type: PropertyType): string | null;
  /** The sheets' "속성 추가": the new property's name and type first. */
  onNewProperty?(button: HTMLElement, pin: boolean): void;
  /** "+"'s "관계형": which database first, its card going where "+" was. */
  onNewRelation?(place: PageLayoutPlace, index: number): void;
  /** The properties "+" may add: those not yet laid out. */
  candidates: readonly GraphProperty[];
  busy: boolean;
  canReset: boolean;
  onApply(): void;
  onReset(): void;
  onLeave(): void;
  /**
   * A wide touch screen's, as Notion's iPad (seen 2026-11-14 in iPad Safari
   * at 834 and 1210 wide): these sheets, but a bar of three thirds — the
   * previewed page's picker, the database's name, "× 취소 · 모든 페이지에 적용
   * · ⋯" — over the page's and the panel's cards side by side, as on a
   * desktop without its frame, handles or "⋯"; "⋯" holds 페이지 설정 and
   * 레이아웃 초기화. The picker's menu is the desktop's, under it, drawn in
   * the builder so its backdrop takes a press beside it.
   */
  wide?: { previewLabel: string; previewRef: RefObject<HTMLButtonElement | null>; previewDisabled: boolean; onPreview(): void; previewMenu: ReactNode };
}) {
  const { t } = useTranslation(["pageLayout", "common"]);
  const [area, setArea] = useState<PageLayoutPlace>("page");
  const [selected, setSelected] = useState<PageLayoutSelection>(null);
  const [sheet, setSheet] = useState<Sheet>(null);
  const addButton = useRef<HTMLButtonElement | null>(null);
  // The tab shown under the pill: the content's (null) until another is chosen, or made.
  const [openTab, setOpenTab] = useState<string | null>(null);
  const [settingsFor, setSettingsFor] = useState<string | null>(null);
  // A wide screen's shown tab is its pill chosen, its view settings opening as the desktop's do.
  const [tabSettings, setTabSettings] = useState<{ tabId: string; request: number } | null>(null);
  const openTabSettings = (tabId: string) => setTabSettings((current) => ({ tabId, request: (current?.request ?? 0) + 1 }));
  const tabIds = layout.tabs.map((tab) => tab.id).join(" ");
  const knownTabs = useRef(tabIds);
  useEffect(() => {
    const before = new Set(knownTabs.current.split(" "));
    knownTabs.current = tabIds;
    const made = layout.tabs.find((tab) => !before.has(tab.id));
    if (made && wide) {
      setSelected(`${TAB_SELECTION}${made.id}`); setSheet(null);
      setTabSettings((current) => ({ tabId: made.id, request: (current?.request ?? 0) + 1 }));
    }
    else if (made) { setOpenTab(made.id); setSheet(null); } else if (openTab && !layout.tabs.some((tab) => tab.id === openTab)) setOpenTab(null);
  }, [tabIds, layout.tabs, openTab, wide]);
  // A tab's "⋯" opens its view's settings once its view is there.
  useEffect(() => {
    if (!settingsFor || settingsFor !== openTab) return undefined;
    let tries = 0;
    const timer = window.setInterval(() => {
      const trigger = rootRef.current?.querySelector<HTMLElement>("[data-layout-module=tab] [data-database-settings-trigger]");
      if (trigger || ++tries > 40) { window.clearInterval(timer); setSettingsFor(null); trigger?.click(); }
    }, 50);
    return () => window.clearInterval(timer);
  }, [openTab, rootRef, settingsFor]);
  const close = () => { setSelected(null); setSheet(null); };
  escapeRef.current = () => {
    if (!selected && !sheet) return false;
    close();
    return true;
  };
  const at = selected && selected !== "heading" && !selected.startsWith(TAB_SELECTION) ? moduleLocation(layout, selected) : null;
  const module = at ? layout[at.place][at.index] : null;
  const change = (next: DatabasePageLayout) => { onLayout(next); close(); };
  const areaName = (place: PageLayoutPlace) => t(place === "page" ? "areaPage" : "areaPanel");
  const sidebar = (selection: PageLayoutSelection) => (
    <PageLayoutSidebar layout={layout} properties={properties} selected={selection} editing={editing} phone
      onLayout={onLayout} onNewProperty={onNewProperty} onSelect={(next) => { if (next === null) close(); else setSelected(next); }} />
  );
  const moduleName = !module ? "" : module.kind === "properties" ? t("modules.properties") : module.kind === "comments" ? t("modules.comments")
    : module.kind === "content" ? t("modules.content") : module.kind === "relations" ? t("modules.relations")
      : properties.find(({ id }) => id === module.propertyId)?.name ?? "";
  // A wide screen's tab chosen shows its view, no sheet.
  const tab = !wide && typeof selected === "string" && selected.startsWith(TAB_SELECTION)
    ? layout.tabs.find((candidate) => `${TAB_SELECTION}${candidate.id}` === selected) : undefined;
  const labels = databaseViewLabels();
  // "N개 보기", as Notion's (seen 2026-10-25 at 375px): the content and each tab with its "⋯", then "새 보기".
  const tabRow = (row: PageLayoutTab | null) => {
    const id = row?.id ?? null;
    const name = row ? row.name || t("untitled") : layout.contentTab?.name || t("contentTab");
    return (
      <div key={id ?? "content"} className={viewStyles.viewRow} data-layout-tab-row={id ?? "content"} data-active={openTab === id ? "true" : undefined}>
        <button type="button" className={viewStyles.viewPick} aria-current={openTab === id ? "true" : undefined} onClick={() => { setOpenTab(id); close(); }}>
          <span className={viewStyles.viewIcon} aria-hidden="true">
            {row ? <ViewTypeIcon type={row.view?.type ?? "table"} size="regular" /> : <ContentTabIcon tab={layout.contentTab} size="regular" />}
          </span>
          <span className={viewStyles.viewName}>{name}</span>
        </button>
        <button type="button" className={viewStyles.viewMore} aria-label={labels.openActions(name)} data-layout-tab-more
          onClick={() => { if (row) { setOpenTab(row.id); setSettingsFor(row.id); close(); } else setSheet({ kind: "contentTab" }); }}>
          <DotsHorizontal size="small" aria-hidden="true" />
        </button>
      </div>
    );
  };

  return (
    <div ref={rootRef} className={wide ? `${styles.builder} ${styles.wide}` : styles.builder} role="dialog" aria-modal="true" aria-label={t("title")} tabIndex={-1}
      data-page-layout-builder data-phone={wide ? undefined : "true"} data-touch-wide={wide ? "true" : undefined}>
      <div className={styles.bar}>
        {wide ? (
          <button ref={wide.previewRef} type="button" className={styles.previewPicker} aria-haspopup="menu" disabled={wide.previewDisabled}
            onClick={wide.onPreview} data-layout-preview-picker>
            <span>{wide.previewLabel}</span><ChevronDown size="tiny" aria-hidden="true" />
          </button>
        ) : (
          <button type="button" className={styles.areaPicker} aria-label={t("areaLabel", { area: areaName(area) })} aria-haspopup="dialog"
            onClick={() => setSheet({ kind: "area" })} data-layout-area-picker>
            {areaName(area)}<ChevronDown size="small" aria-hidden="true" />
          </button>
        )}
        {wide ? <span className={styles.barName}><DatabaseCylinderIcon size="regular" aria-hidden="true" /><span>{title}</span></span> : null}
        <span className={styles.barEnd}>
          {wide ? (
            <button type="button" className={styles.cancel} onClick={onLeave} data-layout-cancel>
              <X size="small" aria-hidden="true" />{t("common:actions.cancel")}
            </button>
          ) : null}
          <button type="button" className={styles.apply} disabled={busy} onClick={onApply} data-layout-apply>{t(wide ? "apply" : "applyShort")}</button>
          <button type="button" className={styles.more} aria-label={t("moreOptions")} aria-haspopup="dialog" disabled={busy}
            onClick={() => setSheet({ kind: "more" })} data-layout-more>
            <DotsHorizontal size="regular" aria-hidden="true" />
          </button>
        </span>
      </div>
      <div className={styles.body} aria-label={title}>
        {wide ? (
          // The content's pill chosen opens its sheet; a tab's shows its view, pressed again its settings.
          <PageLayoutCanvas {...canvas} layout={layout} touch selected={selected} tabSettings={tabSettings} onTabSettings={openTabSettings}
            onSelect={(next) => { if (next === CONTENT_TAB_SELECTION) setSheet({ kind: "contentTab" }); else setSelected(next); }}
            onModuleMenu={() => undefined} onPanel={() => undefined}
            onAdd={(place, index, button) => { addButton.current = button; setSheet({ kind: "add", place, index }); }} />
        ) : (
          <PageLayoutCanvas {...canvas} layout={layout} phoneArea={area} selected={selected}
            phoneTabs={{ openTabId: openTab, onOpen: () => setSheet({ kind: "tabs" }) }}
            onSelect={(next) => { if (next !== null) setSelected(next); }}
            onModuleMenu={() => undefined} onPanel={() => undefined}
            onAdd={(place, index, button) => { addButton.current = button; setSheet({ kind: "add", place, index }); }} />
        )}
      </div>
      {wide?.previewMenu}
      {sheet?.kind === "area" ? (
        <PhoneSheet title={t("areaLabel", { area: areaName(area) })} size="half" head={false} onClose={close} data={{ "data-layout-area-sheet": "" }}>
          <PhoneSheetGroup>
            {(["page", "panel"] as const).map((place) => (
              <PhoneSheetRow key={place} label={areaName(place)} description={t(place === "page" ? "areaPageNote" : "areaPanelNote")} more={false}
                value={place === area ? <span className={styles.chosen}><CheckIcon size="regular" aria-hidden="true" /></span> : undefined}
                onPress={() => { setArea(place); close(); }} data={{ "data-layout-area": place }} />
            ))}
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
      {sheet?.kind === "more" ? (
        <PhoneSheet title={t("moreOptions")} size="half" head={false} onClose={close} data={{ "data-layout-more-sheet": "" }}>
          <PhoneSheetGroup>
            <PhoneSheetRow label={t("pageSettings")} more={false} onPress={() => setSheet({ kind: "settings" })} data={{ "data-layout-page-settings": "" }} />
            {wide ? null : <PhoneSheetRow label={t("common:actions.cancel")} more={false} onPress={() => { close(); onLeave(); }} data={{ "data-layout-cancel": "" }} />}
          </PhoneSheetGroup>
          <PhoneSheetGroup>
            <PhoneSheetRow label={t("resetLayout")} more={false} danger disabled={!canReset} onPress={() => { close(); onReset(); }}
              data={{ "data-layout-reset": "" }} />
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
      {sheet?.kind === "settings" ? (
        <PhoneSheet title={t("pageSettings")} onClose={close} data={{ "data-layout-settings-sheet": "" }}>{sidebar(null)}</PhoneSheet>
      ) : null}
      {sheet?.kind === "tabs" ? (
        <PhoneSheet title={labels.viewsCount(layout.tabs.length + 1)} onClose={close} action={{ label: labels.sheetDone, onPress: close }}
          data={{ "data-layout-tabs-sheet": "" }}>
          <PhoneSheetGroup>{layoutTabKeys(layout).map((key) => tabRow(layout.tabs.find(({ id }) => id === key) ?? null))}</PhoneSheetGroup>
          <PhoneSheetGroup>
            <PhoneSheetRow icon={<AddCircleFillIcon size="regular" className={viewStyles.newViewIcon} />} label={labels.newView}
              description={labels.newViewKinds} more={false} onPress={() => canvas.onAddTab()} data={{ "data-layout-new-tab": "" }} />
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
      {sheet?.kind === "contentTab" ? (
        // The content's own: its icon, name and description, as Notion's "설정 보기" keeps them.
        <PhoneSheet title={t("tabSettings")} onClose={close} data={{ "data-layout-content-tab-sheet": "" }}>
          <PhoneSheetGroup>
            <PageLayoutContentTabFields phone tab={layout.contentTab} onChange={(contentTab) => onLayout({ ...layout, contentTab })} />
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
      {sheet?.kind === "add" ? (
        <PageLayoutAddMenu phone button={addButton} label={t(sheet.place === "panel" ? "addToPanel" : "addToPage")} candidates={candidates}
          canCreate={Boolean(onAddProperty)} canRelate={Boolean(onNewRelation)} onClose={close}
          onPick={(propertyId) => change(addPropertyModule(layout, sheet.place, sheet.index, propertyId))}
          onCreate={(type) => {
            if (type === "relation") { close(); onNewRelation?.(sheet.place, sheet.index); return; }
            const id = onAddProperty?.(type);
            if (id) change(addPropertyModule(layout, sheet.place, sheet.index, id)); else close();
          }} />
      ) : null}
      {selected === "heading" || tab || module?.kind === "properties" ? (
        <PhoneSheet title={selected === "heading" ? t("modules.heading") : tab ? tab.name || t("untitled") : t("modules.properties")} onClose={close}
          data={{ "data-layout-module-sheet": selected === "heading" ? "heading" : tab ? "tab" : "properties" }}>{sidebar(selected)}</PhoneSheet>
      ) : module && at ? (
        // A module's moves, those it can make, as Notion's (its discussion "숨기기", which turns "페이지" off).
        <PhoneSheet title={moduleName} size="half" onClose={close} data={{ "data-layout-module-sheet": module.kind }}>
          <PhoneSheetGroup>
            {canMoveModule(layout, module.id, -1) ? <PhoneSheetRow icon={<ArrowUp size="regular" />} label={t("moveUp")} more={false}
              onPress={() => change(moveModule(layout, module.id, -1))} data={{ "data-layout-module-action": "up" }} /> : null}
            {canMoveModule(layout, module.id, 1) ? <PhoneSheetRow icon={<ArrowDown size="regular" />} label={t("moveDown")} more={false}
              onPress={() => change(moveModule(layout, module.id, 1))} data={{ "data-layout-module-action": "down" }} /> : null}
            {module.kind === "property" ? <PhoneSheetRow icon={at.place === "page" ? <ArrowRight size="regular" /> : <ArrowLeft size="regular" />}
              label={t(at.place === "page" ? "moveToPanel" : "moveToPage")} more={false}
              onPress={() => change(moveModuleTo(layout, module.id, at.place === "page" ? "panel" : "page"))} data={{ "data-layout-module-action": "place" }} /> : null}
            {module.kind === "comments" ? <PhoneSheetRow icon={<EyeSlashIcon size="regular" weight="regular" />} label={t("hidePageComments")} more={false}
              onPress={() => change({ ...layout, options: { ...layout.options, pageComments: false } })} data={{ "data-layout-module-action": "hide" }} />
              : canRemoveModule(module) ? <PhoneSheetRow icon={<Trash size="regular" />} label={t("removeFromLayout")} more={false}
                onPress={() => change(removeModule(layout, module.id))} data={{ "data-layout-module-action": "remove" }} /> : null}
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
    </div>
  );
}
