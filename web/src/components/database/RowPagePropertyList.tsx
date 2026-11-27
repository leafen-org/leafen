"use client";

import { useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type FocusEvent, type KeyboardEvent, type MouseEvent } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronUp } from "@/icons/leafen";
import type { GraphProperty } from "@/contentGraph";
import type { Page, PropertyType, TextSpan } from "@/lib/types";
import type { DatabasePropertyEditing } from "./DatabasePropertyEditPanel";
import { galleryPresentationProperty } from "./galleryViewModel";
import { PropValue, type PropertyValueReadContext } from "./PropValue";
import { PropertyCell } from "./PropertyCell";
import { PropertyAccessLock, PropertyNoAccessValue } from "./PropertyAccessMarks";
import { usePropertyDescriptionTip } from "./PropertyDescriptionTip";
import { PropertyIcon } from "./PropertyIcon";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { PropertyTextCell } from "./PropertyTextCells";
import { COMMENTLESS_PROPERTY_TYPES, COPYABLE_PROPERTY_TYPES, PropertyValueTools } from "./PropertyValueTools";
import { copiedDateForProperty } from "./dateUtils";
import { RowPageAddProperty } from "./RowPageAddProperty";
import { pageShowsStatusCheckbox } from "./rowPageStatusDisplay";
import { TableStatusCheckbox } from "./TableStatusDisplay";
import { RowPagePropertyMenu } from "./RowPagePropertyMenu";
import { RelationLineContext } from "./RowPageLayoutBody";
import { RelationSectionContext } from "./relationSection";
import type { PopoverRect } from "@/lib/popoverPlacement";
import type { PageLayoutGroupItem } from "@/lib/databasePageLayout";
import { RowPropertyCommentsChip, RowPropertyCommentsPopover, type RowPropertyComments } from "./RowPropertyComments";
import {
  canCommentOnProperty, isComputedProperty, isEmptyRowProperty, isHiddenRowProperty, propertiesAfterPageMove, ROW_PROPERTY_EMPTY_HINT_TYPES,
  rowPagePropertyOrder,
} from "./rowPropertiesModel";
import { useDragReorder } from "@/lib/useDragReorder";
import { DragHandleIcon } from "../icons";
import sheet from "./databaseSheet";
import rowPageStyles from "./DatabaseRowPage.module.css";
import layoutStyles from "./RowPageLayout.module.css";
import { inlineStartX } from "@/lib/readingDirection";
import { listenForRowPropertiesRedone } from "./rowPropertyRedo";
import { useRowPropertyTabMove } from "./rowPropertyTabMove";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/** A database page's property value as its list draws it, a template's too; what its editors open over (PropertyCellMenu.tsx). */
export const ROW_PROPERTY_VALUE_CELL = `${rowPageStyles.rowPropertyValue} ${rowPageStyles.rowPropertyValueInset} ${sheet.rowPropertyValue}`;

/**
 * A database page's properties under its title, apart from those set to
 * hide, as Notion lists them: edited, as their cells edit them; read, as
 * their values show, an empty one as Empty and a checkbox as its box. Where
 * the database's properties can change, a name opens its menu and
 * "속성 추가" ends the list (Notion's, seen 2026-10-01). A property with open
 * threads has its value marked and "댓글 N개" at its line's end.
 */
