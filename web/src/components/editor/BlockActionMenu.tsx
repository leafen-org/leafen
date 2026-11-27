import { Fragment, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphBlockType } from "@/contentGraph";
import { BulletedListIcon, ChevronLeft, ChevronRight, GridIcon, TableHeaderColumnIcon, TableHeaderRowIcon } from "@/icons/leafen";
import { matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";
import { placeAround, placeAtPoint, placeBeside, POPOVER_MARGIN, popoverBackdropStyle, popoverPlacementStyle, popoverRectAt, usePopoverPlacement } from "@/lib/popoverPlacement";
import { BlockMenuSubmenu, type BlockSubmenu } from "./BlockMenuSubmenu";
import { blockMenuShape } from "./blockMenuShape";
import { blockTurnChoices } from "./blockTurnInto";
import styles from "./editorStyles.generated.module.css";
import { usePressRehearsal } from "@/lib/pressRehearsal";
import { NativeMenuItem, useNativeMenu, useNativeSubmenu, useOpenedFromKeyboard } from "@/lib/nativeMenu";
import { BlockMenuList, BlockMenuSearch } from "./BlockMenuSearchHead";
import { routeOverlayKeyDown, useOverlayLayer } from "@/lib/overlayLayer";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneMenuHeader } from "./PhoneMenuHeader";
import phoneStyles from "./PhoneMenu.module.css";
import { BlockActionPhoneSheet, blockSheetGroups } from "./BlockActionPhoneSheet";
import { BlockStripPanel } from "./BlockStripPanel";
import { currentStripPanel } from "./stripPanelState";
import { runDatabaseMenuShortcut } from "./useBlockDatabaseMenu";
import { fileCaptionInMore, fileMoreLead, videoFitInMore } from "./blockActionMenuFileRows";
import { IMAGE_MORE_ICONS, imageMoreEntries } from "./blockActionMenuImageRows";
import { ACTION_ICONS } from "./blockActionMenuIcons";
import { blockActionMenuItems, blockMenuKeys, blockTypeLabel, type BlockActionMenuProps, type BlockMenuPanel, type DatabasePageLeftovers } from "./blockActionMenuItems";
import { useBlockMenuShareKey } from "./blockMenuShareKey";

export type { BlockMenuImage, BlockMenuImageFields } from "./blockActionMenuImageRows";
export type { BlockMenuPanel, BlockPageInAnchor, DatabasePageLeftovers } from "./blockActionMenuItems";

/**
 * The block menu of the handle: delete, duplicate, turn into, the image's
 * own actions, copy, copy link to block, move, comment, caption and color,
 * in the preserved order. Turn into and color open beside the menu, as
 * Notion's; alignment opens as a panel of it, an image's link and alt text their own windows (ImageLinkWindow, ImageAltText).
 * Its rows are blockActionMenuItems'.
 */
