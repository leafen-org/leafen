import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type {
  DatabaseToolbarTemplateMetadataWire,
  DatabaseToolbarTemplateRepeatWire,
} from "@/databaseToolbarConfigWire.generated";
import { ChevronDown, ComposeIcon, Copy, DotsHorizontal, DragHandleIcon, FileBlank, FlagIcon, ICON_SIZES, MenuIcon, Plus, RepeatIcon, SplitChevronDown, Trash } from "@/icons/leafen";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { graphPositionBetween } from "@/lib/graphPosition";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { placeAround, placeBeside, popoverAnchorOf, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { scrollKeyWalkedRow } from "@/lib/menuKeyScroll";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useDragReorder, type DragReorder } from "@/lib/useDragReorder";
import { PageActionSearch } from "../PageActionSearch";
import { PhoneTemplateSheet } from "./PhoneTemplateSheet";
import { DatabaseTemplateRepeatChooser, templateRepeatLabel, templateRepeatTip } from "./DatabaseTemplateRepeatChooser";
import { databaseViewLabels } from "./databaseViewLabels";
import { TemplateIconGlyph } from "./databaseViewShared";
import { useTemplateDisplayName } from "./untitledRowName";
import styles from "./databaseSheet";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/** What the template menu lists and does, wherever it opens. */
export type DatabaseTemplateMenuActions = {
  /** The database the menu's head names, as Notion's "템플릿 〈database〉". */
  databaseTitle: string;
  templates: readonly DatabaseToolbarTemplateMetadataWire[];
  /** Whether a page may be made from the menu. */
  writable: boolean;
  /** Whether its templates may be made, changed or chosen as the default; unset, as writable. */
  templatesWritable?: boolean;
  onCreateRow(templateId?: string): void;
  onCreateTemplate(): void;
  onDeleteTemplate(templateId: string): void;
  onDuplicateTemplate(templateId: string): void;
  onEditTemplate(templateId: string): void;
  /** A template dragged to a new place, at `position` between its new neighbours. */
  onMoveTemplate(templateId: string, position: string): void;
  /** "사이드바에 단축키 추가": a sidebar shortcut making a page in this database. */
  onAddShortcut?(): void;
  /** The template this view's new pages start from (viewDefaultTemplate), the empty page when undefined. */
  defaultTemplateId?: string;
  /** "기본으로 설정" on a template, or on the empty page (null): it asks how far the default reaches. */
  onChooseDefault(templateId: string | null): void;
  onSetRepeat(templateId: string, repeat: DatabaseToolbarTemplateRepeatWire | null): void;
};

export function DatabaseTemplateMenu({ open, onToggle, onClose, phone = false, ...actions }: DatabaseTemplateMenuActions & {
  open: boolean;
  onToggle(trigger: HTMLElement, modality: "pointer" | "keyboard"): void;
  onClose(): void;
  /**
   * On a phone "⌄" opens Notion's half sheet (seen 2026-10-21 at 375px):
   * "데이터베이스 템플릿" over a gray line saying what templates are, the
   * templates, and "+ 새 템플릿" in gray, each in a group of its own.
   */
  phone?: boolean;
}) {
  const labels = databaseViewLabels();
  // Its "⌄" is a phone's thin chevron where the toolbar is a phone's (its 18px, DatabaseToolbar.module.css).
  const thinArrow = useSidebarLayout(true).phone;
  const { templates, writable, onCreateRow, onCreateTemplate, onChooseDefault, onEditTemplate, onDuplicateTemplate, onDeleteTemplate, onSetRepeat } = actions;
  // A deleted template is in the trash, as Notion's (seen 2026-10-30), not here.
  const available = templates.filter((template) => !template.deleted);
  const defaultTemplate = available.find((template) => template.id === actions.defaultTemplateId);
  // A layer like the toolbar's other menus, focus in its first field: Escape
  // and a press outside close it (2026-10-16: it stayed open over "+ 속성
  // 추가" and took its clicks).
  const menuRef = useRef<HTMLDivElement>(null);
  const arrowRef = useRef<HTMLButtonElement>(null);
  usePopoverAppear(open && !phone, menuRef, () => arrowRef.current);
  const { zIndex } = useOverlayLayer(open, {
    kind: "menu", ref: menuRef, restoreFocus: false, inside: () => [arrowRef.current], onOutsidePress: onClose, initialFocus: "first", onEscape: onClose,
  });
  const sheet = open && phone ? (
    <PhoneTemplateSheet templates={available} defaultTemplate={defaultTemplate} writable={actions.templatesWritable ?? writable} sheetRef={menuRef} zIndex={zIndex}
      onClose={onClose} onCreateRow={onCreateRow} onCreateTemplate={onCreateTemplate} onChooseDefault={onChooseDefault} onEditTemplate={onEditTemplate}
      onDuplicateTemplate={onDuplicateTemplate} onDeleteTemplate={onDeleteTemplate} onSetRepeat={onSetRepeat} onAddShortcut={actions.onAddShortcut} />
  ) : null;
  return (
    <div className={`${styles.toolbarPopoverWrap} ${styles.newTemplateSplit}`} data-database-new-row>
      <button type="button" className={styles.newTemplateButton} disabled={!writable}
        onClick={() => onCreateRow(defaultTemplate?.id)}>
        <Plus size="small" /> {labels.toolbarNew}
      </button>
      {/* Named as Notion names its "⌄" here, "추가 옵션 더 보기" / "More add options" (seen 2026-11-18). */}
      <button ref={arrowRef} type="button" className={styles.newTemplateArrow} disabled={!writable}
        aria-label={labels.chooseDatabaseTemplate} aria-haspopup="dialog" aria-expanded={open} data-database-new-row-menu
        onClick={(event) => onToggle(event.currentTarget, event.detail === 0 ? "keyboard" : "pointer")}>
        <SplitChevronDown thin={thinArrow} aria-hidden="true" />
      </button>
      {sheet}
      {phone ? null : open && (
        <div ref={menuRef} className={`${styles.toolbarMenu} ${styles.templateMenu}`} role="dialog" style={{ zIndex }}
          aria-label={labels.newDatabasePage} data-database-template-menu>
          <TemplateMenuContent {...actions} onClose={onClose} />
        </div>
      )}
    </div>
  );
}

