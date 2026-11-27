import { useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ChevronDown, Copy, Pencil, RepeatIcon, Trash } from "@/icons/leafen";
import { databaseTemplateActions, useDatabaseTemplates, type DatabaseTemplateChoice } from "@/lib/databaseTemplateRequests";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeAround, popoverAnchorOf, popoverBackdropStyle, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { DatabaseTemplateRepeatChooser, templateRepeatLabel } from "./DatabaseTemplateRepeatChooser";
import menu from "./DatabaseMoreSettingsMenu.module.css";

const MENU_WIDTH = 250;

/**
 * A template's "···" in a database page's empty body, as Notion's (seen
 * 2026-10-13): a 250px menu under it — "복제 주기 〈사용 안 함〉 ⌄ · 편집 · 복제 ·
 * 삭제" — acting through the database's toolbar. "복제 주기" opens the
 * template's repeats under its line, the menu kept, and saving goes back to
 * the menu with the new value, as the toolbar's template menu does (seen
 * 2026-11-02). Both sit on the document, out of the page view's own
 * transform and styles.
 */
export function RowPageTemplateMenu({ anchor, dataSourceId, template, onClose }: {
  anchor: DOMRect; dataSourceId: string; template: DatabaseTemplateChoice; onClose(): void;
}) {
  const [repeat, setRepeat] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  return createPortal(<>
    <RowPageTemplateActions anchor={anchor} dataSourceId={dataSourceId} template={template} menuRef={menuRef}
      onRepeat={() => setRepeat(true)} onClose={onClose} />
    {repeat ? <RowPageTemplateRepeat menuRef={menuRef} dataSourceId={dataSourceId} templateId={template.id} onClose={() => setRepeat(false)} /> : null}
  </>, document.body);
}

function RowPageTemplateActions({ anchor, dataSourceId, template, menuRef, onRepeat, onClose }: {
  anchor: DOMRect; dataSourceId: string; template: DatabaseTemplateChoice; menuRef: RefObject<HTMLDivElement | null>;
  onRepeat(): void; onClose(): void;
}) {
  const { t } = useTranslation("databaseView");
  // Read again as the toolbar offers the templates anew, so the repeat's value follows a save.
  useDatabaseTemplates(dataSourceId);
  const metadata = databaseTemplateActions(dataSourceId)?.template(template.id);
  const items = [
    { id: "repeat", label: t("templateRowMenu.repeat"), icon: RepeatIcon },
    { id: "edit", label: t("templateRowMenu.edit"), icon: Pencil },
    { id: "duplicate", label: t("templateRowMenu.duplicate"), icon: Copy },
    { id: "delete", label: t("templateRowMenu.delete"), icon: Trash },
  ];
  const picker = useNativeMenu({ items, open: true, menuRef, label: t("templateRowMenu.label"), onClose,
    keyOf: item => item.id, textOf: item => item.label, onAction: item => {
      const actions = databaseTemplateActions(dataSourceId);
      if (item.id === "repeat" && actions) return onRepeat();
      onClose();
      if (item.id === "edit") actions?.edit(template.id);
      else if (item.id === "duplicate") actions?.duplicate(template.id);
      else if (item.id === "delete") actions?.remove(template.id);
    } });
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeAround(anchor, size, viewport, { gap: 4, align: "end" }),
    [anchor], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose });
  return <>
    <button type="button" className={menu.backdrop} style={popoverBackdropStyle(place)} tabIndex={-1}
      aria-label={t("templateRowMenu.close")} onClick={onClose} />
    <div {...picker.menuProps} ref={menuRef} className={menu.menu} data-row-page-template-menu
      style={{ width: MENU_WIDTH, ...popoverPlacementStyle(place) }}>
      {items.map(item => <NativeMenuItem key={item.id} state={picker.state} itemKey={item.id} className={menu.item}
        data-row-page-template-action={item.id}>
        {({ labelProps }) => <><item.icon size="regular" aria-hidden="true" /><span {...labelProps}>{item.label}</span>
          {item.id === "repeat" ? <>
            <small>{metadata ? templateRepeatLabel(metadata, t) : t(template.repeating ? "templateRepeat.repeating" : "templateRowMenu.repeatOff")}</small>
            <ChevronDown size="small" aria-hidden="true" />
          </> : null}</>}
      </NativeMenuItem>)}
    </div>
  </>;
}

function RowPageTemplateRepeat({ menuRef, dataSourceId, templateId, onClose }: {
  menuRef: RefObject<HTMLDivElement | null>; dataSourceId: string; templateId: string; onClose(): void;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const actions = databaseTemplateActions(dataSourceId);
  const template = actions?.template(templateId);
  // Under the menu's "복제 주기" line; Escape and a press outside are the chooser's, which asks before dropping a change.
  const line = menuRef.current?.firstElementChild;
  const place = usePopoverPlacement(panelRef, line
    ? (size, viewport) => placeAround(popoverAnchorOf(line), size, viewport, { align: "start", gap: 2 }) : null,
  [line], undefined, false);
  if (!actions || !template) return null;
  return <DatabaseTemplateRepeatChooser template={template} boxRef={panelRef} placement={{ position: "fixed", ...popoverPlacementStyle(place) }}
    onClose={onClose} onSave={(repeat) => { actions.setRepeat(templateId, repeat); onClose(); }} />;
}
