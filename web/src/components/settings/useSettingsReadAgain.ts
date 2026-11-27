import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { spotControls } from "@/lib/focusThroughWaits";
import { isTextField } from "@/lib/menuKeyboard";
import { layerOpenOver, onLayersChanged } from "@/lib/overlayLayer";
import { useOptionalProductToast } from "@/lib/productToast";
import { onServerAnswersAgain } from "@/lib/serverAnswers";
import { onSettingsNoAnswer, type SettingsNoAnswer } from "@/lib/settingsErrors";
import { onUncertainWritesSettled } from "@/lib/uncertainWrites";
import { settingsChangesUnsaved } from "./settingsUnsaved";
import type { WorkspaceSettingsAccess } from "./WorkspaceSettingsPage";

/**
 * The settings window's section read again once the server answers after a
 * request it didn't answer, and what the window says meanwhile; returns the
 * count the section is drawn anew on.
 */
export function useSettingsReadAgain(
  panelRef: RefObject<HTMLDivElement | null>,
  workspaceAccess: WorkspaceSettingsAccess | null,
  workspaceRefresh: () => Promise<void>,
) {
  // A change sent without the server's answer may have been taken after all:
  // once the server answers again, the section reads what it holds. Only the
  // section is drawn again, and the person stays where they were in it — at
  // its scroll, on the control they had — while it reads (2026-11-05: the
  // whole window was drawn anew, the cursor went to the search and the
  // section to its top).
  const [readAgain, setReadAgain] = useState(0);
  const heldPlace = useRef<SettingsPlace | null>(null);
  const accessRef = useRef(workspaceAccess);
  accessRef.current = workspaceAccess;
  const workspaceRefreshRef = useRef(workspaceRefresh);
  workspaceRefreshRef.current = workspaceRefresh;
  const toast = useOptionalProductToast();
  const toastRef = useRef(toast);
  toastRef.current = toast;
  // Changes the person hasn't saved are not read over: a form with its own
  // save keeps them, and a field being typed in is read again once left. A
  // window, menu or picker opened from the section stays too, and the section
  // reads once it closes (2026-11-06: the export window went with the
  // section as the server came back, its export left to finish unseen).
  useEffect(() => {
    let waiting: (() => void) | undefined;
    const waitForLayers = () => {
      waiting?.();
      let frame = 0;
      const stopListening = onLayersChanged(() => {
        const now = panelRef.current;
        if (frame || (now && layerOpenOver(now))) return;
        // What closed hands the focus back first, as late as its next frame,
        // so the section drawn again keeps it on the control it went to.
        frame = window.requestAnimationFrame(() => { frame = window.requestAnimationFrame(read); });
      });
      waiting = () => { stopListening(); window.cancelAnimationFrame(frame); };
    };
    const read = () => {
      waiting?.();
      waiting = undefined;
      const panel = panelRef.current;
      if (settingsChangesUnsaved()) return;
      if (panel && layerOpenOver(panel)) { waitForLayers(); return; }
      const active = document.activeElement;
      if (panel && active instanceof HTMLElement && panel.contains(active) && (isTextField(active) || active.isContentEditable)) {
        const left = () => read();
        active.addEventListener("focusout", left, { once: true });
        waiting = () => active.removeEventListener("focusout", left);
        return;
      }
      // What the window reads for its sections (an organization's policies)
      // is read again with them: the sections start from it (2026-11-06: a
      // switch's change landed as the network came back, and the section
      // drawn again showed the value read before).
      accessRef.current?.enterprise.refresh();
      accessRef.current?.organizationSettings.refresh();
      // The workspace the sections start from (its name, its icon) comes in
      // first; a section drawn before it would show the name read before.
      void workspaceRefreshRef.current().catch(() => undefined).finally(() => {
        // Not over typing begun meanwhile.
        const now = panelRef.current;
        const typing = document.activeElement instanceof HTMLElement && now?.contains(document.activeElement)
          && (isTextField(document.activeElement) || document.activeElement.isContentEditable);
        if (!now || typing || settingsChangesUnsaved()) return;
        if (layerOpenOver(now)) { waitForLayers(); return; }
        heldPlace.current = placeIn(now);
        setReadAgain((count) => count + 1);
      });
    };
    // A change the server didn't answer is said once, here, in a notice that
    // pushes no row and stays until the server answers and the section is
    // read again; a read's is said in its section, read again as the server
    // answers (settingsNoAnswerText).
    let said: SettingsNoAnswer | null = null;
    const unsay = () => {
      if (said?.write) toastRef.current?.dismissMessage(said.text);
      said = null;
    };
    const stopSaying = onSettingsNoAnswer((next) => {
      if (said?.text === next.text || (said?.write && !next.write)) return;
      said = next;
      if (next.write) toastRef.current?.notify(next.text, "default", undefined, undefined, undefined, undefined, true);
    });
    const stopSettled = onUncertainWritesSettled(() => { unsay(); read(); });
    const stopAnswers = onServerAnswersAgain(() => {
      if (!said || said.write) return;
      said = null;
      read();
    });
    return () => { stopSaying(); stopSettled(); stopAnswers(); unsay(); waiting?.(); };
  }, []);
  useLayoutEffect(() => {
    const place = heldPlace.current;
    heldPlace.current = null;
    return place && panelRef.current ? keepPlace(panelRef.current, place) : undefined;
  }, [readAgain]);
  return readAgain;
}

