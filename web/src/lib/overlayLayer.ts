import { type KeyboardEvent as ReactKeyboardEvent, type RefObject, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { enterBeingPressed, goOnToWhatWasPressed, keptFocusWhenPressed, keyCameLast, openerOf } from "./lastPress"; import { pressOnOpener, usePopupOpener } from "./openerPress";
import { useOverlay } from "react-aria";
import { afterNextPaint } from "./afterPress"; import { chosenItemControl, focusChosenItem } from "./chosenItemFocus";
import { createFocusManager } from "@react-aria/focus";
import { focusOwed, focusWhenFree, followRedrawn, redrawnControl, spotOf, standIn, standInWhereGone } from "./focusThroughWaits";
import { isolateBodyForModal, trapModalTab } from "./modalFocus";
import { rehomeModalFocus } from "./modalFocusRehome";
import { holdingTyping } from "./pageTitleFocus"; import { escapeForPeekBeside } from "./peekBesideEscape";
import { holdsPlaced } from "./popoverLayer"; import { handRingOn, holdUnderKind, type KindExceptions, type OverlayKind, takeHandedRing, withKind } from "./overlayKinds";
import { layScreenLayerOnPhonePage } from "./phoneBarGround";
import { sheetPhoneWindow } from "./phoneWindowSheet";
import { fieldFocusOnOpen, phoneScreen, raisesKeyboard } from "./screenKind";
import { type PageShield, syncPageShield } from "./pageHoverShield";
import { usePreventPageScroll } from "./preventPageScroll";
import { sheetLayerTop } from "./sheetLayers";
import { focusLeftBySheet, sheetPressLeavesPage } from "./sheetPressFocus";
import { keyboardPropsEvent, type LayerKey } from "./layerKeyEvent";
import { fieldEscape, imeComposing } from "./fieldKeyBoundary";
export { keyboardPropsEvent };

const LAYER_BASE = 2000, LAYER_STEP = 10;
// Layers that stay above every other, however late those open: a question
// the page waits on (2026-10-18: the settings window, opened after the local
// lock's unlock prompt, covered it, and nothing could answer it).
const LAYER_ABOVE = 9000;
type Layer = {
  zIndex: number; above: boolean; modal: boolean; ref: RefObject<HTMLElement | null>;
  escape: (event: LayerKey) => void; /** Escape leaves it open for what is under it (OverlayLayerOptions.escapeGoesOn). */ escapeGoesOn: () => boolean;
  /** Gives a key pressed at the text's caret to its picker (OverlayLayerOptions.onCaretKeyDown); whether it has one. */
  caretKey: (event: KeyboardEvent) => boolean;
  inside: () => ReadonlyArray<Element | null | undefined>;
  /** What of it still answers the pointer through the page's shield (OverlayLayerOptions.answersThroughShield). */
  answers: () => ReadonlyArray<Element | null | undefined>;
  modalOwned: () => ReadonlyArray<Element | null | undefined>;
  /** What opened it, and the layer that held that, for a layer opened from it; where it leaves the focus while open (focusLeftBySheet). */
  opener: HTMLElement | null; host: Layer | null; kept?: HTMLElement | null;
  /** A hover tip (OverlayLayerOptions.passive); its kind (overlayKinds.ts). */
  passive: () => boolean; kind: () => OverlayKind | undefined;
  /** Floats over the page, which takes no pointer under it (OverlayLayerOptions.floats). */
  shields: () => boolean;
  /** Drawn for the text's selection (OverlayLayerOptions.followsText). */
  followsText: () => boolean;
  /** Its press outside (OverlayLayerOptions.onOutsidePress), false when it has none, and whether that closes its host too (closesHost). */
  outsidePress: () => boolean; closesHost: () => boolean;
};
const layers: Layer[] = [];
// A popover drawn on the body from inside a layer (popoverLayer.tsx) is held by it, as when drawn in it.
function layerHolding(element: Element) { return [...layers].reverse().find(layer => holdsPlaced(layer.ref.current, element)) ?? null; }
const layerChanges = new Set<() => void>();
// Told once the commit is through: a layer whose effects replay closes and
// opens again in the same one.
function tellLayerChange() {
  if (layerChanges.size) queueMicrotask(() => { for (const listener of [...layerChanges]) listener(); });
}
/**
 * Whether a window, menu or picker the person opened is drawn over the layer
 * holding `element` (a hover tip is not): what is drawn under it isn't drawn
 * anew meanwhile (2026-11-06: the settings read again after the server came
 * back and the export window open over them went with the section).
 */
export function layerOpenOver(element: Element): boolean {
  const holding = layerHolding(element);
  return layers.slice(holding ? layers.indexOf(holding) + 1 : 0)
    .some(layer => layer.ref.current?.isConnected && !layer.passive());
}
/**
 * The control that opened the outermost window or menu holding `element`,
 * under all of them; null outside any or where it is gone. A press in a menu
 * belongs to the place it was opened from.
 */
export function layerOrigin(element: Element): HTMLElement | null {
  let layer = layerHolding(element);
  for (let depth = 0; layer?.host && depth < layers.length; depth++) layer = layer.host;
  return layer?.opener?.isConnected ? layer.opener : null;
}
/** The top window or menu now open (not a hover tip): a phone sheet opened from it goes just over it (PhoneSheet.tsx). */
export function topLayerZIndex(): number | undefined {
  const open = layers.filter(layer => layer.ref.current?.isConnected && !layer.passive());
  return open.length ? Math.max(...open.map(layer => layer.zIndex)) : undefined;
}
// The top window or menu Escape closes, past a tip and one Escape goes on from (escapeGoesOn: a hover card, a margin card):
// a menu's search field under its foot's activity card closes the menu, the card with it, as Notion's (2026-11-26).
function escapeLayer() { return [...layers].reverse().find(layer => layer.ref.current?.isConnected && !layer.passive() && !layer.escapeGoesOn()); }
/** Whether a window or menu Escape may close is open, under one it goes on from or not (hoverCard.ts, iconTips.ts). */ export const topLayerTakesEscape = () => escapeLayer() !== undefined, layerTakesEscape = topLayerTakesEscape;
/** Hears each layer open and close. */
export function onLayersChanged(listener: () => void) { layerChanges.add(listener); return () => { layerChanges.delete(listener); }; }
// A layer closed for what its action opens next (the menu's "설정")
// hands its opener on to that, which may draw after the focus has gone to
// the page (2026-10-28: the settings window's code was still loading).
const HAND_OFF_MS = 3000;
let handedOff: { layer: Layer; at: number } | null = null;
function takeHandedOff() {
  const taken = handedOff && Date.now() - handedOff.at < HAND_OFF_MS ? handedOff.layer : null;
  handedOff = null;
  return taken;
}
// The control to give the focus back to for a layer whose opener went with
// the layer that held it (a menu's item, the menu closed as it opened this):
// whatever opened that layer, while it is drawn and that layer is closed
// (2026-10-28: settings opened from the workspace menu left the focus on the
// page when it closed; Notion's page menu gives it back to its button).
function openerBehind(host: Layer | null): HTMLElement | null {
  for (let layer = host; layer && !layers.includes(layer); layer = layer.host) {
    if (layer.opener?.isConnected) return layer.opener;
    const again = redrawnControl(layer.opener);
    if (again) return again;
  }
  return null;
}
// An opener drawn again as another element is found by its mark
// (focusThroughWaits.ts).
function markedOpener(opener: HTMLElement | null, host: Layer | null) {
  if (opener?.dataset.focusReturn) return opener;
  for (let layer = host; layer && !layers.includes(layer); layer = layer.host) {
    if (layer.opener?.dataset.focusReturn) return layer.opener;
  }
  return null;
}
// Where a closed layer leaves the focus, as Notion's (seen 2026-11-10 menu by menu, opened by a press or a key,
// closed by Escape or a press outside): a control of a bar — the top bar, a side peek's bar, the sidebar's head and
// foot, a database's title, tabs and toolbar, a table head's "+" — takes it back; one that belongs to an item of a
// page or of the sidebar's tree — a block, a page's property, a database's row, card, bar or column, a tree's page —
// leaves it on the page, so no ring is left on it and ⌘Z answers at once (the row page's "+ 속성 추가", a bookmark's
// window, a picture's preview, a sidebar page's "⋯" and its rename). A file's row, itself the control its preview
// opened from, takes it back though (seen 2026-11-12, and twice on 2026-11-15 in fresh Chrome tabs with no script
// before the action: × or Escape, then a typed key landing nowhere, the focus on the row, Escape's ringed; a phone's
// was not compared). Text being edited and a table's cell, which its keys move from, keep it. A database's view
// counts as its own place, so an inline one's controls answer as a full-page one's. A control of a phone's drawer
// that closed while what it opened was open leaves it on the page too, as Notion's (seen 2026-11-13 in iPhone
// Safari: search, or the settings, opened from the drawer and closed, the focus on the body).
const FOCUS_KEEPER = 'input, textarea, select, [contenteditable="true"], [role="gridcell"]:not([data-table-row-gutter-cell])';
const FOCUS_BAR = '[data-database-chrome], [data-table-add-property], [data-database-view], [role="toolbar"], [role="tablist"]';
const FOCUS_ITEM = '[data-block-id], [data-row-property-id], [data-row-add-property-row], [data-table-row-id], [data-timeline-table-row-id], [data-timeline-row-id], [data-list-row-id], [data-row-id], [data-table-head] [role="columnheader"], [role="treeitem"]';
const CLOSED_DRAWER = "[data-mobile-sidebar-slot]:not([data-open])";
export function focusGoesToPage(opener: HTMLElement | null) {
  if (!opener || opener.isContentEditable || opener.matches("[data-file-open]") && !phoneScreen() || chosenItemControl(opener)) return false;
  if (opener.closest(CLOSED_DRAWER)) return true;
  const place = opener.closest(`${FOCUS_KEEPER}, ${FOCUS_BAR}, ${FOCUS_ITEM}`);
  return Boolean(place && !place.matches(`${FOCUS_KEEPER}, ${FOCUS_BAR}`)
    && place.closest("[data-page-scroll], [data-app-sidebar]"));
}
// A focus a layer held and dropped to the page while it stayed open (its line went with a step it replaced, or a
// deletion) stays there when it closes, as Notion's (seen 2026-11-10: the view settings' "레이아웃" step, then
// Escape). A modal window gives such a focus a stand-in instead (keepModalFocus). Layers opened so far, hover tips aside.
let droppedAt = 0, openings = 0; export const layersOpened = () => openings;
/** Gives the focus back, by those rules, to the control a menu that keeps its
 *  own opener came from (a chosen item's, chosenItemFocus.ts). */
export function focusOpener(control: HTMLElement | null | undefined, options?: FocusOptions) {
  if (control?.isConnected && !focusGoesToPage(control) && Date.now() - droppedAt > 1000) focusChosenItem(control, options);
}
/** focusOpener once the closing is painted, unless another layer opened meanwhile, which has the focus now (2026-11-25:
 *  a row's menu closed by Escape gave its title cell the focus after a right press at once had opened the next menu,
 *  and that menu's Escape went to the cell, the menu staying open). */
export function focusOpenerAfterPaint(control: HTMLElement | null | undefined, options?: FocusOptions) {
  const at = openings; window.requestAnimationFrame(() => { if (openings === at) focusOpener(control, options); });
}
let listening = false;
/** The floating layers on top (hover tips aside), as one: the page's backdrop goes under the lowest of them, below a
 * layer's own backdrop at its foot (popoverBackdropStyle), and what each answers through it is cut out (pageHoverShield.ts), the layers' own elements (`own`) with all they draw. */
function floatingShield(): PageShield | null {
  const drawn = layers.filter(layer => layer.ref.current?.isConnected && !layer.passive());
  let top = drawn.length - 1;
  while (top >= 0 && !drawn[top].shields()) top--;
  if (top < 0) return null;
  let low = top;
  while (low > 0 && drawn[low - 1].shields()) low--;
  const run = drawn.slice(low, top + 1), connected = <T extends Element>(part: T | null | undefined): part is T => part instanceof Element && part.isConnected;
  const own = run.flatMap(layer => [layer.ref.current, menuBeside(layer)?.ref.current]).filter(connected);
  return { zIndex: run[0].zIndex - 2, layers: run.map(layer => layer.ref.current).filter(connected), own,
    open: run.flatMap(layer => [layer.ref.current, ...layer.answers(), menuBeside(layer)?.ref.current]).filter(connected) };
}
let isolated: HTMLElement[] = [];
let restoreIsolation: (() => void) | undefined;
function syncModalIsolation() {
  // Feedback reads above whatever layer is open, not only a modal one
  // (2026-11-05: a legal hold's refusal sat under the trash panel, half hidden).
  if (typeof document !== "undefined") document.body.toggleAttribute("data-layer-open", layers.length > 0);
  const shield = floatingShield();
  const backdrop = syncPageShield(shield);
  const modal = topModalLayer();
  const modalIndex = modal ? layers.indexOf(modal) : -1;
  // A menu opened in a window (a center peek, the settings) shields the window
  // under it too, as Notion's backdrop covers its search window.
  const targets = modalIndex < 0 ? [] : [...layers.slice(modalIndex).flatMap((layer) =>
    [layer.ref.current, ...layer.modalOwned(), layer.kept !== document.body ? layer.kept : null]), shield && modal && shield.zIndex > modal.zIndex ? backdrop : null]
    .filter((node): node is HTMLElement => node instanceof HTMLElement && node.isConnected);
  if (targets.length === isolated.length && targets.every((node, index) => node === isolated[index])) return;
  restoreIsolation?.(); isolated = targets;
  restoreIsolation = targets.length ? isolateBodyForModal(targets) : undefined;
}
// The Tab a layer keeps, read field by field: a DOM event's key and
// defaultPrevented are its prototype's getters, which spreading it drops
// (2026-10-28: no modal kept Tab from a window listener, so it left them).
function tabKey(event: LayerKey) {
  return { key: event.key, shiftKey: Boolean(event.shiftKey), defaultPrevented: event.defaultPrevented, preventDefault: () => event.preventDefault() };
}
// A hover tip is drawn over the others yet takes none of their keys or presses: Escape and a press outside still reach
// the menu or editor under it (2026-11-06: a card value's tip held the first Escape meant for a filter's editor, and
// the next press outside was swallowed).
function topDrawnLayer() { return [...layers].reverse().find(layer => layer.ref.current?.isConnected && !layer.passive()); }
/** The top window or menu's kind (not a hover tip's): what a page key pressed over it may do (⌘⇧O, ⌘/). */ export const topLayerKind = () => topDrawnLayer()?.kind();
function topModalLayer() { return [...layers].reverse().find(layer => layer.modal && layer.ref.current?.isConnected); }
/**
 * A modal layer keeps the focus when what held it inside goes away (a step
 * replaced in place, a removed row): with the focus left on the page, the
 * layer's first control takes it (2026-10-28: it stayed on the page). A
 * control only locked while its request is out comes back by itself, as
 * does the opener of a window drawn inside it that its answer locked or
 * replaced (focusThroughWaits.ts; 2026-10-28: a profile photo picked, the
 * focus went to the settings search instead of the photo button).
 */
/**
 * A layer with nothing to focus yet — its content still coming — takes the
 * focus itself, so the page under it doesn't keep it, and gives it to its
 * first control once one comes (2026-10-28: the first run's outside address
 * window showed "살펴보는 중…" four seconds with the focus on the page).
 */
function focusLayerItself(layer: Layer, root: HTMLElement): () => void {
  if (!root.hasAttribute("tabindex")) root.tabIndex = -1;
  // The window is read by its own title (nameByHeading), so it takes the
  // focus once that is drawn, or after a moment without one (2026-10-28:
  // drawn with the product's mark alone, it was read as "Leafen", its title
  // a tenth of a second behind).
  const nowhere = () => {
    const active = document.activeElement;
    return !active || active === document.body || active === document.documentElement;
  };
  const enter = () => { if (nowhere()) root.focus({ preventScroll: true }); };
  if (root.querySelector("h2")) enter();
  const late = window.setTimeout(enter, 800);
  // A frame later, so a view's own entry (viewEntryFocus.ts) goes first.
  let frame = 0;
  const done = () => { observer.disconnect(); window.clearTimeout(late); };
  const observer = new MutationObserver(() => {
    if (frame) return;
    frame = window.requestAnimationFrame(() => {
      frame = 0;
      if (document.activeElement !== root && !nowhere()) done();
      else if (createFocusManager(layer.ref).focusFirst({ tabbable: true })) done();
      else if (root.querySelector("h2")) enter();
    });
  });
  observer.observe(root, { childList: true, subtree: true });
  return () => {
    done();
    if (frame) window.cancelAnimationFrame(frame);
  };
}
const FIRST_CONTROL = 'input:not([type="hidden"]):not(:disabled), textarea:not(:disabled), select:not(:disabled), button:not(:disabled), a[href], [contenteditable="true"], [tabindex]';
/**
 * On a touch screen a window opens with the keyboard down, as Notion's phone
 * settings and trash do (seen 2026-11-10) and its iPad's new token window
 * (seen 2026-11-12, 834 wide): a field it names for the focus waits for a
 * tap (fieldFocusOnOpen, screenKind.ts), and the window itself takes the
 * focus. A window for typing says so (typesOnPhone), as search.
 */
function phoneFieldWaits(layer: { modal: boolean }, entry: OverlayLayerOptions["initialFocus"], root: HTMLElement | null, types: boolean) {
  if (!layer.modal || types || !root || fieldFocusOnOpen()) return false;
  // A window naming no field enters at its first control, a field as well
  // (2026-11-13: the new token window's name raised the iPad's keyboard).
  const field = entry === "first" || entry === "opener" || entry === undefined || !entry.current
    ? [...root.querySelectorAll<HTMLElement>(FIRST_CONTROL)].find((control) => control.tabIndex >= 0 && control.getClientRects().length > 0)
    : entry.current;
  return raisesKeyboard(field);
}
/**
 * A modal window with no name of its own is named by its heading, as
 * that comes and changes (2026-10-28: the first run's window had none, so
 * the window itself, focused while it loaded, was read as only "dialog").
 */
let layerHeadings = 0;
function nameByHeading(root: HTMLElement) {
  const named = root.hasAttribute("data-layer-named");
  if (root.getAttribute("role") !== "dialog" || root.hasAttribute("aria-label")
    || (root.hasAttribute("aria-labelledby") && !named)) return;
  // A window's own title is its h2; an h1 above it is the product's mark.
  const heading = root.querySelector<HTMLElement>("h2") ?? root.querySelector<HTMLElement>("h1, h3, [role=heading]");
  if (!heading) {
    if (named) { root.removeAttribute("aria-labelledby"); root.removeAttribute("data-layer-named"); }
    return;
  }
  if (!heading.id) heading.id = `layer-heading-${++layerHeadings}`;
  if (root.getAttribute("aria-labelledby") !== heading.id) root.setAttribute("aria-labelledby", heading.id);
  root.setAttribute("data-layer-named", "");
}
function keepModalFocus(layer: Layer, root: HTMLElement) {
  let held: Element | null = null, frame = 0;
  // Where the focused control went out: what took its place there takes the
  // focus, failing that the first control of the nearest group around it still
  // drawn (2026-11-05: a settings save bar that went; 2026-11-06: "에이전트 연결"
  // drawn anew) — but not from a part marked data-focus-leaves, whose removal
  // leaves the keys on the page (Notion's automation conditions, 2026-11-15).
  let gone: { parent: Node; before: Node | null } | null = null;
  let around: Element[] = [];
  const hold = (event: FocusEvent) => {
    held = event.target instanceof Element ? event.target : null;
    gone = null;
    around = [];
    for (let group = held?.parentElement ?? null; group && group !== root; group = group.parentElement) around.push(group);
  };
  nameByHeading(root);
  const observer = new MutationObserver((records) => {
    nameByHeading(root);
    const focused = held;
    if (!focused || focused.isConnected || focused.closest("[data-focus-leaves]")) return;
    if (!gone) {
      const out = records.find((record) => [...record.removedNodes].some((node) => node === focused || node.contains(focused)));
      if (out) gone = { parent: out.target, before: out.previousSibling };
    }
    if (frame) return;
    frame = window.requestAnimationFrame(() => {
      frame = 0;
      const active = document.activeElement;
      if (!held || held.isConnected || !root.isConnected || topModalLayer() !== layer || focusOwed(root)
        || (active && active !== document.body && active !== document.documentElement)) return;
      held = null;
      const place = gone;
      gone = null;
      const stand = place ? standInWhereGone(root, place.parent, place.before) : null;
      const groups = around.filter((group) => group.isConnected && root.contains(group));
      around = [];
      // In its place, else outward from the nearest group still drawn; a touch screen's field waits for a tap.
      rehomeModalFocus(root, stand, groups);
    });
  });
  root.addEventListener("focusin", hold);
  observer.observe(root, { childList: true, subtree: true });
  return () => {
    root.removeEventListener("focusin", hold); observer.disconnect();
    if (frame) window.cancelAnimationFrame(frame);
  };
}
/**
 * Escape on a surface with a popup drawn over it — opened after it — closes the popup, wherever the focus is;
 * answers whether one was. One Escape goes on from (escapeGoesOn, a margin card's discussion) is passed over (escapeLayer).
 */
export function escapeLayerOver(surface: Element | null, event: KeyboardEvent) {
  const layer = escapeLayer(), top = layer?.ref.current;
  if (!layer || !top || !surface || layer.followsText() || !(holdsPlaced(surface, top) || surface.compareDocumentPosition(top) & Node.DOCUMENT_POSITION_FOLLOWING)) return false;
  // The surface took the key from the page; the popup answers it as its own.
  layer.escape({ key: "Escape", defaultPrevented: false, preventDefault: () => {}, target: top, nativeEvent: event });
  return true;
}
function holds(layer: Layer, node: EventTarget | null | undefined) {
  return node instanceof Node && (holdsPlaced(layer.ref.current, node) || layer.inside().some(part => holdsPlaced(part, node)));
}
// The menu a list beside its row stands beside (closesHost; the layer holding its first `inside` part, not a window)
// answers the pointer through the page's shield whole, as Notion's: another row pointed at closes the list, pressed it
// acts (2026-11-18: shielded, "단축키 추가 › 페이지 ›" kept its picker under the pointer on "설정"). A press outside both
// closes it too, and so on down to one holding the press or a window, or as a press on its shade (a view's settings).
function menuBeside(layer: Layer) { const row = layer.closesHost() ? layer.inside()[0] : null, host = row ? layerHolding(row) : null; return host && host !== layer && !host.modal ? host : null; }
let pressedAt = { x: -1, y: -1 };
if (typeof document !== "undefined") document.addEventListener("pointerdown", event => { pressedAt = { x: event.clientX, y: event.clientY }; }, true);
function closeHostsOutside(layer: Layer | null, target: Element | null) {
  for (let at = layer; target && at;) {
    const host = menuBeside(at), box = host?.ref.current?.getBoundingClientRect();
    if (!host || holds(host, target) || box && pressedAt.x >= box.left && pressedAt.x <= box.right && pressedAt.y >= box.top && pressedAt.y <= box.bottom) return;
    const shade = host.outsidePress() ? null : host.modalOwned().find((part): part is HTMLButtonElement => part instanceof HTMLButtonElement);
    if (shade) window.requestAnimationFrame(() => shade.click()); at = host;
  }
}
// Native ownership picks the top layer and lets a child take a key first.
// React Aria owns Escape dispatch and outside pointer lifetime; its public
// keyboard props are forwarded once after the page's existing React handlers.
export function routeOverlayKeyDown(event: LayerKey) {
  const plainTab = event.key === "Tab" && !event.altKey && !event.ctrlKey && !event.metaKey, top = topDrawnLayer();
  // The text selection's toolbar goes as Tab does its work in the text (an indent, the text's own key), as Notion's (2026-11-26, two tabs).
  if (plainTab && !imeComposing(event) && top?.kind() === "toolbar" && !holdsPlaced(top.ref.current, document.activeElement)) top.outsidePress();
  if (event.defaultPrevented || imeComposing(event)) return;
  if (plainTab) {
    const modal = topModalLayer(); if (modal && !layers.slice(layers.indexOf(modal) + 1).some(over => holds(over, document.activeElement))) trapModalTab(tabKey(event), modal.ref.current);
    return;
  }
  if (event.key === "Escape") escapeTaker(event)?.escape(event);
}
/** The window or menu an Escape pressed now closes (routeOverlayKeyDown), null for none: a view's chosen items leave it that one (viewWindowEscape.ts). */ export const escapeClosing = (event: Pick<LayerKey, "target">) => escapeTaker(event)?.ref.current ?? null;
function escapeTaker(event: Pick<LayerKey, "target">) {
  // An Escape that already closed the layer it was pressed in, taking its field away, closes nothing more
  // (2026-10-06: a tag picker's Escape closed the row's side peek under it too).
  if (event.target instanceof Node && !event.target.isConnected) return undefined;
  const layer = escapeLayer();
  const active = document.activeElement;
  // A side peek beside the page takes it from a control of the page as from the page (escapeForPeekBeside, 2026-11-25:
  // a table's row box or its title cell held for the row kept every Escape from the peek).
  const onPage = !active || active === document.body || active === document.documentElement || holdingTyping()
    || escapeForPeekBeside(layer?.ref.current, active, layers.some(under => holds(under, active)));
  // A modal on top owns the keys even before the focus has gone into it: a confirmation with no field opens with the
  // focus still on its opener in the window under it, and its first Escape closed nothing (2026-11-09). So does a menu
  // that leaves the focus on the control it is open from, as a page property's name (seen 2026-11-10: Notion's closes
  // on the first Escape, the focus staying on the name).
  const opensIt = active instanceof Element && active.matches('[aria-haspopup]:not([aria-haspopup="false"])[aria-expanded="true"]');
  // So does a popup drawn over the window that holds the focus, as on a surface (escapeLayerOver): a role menu opened
  // in the share window while the focus stayed on a control of the window kept its Escape, and neither closed
  // (2026-11-14; Notion's Escape closes the menu on top).
  const over = layer && !layer.followsText() && layers.slice(0, layers.indexOf(layer)).some(under => holds(under, active));
  return layer && (onPage || layer.modal || opensIt || over || holds(layer, active) || holds(layer, event.target)) ? layer : undefined;
}
/**
 * A popup that keeps its keys from the page under it: none goes on to the
 * page, yet Escape still closes the top layer, which listens beyond the page
 * (2026-10-08: the page menu's search field kept the menu open on Escape).
 */
export function keepLayerKeys(event: ReactKeyboardEvent<HTMLElement>) {
  event.stopPropagation();
  if (event.key === "Escape") routeOverlayKeyDown(event);
}
function listen() {
  if (listening) return;
  document.addEventListener("keydown", routeCaretKey, true); window.addEventListener("keydown", routeOverlayKeyDown); listening = true;
}
function unlisten() {
  if (layers.length || !listening) return;
  document.removeEventListener("keydown", routeCaretKey, true); window.removeEventListener("keydown", routeOverlayKeyDown); listening = false;
}
// A native editor keeps DOM focus at its caret. Only its top owned picker may forward navigation to Aria before the editor
// consumes the same key: the top layer's, or the one under layers with no picker, no focus and no modal hold — a margin card
// opened for what was typed leaves "/" and "@" their keys, as Notion's (seen 2026-11-18: suggesting, the card open, Enter picked "/"'s row).
function routeCaretKey(event: KeyboardEvent) {
  if (event.defaultPrevented || imeComposing(event)) return;
  for (const layer of [...layers].reverse()) if (layer.ref.current?.isConnected && !layer.passive() && (layer.caretKey(event) || layer.modal || holds(layer, document.activeElement))) return;
}

export type OverlayLayerOptions = KindExceptions & {
  ref: RefObject<HTMLElement | null>; /** Its kind, whose options go under its own (overlayKinds.ts). */ kind?: OverlayKind;
  /** A hover tip: drawn over the others yet taking none of their keys or presses. */
  passive?: boolean;
  /** Drawn for the selection of the text under it: Escape pressed in that text
   * does the text's work, the layer going with the selection, as Notion's
   * selection toolbar (seen 2026-11-06: one Escape chose the line, or went to
   * a caption's start, with the toolbar up). */
  followsText?: boolean;
  /** A press outside closes it and goes on whole (its click too) to what it pressed, as beside Notion's side peek, "/", "+" and ":" menus, text colour and
   * turn lists and a card reading a block's threads (seen 2026-11-09, 11-16, 11-26: the top bar's "공유" or "⋯" opening at that one press). A layer that
   * neither floats nor is modal takes it, else Aria's dismissal cancels the click: the caret moved, the button pressed doing nothing. A control that
   * has it open, pressed, only closes it (openerPress.ts: a block's 💬 mark, Notion's seen 2026-11-06 and 11-27). */ pressGoesOn?: boolean;
  /** With pressGoesOn, a side peek's: the press closes it once its click has
   * done its own work, so nothing moves under the pointer while it is down
   * (the page takes its room back as the peek goes), and not when that click
   * changed the address (it opened something there, or closed it) or the
   * press is one it keeps (`keepsPress`: any not on the page's room, as a
   * row's "열기" turning it to that row) — Notion's peek is still there at the
   * click (seen 2026-11-12). */
  closesAfterClick?: boolean;
  keepsPress?: (target: Element) => boolean;
  /** A popover floating over the page (usePopoverPlacement): while it is
   * open, and a press beside it closes it, the page under it takes no
   * pointer — hover, press or wheel (pageHoverShield.ts). */
  floats?: boolean;
  onEscape?: () => void;
  /** Escape leaves it open, going on to what holds the focus under it (a margin card's discussion). */ escapeGoesOn?: boolean;
  onOutsidePress?: () => void;
  /** A list beside a menu's row (its first `inside` part): a press outside both closes that menu too, as Notion's
   * page "⋯" with "테마와 레이아웃 ›" or "알림 받기 ›" open closes at one press beside it (seen 2026-11-16). */
  closesHost?: boolean;
  onCaretKeyDown?: (event: KeyboardEvent) => void;
  inside?: () => ReadonlyArray<Element | null | undefined>;
  /** What of `inside` still answers the pointer through the page's shield, when not all of it: a link window's
   * text it was opened from counts as its own for the focus, yet takes no wheel or press under it, as Notion's. */
  answersThroughShield?: () => ReadonlyArray<Element | null | undefined>;
  modal?: boolean;
  /** Backdrop/portal parts that share the layer's lifetime: a modal's, or a layer's over a modal, which stay
   * pressable while the page behind is inert (2026-11-12: a layout builder tab's view settings kept open on a press
   * beside them, their shade inert under the builder). */
  modalOwned?: () => ReadonlyArray<Element | null | undefined>;
  restoreFocus?: boolean;
  /** A native flow may replace its menu opener with a dialog. */
  returnFocusRef?: RefObject<HTMLElement | null>;
  /** A native action may move to a new screen instead of returning to its opener. */
  shouldRestoreFocus?: () => boolean;
  /** A native async action can temporarily disable the same return target. */
  canRestoreFocus?: () => boolean;
  /** Where focus enters as the layer opens: a field or button, or the
   * layer's first tabbable element — a modal layer's default; "opener"
   * leaves it where it was. A layer already holding focus keeps it. */
  initialFocus?: RefObject<HTMLElement | null> | "first" | "opener";
  /** On a touch screen a window's field waits for a tap (phoneFieldWaits,
   * below); this one is for typing and takes the focus as it opens, as
   * Notion's phone search does (seen 2026-11-10) — on a phone, opened by a
   * press, a frame after it, so the keyboard stays down until the field is
   * pressed, as Notion's in iPhone Safari (seen 2026-11-14). */
  typesOnPhone?: boolean;
  /** Re-enter focus when the same layer replaces its content. */
  focusKey?: unknown;
  /** Stays above, and in front of, every layer that isn't, opened before or after. */
  above?: boolean;
  /** Shows a page typing may be held for: while it is, the focus stays with what is typed. */
  waitsForHeldTyping?: boolean;
};

/** One native layer boundary: order, owned parts and opener policy. Aria
 * supplies overlay interaction and browser scroll locking; its focus manager
 * supplies the Tab ring through the common modal-focus adapter. */
export function useOverlayLayer(open: boolean, given: OverlayLayerOptions): {
  zIndex: number | undefined;
  onKeyDown: (event: LayerKey) => void;
} {
  const options = withKind(given), [zIndex, setZIndex] = useState<number>(), [visible, setVisible] = useState(false);
  const latest = useRef(options); latest.current = options;
  const current = useRef<Layer | null>(null);
  const pendingReturn = useRef<(() => void) | null>(null);
  const keyboardClose = useRef(false), pressedOutside = useRef<Element | null>(null), popupOpener = usePopupOpener(open);
  // The control the focus was last on inside it, and whether that went while
  // it stayed open, taking the focus to the page (droppedAt, above).
  const held = useRef<Element | null>(null), dropped = useRef(false);
  const modal = options.modal === true, restoreFocus = options.restoreFocus ?? modal;
  const focusLease = useRef<{ open: boolean; restore: boolean; opener: HTMLElement | null; host: Layer | null }>({ open: false, restore: false, opener: null, host: null });
  // Capture the native opener before a descendant Aria item autofocuses in
  // commit effects, including when that item shares this component's hooks,
  // and the layer holding it while that is still open.
  if (open && (!focusLease.current.open || restoreFocus && !focusLease.current.restore)) {
    const focused = typeof document !== "undefined" && document.activeElement instanceof HTMLElement
      && document.activeElement !== document.body ? document.activeElement : null;
    // WebKit leaves the focus off a clicked button: the control just pressed
    // opened it then (lastPress.ts), whether or not its opener is named.
    const active = openerOf(focused);
    const behind = takeHandedOff();
    const named = options.returnFocusRef?.current && openerOf(options.returnFocusRef.current);
    focusLease.current.opener = !restoreFocus ? null : named && !keptFocusWhenPressed(named, focused) ? named : active;
    focusLease.current.host = !restoreFocus ? null : focusLease.current.opener ? layerHolding(focusLease.current.opener) : behind;
  }
  focusLease.current.open = open; focusLease.current.restore = restoreFocus;
  // A tip stays out of Aria's stack too, which answers Escape and outside presses for its top overlay alone, and so does
  // one Escape goes on from (escapeLayer). Aria stops the press it closes on, so one that follows the text takes it below.
  const { overlayProps } = useOverlay({
    isOpen: open && visible && options.passive !== true && options.escapeGoesOn !== true,
    isDismissable: Boolean(options.onOutsidePress) && options.followsText !== true && options.pressGoesOn !== true,
    isKeyboardDismissDisabled: !options.onEscape,
    shouldCloseOnInteractOutside: target => { pressedOutside.current = target; return !target.closest("[data-react-aria-top-layer]")
      && Boolean(latest.current.onOutsidePress && current.current === topDrawnLayer() && !holds(current.current!, target)); },
    onClose: () => {
      if (current.current !== (keyboardClose.current ? escapeLayer() : topDrawnLayer())) return;
      if (keyboardClose.current) latest.current.onEscape?.();
      else { latest.current.onOutsidePress?.(); closeHostsOutside(current.current, pressedOutside.current); pressedOutside.current = null; }
    },
  }, options.ref);
  // A layer drawn for the text's selection (or a side peek, pressGoesOn) goes with a press outside it, and the press
  // goes on to do its own work, as Notion's selection toolbar does (seen 2026-11-08: with words chosen, one click under
  // the page's last block makes a line there with the caret in it; here the click only closed the toolbar, and what was
  // typed next went nowhere).
  const pressGoesOn = open && (options.followsText === true || options.pressGoesOn === true);
  const afterClick = pressGoesOn && options.closesAfterClick === true;
  useEffect(() => {
    if (!pressGoesOn) return undefined;
    // The address an outside press began at, for its click (closesAfterClick).
    let pressedAt: string | null = null, live = true;
    const press = (event: PointerEvent) => {
      const layer = current.current, target = event.target;
      pressedAt = null;
      if (event.button > 0 || !layer || layer !== topDrawnLayer() || !(target instanceof Element)
        || target.closest("[data-react-aria-top-layer]") || holds(layer, target)) return;
      if (!afterClick) { if (!pressOnOpener(popupOpener.current, event)) goOnToWhatWasPressed(event); latest.current.onOutsidePress?.(); return; }
      if (!latest.current.keepsPress?.(target)) pressedAt = window.location.href;
    };
    const click = () => {
      const at = pressedAt, layer = current.current;
      pressedAt = null;
      if (at === null) return;
      window.setTimeout(() => {
        if (live && layer && current.current === layer && layer === topDrawnLayer() && window.location.href === at) latest.current.onOutsidePress?.();
      }, 0);
    };
    const cancel = () => { pressedAt = null; };
    document.addEventListener("pointerdown", press, true);
    if (afterClick) { document.addEventListener("click", click, true); document.addEventListener("pointercancel", cancel, true); }
    return () => {
      live = false;
      document.removeEventListener("pointerdown", press, true);
      document.removeEventListener("click", click, true);
      document.removeEventListener("pointercancel", cancel, true);
    };
  }, [pressGoesOn, afterClick]);
  const ariaKeys = useRef(overlayProps.onKeyDown); ariaKeys.current = overlayProps.onKeyDown;
  usePreventPageScroll({ isDisabled: !(open && visible && modal) });
  const onKeyDown = useCallback((event: LayerKey) => {
    const layer = current.current, root = layer?.ref.current;
    if (event.key === "Tab" && !event.defaultPrevented && layer?.modal && root
      && !layers.slice(layers.indexOf(layer) + 1).some(over => holds(over, document.activeElement))) {
      trapModalTab(tabKey(event), root);
      return;
    }
    if (!root || !fieldEscape(event) || event.defaultPrevented || !latest.current.onEscape || escapeLayer() !== layer) return;
    keyboardClose.current = true;
    // An owned trigger/caret may remain outside the popup DOM. Aria's
    // keyboard hook deliberately ignores portal events; forward this same
    // native owner's event with the top layer as its virtual target (as one
    // from a popover the layer drew apart on the body, popoverLayer.tsx).
    const targetInside = event.target instanceof Node && root.contains(event.target);
    try { ariaKeys.current?.(keyboardPropsEvent(event, root, !targetInside)); }
    finally { keyboardClose.current = false; }
  }, []);
  // A caller may keep an open layer while drawing nothing. Do not let that
  // invisible ref mask a drawn layer in Aria's overlay lifetime. Read after
  // every render, it is set only when it turns.
  useLayoutEffect(() => {
    const drawn = Boolean(open && options.ref.current?.isConnected);
    if (drawn !== visible) setVisible(drawn);
    syncModalIsolation();
  });
  useLayoutEffect(() => { pendingReturn.current?.(); });
  useLayoutEffect(() => {
    if (!open || typeof document === "undefined") { setZIndex(undefined); return; }
    const above = latest.current.above === true;
    const firstAbove = layers.findIndex(candidate => candidate.above);
    const beneath = firstAbove < 0 ? layers : layers.slice(0, firstAbove);
    const opener = restoreFocus ? focusLease.current.opener : null;
    const host = restoreFocus ? focusLease.current.host : null;
    const layer: Layer = {
      zIndex: above ? (layers.at(-1)?.above ? layers.at(-1)!.zIndex : LAYER_ABOVE - LAYER_STEP) + LAYER_STEP
        : sheetLayerTop(beneath.at(-1)?.zIndex ?? LAYER_BASE - LAYER_STEP) + LAYER_STEP,
      above, modal, ref: latest.current.ref, escape: onKeyDown, escapeGoesOn: () => latest.current.escapeGoesOn === true, inside: () => latest.current.inside?.() ?? [],
      answers: () => (latest.current.answersThroughShield ?? latest.current.inside)?.() ?? [],
      caretKey: event => (latest.current.onCaretKeyDown?.(event), Boolean(latest.current.onCaretKeyDown)),
      modalOwned: () => latest.current.modalOwned?.() ?? [],
      opener, host, passive: () => latest.current.passive === true, kind: () => latest.current.kind, followsText: () => latest.current.followsText === true,
      outsidePress: () => (latest.current.onOutsidePress?.(), Boolean(latest.current.onOutsidePress)), closesHost: () => latest.current.closesHost === true,
      shields: () => latest.current.floats === true && Boolean(latest.current.onOutsidePress) && latest.current.passive !== true
        && latest.current.followsText !== true && latest.current.pressGoesOn !== true && latest.current.modal !== true,
    };
    const openerSpot = opener?.isConnected ? spotOf(opener) : null;
    pendingReturn.current = null;
    // A phone's window rises from the screen's foot as a sheet (phoneWindowSheet.ts), judged by its own style before the lines below lay it on the page.
    const drawn = layer.ref.current, unsheet = modal && drawn ? sheetPhoneWindow(drawn, () => latest.current.onEscape?.()) : undefined, unhold = holdUnderKind(layer.ref, latest.current);
    const asked = latest.current.initialFocus, types = latest.current.typesOnPhone === true, waits = phoneFieldWaits(layer, asked, drawn, types);
    // One drawn as a phone sheet, naming no field to type in now, leaves the focus where the press that opened it put it (focusLeftBySheet).
    layer.kept = asked === "opener" || !waits && typeof asked === "object" && raisesKeyboard(asked.current) ? null : focusLeftBySheet(drawn, types);
    if (above || firstAbove < 0) layers.push(layer); else layers.splice(firstAbove, 0, layer); if (!layer.passive()) openings += 1;
    current.current = layer; setZIndex(layer.zIndex); listen(); syncModalIsolation(); tellLayerChange();
    // After the layer is drawn and placed, focus enters it once: a modal
    // layer does unless it says "opener" or keeps it so (2026-10-28: one that set nowhere
    // left the focus on its opener behind it, or on the page meanwhile).
    // A layer over a phone's whole screen is laid on the page, before it is first drawn, so the status bar keeps the soft ground under it.
    if (drawn && !latest.current.passive) layScreenLayerOnPhonePage(drawn);
    const entry = waits || asked === "opener" || layer.kept ? undefined : asked ?? (modal ? "first" : undefined);
    if (waits && !layer.kept && drawn && !holdsPlaced(drawn, document.activeElement)) {
      if (!drawn.hasAttribute("tabindex")) drawn.tabIndex = -1;
      drawn.focus({ preventScroll: true });
    }
    const release = modal && drawn ? keepModalFocus(layer, drawn) : undefined;
    held.current = null; dropped.current = false;
    if (!latest.current.passive) droppedAt = 0;
    const noteHeld = (event: FocusEvent) => {
      if (event.target instanceof Element && holdsPlaced(layer.ref.current, event.target)) { held.current = event.target; dropped.current = false; }
    };
    document.addEventListener("focusin", noteHeld, true);
    // A menu or panel only: a window keeps its own, and a peek or a tip is no menu.
    const watchDrop = new MutationObserver(() => {
      const root = layer.ref.current, active = document.activeElement;
      if (root?.isConnected && held.current && !held.current.isConnected && (!active || active === document.body)) dropped.current = true;
    });
    const { passive, followsText, pressGoesOn: surface } = latest.current;
    if (drawn && !modal && !passive && !followsText && !surface) watchDrop.observe(drawn, { childList: true, subtree: true });
    let waitForControls: (() => void) | undefined;
    // A field the layer names (a search, a name to fill in) takes the focus in this same pass, so keys typed right
    // after opening reach it: a frame later, under load, ⌘K's first letters went to the page (2026-11-05). The frame
    // below still places it if the field wasn't drawn yet, and places a phone's typing field opened by a press: focused
    // outside the press, iPhone Safari leaves its keyboard down (typesOnPhone).
    const typesLater = latest.current.typesOnPhone === true && phoneScreen() && !keyCameLast();
    if (entry && entry !== "first" && !typesLater && entry.current?.isConnected && holdsPlaced(drawn, entry.current)
      && !(latest.current.waitsForHeldTyping && holdingTyping())) {
      entry.current.focus({ preventScroll: true });
    }
    const frame = entry ? window.requestAnimationFrame(() => {
      const root = layer.ref.current;
      if (!root?.isConnected || current.current !== layer || holdsPlaced(root, document.activeElement)) return;
      if (latest.current.waitsForHeldTyping && holdingTyping()) return;
      const enterFirst = () => {
        if (!createFocusManager(layer.ref).focusFirst({ tabbable: true })) waitForControls = focusLayerItself(layer, root);
      };
      if (entry === "first") { enterFirst(); return; }
      entry.current?.focus({ preventScroll: true });
      // A named control that can't take the focus yet (an "설치" off until
      // its box is ticked) leaves it to the layer's first control, or the
      // layer itself, rather than on the page behind (2026-11-08).
      if (modal && !holdsPlaced(root, document.activeElement)) enterFirst();
    }) : 0;
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      waitForControls?.();
      release?.();
      unsheet?.(); unhold?.();
      document.removeEventListener("focusin", noteHeld, true);
      watchDrop.disconnect();
      const leftOnPage = dropped.current;
      dropped.current = false;
      if (leftOnPage) droppedAt = Date.now(); if (latest.current.ringsAfterPress) handRingOn();
      const drawnRoot = layer.ref.current, closing = document.activeElement, focusWasIn = !closing || closing === document.body || holdsPlaced(drawnRoot, closing);
      // A phone sheet a press closed leaves the focus where Notion's does (sheetPressFocus.ts).
      const sheetLeaves = sheetPressLeavesPage(drawnRoot, closing, opener);
      const index = layers.indexOf(layer); if (index >= 0) layers.splice(index, 1);
      if (current.current === layer) current.current = null;
      unlisten(); syncModalIsolation(); tellLayerChange();
      // StrictMode replays a mounted layer's effects, whose cleanup must not steal Aria's first item's focus back:
      // restore only once the native lease has really ended, with no layer in its place.
      queueMicrotask(() => {
        if (current.current) return;
        if (latest.current.shouldRestoreFocus?.() === false) {
          handedOff = { layer, at: Date.now() };
          return;
        }
        if (!opener && !host) return;
        const restore = () => {
          const active = document.activeElement;
          const lost = !active || active === document.body || !active.isConnected || holdsPlaced(drawnRoot, active) || focusWasIn && active === host?.ref.current;
          // An opener its answer replaced where it stood (two-step "끄기" turned "켜기") hands its turn to what stands there now.
          const target = opener?.isConnected ? opener : (opener ? standIn(openerSpot) ?? redrawnControl(opener) : null) ?? openerBehind(host);
          if (current.current || !target || !lost || latest.current.shouldRestoreFocus?.() === false) {
            // One its answer draws again after this closed takes it then.
            if (!current.current && !target && lost) followRedrawn(markedOpener(opener, host));
            pendingReturn.current = null; return;
          }
          if (leftOnPage || sheetLeaves || focusGoesToPage(target)) {
            pendingReturn.current = null;
            if (active instanceof HTMLElement && active !== document.body && active.isConnected) active.blur();
            return;
          }
          if (latest.current.canRestoreFocus?.() === false) return;
          pendingReturn.current = null;
          // An Enter that closed it ends there: its press would go on to the control the focus goes back to and open it
          // again (seen 2026-11-11 from a cell's field), as Notion's doesn't.
          enterBeingPressed()?.preventDefault();
          // An opener its action locked gets the focus once it is free. After a key at once, for the next key; after a
          // press once the closing is painted, if the focus is still nowhere then: a focus change works out styles
          // across a large page, which the press's frame need not wait for (iPhone Safari, 38k elements: 20–40 ms a
          // focus, 2026-11-13); Notion's phone leaves it on the page.
          if (keyCameLast()) { focusWhenFree(target); return; }
          afterNextPaint(() => { // The window it stands in (host), focused as itself as the focus in a menu drawn outside it went, holds none (2026-11-13: a share role picked by a press).
            const now = document.activeElement;
            const kept = now && now !== document.body && now.isConnected && !holdsPlaced(drawnRoot, now) && !(focusWasIn && now === host?.ref.current);
            if (!current.current && !kept) focusWhenFree(target, latest.current.ringsAfterPress || takeHandedRing() || undefined);
          });
        };
        pendingReturn.current = restore; restore();
      });
    };
  }, [modal, open, restoreFocus, onKeyDown, options.focusKey]);
  return { zIndex: open ? zIndex : undefined, onKeyDown };
}
