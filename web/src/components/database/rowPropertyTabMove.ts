import { useEffect, useRef, type RefObject } from "react";
import { isTextField } from "@/lib/menuKeyboard";
import { imeComposing } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

// Tab and Shift+Tab in a database page's property value being written — its
// own field, or the search of the picker it opened — close that edit and open
// the next or the previous property's, from the last round to the first, as
// Notion's side peek does (seen 2026-11-15 at 1440 in the free workspace, a
// database made by hand with 날짜, 선택 and 텍스트, judged by the letter typed
// after the key: Tab from 선택's search opened 텍스트's field and the letter
// landed there; Tab from 텍스트, the last, opened 날짜's picker; Shift+Tab from
// 텍스트 opened 선택's search). A date's picker keeps Tab among its own
// controls, as Notion's (its "오늘" ringed), and so does any value whose edit
// is no text field.
const KEEPS_TAB = new Set(["date", "created_time", "last_edited_time"]);

export function useRowPropertyTabMove(listRef: RefObject<HTMLElement | null>, typeOf: (propertyId: string) => string | undefined) {
  const type = useRef(typeOf);
  type.current = typeOf;
  useEffect(() => {
    // The property whose value was last pressed or focused in this list: a
    // picker it opens lies outside the list, in a layer of its own.
    let editing: string | null = null;
    const lineOf = (element: Element | null) => element?.closest<HTMLElement>("[data-row-property-id]") ?? null;
    const note = (event: Event) => {
      const list = listRef.current, target = event.target;
      if (!(target instanceof Element)) return;
      if (!list?.contains(target)) {
        // A press outside the list and outside any layer over it ends the edit there.
        if (event.type === "pointerdown" && !target.closest("[role=dialog], [data-native-menu]")) editing = null;
        return;
      }
      const value = target.closest("[data-row-property-value]");
      editing = value && !target.closest("[data-cell-value-tools]") ? lineOf(value)?.dataset.rowPropertyId ?? null : null;
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Tab" || imeComposing(event) || event.altKey || event.ctrlKey || event.metaKey || event.defaultPrevented) return;
      const list = listRef.current, active = document.activeElement;
      if (!list || !(active instanceof HTMLElement) || !(isTextField(active) || active.isContentEditable)) return;
      const inList = list.contains(active);
      // Off the list the edit goes on only in a picker it opened, a layer outside the row's page: the page's own title
      // or body keeps its Tab (2026-11-26: a value written, then the title pressed in a side peek, its Tab closed the peek).
      if (!inList && list.closest("[data-row-page]")?.contains(active)) return;
      const id = inList ? lineOf(active.closest("[data-row-property-value]"))?.dataset.rowPropertyId : editing;
      if (!id || KEEPS_TAB.has(type.current(id) ?? "")) return;
      const lines = [...list.querySelectorAll<HTMLElement>("[data-row-property-id]")].filter((line) => opener(line));
      const at = lines.findIndex((line) => line.dataset.rowPropertyId === id);
      if (at < 0 || lines.length < 2) return;
      event.preventDefault();
      event.stopPropagation();
      const next = lines[(at + (event.shiftKey ? lines.length - 1 : 1)) % lines.length];
      // The edit closes as Escape or leaving the field closes it, its value kept.
      if (inList) active.blur();
      else active.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", code: "Escape", bubbles: true, cancelable: true }));
      editing = next.dataset.rowPropertyId ?? null;
      // The next field takes the caret at its end a frame on, what is typed
      // after the key going in (focusOpenedField); a picker's button opens it.
      focusOpenedField(() => {
        const target = opener(next);
        if (!target || target.isContentEditable || isTextField(target)) return target;
        target.click();
        return null;
      }, { scroll: false });
    };
    document.addEventListener("pointerdown", note, true);
    document.addEventListener("focusin", note, true);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("pointerdown", note, true);
      document.removeEventListener("focusin", note, true);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [listRef]);
}

// What opens a line's value to write it: its field, or the button that opens its picker; a value the person can't
// change has none.
function opener(line: HTMLElement): HTMLElement | null {
  const value = line.querySelector<HTMLElement>("[data-row-property-value]");
  if (!value) return null;
  if (value.getAttribute("role") === "button") return value;
  const found = [...value.querySelectorAll<HTMLElement>("[contenteditable='true'], input, textarea, button, [role=button]")]
    .find((element) => !element.closest("[data-cell-value-tools]") && !(element as HTMLButtonElement).disabled);
  return found ?? null;
}
