import { useRef, useState, type KeyboardEvent, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { keysMovedHighlight, pointerTakenRow } from "@/lib/pointerHover";
import { placeAround, placeBeside, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { PropertyType } from "@/lib/types";
import { Search } from "@/icons/leafen";
import { roomAfterInCanvas } from "./PageLayoutCardPopover";
import { PhoneSheet, PhoneSheetGroup } from "../shell/PhoneSheet";
import { PropertyIcon } from "./PropertyIcon";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import { ROW_PAGE_ADD_PROPERTY_TYPES } from "./tableAddPropertyModel";
import { NewPropertyMenu } from "./NewPropertyMenu";
import { propertyTypeFound, propertyTypeLabel } from "./propertyTypes";
import styles from "./PageLayoutAddMenu.module.css";
import phoneStyles from "./PhonePageLayoutBuilder.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { SearchClearButton } from "@/components/PageActionSearch";
import { PropertyTypeSearchToggle } from "./PropertyTypeSearchToggle";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

// The types offered before "더 보기", as Notion's: files, number, text, person, select.
const FIRST_TYPES: readonly PropertyType[] = ["files", "number", "rich_text", "person", "select"];
// "더 보기" adds the rest after them in a page's order, as Notion's (seen
// 2026-11-13), which leaves out ID and verification here; a relation, after
// the formula, asks which database first (seen 2026-11-14), where the
// builder can ask (`canRelate`).
const LEFT_OUT: readonly PropertyType[] = ["unique_id", "verification"];
const TYPES = [...FIRST_TYPES, ...ROW_PAGE_ADD_PROPERTY_TYPES.filter((type) => !FIRST_TYPES.includes(type) && !LEFT_OUT.includes(type))];
// Notion's new property's types (seen 2026-10-25), less a relation (its own
// dialog makes it at once).
const NEW_TYPES: readonly PropertyType[] = [
  "rich_text", "number", "select", "multi_select", "status", "date", "person", "files", "checkbox", "url", "email", "phone",
  "formula", "rollup", "created_time", "created_by", "last_edited_time", "last_edited_by", "button", "place", "verification",
];
const FIRST_EXISTING = 4;
// The room over and under the slot a card will take, which the menu's top meets.
const SLOT_ROOM = 12;

/**
 * "+" between a layout's modules, as Notion's (seen 2026-10-12): "페이지에
 * 추가" with a search, "기존 속성 사용" (four, then "더 보기") and "새 속성
 * 생성" (five types, then "더 보기"), each folded again by "접기" (seen
 * 2026-11-13). Searching looks through both whole. On a
 * desktop it opens beside the blue slot the card will take (seen 2026-11-12
 * at 1440: its top 12px over the slot, 8px from it, on the side the canvas has
 * room on), its first row marked; the arrows move the mark and Enter takes it.
 * The pointer marks the row it moves onto, after the arrows only once it
 * moves into another row, the row under it keeping its ground as the arrows
 * go on (Notion's "패널에 추가", seen 2026-11-14, `pointerHover`).
 * Made only for a new property — the title's or the group's "속성 추가" — it
 * is Notion's new property (NewPropertyMenu, seen 2026-10-25 at 1280px,
 * 2026-11-17 at 1416×838): a "속성 이름" field beside the tile that picks its
 * icon, then "유형" with its 🔍 ("유형 검색 중…") over the types in Notion's
 * order, as a database page's "속성 추가" is.
 */
export function PageLayoutAddMenu({ button, slot, label, candidates, canCreate, canRelate = false, onPick, onCreate, onClose, phone = false, createOnly = false }: {
  button: RefObject<HTMLElement | null>;
  /** The blue slot the card will take, which a desktop's menu opens beside. */
  slot?: () => HTMLElement | null;
  label: string;
  candidates: readonly GraphProperty[];
  canCreate: boolean;
  /** "관계형" among the new types: the builder asks which database (RelationPropertyCreationDialog). */
  canRelate?: boolean;
  onPick(propertyId: string): void;
  /** A new property's type, and from the title's or the group's "속성 추가" its name and the icon its tile picked. */
  onCreate(type: PropertyType, name?: string, icon?: string): void;
  onClose(): void;
  /** A phone's whole sheet of the same lists in white groups, as Notion's (seen 2026-10-24 at 375px). */
  phone?: boolean;
  createOnly?: boolean;
}) {
  const { t } = useTranslation("pageLayout");
  const [query, setQuery] = useState("");
  const [name, setName] = useState("");
  const [typeQuery, setTypeQuery] = useState<string | null>(null);
  const newTypes = NEW_TYPES.filter((type) => !typeQuery || propertyTypeFound(type, typeQuery));
  const typeHead = (className: string) => (
    <div className={className}>
      <span>{t("propertyType")}</span>
      <PropertyTypeSearchToggle label={t("searchTypes")} open={typeQuery !== null}
        onClick={() => setTypeQuery((current) => current === null ? "" : null)} data={{ "data-layout-type-search-button": "" }} />
    </div>
  );
  const typeSearch = (className: string) => typeQuery === null ? null : (
    <input autoFocus={fieldFocusOnOpen()} className={className} value={typeQuery} placeholder={t("searchingTypes")} aria-label={t("searchTypes")}
      onChange={(event) => setTypeQuery(event.target.value)} data-layout-type-search />
  );
  const [allExisting, setAllExisting] = useState(false);
  const [allTypes, setAllTypes] = useState(false);
  const [marked, setMarked] = useState(0);
  const menu = useRef<HTMLDivElement>(null);
  const rowsRef = useRef<HTMLDivElement>(null);
  const phoneSearchRef = useRef<HTMLInputElement>(null);
  // A new property alone is NewPropertyMenu's, which places itself.
  const ownMenu = !phone && !createOnly;
  const place = usePopoverPlacement(menu, ownMenu ? (size, viewport) => {
    const room = slot?.();
    const at = popoverRectOf(room ?? button.current) ?? { top: 0, bottom: 0, left: 0, right: 0 };
    // Notion's menu is up to 70% of the window high (seen 2026-11-12: 506 in a 723 window).
    return room ? placeBeside({ ...at, top: at.top - SLOT_ROOM, bottom: at.bottom + SLOT_ROOM }, size, viewport,
      { gap: 8, cap: Math.round(viewport.height * 0.7), prefer: roomAfterInCanvas(room, size.width) ? "right" : "left" })
      : placeAround(at, size, viewport, { gap: 4, cap: 480, minHeight: 200 });
  } : null, [], undefined, phone || ownMenu ? { kind: "menu", onEscape: onClose, onOutsidePress: onClose, inside: () => [button.current] } : false);
  const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const matches = (text: string) => terms.every((term) => text.toLowerCase().includes(term));
  const existing = candidates.filter((property) => matches(`${property.name} ${propertyTypeLabel(property.type as PropertyType)}`));
  const types = TYPES.filter((type) => (canRelate || type !== "relation") && matches(`${propertyTypeLabel(type)} ${type}`));
  const searching = terms.length > 0;
  const shownExisting = searching || allExisting ? existing : existing.slice(0, FIRST_EXISTING);
  const shownTypes = searching || allTypes ? types : FIRST_TYPES;

  if (phone) {
    const row = (key: string, icon: ReactNode, words: string, detail: string | null, onPress: () => void, data: Record<string, string>) => (
      <button key={key} type="button" className={phoneStyles.addRow} onClick={onPress} {...data}>
        <span className={phoneStyles.addTile} aria-hidden="true">{icon}</span>
        <span className={phoneStyles.addText}><span>{words}</span>{detail ? <small>{detail}</small> : null}</span>
      </button>
    );
    if (createOnly) {
      return (
        <PhoneSheet title={label} onClose={onClose} data={{ "data-layout-new-property": "" }}>
          <PhoneSheetGroup>
            <label className={phoneStyles.searchRow}>
              <PropertyTypeIcon type="rich_text" size="page" />
              <input autoFocus className={phoneStyles.search} value={name} placeholder={t("propertyName")} aria-label={t("propertyName")}
                onChange={(event) => setName(event.target.value)} data-layout-new-property-name />
            </label>
          </PhoneSheetGroup>
          <PhoneSheetGroup label={typeHead(styles.typeHeadPhone)}>
            {typeQuery === null ? null : <label className={phoneStyles.searchRow}><Search size="page" aria-hidden="true" />{typeSearch(phoneStyles.search)}</label>}
            {newTypes.map((type) => row(type, <PropertyTypeIcon type={type} size="regular" />, propertyTypeLabel(type), null,
              () => onCreate(type, name), { "data-layout-add-type": type }))}
          </PhoneSheetGroup>
          {newTypes.length === 0 ? <p className={phoneStyles.empty}>{t("noResults")}</p> : null}
        </PhoneSheet>
      );
    }
    return (
      <PhoneSheet title={label} onClose={onClose} data={{ "data-layout-add-menu": "" }}>
        <PhoneSheetGroup>
          <label className={phoneStyles.searchRow}>
            <Search size="page" aria-hidden="true" />
            <input ref={phoneSearchRef} className={phoneStyles.search} value={query} placeholder={t("searchPlaceholder")} aria-label={t("searchPlaceholder")}
              onChange={(event) => setQuery(event.target.value)} data-layout-add-search />
            <SearchClearButton value={query} inputRef={phoneSearchRef} onClear={() => setQuery("")} />
          </label>
        </PhoneSheetGroup>
        {shownExisting.length > 0 ? (
          <PhoneSheetGroup label={t("useExisting")}>
            {shownExisting.map((property) => row(property.id, <PropertyIcon property={property} size="regular" />, property.name,
              propertyTypeLabel(property.type as PropertyType), () => onPick(property.id), { "data-layout-add-existing": property.id }))}
            {!searching && existing.length > FIRST_EXISTING ? (
              <button type="button" className={phoneStyles.addMore} onClick={() => setAllExisting(!allExisting)} data-layout-add-more-existing>
                {allExisting ? t("showLess") : t("showMore", { count: existing.length - FIRST_EXISTING })}
              </button>
            ) : null}
          </PhoneSheetGroup>
        ) : null}
        {canCreate && shownTypes.length > 0 ? (
          <PhoneSheetGroup label={t("newProperty")}>
            {shownTypes.map((type) => row(type, <PropertyTypeIcon type={type} size="regular" />, propertyTypeLabel(type), null,
              () => onCreate(type), { "data-layout-add-type": type }))}
            {!searching ? (
              <button type="button" className={phoneStyles.addMore} onClick={() => setAllTypes(!allTypes)} data-layout-add-more-types>
                {t(allTypes ? "showLess" : "moreTypes")}
              </button>
            ) : null}
          </PhoneSheetGroup>
        ) : null}
        {shownExisting.length === 0 && (!canCreate || shownTypes.length === 0) ? <p className={phoneStyles.empty}>{t("noResults")}</p> : null}
      </PhoneSheet>
    );
  }
  if (createOnly) {
    return <NewPropertyMenu anchor={button} label={label} types={NEW_TYPES} onCreate={onCreate} onClose={onClose}
      data={{ "data-layout-new-property": "" }} />;
  }
  // Each row's press, in the menu's order, for the mark the arrows move.
  const runs: (() => void)[] = [];
  const option = (key: string, className: string, run: () => void, children: ReactNode, data: Record<string, string>) => {
    const index = runs.push(run) - 1;
    return (
      <button key={key} type="button" className={className} data-active={index === marked || undefined}
        data-key={index} onClick={run} {...data}>
        {children}
      </button>
    );
  };
  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (imeComposing(event)) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      keysMovedHighlight(rowsRef.current);
      const step = event.key === "ArrowDown" ? 1 : -1;
      setMarked((current) => Math.max(0, Math.min(runs.length - 1, current + step)));
    } else if (fieldEnter(event)) {
      event.preventDefault();
      runs[marked]?.();
    }
  }
  const tileRow = `${styles.row} ${styles.tileRow}`;
  const existingRows = shownExisting.map((property) => option(property.id, tileRow, () => onPick(property.id), <>
    <span className={styles.tile}><PropertyIcon property={property} size="regular" /></span>
    <span className={styles.text}><span>{property.name}</span><small>{propertyTypeLabel(property.type as PropertyType)}</small></span>
  </>, { "data-layout-add-existing": property.id }));
  const moreExisting = !searching && existing.length > FIRST_EXISTING
    // "더 보기" alone, as the types' (Notion's, seen 2026-11-12), and "접기" once shown (2026-11-13).
    ? option("more-existing", styles.more, () => setAllExisting(!allExisting), t(allExisting ? "showLess" : "moreTypes"), { "data-layout-add-more-existing": "" })
    : null;
  const typeRows = canCreate ? shownTypes.map((type) => option(type, tileRow, () => onCreate(type), <>
    <span className={styles.tile}><PropertyTypeIcon type={type} size="regular" /></span>
    <span className={styles.text}><span>{propertyTypeLabel(type)}</span></span>
  </>, { "data-layout-add-type": type })) : [];
  const moreTypes = canCreate && !searching
    ? option("more-types", styles.more, () => setAllTypes(!allTypes), t(allTypes ? "showLess" : "moreTypes"), { "data-layout-add-more-types": "" }) : null;
  return createPortal(
    <div ref={menu} className={`${styles.menu} ${styles.propertyMenu}`} role="dialog" aria-label={label} style={popoverPlacementStyle(place)} data-layout-add-menu>
      <div className={styles.head}>{label}</div>
      <label className={styles.searchBox}>
        <Search size="micro" aria-hidden="true" />
        <input value={query} placeholder={t("searchPlaceholder")} aria-label={t("searchPlaceholder")} autoFocus={fieldFocusOnOpen()} onKeyDown={onKeyDown}
          onChange={(event) => { setQuery(event.target.value); setMarked(0); }} data-layout-add-search />
      </label>
      <div ref={rowsRef} className={styles.groups} onPointerMove={(event) => {
        const taken = pointerTakenRow(event);
        if (taken !== null && Number(taken) !== marked) setMarked(Number(taken));
      }}>
        {existingRows.length > 0 ? <div className={styles.group}><div className={styles.label}>{t("useExisting")}</div>{existingRows}{moreExisting}</div> : null}
        {typeRows.length > 0 ? <div className={styles.group}><div className={styles.label}>{t("newProperty")}</div>{typeRows}{moreTypes}</div> : null}
      </div>
      {runs.length === 0 ? <div className={styles.empty}>{t("noResults")}</div> : null}
    </div>,
    document.body,
  );
}
