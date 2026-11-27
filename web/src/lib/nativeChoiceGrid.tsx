import { type ReactNode, type RefObject } from "react";
import { useNativeList } from "./nativeList";
import { NativePickerOption } from "./nativeAutocomplete";

/** Native checked values are independent of the engine's transient focus.
 * Moving around a palette never writes a document or a person preference. */
export function NativeChoiceGrid<T>({ items, keyOf, textOf, onAction, checkedKey, disabledKeys,
  listRef, label, className, itemClassName, children, open = true, autoFocus = true, marker }: {
  items: readonly T[]; keyOf(item: T): string; textOf(item: T): string; onAction(item: T): void;
  checkedKey?: string; disabledKeys?: string[]; listRef: RefObject<HTMLDivElement | null>;
  label: string; className?: string; itemClassName?: string; open?: boolean;
  autoFocus?: boolean;
  children(item: T): ReactNode; marker?: (item: T) => Record<string, string>;
}) {
  const list = useNativeList({ items, keyOf, textOf, onAction, listRef, label, open,
    firstKey: checkedKey, disabledKeys, layout: "grid", autoFocus });
  return <div {...list.listBoxProps} ref={listRef} className={className}>
    {items.map(item => <NativePickerOption key={keyOf(item)} itemKey={keyOf(item)} state={list.state}
      className={itemClassName} aria-label={textOf(item)} aria-selected={checkedKey === keyOf(item)}
      {...marker?.(item)}>{() => children(item)}</NativePickerOption>)}
  </div>;
}