// The rows after the templates the "필터…" field's arrows shade (TemplateMenuContent).
const LEAD_EMPTY = "\u0000empty", LEAD_NEW = "\u0000new", LEAD_SHORTCUT = "\u0000shortcut";
// How many templates the menu holds before it leads with "필터…" (TemplateMenuContent).
const TEMPLATE_FILTER_FROM = 7;
// A template's picked icon in its menu row, as Notion's (seen 2026-11-15: 19.2 in a 20 box);
// one with none shows a page's document at a page icon's 18.
const TEMPLATE_ICON_PIXELS = 19.2;

/**
 * The template menu's list, as Notion's (seen 2026-10-30): "템플릿
 * 〈database〉", each template with "기본" when it is the default and its
 * "⋯", the empty page as "비어 있음" (the default while no template is),
 * then "+ 새 템플릿" and "사이드바에 단축키 추가". A template's repeat is its
 * editor's "복제 주기".
 */
function TemplateMenuContent({ databaseTitle, templates, writable: pagesWritable, templatesWritable, defaultTemplateId, onCreateRow, onCreateTemplate,
  onDeleteTemplate, onDuplicateTemplate, onEditTemplate, onMoveTemplate, onAddShortcut, onChooseDefault, onSetRepeat, onClose }: DatabaseTemplateMenuActions & { onClose(): void }) {
  // What the menu changes of its templates goes by who may change them.
  const writable = templatesWritable ?? pagesWritable;
  const labels = databaseViewLabels();
  const { t } = useTranslation("databaseView");
  const available = templates.filter((template) => !template.deleted);
  const defaultTemplate = available.find((template) => template.id === defaultTemplateId);
  // One made with no name reads as its database's untitled pages do, as Notion's.
  const nameOf = useTemplateDisplayName(available);
  // From seven templates a "필터…" field leads the list, the caret in it as the menu opens, as Notion's (seen
  // 2026-11-16 at 1440: none at six; a letter typed at once landed there). What is typed keeps the templates
  // whose names hold it, case aside, without drag handles or "비어 있음", "+ 새 템플릿" staying.
  const [query, setQuery] = useState("");
  const filterRef = useRef<HTMLInputElement>(null);
  const filtered = available.length >= TEMPLATE_FILTER_FROM && query !== "";
  const shown = filtered ? available.filter((template) => nameOf(template).toLocaleLowerCase().includes(query.toLocaleLowerCase())) : available;
  // The field's ↓ and ↑ shade the menu's rows in turn — its templates, "비어 있음", "+ 새 템플릿", "사이드바에
  // 단축키 추가" — round from the last to the first, the field keeping its ring and its caret, and Enter does
  // what the shaded row does; a row still found as the words change stays shaded, as Notion's (seen
  // 2026-11-16 at 1440 in two tabs: ↓ 가1, ↓↓ 가2, ↑ from the first the last row, Enter on 가3 a new page from
  // it in the side peek; "5" typed with the last row shaded kept it, ↓ then 가5).
  const leads: { key: string; run(): void }[] = [...shown.map((template) => ({ key: template.id, run: () => onCreateRow(template.id) })),
    ...(filtered || available.length === 0 ? [] : [{ key: LEAD_EMPTY, run: () => onCreateRow("") }]),
    ...(writable ? [{ key: LEAD_NEW, run: onCreateTemplate }] : []),
    ...(onAddShortcut ? [{ key: LEAD_SHORTCUT, run: () => { onClose(); onAddShortcut(); } }] : [])];
  const [litKey, setLitKey] = useState<string | null>(null);
  const lit = leads.findIndex(({ key }) => key === litKey);
  const filterKeys = (event: KeyboardEvent<HTMLInputElement>) => {
    if (imeComposing(event) || event.altKey || event.metaKey || event.ctrlKey || !leads.length) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const step = event.key === "ArrowDown" ? 1 : -1;
      setLitKey(leads[lit < 0 ? (step > 0 ? 0 : leads.length - 1) : (lit + step + leads.length) % leads.length].key);
    } else if (fieldEnter(event) && lit >= 0) {
      event.preventDefault();
      leads[lit].run();
    }
  };
  const litAt = (key: string) => litKey === key && lit >= 0 ? "true" : undefined;
  useEffect(() => {
    const menu = filterRef.current?.closest<HTMLElement>("[data-database-template-menu]") ?? null;
    if (lit >= 0) scrollKeyWalkedRow(menu?.querySelector<HTMLElement>("[data-lit='true']"), menu);
  }, [lit]);
  // A template is dragged by its handle to another place, as Notion's (seen
  // 2026-10-30): its order is the database's, so everyone sees it.
  const reorderId = useId();
  const reorder = useDragReorder(`database-template-menu:${reorderId}`, (sourceId, targetId, side) => {
    const rest = available.filter((template) => template.id !== sourceId), at = rest.findIndex((template) => template.id === targetId);
    if (at < 0) return;
    const [before, after] = side === "before" ? [rest[at - 1], rest[at]] : [rest[at], rest[at + 1]];
    try {
      onMoveTemplate(sourceId, graphPositionBetween(before?.position, after?.position));
    } catch {
      // Two templates kept at one place have no room between them; the order stays.
    }
  }, { live: true });
  return <>
    <div className={styles.templateMenuHead}><span>{t("templateMenu.title")}</span><span>{databaseTitle}</span></div>
    {available.length >= TEMPLATE_FILTER_FROM ? <PageActionSearch className={styles.templateMenuFilter} label={t("templateMenu.filter")}
      value={query} inputRef={filterRef} marker="template-menu-filter" onChange={setQuery} onKeyDown={filterKeys} /> : null}
    {shown.map((template, index) => (
      <TemplateMenuRow key={template.id} icon={<TemplateIconGlyph icon={template.icon} empty={!template.hasContent}
        pixels={template.icon.trim() ? TEMPLATE_ICON_PIXELS : ICON_SIZES.page} />}
        drag={writable && !filtered ? { reorder, id: template.id, above: available[index - 1]?.id, below: available[index + 1]?.id } : undefined}
        moreTip={t("templateMenu.editTip")} bare={filtered} lit={Boolean(litAt(template.id))}
        name={nameOf(template)} isDefault={template.id === defaultTemplate?.id}
        onUse={() => onCreateRow(template.id)} actions={writable ? [
          // Its row opens the repeat chooser itself.
          { key: "repeat", label: t("templateMenu.repeat"), icon: <RepeatIcon size="regular" />, run: () => undefined,
            value: templateRepeatLabel(template, t) },
          // Notion asks how far it reaches, on the default too.
          { key: "default", label: t("templateMenu.setDefault"), icon: <FlagIcon size="regular" />, run: () => onChooseDefault(template.id) },
          { key: "edit", label: t("templateMenu.edit"), icon: <ComposeIcon size="regular" />, run: () => onEditTemplate(template.id) },
          { key: "duplicate", label: t("templateMenu.duplicate"), icon: <Copy size="regular" />, run: () => onDuplicateTemplate(template.id) },
          { key: "delete", label: t("templateMenu.delete"), icon: <Trash size="regular" />, run: () => onDeleteTemplate(template.id) },
        ] : []} repeat={writable ? { template, onSave: (repeat) => onSetRepeat(template.id, repeat) } : undefined} />
    ))}
    {/* With no template yet, what templates are for in gray, and no "비어 있음", as Notion's (seen 2026-11-02). */}
    {filtered ? null : available.length === 0 ? <p className={styles.templateMenuHint}>{labels.sheetTemplatesHint}</p>
      : <TemplateMenuRow icon={<FileBlank size="page" aria-hidden="true" />} name={t("templateMenu.empty")} isDefault={!defaultTemplate} lit={Boolean(litAt(LEAD_EMPTY))}
      moreTip={t("templateMenu.viewTip")}
      onUse={() => onCreateRow("")} actions={writable
        ? [{ key: "default", label: t("templateMenu.setDefault"), icon: <FlagIcon size="regular" />, run: () => onChooseDefault(null) }] : []} />}
    {/* Who reads the templates is offered no new one, as Notion's reader (seen 2026-11-15). */}
    {writable ? <>
      <div className={styles.templateSectionDivider} />
      <button type="button" className={styles.templateNewItem} data-lit={litAt(LEAD_NEW)} onClick={onCreateTemplate}>
        <Plus size="regular" /> {labels.newTemplate}
      </button>
    </> : null}
    {onAddShortcut ? <>
      <div className={styles.templateSectionDivider} />
      <button type="button" className={styles.templateNewItem} data-template-menu-add-shortcut data-lit={litAt(LEAD_SHORTCUT)}
        onClick={() => { onClose(); onAddShortcut(); }}>
        <MenuIcon size="regular" aria-hidden="true" /> {t("templateMenu.addShortcut")}
      </button>
    </> : null}
  </>;
}