export function RowPagePropertyList({
  rowId,
  properties,
  values,
  valueRow,
  propertyValueContext,
  editableRow,
  onRichTextChange,
  propertyCellEnvironment,
  propertyEditing,
  onAddProperty,
  onCustomizeLayout,
  comments,
  layout,
  only,
  access,
  visitor,
}: {
  rowId: string;
  properties: readonly GraphProperty[];
  values: Readonly<Record<string, { value?: unknown } | undefined>>;
  valueRow: Page;
  propertyValueContext: PropertyValueReadContext;
  editableRow?: Page;
  onRichTextChange?: (property: GraphProperty, value: string, spans?: TextSpan[]) => void;
  propertyCellEnvironment?: PropertyCellEnvironment;
  propertyEditing?: DatabasePropertyEditing;
  /** Makes a property; the one it answers opens its settings at its name. */
  onAddProperty?(type: PropertyType, name?: string, icon?: string): string | void;
  /** A property menu's last row, "레이아웃 사용자 지정", for whoever may lay out the database's pages. */
  onCustomizeLayout?(): void;
  /** The row's property discussions, where the viewer can comment. */
  comments?: RowPropertyComments;
  /**
   * A page layout's property group: its order and named sections (a
   * property it leaves out comes last), without the properties the layout
   * places elsewhere, and without their icons where it turns them off.
   */
  layout?: { items: readonly PageLayoutGroupItem[]; placed: ReadonlySet<string>; icons: boolean };
  /** Shows this property alone, as a layout's module of its own. */
  only?: string;
  /** The values the reader may not see ("사용 권한 없음") or may not change, by the database's property access rules. */
  access?: { hidden?: readonly string[]; readOnly?: readonly string[] };
  /**
   * A visitor's published database page, as Notion's (seen 2026-11-11 on an
   * iPad): "속성" names the list over it — a layout's group is named where
   * the layout names it (RowPageLayoutBody) — and a touch screen, where no
   * pointer brings a value's copy tray, shows it beside each value.
   */
  visitor?: boolean;
}) {
  const { t } = useTranslation("rowProperties");
  const listRef = useRef<HTMLDListElement>(null);
  const [showAllProperties, setShowAllProperties] = useState(false);
  const [menu, setMenu] = useState<{ propertyId: string; anchor: PopoverRect; opened?: "new" | "rollupForm" | "redo" } | null>(null);
  const [discussion, setDiscussion] = useState<{ propertyId: string; quote: string; left: number; top: number } | null>(null);
  const menuProperty = menu
    ? (propertyEditing?.properties ?? properties).find((property) => property.id === menu.propertyId)
    : undefined;
  // A property's menu names the databases its relations reach, read as it opens (DatabasePropertyEditing).
  const readRelated = menu ? propertyEditing?.readRelated : undefined;
  useEffect(() => readRelated?.(), [readRelated]);
  const discussionProperty = discussion ? properties.find((property) => property.id === discussion.propertyId) : undefined;
  const shownRow = editableRow ?? valueRow;
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(() => new Set());
  const listedProperties = useMemo(() => {
    const listed = rowPagePropertyOrder(properties
      .filter((property) => property.type !== "title")
      .map((property, index) => ({
        property,
        presentationProperty: galleryPresentationProperty(property, index),
        value: values[property.id],
        section: undefined as { id: string; name: string } | undefined,
      })));
    if (only) return listed.filter(({ property }) => property.id === only);
    if (!layout) return listed;
    const byId = new Map(listed.filter(({ property }) => !layout.placed.has(property.id)).map((entry) => [entry.property.id, entry]));
    const ordered: typeof listed = [];
    let section: { id: string; name: string } | undefined;
    for (const item of layout.items) {
      if (item.kind === "section") { section = { id: item.id, name: item.name }; continue; }
      const entry = byId.get(item.propertyId);
      if (!entry) continue;
      byId.delete(item.propertyId);
      ordered.push({ ...entry, section });
    }
    return [...ordered, ...[...byId.values()].map((entry) => ({ ...entry, section }))];
  }, [layout, only, properties, values]);
  const displayedProperties = useMemo(
    () => listedProperties.filter(({ presentationProperty }) => !isHiddenRowProperty(shownRow, presentationProperty)),
    [listedProperties, shownRow],
  );
  // Properties set to hide wait behind "속성 N개 더 보기", which shows them
  // in their places; every other one shows, however many (Notion's, seen
  // 2026-11-03 with twenty).
  const hiddenPropertyCount = listedProperties.length - displayedProperties.length;
  const renderedProperties = only || showAllProperties ? listedProperties : displayedProperties;
  useEffect(() => setShowAllProperties(false), [rowId]);
  // Tab in a value being written opens the next property's (rowPropertyTabMove.ts).
  useRowPropertyTabMove(listRef, (id) => renderedProperties.find(({ property }) => property.id === id)?.presentationProperty.type);
  // "속성 N개 더 보기" ends the properties before the layout's first section, as Notion's sprint page (seen 2026-11-05).
  const firstSection = renderedProperties.findIndex(({ section }) => Boolean(section));
  const hiddenToggle = (
    <div className={rowPageStyles.rowProperty} key="hidden-properties">
      <dd className={`${rowPageStyles.rowPropertyValue} ${sheet.rowHiddenPropertiesLine}`}>
        <button type="button" className={sheet.rowHiddenProperties}
          data-row-properties-toggle aria-expanded={showAllProperties}
          onClick={() => setShowAllProperties((current) => !current)}>
          {showAllProperties ? (
            <ChevronUp className={sheet.rowHiddenPropertiesIcon}
              data-row-properties-toggle-icon="up" size="tiny" aria-hidden="true" />
          ) : (
            <ChevronDown className={sheet.rowHiddenPropertiesIcon}
              data-row-properties-toggle-icon="down" size="tiny" aria-hidden="true" />
          )}
          {showAllProperties
            ? t("fewerProperties", { n: hiddenPropertyCount })
            : t("moreProperties", { n: hiddenPropertyCount })}
        </button>
      </dd>
    </div>
  );
  // A property just made opens its settings at its name once its line is drawn, as Notion's (seen 2026-11-01);
  // one ⌘⇧Z made again opens its line's own menu there (Notion's row page and side peek, seen 2026-11-11).
  const made = useRef<string | null>(null);
  const redone = useRef<string | null>(null);
  const [redoneAt, setRedoneAt] = useState(0);
  // Not drawn here soon (hidden, another row's), it opens nothing later.
  useEffect(() => listenForRowPropertiesRedone((ids) => {
    const id = ids[0] ?? null;
    redone.current = id;
    setRedoneAt((at) => at + 1);
    window.setTimeout(() => { if (redone.current === id) redone.current = null; }, 2000);
  }), []);
  useLayoutEffect(() => {
    const id = made.current ?? redone.current;
    const name = id ? listRef.current?.querySelector<HTMLElement>(`[data-row-property-id="${CSS.escape(id)}"] > :first-child`) : null;
    if (!id || !name) return;
    const opened = made.current === id ? "new" as const : "redo" as const;
    made.current = null;
    redone.current = null;
    const rect = name.getBoundingClientRect();
    setMenu({ propertyId: id, anchor: { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom }, opened });
  }, [renderedProperties, redoneAt]);
  useEffect(() => setDiscussion(null), [rowId]);
  // A relation drawn as a page section keeps its lines' ends for their own box ("−" and rename), as Notion's has no value tools there (seen 2026-11-07).
  const relationSection = useContext(RelationSectionContext);
  const commentable = (propertyId: string) => {
    const listed = listedProperties.find(({ property }) => property.id === propertyId);
    return Boolean(comments && listed) && canCommentOnProperty(shownRow, listed!.presentationProperty);
  };

  // A line is dragged by the handle its icon turns into or by its name, the
  // lines it passes making way for it, and where it lands becomes the
  // database's page order, every page of it listing the properties so
  // (Notion's, seen 2026-11-08, 2026-11-11 and 2026-11-16 in Chrome: the
  // line dragged by its name goes with the pointer, its icon a handle, the
  // others sliding by); a page laid out by a layout keeps the layout's order.
  const movable = Boolean(propertyEditing?.writable) && !layout && !only;
  const reorder = useDragReorder("row-page-property", (sourceId, targetId, side) => {
    const moved = propertiesAfterPageMove(listedProperties.map(({ property }) => property), sourceId, targetId, side);
    if (moved.length) void propertyEditing?.onCommitProperties(moved);
  }, { live: true });

  // A property's discussion opens under its line, where its value shows,
  // a new thread quoting the value as it shows.
  function openDiscussion(propertyId: string) {
    const line = listRef.current?.querySelector<HTMLElement>(`[data-row-property-id="${CSS.escape(propertyId)}"]`);
    const value = line?.querySelector<HTMLElement>("[data-row-property-value]");
    if (!line || !value) return;
    setMenu(null);
    setDiscussion({
      propertyId,
      quote: (value.innerText || "").replace(/\s+/gu, " ").trim().slice(0, 200),
      // Its start: the value's right where the screen reads from the right.
      left: inlineStartX(value.getBoundingClientRect(), 0, value),
      top: line.getBoundingClientRect().bottom,
    });
  }

  return (<>
    {visitor && !layout ? <div className={layoutStyles.groupLabel} data-row-properties-heading>{t("properties")}</div> : null}
    <dl ref={listRef} className={rowPageStyles.rowProperties} aria-label={t("properties")}
      data-property-icons={layout && !layout.icons ? "false" : undefined} data-visitor-properties={visitor ? "true" : undefined}>
      {/* One flat list keyed by property, so a renamed property that moves keeps its line and its focus, as Notion's (seen 2026-11-07). */}
      {renderedProperties.flatMap(({ property, presentationProperty, value, section }, index) => {
        const openThreads = comments?.openByProperty.get(property.id) ?? 0;
        // A layout's section heads its properties and folds them away, as Notion's "새 섹션 ⌄".
        const toggleHere = index === firstSection && hiddenPropertyCount > 0 && !only;
        const head = section && section.id !== renderedProperties[index - 1]?.section?.id ? (
          <div className={rowPageStyles.rowPropertySection} key={`section:${section.id}`}>
            <button type="button" aria-expanded={!collapsed.has(section.id)} data-row-property-section={section.id}
              onClick={() => setCollapsed((current) => {
                const next = new Set(current);
                if (!next.delete(section.id)) next.add(section.id);
                return next;
              })}>
              <span>{section.name || t("newSection")}</span>
              {collapsed.has(section.id) ? <ChevronUp size="tiny" aria-hidden="true" /> : <ChevronDown size="tiny" aria-hidden="true" />}
            </button>
          </div>
        ) : null;
        const lead = toggleHere ? hiddenToggle : null;
        if (section && collapsed.has(section.id)) return [lead, head];
        return [lead, head, (
        <div className={rowPageStyles.rowProperty} data-row-property-id={property.id} key={property.id}
          data-property-comments={openThreads > 0 ? "true" : undefined} {...(movable ? reorder.row(property.id) : {})}
          onFocus={markValueEditing} onBlur={markValueEditing}>
          <RowPagePropertyName property={property} open={menu?.propertyId === property.id && menu.opened !== "rollupForm"}
            handle={movable ? reorder.handle(property.id, true, {
              above: renderedProperties[index - 1]?.property.id, below: renderedProperties[index + 1]?.property.id,
            }) : undefined} grip={movable ? reorder.grip(property.id).ref : undefined}
            onOpenMenu={propertyEditing || commentable(property.id)
              ? (rect) => setMenu({ propertyId: property.id, anchor: { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom } })
              : undefined} />
          <dd className={ROW_PROPERTY_VALUE_CELL} data-row-property-value
            data-property-commented={(comments?.wholeValueProperties ? comments.wholeValueProperties.has(property.id) : openThreads > 0) ? "true" : undefined}
            {...(property.type === "rollup" && propertyEditing?.writable && editableRow && !access?.hidden?.includes(property.id)
              ? rollupFormOpener((rect) => setMenu({ propertyId: property.id, anchor: rect, opened: "rollupForm" })) : {})}>
            {access?.hidden?.includes(property.id) ? <PropertyNoAccessValue /> : (<>
              <RowPagePropertyValue property={property} presentationProperty={presentationProperty} value={value}
                valueRow={valueRow} editableRow={access?.readOnly?.includes(property.id) ? undefined : editableRow}
                propertyValueContext={propertyValueContext} onRichTextChange={onRichTextChange} propertyCellEnvironment={propertyCellEnvironment} />
              {relationSection ? null : <PropertyValueTools place="line"
                onConfigure={property.type === "rollup" && propertyEditing?.writable && editableRow ? (button) => {
                  const rect = button.closest<HTMLElement>("[data-row-property-value]")?.getBoundingClientRect();
                  if (rect) setMenu({ propertyId: property.id, anchor: { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom }, opened: "rollupForm" });
                } : undefined}
                onComment={comments && !COMMENTLESS_PROPERTY_TYPES.has(presentationProperty.type) ? () => openDiscussion(property.id) : undefined}
                copyValue={COPYABLE_PROPERTY_TYPES.has(presentationProperty.type) ? (button) => presentationProperty.type === "url" ? String(value?.value ?? "")
                  : presentationProperty.type === "date" ? copiedDateForProperty(value?.value, presentationProperty as never)
                  : button.closest<HTMLElement>("[data-row-property-value]")?.innerText.replace(/\s+/gu, " ") ?? "" : undefined} />}
            </>)}
          </dd>
          {openThreads > 0 ? (
            <RowPropertyCommentsChip count={openThreads} open={discussion?.propertyId === property.id}
              onOpen={() => openDiscussion(property.id)} />
          ) : null}
        </div>
        )];
      })}
      {onAddProperty && !only && (showAllProperties || hiddenPropertyCount === 0) ? (
        <div className={rowPageStyles.rowProperty}>
          <dd className={rowPageStyles.rowPropertyValue} data-row-add-property-row>
            <RowPageAddProperty onAdd={(type, name, icon) => { const id = onAddProperty(type, name, icon); if (id) made.current = id; return Boolean(id); }}
              onRelationMade={(id) => { made.current = id; setRedoneAt((at) => at + 1); }} relation={propertyEditing?.relationCreation
              ? { databaseId: propertyEditing.dataSource.id, creation: propertyEditing.relationCreation,
                takenNames: propertyEditing.properties.filter((property) => property.dataSourceId === propertyEditing.dataSource.id).map((property) => property.name) }
              : undefined} idSource={propertyEditing ? { dataSourceId: propertyEditing.dataSource.id,
                has: properties.some((property) => property.type === "unique_id") } : undefined} />
          </dd>
        </div>
      ) : null}
      {hiddenPropertyCount > 0 && !only && firstSection < 0 && hiddenToggle}
      {menu && menuProperty ? (
        <RowPagePropertyMenu key={`${menu.propertyId}:${menu.opened ?? ""}`} property={menuProperty} anchor={menu.anchor} editing={propertyEditing}
          opened={menu.opened} onCustomizeLayout={onCustomizeLayout}
          onComment={commentable(menuProperty.id) ? () => openDiscussion(menuProperty.id) : undefined}
          onClose={() => setMenu(null)} />
      ) : null}
      {discussion && discussionProperty && comments ? (
        <RowPropertyCommentsPopover comments={comments} property={{ id: discussionProperty.id, name: discussionProperty.name,
          type: discussionProperty.type, config: discussionProperty.config }}
          quote={discussion.quote} anchor={{ left: discussion.left, top: discussion.top }} onClose={() => setDiscussion(null)} />
      ) : null}
    </dl>
  </>);
}

