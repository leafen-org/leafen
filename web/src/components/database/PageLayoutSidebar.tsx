import { SwitchMark } from "@/components/Switch";
import { OptionInfoTip } from "@/components/IconTipButton";
import { Fragment, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { SidebarActionMenu } from "@/components/sidebar/SidebarActionMenu";
import { ArrowRight, ChevronRight, DragHandleIcon, EyeClosedIcon, EyeIcon, EyeSlashIcon, PinIcon, Plus, Search, X } from "@/icons/leafen";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import { PAGE_LAYOUT_MAX_PINNED, type DatabasePageLayout } from "@/lib/databasePageLayout";
import type { DatabasePropertyEditing } from "./DatabasePropertyEditPanel";
import { PropertyIcon } from "./PropertyIcon";
import {
  addSection, dropGroupItem, dropPinned, moduleLocation, moveModuleTo, pinProperty, propertyGroupItems, removeSection, renameSection, unpinProperty,
} from "./pageLayoutModel";
import { usePageLayoutDrag } from "./usePageLayoutDrag";
import { TAB_SELECTION, type PageLayoutSelection } from "./PageLayoutCanvas";
import { removeLayoutTab, removeLayoutTabs } from "./pageLayoutTabOrder";
import styles from "./DatabasePageLayoutBuilder.module.css";
import moduleStyles from "./DatabasePageLayoutModules.module.css";
import phoneStyles from "./PhonePageLayoutBuilder.module.css";
import popoverStyles from "./PageLayoutCardPopover.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { SearchClearButton } from "@/components/PageActionSearch";

type Visibility = "always" | "whenEmpty" | "hidden";

function visibilityOf(property: GraphProperty): Visibility {
  if (property.config.hideInPagePanel) return "hidden";
  return property.config.hideWhenEmpty ? "whenEmpty" : "always";
}

/**
 * The builder's settings, as Notion's (seen 2026-10-12): in its right column
 * "페이지 설정" (구조 and 옵션), whatever is chosen (seen 2026-11-12: a tab
 * chosen opens its view's settings on the canvas, the column unchanged);
 * beside a chosen card (seen 2026-11-01) the title's pinned and unpinned
 * properties with "속성 라벨", or the property group's sections, order and
 * visibility, and "패널로 옮기기"; on a phone a tab's sheet too.
 */
export function PageLayoutSidebar({ layout, properties, selected, editing, onLayout, onNewProperty, onSelect, phone = false, popover = false }: {
  layout: DatabasePageLayout;
  properties: readonly GraphProperty[];
  selected: PageLayoutSelection;
  editing?: DatabasePropertyEditing;
  onLayout(next: DatabasePageLayout): void;
  /** Asks for a new property's name and type, under the button; the title's is pinned. Where the database's properties can change. */
  onNewProperty?(button: HTMLElement, pin: boolean): void;
  onSelect(selection: PageLayoutSelection): void;
  /**
   * In a phone's sheet, which names it: its rows in white groups under grey
   * names, as Notion's (seen 2026-10-24 at 375px), "섹션 추가" a row of its
   * own and two options saying more.
   */
  phone?: boolean;
  /** In the popover beside the chosen card, its search taking the keys. */
  popover?: boolean;
}) {
  const { t } = useTranslation(["pageLayout", "rowProperties"]);
  const [query, setQuery] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [visibilityFor, setVisibilityFor] = useState<{ id: string; button: HTMLButtonElement } | null>(null);
  const [inlineMenu, setInlineMenu] = useState(false);
  const inlineButton = useRef<HTMLButtonElement>(null);
  const visibilityButton = useRef<HTMLButtonElement | null>(null);
  const listed = properties.filter((property) => property.type !== "title");
  const matches = (property: GraphProperty) => !query.trim() || property.name.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase());
  const byId = (id: string) => listed.find((property) => property.id === id);
  // What an option does: on a phone a line under its name, elsewhere its name's ⓘ, as Notion's (seen 2026-11-14).
  const switchRow = (label: string, on: boolean, change: (on: boolean) => void, data: string, note?: string) => (
    <button type="button" role="switch" aria-checked={on} className={styles.optionRow} onClick={() => change(!on)} data-layout-option={data}
      aria-description={note && !phone ? note : undefined}>
      {note && phone ? <span className={phoneStyles.optionText}><span>{label}</span><small>{note}</small></span>
        : note ? <span className={styles.optionName}>{label}<OptionInfoTip label={note} className={styles.optionTip} /></span> : <span>{label}</span>}
      <SwitchMark on={on} />
    </button>
  );
  const group = (key: string, children: ReactNode) => phone
    ? <div key={key} className={phoneStyles.group}>{children}</div> : <Fragment key={key}>{children}</Fragment>;
  const sideClass = popover ? undefined : phone ? `${styles.side} ${phoneStyles.side}` : styles.side;
  const labelClass = popover ? popoverStyles.label : phone ? `${styles.sideLabel} ${phoneStyles.label}` : styles.sideLabel;
  const groupIds = listed.map((property) => property.id);
  const groupDrag = usePageLayoutDrag(propertyGroupItems(layout, groupIds).map((_, index) => index),
    (from, to, after) => onLayout(dropGroupItem(layout, groupIds, from, to, after)));
  const pinnedDrag = usePageLayoutDrag(layout.heading.pinned.map((_, index) => index),
    (from, to, after) => onLayout(dropPinned(layout, from, to, after)));
  // A row's "⠿" drags it among the rows of its list.
  const handle = (name: string, props: ReturnType<typeof groupDrag.handle>) => (
    <span role="button" tabIndex={0} className={`${styles.dragHandle} ${moduleStyles.dragHandle}`} aria-label={t("dragRow", { name })} {...props}>
      <DragHandleIcon size="tiny" aria-hidden="true" />
    </span>
  );
  const propertyRow = (property: GraphProperty, action: ReactNode, drag?: { handle: ReturnType<typeof groupDrag.handle>; target: ReturnType<typeof groupDrag.target> }) => (
    <div key={property.id} className={styles.listRow} data-layout-list-property={property.id} {...drag?.target}>
      {drag ? handle(property.name, drag.handle) : null}
      <PropertyIcon property={property} size="regular" /><span>{property.name}</span>{action}
    </div>
  );
  const searchClass = popover ? popoverStyles.searchInput : phone ? phoneStyles.search : styles.search;
  const searchField = (
    <input ref={searchRef} className={searchClass} value={query} placeholder={t("searchProperties")}
      aria-label={t("searchProperties")} autoFocus={popover && fieldFocusOnOpen()} onChange={(event) => setQuery(event.target.value)} />
  );
  // A phone's clears by the shared "입력 값 지우기", as Notion's sheet searches (seen 2026-11-17 in Android Chrome at 411).
  const search = phone ? <label className={`${phoneStyles.searchRow} ${phoneStyles.sideSearch}`}><Search size="page" aria-hidden="true" />{searchField}
    <SearchClearButton value={query} inputRef={searchRef} onClear={() => setQuery("")} /></label>
    : popover ? <label className={popoverStyles.search}><Search size="small" aria-hidden="true" />{searchField}</label> : searchField;

  if (selected === "heading") {
    const pinned = layout.heading.pinned.flatMap((id) => (byId(id) ? [byId(id)!] : []));
    const unpinned = listed.filter((property) => !layout.heading.pinned.includes(property.id));
    return (
      <div className={sideClass} data-layout-side="heading">
        {phone ? null : <div className={popover ? popoverStyles.head : styles.sideHead}><span>{t("modules.heading")}</span></div>}
        {group("search", search)}
        <div className={labelClass}>{t("pinnedProperties")}</div>
        {/* Notion's 15: told as it comes near, and why pinning stops at it. */}
        {pinned.length >= PAGE_LAYOUT_MAX_PINNED - 3 ? (
          <p className={styles.sideNote} role="status" data-layout-pinned-limit={pinned.length >= PAGE_LAYOUT_MAX_PINNED ? "full" : "near"}>
            {pinned.length >= PAGE_LAYOUT_MAX_PINNED
              ? t("pinnedFull", { max: PAGE_LAYOUT_MAX_PINNED })
              : t("pinnedNear", { count: pinned.length, max: PAGE_LAYOUT_MAX_PINNED })}
          </p>
        ) : null}
        {group("pinned", <>{pinned.filter(matches).map((property) => propertyRow(property, (
          <button type="button" aria-label={t("unpin", { name: property.name })} aria-pressed="true"
            onClick={() => onLayout(unpinProperty(layout, property.id))}><PinIcon size="small" weight="fill" aria-hidden="true" /></button>
        ), { handle: pinnedDrag.handle(layout.heading.pinned.indexOf(property.id)), target: pinnedDrag.target(layout.heading.pinned.indexOf(property.id)) }))}
        {onNewProperty && pinned.length < PAGE_LAYOUT_MAX_PINNED ? (
          <button type="button" className={styles.textButton} onClick={(event) => onNewProperty(event.currentTarget, true)} data-layout-new-property-button>
            <Plus size="small" aria-hidden="true" />{t("rowProperties:addProperty")}
          </button>
        ) : null}</>)}
        <div className={labelClass}>{t("unpinnedProperties")}</div>
        {group("unpinned", unpinned.filter(matches).map((property) => propertyRow(property, (
          <button type="button" aria-label={t("pin", { name: property.name })} aria-pressed="false" disabled={pinned.length >= PAGE_LAYOUT_MAX_PINNED}
            title={pinned.length >= PAGE_LAYOUT_MAX_PINNED ? t("pinnedFull", { max: PAGE_LAYOUT_MAX_PINNED }) : undefined}
            onClick={() => onLayout(pinProperty(layout, property.id))}><PinIcon size="small" aria-hidden="true" /></button>
        ))))}
        <div className={labelClass}>{t("options")}</div>
        {group("options", switchRow(t("propertyLabels"), layout.heading.labels, (labels) => onLayout({ ...layout, heading: { ...layout.heading, labels } }), "labels"))}
      </div>
    );
  }

  // A phone's tab sheet: its data source and relation, and "탭 삭제".
  const tab = typeof selected === "string" && selected.startsWith(TAB_SELECTION)
    ? layout.tabs.find((candidate) => `${TAB_SELECTION}${candidate.id}` === selected) : undefined;
  if (tab) {
    const relation = tab.relationPropertyId ? byId(tab.relationPropertyId) : undefined;
    return (
      <div className={styles.side} data-layout-side="tab">
        <div className={styles.sideHead}><span>{tab.name || t("untitled")}</span></div>
        {relation ? <>
          <div className={labelClass}>{t("tabRelation")}</div>
          {propertyRow(relation, null)}
        </> : <>
          <div className={labelClass}>{t("tabSource")}</div>
          <div className={styles.listRow}><span>{tab.name || t("untitled")}</span></div>
        </>}
        <div className={styles.divider} />
        <button type="button" className={styles.textButton} data-layout-remove-tab onClick={() => {
          onLayout(removeLayoutTab(layout, tab.id));
          onSelect(null);
        }}><X size="small" aria-hidden="true" />{t("removeTab")}</button>
      </div>
    );
  }

  const groupModule = selected ? moduleLocation(layout, selected) : null;
  if (selected && groupModule && layout[groupModule.place][groupModule.index].kind === "properties") {
    const ids = listed.map((property) => property.id);
    const items = propertyGroupItems(layout, ids);
    const visibility = visibilityFor ? listed.find((property) => property.id === visibilityFor.id) : undefined;
    const newSection = () => {
      const next = addSection(layout, ids, t("newSection"));
      onLayout(next);
      setRenaming(next.propertyGroup[0].kind === "section" ? next.propertyGroup[0].id : null);
    };
    return (
      <div className={sideClass} data-layout-side="properties">
        {phone ? null : (
          <div className={popover ? popoverStyles.head : styles.sideHead}>
            <span>{t("modules.properties")}</span>
            <button type="button" onClick={newSection} data-layout-add-section><Plus size="tiny" aria-hidden="true" />{t("addSection")}</button>
          </div>
        )}
        {group("search", search)}
        {group("items", <>{items.map((item, index) => {
          if (item.kind === "section") {
            return renaming === item.id ? (
              <form key={item.id} className={styles.listRow} data-section="true" onSubmit={(event) => { event.preventDefault(); setRenaming(null); }}>
                <input autoFocus defaultValue={item.name} aria-label={t("sectionName")} onFocus={(event) => event.currentTarget.select()}
                  onChange={(event) => onLayout(renameSection(layout, item.id, event.target.value.slice(0, 100)))} />
                <button type="submit">{t("done")}</button>
              </form>
            ) : (
              <div key={item.id} className={styles.listRow} data-section="true" data-layout-list-section={item.id} {...groupDrag.target(index)}>
                {handle(item.name || t("newSection"), groupDrag.handle(index))}
                <span onDoubleClick={() => setRenaming(item.id)}>{item.name || t("newSection")}</span>
                <button type="button" aria-label={t("removeSection")} onClick={() => onLayout(removeSection(layout, item.id))}><X size="small" aria-hidden="true" /></button>
              </div>
            );
          }
          const property = byId(item.propertyId);
          if (!property || !matches(property)) return null;
          const shown = visibilityOf(property);
          return propertyRow(property, <>
            {editing ? (
              <button type="button" aria-label={t("rowProperties:propertyVisibility")} aria-haspopup="menu"
                onClick={(event) => { visibilityButton.current = event.currentTarget; setVisibilityFor({ id: property.id, button: event.currentTarget }); }}
                data-layout-visibility={shown}>
                {/* The sheet's own marks, as Notion's (seen 2026-10-25): an eye, an outlined slash, a shut eye. */}
                {shown === "always" ? <EyeIcon size="small" aria-hidden="true" /> : shown === "whenEmpty"
                  ? <EyeSlashIcon size="small" weight="regular" aria-hidden="true" /> : <EyeClosedIcon size="small" aria-hidden="true" />}
              </button>
            ) : null}
          </>, { handle: groupDrag.handle(index), target: groupDrag.target(index) });
        })}
        {onNewProperty ? (
          <button type="button" className={styles.textButton} onClick={(event) => onNewProperty(event.currentTarget, false)} data-layout-new-property-button>
            <Plus size="small" aria-hidden="true" />{t("rowProperties:addProperty")}
          </button>
        ) : null}</>)}
        {phone ? group("section", (
          <button type="button" className={styles.textButton} onClick={newSection} data-layout-add-section>
            <Plus size="small" aria-hidden="true" />{t("addSection")}
          </button>
        )) : <div className={popover ? popoverStyles.divider : styles.divider} />}
        {group("move", (
          <button type="button" className={styles.textButton} data-layout-move-group
            onClick={() => onLayout(moveModuleTo(layout, selected, groupModule.place === "page" ? "panel" : "page"))}>
            <ArrowRight size="small" aria-hidden="true" />{t(groupModule.place === "page" ? "moveToPanel" : "moveToPage")}
          </button>
        ))}
        {visibility && editing ? (() => {
          const choose = (value: Visibility) => {
            editing.onCommitProperties([{ ...visibility, config: { ...visibility.config, hideInPagePanel: value === "hidden", hideWhenEmpty: value === "whenEmpty" } }]);
            setVisibilityFor(null);
          };
          const label = (value: Visibility) => t(value === "always" ? "rowProperties:visibilityAlways"
            : value === "whenEmpty" ? "rowProperties:visibilityWhenEmpty" : "rowProperties:visibilityHidden");
          // On a phone a half sheet over the group's, its head empty and no tick, as Notion's (seen 2026-10-25 at 375px).
          return phone ? (
            <PhoneSheet title="" label={t("rowProperties:propertyVisibility")} size="half" onClose={() => setVisibilityFor(null)}
              data={{ "data-layout-visibility-sheet": "" }}>
              <PhoneSheetGroup>
                {(["always", "whenEmpty", "hidden"] as const).map((value) => (
                  <PhoneSheetRow key={value} label={label(value)} more={false} onPress={() => choose(value)} data={{ "data-visibility": value }}
                    icon={value === "always" ? <EyeIcon size="wide" aria-hidden="true" /> : value === "whenEmpty"
                      ? <EyeSlashIcon size="wide" weight="regular" aria-hidden="true" /> : <EyeClosedIcon size="wide" aria-hidden="true" />} />
                ))}
              </PhoneSheetGroup>
            </PhoneSheet>
          ) : createPortal(
            <SidebarActionMenu button={visibilityButton} label={t("rowProperties:propertyVisibility")} width={200} backdropLabel={t("close")}
              close={() => setVisibilityFor(null)} rows={(["always", "whenEmpty", "hidden"] as const).map((value) => ({
                key: value, checked: visibilityOf(visibility) === value, label: label(value), run: () => choose(value),
              }))} />, document.body);
        })() : null}
      </div>
    );
  }

  return (
    <div className={phone ? sideClass : `${sideClass} ${styles.pageSide}`} data-layout-side="page">
      {phone ? null : <h2 className={styles.sideTitle}>{t("pageSettings")}</h2>}
      <div className={labelClass}>{t("structure")}</div>
      <div className={phone ? `${styles.structures} ${phoneStyles.structures}` : styles.structures}>
        {(["simple", "tabbed"] as const).map((structure) => (
          <button key={structure} type="button" className={styles.structure} aria-pressed={layout.structure === structure}
            onClick={() => onLayout(structure === "simple" ? removeLayoutTabs(layout) : { ...layout, structure })} data-layout-structure={structure}>
            <span aria-hidden="true"><StructureSketch tabbed={structure === "tabbed"} /></span>
            <span>{t(structure === "simple" ? "simple" : "tabbed")}</span>
          </button>
        ))}
      </div>
      <div className={labelClass}>{t("options")}</div>
      {group("options", <>
      <button ref={inlineButton} type="button" className={styles.optionRow} aria-haspopup="menu" aria-expanded={inlineMenu}
        onClick={() => setInlineMenu(true)} data-layout-option="inlineComments">
        <span>{t("inlineComments")}</span>
        <span className={styles.optionValue}>{t(layout.options.inlineComments === "minimal" ? "inlineMinimal" : "inlineDefault")}<ChevronRight size="tiny" aria-hidden="true" /></span>
      </button>
      {inlineMenu ? createPortal(
        <SidebarActionMenu button={inlineButton} label={t("inlineComments")} width={180} backdropLabel={t("close")} close={() => setInlineMenu(false)}
          rows={(["default", "minimal"] as const).map((value) => ({
            key: value, label: t(value === "minimal" ? "inlineMinimal" : "inlineDefault"), checked: layout.options.inlineComments === value,
            run: () => { onLayout({ ...layout, options: { ...layout.options, inlineComments: value } }); setInlineMenu(false); },
          }))} />, document.body) : null}
      {switchRow(t("pageComments"), layout.options.pageComments, (pageComments) => onLayout({ ...layout, options: { ...layout.options, pageComments } }), "pageComments")}
      {switchRow(t("propertyIcons"), layout.options.propertyIcons, (propertyIcons) => onLayout({ ...layout, options: { ...layout.options, propertyIcons } }), "propertyIcons")}
      {switchRow(t("fullWidth"), layout.options.fullWidth, (fullWidth) => onLayout({ ...layout, options: { ...layout.options, fullWidth } }), "fullWidth",
        t("fullWidthNote"))}
      {switchRow(t("templates"), layout.options.templates, (templates) => onLayout({ ...layout, options: { ...layout.options, templates } }), "templates",
        t("templatesNote"))}
      </>)}
    </div>
  );
}

/**
 * A structure's picture, drawn as Notion's (seen 2026-10-25): a narrow page
 * from 16px down, its title's line, the tabs' three pills (the first dark)
 * and its body below — 112×80 at the top of the tile.
 */
function StructureSketch({ tabbed }: { tabbed: boolean }) {
  const left = tabbed ? 24 : 32;
  return (
    <svg width="112" height="80" viewBox="0 0 112 80" data-layout-structure-sketch>
      <rect x="16.5" y="16.5" width="79" height="70" rx="3.5" fill="none" stroke="currentColor" strokeOpacity="0.2" />
      <rect x={left} y="24" width="31" height="4" rx="2" fill="currentColor" fillOpacity="0.4" />
      {tabbed ? [24, 40.5, 57].map((x, index) => (
        <rect key={x} x={x} y="31" width="14.5" height="4" rx="2" fill="currentColor" fillOpacity={index === 0 ? 1 : 0.4} />
      )) : null}
      <rect x={left} y={tabbed ? 41 : 34} width={tabbed ? 64 : 48} height="50" rx="2" fill="currentColor" fillOpacity="0.2" />
    </svg>
  );
}