/**
 * The same list beside a menu's row, as the narrow window's settings menu
 * opens it from "페이지 템플릿 ›" (Notion's, seen 2026-11-02 at 768px: left
 * of the menu where the right has no room).
 */
export function DatabaseTemplateMenuBeside({ anchor, onClose, ...actions }: DatabaseTemplateMenuActions & { anchor: DOMRect; onClose(): void }) {
  const labels = databaseViewLabels();
  const menuRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeBeside(anchor, size, viewport, { gap: 0 }),
    [anchor], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose });
  if (typeof document === "undefined") return null;
  return createPortal(
    <div ref={menuRef} className={`${styles.toolbarMenu} ${styles.templateMenu}`} role="dialog" aria-label={labels.newDatabasePage}
      data-database-template-menu data-beside="true" style={popoverPlacementStyle(place)}>
      <TemplateMenuContent {...actions} onClose={onClose} />
    </div>,
    document.body,
  );
}

type TemplateMenuAction = { key: string; label: string; icon: React.ReactNode; value?: string; run(): void };

/**
 * A row of the template menu: using it makes a page from it; its "⋯" holds
 * its actions, in Notion's 250px menu left of the "⋯" (seen 2026-10-30).
 */
function TemplateMenuRow({ icon, name, isDefault, onUse, actions, repeat, drag, moreTip, bare = false, lit = false }: {
  icon: React.ReactNode; name: string; isDefault: boolean; onUse(): void; actions: TemplateMenuAction[];
  /** Shaded by the "필터…" field's arrows. */
  lit?: boolean;
  /** Found by "필터…": no handle's room before its mark, which starts 8px in, as Notion's (seen 2026-11-16). */
  bare?: boolean;
  /** Its handle and its place in the live reorder, for a template the menu may move. */
  drag?: { reorder: DragReorder; id: string; above?: string; below?: string };
  /** The "⋯"'s tip above it, as Notion's "이 템플릿 편집" and the empty page's "이 템플릿 보기". */
  moreTip?: string;
  /** "복제 주기" opens its list under its own line, the menu kept open, as Notion's. */
  repeat?: { template: DatabaseToolbarTemplateMetadataWire; onSave(repeat: DatabaseToolbarTemplateRepeatWire | null): void };
}) {
  const { t } = useTranslation("databaseView");
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null), menuRef = useRef<HTMLDivElement>(null);
  // Over the page, not inside the scrolling template menu: 12px left of
  // the "⋯", its top on the button's.
  const place = usePopoverPlacement(menuRef, open && buttonRef.current
    ? (size, viewport) => placeBeside(popoverAnchorOf(buttonRef.current!), size, viewport, { prefer: "left", gap: 12 }) : null,
  [open], undefined, { kind: "menu", onEscape: () => setOpen(false), onOutsidePress: () => setOpen(false),
    inside: () => [buttonRef.current], returnFocusRef: buttonRef });
  const [choosing, setChoosing] = useState(false), chooserRef = useRef<HTMLDivElement>(null);
  const chooserPlace = usePopoverPlacement(chooserRef, choosing && open && menuRef.current?.firstElementChild
    ? (size, viewport) => placeAround(popoverAnchorOf(menuRef.current!.firstElementChild!), size, viewport, { align: "start", gap: 2 }) : null,
  // The chooser keeps its own layer: Escape and a press outside ask before dropping a change.
  [choosing, open], undefined, false);
  // A repeating template's blue mark before its "⋯" opens its "주기 편집" under it (Notion's, seen 2026-11-03).
  const repeatTip = repeat ? templateRepeatTip(repeat.template, t) : "";
  const [marked, setMarked] = useState(false), markRef = useRef<HTMLButtonElement>(null), markChooserRef = useRef<HTMLDivElement>(null);
  const markPlace = usePopoverPlacement(markChooserRef, marked && markRef.current
    ? (size, viewport) => placeAround(popoverAnchorOf(markRef.current!), size, viewport, { align: "start", gap: 2 }) : null,
  [marked], undefined, false);
  const menu = useNativeMenu({ items: actions, open, menuRef, label: t("templateMenu.actions", { name }), keyOf: (action) => action.key,
    textOf: (action) => action.label, onAction: (action) => {
      if (action.key === "repeat" && repeat) { setChoosing(true); return; }
      setOpen(false); action.run();
    } });
  return (
    <div {...drag?.reorder.row(drag.id)} className={styles.templateMenuRow} data-template-menu-row data-open={open ? "true" : undefined} data-bare={bare || undefined} data-lit={lit ? "true" : undefined}
      data-repeating={repeatTip ? "true" : undefined}>
      {drag ? <button type="button" {...drag.reorder.handle(drag.id, true, { above: drag.above, below: drag.below })}
        className={styles.templateMenuHandle} aria-label={t("templateMenu.move", { name })}>
        <DragHandleIcon size="small" aria-hidden="true" />
      </button> : null}
      <button type="button" className={styles.templateMenuUse} onClick={onUse}>
        <span className={styles.templateIcon}>{icon}</span>
        <span className={styles.templateMenuName}>{name}</span>
        {isDefault ? <span className={styles.templateMenuBadge}>{t("templateMenu.default")}</span> : null}
      </button>
      {repeatTip ? <button ref={markRef} type="button" className={styles.templateMenuRepeat} aria-haspopup="dialog" aria-expanded={marked}
        aria-label={repeatTip} title={repeatTip} data-tip-side="top" onClick={() => setMarked((value) => !value)}>
        <RepeatIcon size="small" aria-hidden="true" />
      </button> : null}
      {actions.length ? <button ref={buttonRef} type="button" className={styles.templateMenuMore} aria-haspopup="menu" aria-expanded={open}
        aria-label={t("templateMenu.actions", { name })} title={moreTip} data-tip-side="top"
        onClick={() => setOpen((value) => !value)}>
        <DotsHorizontal size="small" aria-hidden="true" />
      </button> : null}
      {open ? createPortal(<div {...menu.menuProps} ref={menuRef} className={styles.templateMenuActions}
        style={{ position: "fixed", ...popoverPlacementStyle(place) }}>
        {actions.map((action) => (
          <NativeMenuItem key={action.key} state={menu.state} itemKey={action.key} className={styles.templateMenuAction}>
            {({ labelProps }) => <>
              <span className={styles.templateMenuActionIcon} aria-hidden="true">{action.icon}</span>
              <span {...labelProps}>{action.label}</span>
              {action.value ? <span className={styles.templateMenuActionValue}>{action.value}<ChevronDown size="small" aria-hidden="true" /></span> : null}
            </>}
          </NativeMenuItem>
        ))}
      </div>, document.body) : null}
      {open && choosing && repeat ? createPortal(
        <DatabaseTemplateRepeatChooser template={repeat.template} boxRef={chooserRef}
          placement={{ position: "fixed", ...popoverPlacementStyle(chooserPlace) }} onClose={() => setChoosing(false)}
          // Saving goes back to the "⋯" menu, which shows the new value (Notion's, seen 2026-11-02).
          onSave={(next) => { setChoosing(false); repeat.onSave(next); }} />, document.body) : null}
      {marked && repeat ? createPortal(
        <DatabaseTemplateRepeatChooser template={repeat.template} boxRef={markChooserRef}
          placement={{ position: "fixed", ...popoverPlacementStyle(markPlace) }} onClose={() => setMarked(false)}
          onSave={(next) => { setMarked(false); repeat.onSave(next); }} />, document.body) : null}
    </div>
  );
}