// A line whose value is being written in its box over the line keeps its
// icon (DatabaseRowPage.module.css), marked on the line itself, no drawing again.
function markValueEditing(event: FocusEvent<HTMLDivElement>) {
  const line = event.currentTarget;
  const into = event.type === "focus" ? event.target : event.relatedTarget;
  if (into instanceof Element && line.contains(into) && into.closest("[data-row-property-value]")) line.dataset.valueEditing = "true";
  else delete line.dataset.valueEditing;
}

// A rollup's value, empty or not, opens its small form by it, as its cell
// does; it is a button Tab reaches and Enter presses, as Notion's (seen
// 2026-11-06 and 2026-11-11: role=button, tabindex 0).
function rollupFormOpener(open: (anchor: { left: number; right: number; top: number; bottom: number }) => void) {
  const openBy = (element: HTMLElement) => {
    const { left, right, top, bottom } = element.getBoundingClientRect();
    open({ left, right, top, bottom });
  };
  return {
    role: "button",
    tabIndex: 0,
    "aria-haspopup": "dialog" as const,
    onClick(event: MouseEvent<HTMLElement>) {
      if (!(event.target as HTMLElement).closest("a, button")) openBy(event.currentTarget);
    },
    onKeyDown(event: KeyboardEvent<HTMLElement>) {
      if (event.target !== event.currentTarget || !fieldEnter(event)) return;
      event.preventDefault();
      openBy(event.currentTarget);
    },
  };
}