export function BlockActionMenu(props: BlockActionMenuProps) {
  const { activeColor, activeType, captionVisible, direction, image, emptyImage, file, database, listFormat, selectionCount, several, turnIntoSynced = true,
    onClose, onColor, onDuplicate, onDuplicateWithoutContent, toggleCaption, turnInto, onPageIn, onTurnDatabase, code, table, initialPanel = "main",
    anchor, footer, returnFocusRef, readOnly = false } = props;
  // A short menu has the search only where its handle says (ReadOnlyBlockHandle).
  const searchable = !readOnly || props.searchable === true;
  const { t } = useTranslation(["blockHandle", "common", "editor"]);
  const { phone, touch } = useSidebarLayout(true), rehearsal = usePressRehearsal();
  // A phone's menu is Notion's "작업" sheet; its tools' "전환" and "색" keep their own screens, in the
  // keyboard's place where the strip opened them there (BlockStripPanel).
  const [strip] = useState(() => phone && (initialPanel === "turn" || initialPanel === "color") && currentStripPanel()?.kind === initialPanel
    ? initialPanel : null);
  const sheet = phone && initialPanel === "main", own = sheet || strip !== null;
  const menuRef = useRef<HTMLDivElement>(null), searchRef = useRef<HTMLInputElement>(null);
  const turnRef = useRef<HTMLButtonElement>(null), colorRef = useRef<HTMLButtonElement>(null), directionRef = useRef<HTMLButtonElement>(null);
  const turnMenuRef = useRef<HTMLDivElement>(null), colorMenuRef = useRef<HTMLDivElement>(null), directionMenuRef = useRef<HTMLDivElement>(null);
  const moreRef = useRef<HTMLButtonElement>(null), moreMenuRef = useRef<HTMLDivElement>(null);
  const openWithRef = useRef<HTMLButtonElement>(null), openWithMenuRef = useRef<HTMLDivElement>(null);
  const [panel, setPanel] = useState<BlockMenuPanel>("main"), [query, setQuery] = useState("");
  const [leftovers, setLeftovers] = useState<DatabasePageLeftovers | null>(null);
  const request = useRef(0), live = useRef(true);
  useLayoutEffect(() => { live.current = true; return () => { live.current = false; request.current++; }; }, []);
  const many = selectionCount > 1;
  // The common rows (blockMenuShape); an empty image has no turn or color, only a link for the picture to come (Notion's, 2026-11-03).
  // The head names the blocks' kind, several of more than one kind by their count, as Notion's "블록 3개" (seen 2026-11-13) —
  // the blocks the handle gives (blockMenuSelection), none inside another, so lines chosen inside a chosen one count for nothing;
  // several as their first leads them (blockMenuSeveral).
  const shape = many ? several?.shape ?? blockMenuShape(activeType === "mixed" ? [] : [activeType]) : { ...blockMenuShape(activeType), ...(emptyImage ? { turn: false, color: false } : {}) };
  const kindHead = <div className={styles.menuLabel} data-block-menu-type>{many && several ? several.head
    : activeType === "mixed" ? t("blockHandle:menu.blockCount", { count: selectionCount }) : blockTypeLabel(activeType)}</div>;
  const turnChoices = !many && (image || file || database)
    ? image?.turnChoices ?? file?.turnChoices ?? database?.turnChoices ?? [] : blockTurnChoices(selectionCount, turnIntoSynced);
  // A file row that turns into another block keeps its original and rename
  // under "추가 옵션", as Notion's (seen 2026-11-07: a .docx and a .pdf row);
  // one that doesn't, a .txt, shows them in the menu but on a touch screen (BlockMenuFile.groupMore).
  const fileMore = Boolean(file) && activeType === "file" && (turnChoices.length > 0 || file?.groupMore === true);
  // From its handle the menu stands left of it (right where there is no
  // room), 5px off, its middle on the handle's and kept 12px inside the
  // window, as Notion's (seen 2026-10-31; again 2026-11-01: the gap on both
  // sides, 12 above and below); a right click opens it at the pointer. From
  // the keyboard it stands under the block's box at its left edge, or over it
  // where only that has room, and where neither has moved up to show whole
  // (seen 2026-11-04 and, a simple table's, 2026-11-09).
  const handle = anchor?.handle, box = anchor?.box;
  // No taller than 70% of the window, its rows scrolling inside, as Notion's
  // (seen 2026-11-07: 408 high in a 583 window, where it was all 533).
  const placed = usePopoverPlacement(menuRef, anchor && !own ? (measured, viewport) => {
    const cap = Math.floor(viewport.height * 0.7);
    const size = { ...measured, height: Math.min(measured.height, cap) };
    return handle
      ? placeBeside({ ...popoverRectAt({ top: handle.top, bottom: handle.bottom, left: handle.left }), right: handle.right }, size, viewport,
        { prefer: "left", gap: 5, offsetTop: (handle.bottom - handle.top - size.height) / 2, cap })
      : box ? placeAround(popoverRectAt({ top: box.top, bottom: box.bottom, left: box.left }), size, viewport, { gap: 0, rise: true, cap,
        prefer: viewport.height - box.bottom - POPOVER_MARGIN < size.height && box.top - POPOVER_MARGIN >= size.height ? "above" : "below" })
      // At a press (a right click, a tool's "…"), from the pixel under the pointer, whole (pressedPoint).
      : anchor.pressed ? placeAtPoint(anchor, size, viewport, { cap })
      // At a point worked out from a box (a key on a tool's "…"), over it where
      // only that has room: its bottom at the point (Notion's code "…", seen 2026-11-06).
      : viewport.height - anchor.y - POPOVER_MARGIN < size.height && anchor.y - POPOVER_MARGIN >= size.height
        ? placeAround(popoverRectAt({ top: anchor.y, bottom: anchor.y, left: anchor.x }), size, viewport, { gap: 0, rise: true, prefer: "above", cap })
        : placeAtPoint(anchor, size, viewport, { cap });
  } : null,
    [anchor], undefined, false);
  const visible = !anchor || placed !== null;
  // A menu opened by the pointer leaves no focus ring on its handle when it
  // closes, as Notion's (seen 2026-10-31); one opened from the keys gives the
  // focus back to where it came from.
  const keysNow = useOpenedFromKeyboard();
  const [openedFromKeys] = useState(keysNow);
  useBlockMenuShareKey(!own && !rehearsal, onClose);
  const layer = useOverlayLayer(!own && !rehearsal, { kind: "menu", ref: menuRef, floats: false, onEscape: () => panel === "main" ? onClose() : setPanel("main"),
    onOutsidePress: onClose, restoreFocus: openedFromKeys, returnFocusRef });
  async function turnDatabase() {
    if (!onTurnDatabase) return;
    const generation = ++request.current, command = onTurnDatabase;
    const found = command.into === "inline" ? await command.leftovers?.() : null;
    if (!live.current || generation !== request.current) return;
    if (found && (found.shares > 0 || found.comments > 0)) { setLeftovers(found); setPanel("turnInline"); }
    else command.run();
  }
  const actions = blockActionMenuItems(props, { t, panel, setPanel, query, many, shape, turnChoices, fileMore, kindHead, sheet, touch, leftovers, turnDatabase });

  const picker = useNativeMenu({ items: actions, open: visible && !own && !rehearsal, menuRef, label: t("blockHandle:menu.ariaLabel"),
    disabledKeys: database?.rows.filter(row => row.disabled).map(row => row.id),
    focusOwnerRef: panel === "main" && searchable ? searchRef : undefined,
    focusOnOpen: panel === "turnInline" || !searchable,
    keyOf: action => action.id, textOf: action => action.label, onAction: action => action.run() });
  const turn = useNativeSubmenu({ triggerKey: "turn", rootTrigger: picker.rootTrigger, parentMenuRef: menuRef, submenuRef: turnMenuRef, triggerRef: turnRef });
  const color = useNativeSubmenu({ triggerKey: "color", rootTrigger: picker.rootTrigger, parentMenuRef: menuRef, submenuRef: colorMenuRef, triggerRef: colorRef });
  const textDirection = useNativeSubmenu({ triggerKey: "direction", rootTrigger: picker.rootTrigger, parentMenuRef: menuRef, submenuRef: directionMenuRef, triggerRef: directionRef });
  const more = useNativeSubmenu({ triggerKey: "more", rootTrigger: picker.rootTrigger, parentMenuRef: menuRef, submenuRef: moreMenuRef, triggerRef: moreRef });
  const duplicateRef = useRef<HTMLButtonElement>(null), duplicateMenuRef = useRef<HTMLDivElement>(null);
  const duplicates = useNativeSubmenu({ triggerKey: "duplicate", rootTrigger: picker.rootTrigger, parentMenuRef: menuRef, submenuRef: duplicateMenuRef, triggerRef: duplicateRef });
  const alignRef = useRef<HTMLButtonElement>(null), alignMenuRef = useRef<HTMLDivElement>(null);
  const alignMenu = useNativeSubmenu({ triggerKey: "align", rootTrigger: picker.rootTrigger, parentMenuRef: menuRef, submenuRef: alignMenuRef, triggerRef: alignRef });
  const openWith = useNativeSubmenu({ triggerKey: "openWith", rootTrigger: picker.rootTrigger, parentMenuRef: menuRef, submenuRef: openWithMenuRef, triggerRef: openWithRef });
  const listFormatRef = useRef<HTMLButtonElement>(null), listFormatMenuRef = useRef<HTMLDivElement>(null);
  const listFormats = useNativeSubmenu({ triggerKey: "listFormat", rootTrigger: picker.rootTrigger, parentMenuRef: menuRef, submenuRef: listFormatMenuRef, triggerRef: listFormatRef });
  const languageRef = useRef<HTMLButtonElement>(null), languageMenuRef = useRef<HTMLDivElement>(null);
  const languages = useNativeSubmenu({ triggerKey: "language", rootTrigger: picker.rootTrigger, parentMenuRef: menuRef, submenuRef: languageMenuRef, triggerRef: languageRef });
  const submenus = { turn, color, direction: textDirection, more, duplicate: duplicates, align: alignMenu, openWith, listFormat: listFormats, language: languages };
  const triggerRefs = { turn: turnRef, color: colorRef, direction: directionRef, more: moreRef, duplicate: duplicateRef, align: alignRef, openWith: openWithRef, listFormat: listFormatRef, language: languageRef };
  // A submenu its row's press opened takes the keys at its first row, unringed till a key moves; the keys' opening
  // rings it at once (Notion's, seen 2026-11-11: "전환" pressed, "텍스트" only greyed).
  const [pressedOpen, setPressedOpen] = useState<BlockSubmenu | null>(null);
  useLayoutEffect(() => { if (pressedOpen && !submenus[pressedOpen].state.isOpen) setPressedOpen(null); });
  const submenuRefs = { turn: turnMenuRef, color: colorMenuRef, direction: directionMenuRef, more: moreMenuRef, duplicate: duplicateMenuRef, align: alignMenuRef, openWith: openWithMenuRef,
    listFormat: listFormatMenuRef, language: languageMenuRef };
  const duplicateEntries = [
    { id: "duplicate", label: t("blockHandle:menu.duplicateWithContent"), icon: null, run: onDuplicate, hint: blockMenuKeys("duplicateBlock") },
    { id: "duplicate-empty", label: t("blockHandle:menu.duplicateWithoutContent"), icon: null, run: onDuplicateWithoutContent ?? onDuplicate, hint: blockMenuKeys("duplicateBlockWithoutContent") },
  ];
  const openWithEntries = (file?.openWith ?? []).map(app => ({ id: `open-with:${app.id}`, label: app.name, icon: <GridIcon size="regular" />, run: app.run }));
  // "추가 옵션": alt text (a panel of this menu), full screen, the original, the image's link.
  const moreEntries = image ? imageMoreEntries(image, t, { close: () => more.state.close(), rect: () => moreMenuRef.current?.getBoundingClientRect() }, sheet)
  : database ? database.rows.filter(row => row.place === "more").map(row => ({ ...row, icon: ACTION_ICONS[row.id], hint: row.shortcut ? blockMenuKeys(row.shortcut) : row.hint }))
  : fileMore && file ? [
    ...(fileCaptionInMore(file, captionVisible) ? [{ id: "caption", label: t("blockHandle:media.caption"), icon: ACTION_ICONS.caption, run: toggleCaption, hint: blockMenuKeys("toggleCaption") }] : []),
    { id: "view-original", label: t("blockHandle:media.viewOriginal"), icon: IMAGE_MORE_ICONS["view-original"], run: file.onViewOriginal },
    ...(file.onRename ? [{ id: "rename", label: t("blockHandle:media.rename"), icon: ACTION_ICONS.rename, run: file.onRename, hint: blockMenuKeys("renamePage") }] : []),
  ] : file?.onFullScreen || file && videoFitInMore(file, activeType, sheet) ? [
    fileMoreLead(file, t, { fullScreen: IMAGE_MORE_ICONS["full-screen"], fit: ACTION_ICONS.align }),
    { id: "view-original", label: t("blockHandle:media.viewOriginal"), icon: IMAGE_MORE_ICONS["view-original"], run: file.onViewOriginal },
  ] : sheet && code ? [
    { id: "language", label: t("blockHandle:menu.codeLanguage"), icon: ACTION_ICONS.language, run: () => {}, opens: "language" as const },
    ...(code.onFormat ? [{ id: "code-format", label: t("blockHandle:menu.formatCode"), icon: ACTION_ICONS["code-format"], run: code.onFormat }] : []),
  ] : [];
  const initialColor = useRef(false), lastPanel = useRef<BlockMenuPanel | null>(null);
  useLayoutEffect(() => {
    if (!visible || own || rehearsal) return;
    if ((initialPanel === "color" || initialPanel === "turn") && !initialColor.current) {
      initialColor.current = true; (initialPanel === "turn" ? turn : color).state.open("first");
    }
    // A touch screen's search waits for a tap with the keyboard down: Measured 2026-11-16 after the press only (no script in the page before it), Notion's iPad Pro 11 Safari (two tabs 11 minutes apart), ⋮⋮ tapped, no caret in it.
    // Elsewhere the search takes the keys: Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440, ⌘/ from a line opened it with its search ringed.
    else if (panel === "main" && lastPanel.current !== panel && !touch)
      searchRef.current?.focus({ preventScroll: true });
    lastPanel.current = panel;
  }, [visible, panel, initialPanel, color.state.open, turn.state.open, touch, rehearsal]);
  // The submenu machinery above reads the menu's own element, which stands hidden for the panel.
  if (strip) return <><div ref={menuRef} hidden />
    <BlockStripPanel kind={strip} onClose={onClose} onColor={onColor} turnInto={turnInto}
      source={{ activeColor, activeType, selectionCount, direction, choices: turnChoices }} /></>;
  if (sheet) {
    const iconOf = (id: string) => id === "listFormat" && !listFormat?.label ? <BulletedListIcon size="regular" />
      : id === "header-row" ? <TableHeaderRowIcon size="regular" /> : id === "header-column" ? <TableHeaderColumnIcon size="regular" /> : ACTION_ICONS[id];
    const rows = blockSheetGroups(actions, iconOf, t("blockHandle:menu.beta"), new Set(database?.rows.filter(row => row.disabled).map(row => row.id)));
    const kindName = readOnly || panel === "main" ? image && !many ? blockTypeLabel("image") : file && !many ? file.label ?? blockTypeLabel(activeType as GraphBlockType)
      : database && !many ? database.header : table && !many ? blockTypeLabel("table") : activeType !== "mixed" ? blockTypeLabel(activeType) : undefined : undefined;
    return <BlockActionPhoneSheet typeLabel={kindName} groups={panel === "main" ? rows : []} footer={panel === "main" ? footer : undefined}
      source={kind => ({ activeColor, activeType, selectionCount, direction, choices: turnChoices, alignment: file?.align ?? image?.align, listFormat,
        language: many ? several?.language : code?.language, entries: kind === "more" ? moreEntries : kind === "duplicate" ? duplicateEntries : kind === "openWith" ? openWithEntries : undefined })}
      panel={panel === "main" ? undefined : { title: actions.find(action => action.id === "back")?.label ?? "", onBack: () => setPanel("main"), rows: rows.flat(),
        children: panel === "turnInline" && leftovers ? <>
          <p className={styles.blockMenuFieldHelp}>{t("blockHandle:turnInline.links")}</p>
          {leftovers.shares > 0 ? <p className={styles.blockMenuFieldHelp}>{t("blockHandle:turnInline.shares", { count: leftovers.shares })}</p> : null}
          {leftovers.comments > 0 ? <p className={styles.blockMenuFieldHelp}>{t("blockHandle:turnInline.comments", { count: leftovers.comments })}</p> : null}
        </> : undefined }}
      sheetRef={menuRef} onClose={onClose} onColor={onColor} turnInto={turnInto} />;
  }
  // The search stands above the rows, which scroll under it (BlockMenuList); a phone's menu keeps one list.
  const head = panel === "main" && searchable && !phone;
  const divider = <div className={styles.menuDivider} role="separator" />;
  const closeChildren = () => {
    const expanded = picker.rootTrigger.expandedKeysStack[0];
    if (expanded) picker.rootTrigger.closeSubmenu(expanded, 0);
  };
  const menu = <>
    <button type="button" tabIndex={-1} className={`${styles.menuBackdrop} ${styles.blockMenuBackdrop}`}
      style={{ ...popoverBackdropStyle(placed), zIndex: (layer.zIndex ?? 2000) - 1 }}
      aria-label={t("blockHandle:buttons.closeBlockActions")} onClick={onClose} />
    <div {...picker.menuProps} ref={menuRef} className={`${styles.blockMenu} ${phone ? phoneStyles.menu : head ? styles.blockMenuHead : ""}`} data-block-menu-panel={panel}
      data-turn-inline-confirm={panel === "turnInline" || undefined}
      role={panel === "turnInline" ? "alertdialog" : "menu"}
      aria-label={panel === "turnInline" ? t("blockHandle:turnInline.title") : t("blockHandle:menu.ariaLabel")}
      style={{ ...(anchor ? (placed ? popoverPlacementStyle(placed) : { left: anchor.x, top: anchor.y }) : {}),
        zIndex: layer.zIndex, visibility: visible ? "visible" : "hidden", ...(head ? { overflowY: "hidden" } : {}) }}
      onFocus={event => {
        if (event.target === searchRef.current) { picker.menuProps.onFocus(event); return; }
        if (event.target !== event.currentTarget && !event.target.hasAttribute("data-block-menu-item")) {
          picker.state.selectionManager.setFocused(false); return;
        }
        picker.menuProps.onFocus?.(event);
      }}
      onKeyDown={event => {
        if (event.key === "Escape") { routeOverlayKeyDown(event); return; }
        if (runDatabaseMenuShortcut(database, event)) return;
        if (fileMore && file?.onRename && matchesKeyboardShortcut("renamePage", event)) { event.preventDefault(); file.onRename(); return; }
        if (fileCaptionInMore(file, captionVisible) && matchesKeyboardShortcut("toggleCaption", event)) { event.preventDefault(); toggleCaption(); return; }
        const keyedAction = actions.find(action => action.shortcut && matchesKeyboardShortcut(action.shortcut, event));
        if (keyedAction) { event.preventDefault(); keyedAction.run(); return; }
        if (event.key === "ArrowLeft" && panel !== "main" && !(event.target instanceof HTMLInputElement)) {
          event.preventDefault(); setPanel("main"); return;
        }
        if (!(event.target instanceof HTMLInputElement)) picker.menuProps.onKeyDown?.(event);
      }}>
      {phone ? <PhoneMenuHeader title={t("blockHandle:menu.ariaLabel")} onClose={onClose} /> : null}
      {panel === "main" && searchable ? <BlockMenuSearch inputRef={searchRef} query={query} label={t("blockHandle:menu.searchActions")} ownerProps={picker.ownerProps}
        onQuery={value => { setQuery(value); closeChildren(); }} onMove={closeChildren}
        onEnter={() => Boolean(query.trim() && actions[0]) && (actions[0].run(), true)} /> : null}
      <BlockMenuList head={head}>{actions.map(action => <Fragment key={action.id}>{action.before}
        <NativeMenuItem state={picker.state} itemKey={action.id} className={styles.menuItem} data-block-menu-item
          data-block-menu-search-result={panel === "main" && !!query.trim() || undefined}
          {...action.attributes} buttonRef={action.submenu ? triggerRefs[action.submenu] : undefined}
          data-submenu={action.submenu ?? action.attributes?.["data-submenu"]}
          itemOptions={action.submenu ? { ...submenus[action.submenu].submenuTriggerProps,
            onPress: event => { submenus[action.submenu!].submenuTriggerProps.onPress?.(event);
              if (event.pointerType === "mouse" || event.pointerType === "touch") { submenus[action.submenu!].state.open("first"); setPressedOpen(action.submenu!); } } } : undefined}>
          {({ labelProps }) => <>{action.content?.(labelProps) ?? <>
            {ACTION_ICONS[action.id] ? <span aria-hidden="true">{ACTION_ICONS[action.id]}</span> : null}
            <span {...labelProps}>{action.id === "back" ? <ChevronLeft size="small" aria-hidden="true" /> : null}{action.label}</span></>}
            {action.hint ? <span className={styles.menuHint} aria-hidden="true">{action.hint}</span> : null}
            {action.submenu ? <ChevronRight className={styles.menuChevron} size="small" aria-hidden="true" /> : null}</>}
        </NativeMenuItem>
        {action.id === "back" && panel === "turnInline" && leftovers ? <>
          <p className={styles.blockMenuFieldHelp}>{t("blockHandle:turnInline.links")}</p>
          {leftovers.shares > 0 ? <p className={styles.blockMenuFieldHelp}>{t("blockHandle:turnInline.shares", { count: leftovers.shares })}</p> : null}
          {leftovers.comments > 0 ? <p className={styles.blockMenuFieldHelp}>{t("blockHandle:turnInline.comments", { count: leftovers.comments })}</p> : null}
        </> : null}
      </Fragment>)}
      {panel === "main" && !query.trim() && footer ? <>{divider}{footer}</> : null}</BlockMenuList>
    </div>
    {(["turn", "color", "direction", "more", "duplicate", "align", "openWith", "listFormat", "language"] as const).map(kind => submenus[kind].state.isOpen && panel === "main" && !query.trim()
      ? <BlockMenuSubmenu key={kind} kind={kind} main={menuRef} triggerRef={triggerRefs[kind]} menuRef={submenuRefs[kind]}
          mainPlace={placed ?? undefined} activeColor={activeColor} activeType={activeType} selectionCount={selectionCount}
          onBack={submenus[kind].state.close} onClose={onClose} onColor={onColor} turnInto={turnInto} onPageIn={onPageIn} direction={direction}
          entries={kind === "more" ? moreEntries : kind === "duplicate" ? duplicateEntries : kind === "openWith" ? openWithEntries : undefined}
          choices={turnChoices} alignment={file?.align ?? image?.align} listFormat={listFormat} language={many ? several?.language : code?.language}
          // Opened by the pointer too, the list takes the keys at its first row, unringed until a key moves
          // (Notion's: ↓ then goes to the second row, seen 2026-11-15; ← or Esc gives them back to the menu, 2026-11-01).
          menuOptions={submenus[kind].submenuProps} focusOnOpen openedByKeys={submenus[kind].state.focusStrategy !== null && pressedOpen !== kind} /> : null)}
  </>;
  return typeof document === "undefined" ? menu : createPortal(menu, rehearsal ?? document.body);
}