/** Where the focus was: the control itself, and the first control of the row
 * it sat in, each as what the control is rather than its place in the order. */
type FocusSpot = { key: string; nth: number };
type SettingsPlace = { scroll: number; focus: FocusSpot | null; row: FocusSpot | null; tabs: string[] };

function controlKey(control: HTMLElement) {
  const data = [...control.attributes].filter(({ name }) => name.startsWith("data-") && name !== "data-focus-visible")
    .map(({ name, value }) => `${name}=${value}`).join(",");
  const words = control instanceof HTMLInputElement || control instanceof HTMLTextAreaElement || control instanceof HTMLSelectElement
    ? control.getAttribute("type") ?? "" : (control.textContent ?? "").trim().slice(0, 60);
  return [control.tagName, control.id, control.getAttribute("name") ?? "", control.getAttribute("aria-label") ?? "", data, words].join("|");
}

function spotIn(controls: HTMLElement[], control: HTMLElement): FocusSpot {
  const key = controlKey(control);
  return { key, nth: controls.filter((candidate) => controlKey(candidate) === key).indexOf(control) };
}

function findSpot(controls: HTMLElement[], spot: FocusSpot | null) {
  return spot ? controls.filter((candidate) => controlKey(candidate) === spot.key)[Math.max(0, spot.nth)] : undefined;
}

/** The first control of the row a control sits in: the nearest box around it
 * holding more than one control (a name field and its "저장하기", say). */
function rowFirst(panel: HTMLElement, control: HTMLElement) {
  for (let box = control.parentElement; box && box !== panel; box = box.parentElement) {
    const inside = spotControls(box);
    if (inside.length > 1) return inside[0];
  }
  return null;
}

function placeIn(panel: HTMLElement): SettingsPlace {
  const active = document.activeElement;
  const controls = spotControls(panel);
  const mine = active instanceof HTMLElement && panel.contains(active) && controls.includes(active) ? active : null;
  const first = mine ? rowFirst(panel, mine) : null;
  const focus = mine ? spotIn(controls, mine) : null;
  const row = first ? spotIn(controls, first) : null;
  // The tab a section was on (사람's 그룹, say) is part of where the person
  // was (2026-11-06: drawn again, it went back to 멤버).
  const tabs = [...panel.querySelectorAll<HTMLElement>('[role="tab"][aria-selected="true"]')].map((tab) => tab.textContent ?? "");
  return { scroll: panel.scrollTop, focus, row, tabs };
}

/** Keeps the scroll and the focused control a section had while it is drawn
 * again and reads, until it settles or the person scrolls, presses or types. */
function keepPlace(panel: HTMLElement, place: SettingsPlace) {
  // The window's own focus keeper hands a focus whose control went to the
  // next one there, and on to "닫기" (2026-11-06): until the person acts,
  // the control they had takes it back wherever in the window it went.
  const window_ = panel.closest<HTMLElement>("[role=dialog]") ?? panel;
  const tabsLeft = [...place.tabs];
  const keep = () => {
    for (const label of [...tabsLeft]) {
      const tab = [...panel.querySelectorAll<HTMLElement>('[role="tab"]')].find((candidate) => candidate.textContent === label);
      if (!tab) continue;
      tabsLeft.splice(tabsLeft.indexOf(label), 1);
      if (tab.getAttribute("aria-selected") !== "true") tab.click();
    }
    if (panel.scrollHeight - panel.clientHeight >= place.scroll) panel.scrollTop = place.scroll;
    if (!place.focus) return;
    const active = document.activeElement;
    const nowhere = !active || active === document.body || active === document.documentElement;
    // The same control, or where it went (a "저장하기" shown only while the
    // name is unsaved) its row's first, never the control at its old place in
    // the order (2026-11-07: the focus went on to the next row's field).
    const controls = spotControls(panel);
    const control = findSpot(controls, place.focus) ?? findSpot(controls, place.row);
    if (control && control !== active && (nowhere || window_.contains(active))) control.focus({ preventScroll: true });
  };
  const observer = new MutationObserver(keep);
  const stop = () => observer.disconnect();
  keep();
  observer.observe(panel, { childList: true, subtree: true, attributes: true, attributeFilter: ["disabled", "aria-busy"] });
  const timer = window.setTimeout(stop, 3000);
  const events = ["wheel", "touchstart", "keydown", "pointerdown"] as const;
  for (const name of events) window_.addEventListener(name, stop, { passive: true });
  return () => {
    stop();
    window.clearTimeout(timer);
    for (const name of events) window_.removeEventListener(name, stop);
  };
}