/** A database page's property value: edited, as its cell edits it; read, as it shows, an empty one as "비어 있음". */
export function RowPagePropertyValue({ property, presentationProperty, value, valueRow, editableRow, propertyValueContext,
  onRichTextChange, propertyCellEnvironment }: {
  property: GraphProperty;
  presentationProperty: ReturnType<typeof galleryPresentationProperty>;
  value: { value?: unknown } | undefined;
  valueRow: Page;
  editableRow?: Page;
  propertyValueContext: PropertyValueReadContext;
  onRichTextChange?: (property: GraphProperty, value: string, spans?: TextSpan[]) => void;
  propertyCellEnvironment?: PropertyCellEnvironment;
}) {
  const { t } = useTranslation(["rowProperties", "pageLayout", "propertyCell"]);
  const computed = isComputedProperty(property);
  // A template's date taking the day it makes a page reads so (its cell edits it so).
  const templateDate = property.type === "date" && (value?.value === "@today" || value?.value === "@now") ? value.value : "";
  // An empty relation in a relations group's line says what pressing it adds, as Notion's "↗ 작업 추가";
  // one holding pages, how many, as Notion's "↗ 작업 1개" (seen 2026-11-06), pressing it choosing pages too.
  const line = useContext(RelationLineContext) && property.type === "relation";
  const empty = line ? <><PropertyIcon property={property} size="small" />{t("pageLayout:relationAdd", { name: property.name })}</> : t("empty");
  const linked = line ? (editableRow ?? valueRow).properties?.[property.id] : undefined;
  return <>
    {property.type === "rich_text" && editableRow && onRichTextChange ? (
      <PropertyTextCell
        row={editableRow}
        prop={presentationProperty}
        value={editableRow.properties?.[property.id] ?? value?.value}
        onInput={(next, spans) => onRichTextChange(property, next, spans)}
        presentation="rowDetail"
      />
    ) : editableRow && propertyCellEnvironment ? (
      <PropertyCell
        row={editableRow}
        prop={presentationProperty}
        presentation="rowDetail"
        {...propertyCellEnvironment}
      />
    ) : templateDate ? (
      <span data-template-value={templateDate}>{t(`propertyCell:templateValue.${templateDate.slice(1)}`)}</span>
    ) : pageShowsStatusCheckbox(property) ? (
      // A page nobody here may edit (locked, or read only) shows it so too, its box not pressable (Notion's, seen 2026-11-16 on a locked page).
      <TableStatusCheckbox property={property} value={value?.value} readOnly label={property.name} onChange={() => undefined} />
    ) : (
      <span data-row-property-typed-value>
        <PropValue
          context={propertyValueContext}
          row={valueRow}
          prop={presentationProperty}
          interactive={false}
          presentation="rowDetail"
        />
        {property.type !== "checkbox" && (computed || isEmptyRowProperty(valueRow, presentationProperty)) ? (
          <span className={rowPageStyles.rowPropertyEmpty} data-row-property-empty data-computed-hint={computed ? "" : undefined}>{empty}</span>
        ) : null}
      </span>
    )}
    {editableRow && (computed ? Boolean(propertyCellEnvironment)
      : ROW_PROPERTY_EMPTY_HINT_TYPES.has(property.type) && isEmptyRowProperty(editableRow, presentationProperty)) ? (
      <span className={rowPageStyles.rowPropertyEmptyHint} data-row-property-empty data-computed-hint={computed ? "" : undefined}
        aria-hidden="true">{empty}</span>
    ) : null}
    {Array.isArray(linked) && linked.length > 0 ? (
      <span className={rowPageStyles.rowPropertyEmptyHint} data-row-relation-summary aria-hidden="true">
        <PropertyIcon property={property} size="small" />{t("pageLayout:relationCount", { name: property.name, count: linked.length })}
      </span>
    ) : null}
  </>;
}

