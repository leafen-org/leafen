import { type HTMLAttributes, type ReactNode, type RefObject } from "react";
import type { ListState } from "react-stately";

// A list whose focus stays elsewhere (the editor's caret, a picker's search
// field) draws its rows as `PickerRow`s: what it tells them, by its list
// state, is its id and its collection's (Aria's, which name and find a row)
// and how a row is picked.
const lists = new WeakMap<object, { id?: string; collection?: string; pick(key: string): void }>();

/** Lets a list's rows draw as `PickerRow`s; called as the list renders. */
export function drawPickerRows(state: object, listBoxProps: HTMLAttributes<HTMLElement>, pick: (key: string) => void) {
  lists.set(state, { id: listBoxProps.id, collection: (listBoxProps as { "data-collection"?: string })["data-collection"], pick });
}

/** Whether a list draws its rows as `PickerRow`s. */
export const drawsPickerRows = (state: object) => lists.has(state);

/** A row's id, as Aria's listbox names an option (`aria-activedescendant`). */
export const pickerRowId = (list: string | undefined, key: string) => `${list}-option-${key.replace(/\s*/g, "")}`;

/** A row's parts, each spread on what draws it: its label, the place under it
 * (a page's, PlaceLine) and its description (`rowNameProps`). */
export type PickerRowParts = {
  active: boolean; labelProps: HTMLAttributes<HTMLElement>; placeProps: HTMLAttributes<HTMLElement>; descriptionProps: HTMLAttributes<HTMLElement>;
};

/**
 * A row's name, the one rule every row of a menu or picker follows
 * (`PickerRow`, Aria's option in `NativePickerOption`, `NativeMenuItem`):
 * the words it shows, as Notion's rows are named by what they show (seen
 * 2026-11-18 in Chrome's tree, the move list's and the "@" menu's rows):
 * its label, then the place under it where it shows one ("Nested Beta
 * 한지테스트페이지 / ... / Destination Alpha"); its description describes
 * it. A row that draws no label (an icon's cell, a swatch) is named by its
 * own `aria-label`; one that draws it keeps none, as a second name would
 * sit under the label's unread.
 */
export function rowNameProps(parts: { label?: string; place?: string; description?: string }, ariaLabel?: string) {
  const { label, place, description } = parts;
  return label ? { "aria-label": undefined, "aria-labelledby": place ? `${label} ${place}` : label, "aria-describedby": description }
    : { "aria-label": ariaLabel, "aria-labelledby": undefined, "aria-describedby": description };
}

/**
 * A row of such a list, as Aria's option and holding no state of its own, so
 * a menu opens without an update for each row: marked while the list's
 * highlight is on it, named and described by the parts it draws
 * (`rowNameProps`), and pressed as Aria's option with the focus left
 * where it is (a press marks it, a click picks it). Its list's pointer moves
 * highlight it (`pointerHover`).
 */
export function PickerRow<T>({ state, itemKey, element = "button", rowRef, children, ...attributes }: {
  state: ListState<T>; itemKey: string; element?: "button" | "div"; rowRef?: RefObject<HTMLElement | null>;
  children(parts: PickerRowParts): ReactNode;
} & Omit<HTMLAttributes<HTMLElement>, "children"> & { disabled?: boolean; [data: `data-${string}`]: unknown }) {
  const list = lists.get(state), id = pickerRowId(list?.id, itemKey), manager = state.selectionManager;
  const active = manager.isFocused && manager.focusedKey === itemKey, disabled = manager.isDisabled(itemKey);
  let labelled = false, placed = false, described = false;
  const labelProps = { get id() { labelled = true; return `${id}-label`; } };
  const placeProps = { get id() { placed = true; return `${id}-place`; } };
  const descriptionProps = { get id() { described = true; return `${id}-description`; } };
  const content = children({ active, labelProps, placeProps, descriptionProps });
  const mark = () => { manager.setFocused(true); manager.setFocusedKey(itemKey); };
  const props = {
    role: "option", id, "aria-disabled": disabled || undefined,
    "aria-selected": manager.selectionMode !== "none" ? manager.isSelected(itemKey) : undefined,
    "data-key": itemKey, "data-collection": list?.collection,
    onMouseDown: (event: { preventDefault(): void }) => event.preventDefault(),
    onPointerDown: (event: { pointerType: string; button: number }) => {
      if (!disabled && event.pointerType !== "touch" && event.button === 0) mark();
    },
    onClick: () => { if (!disabled) { mark(); list?.pick(itemKey); } },
    ...attributes, "data-active": active ? "true" : undefined,
    ...rowNameProps({ label: labelled ? `${id}-label` : undefined, place: placed ? `${id}-place` : undefined,
      description: described ? `${id}-description` : undefined }, attributes["aria-label"]),
  };
  if (element === "div") return <div {...props} ref={rowRef as RefObject<HTMLDivElement | null>}>{content}</div>;
  return <button {...props} ref={rowRef as RefObject<HTMLButtonElement | null>} type="button">{content}</button>;
}