// A property's name, with its name and description in a tip beside it while
// the pointer is over it, as Notion's database page shows it; where the
// database's properties can change, pressing it opens the property's menu.
function RowPagePropertyName({ property, open, handle, grip, onOpenMenu }: {
  property: GraphProperty;
  open: boolean;
  /** The drag handle its icon turns into while the line is under the pointer. */
  handle?: ReturnType<ReturnType<typeof useDragReorder>["handle"]>;
  /** The name drags its line too. */
  grip?: (element: HTMLElement | null) => void;
  onOpenMenu?(rect: DOMRect): void;
}) {
  const { t } = useTranslation(["rowProperties", "databaseView"]);
  const { description, handlers, tip } = usePropertyDescriptionTip(property, "left", { nameAlone: true, delay: 500 });
  const name = (
    <>
      <span className={rowPageStyles.rowPropertyGlyph} aria-hidden="true"><PropertyIcon property={property} size="regular" /></span>
      <span data-row-property-label>{property.name}</span>
      {property.access?.restricted ? <PropertyAccessLock /> : null}
    </>
  );
  return (
    <dt ref={grip} className={rowPageStyles.rowPropertyName} aria-description={description || undefined} {...handlers}>
      {handle ? (
        <span className={rowPageStyles.rowPropertyHandle} data-row-property-handle role="button" tabIndex={-1}
          aria-label={t("databaseView:dragToMoveProperty", { name: property.name })} {...handle}
          onClick={(event) => onOpenMenu?.(event.currentTarget.parentElement!.getBoundingClientRect())}>
          <DragHandleIcon size="small" aria-hidden="true" />
        </span>
      ) : null}
      {onOpenMenu ? (
        <button type="button" className={rowPageStyles.rowPropertyNameButton} aria-haspopup="menu" aria-expanded={open}
          aria-label={t("propertyOptions", { name: property.name })} data-row-property-name
          onClick={(event) => onOpenMenu(event.currentTarget.getBoundingClientRect())}>
          {name}
        </button>
      ) : name}
      {tip}
    </dt>
  );
}
