import { type ButtonHTMLAttributes, type ReactNode, type RefObject, useRef } from "react";
import { mergeProps, useTab, useTabList } from "react-aria";
import { Item, type TabListState, useTabListState } from "react-stately";

type TabItem<K extends string> = { key: K; text: string };

/**
 * A row (or column) of tabs, as React Aria keeps them: the arrows, Home and
 * End move between tabs and choose the one moved to, only the chosen tab is
 * in the Tab order, and the list and its tabs carry their roles. What the
 * tabs show, and what choosing one does, stay the caller's.
 */
export function useNativeTabs<K extends string>({ tabs, selected, onSelect, label, listRef, orientation = "horizontal" }: {
  tabs: readonly TabItem<K>[];
  selected: K;
  onSelect(key: K): void;
  label: string;
  listRef: RefObject<HTMLElement | null>;
  orientation?: "horizontal" | "vertical";
}) {
  const state = useTabListState<TabItem<K>>({
    items: tabs, selectedKey: selected,
    onSelectionChange: (key) => { if (key !== null && String(key) !== selected) onSelect(String(key) as K); },
    children: (tab) => <Item key={tab.key} textValue={tab.text}>{tab.text}</Item>,
  });
  const { tabListProps } = useTabList({ "aria-label": label, orientation, keyboardActivation: "automatic" }, state, listRef);
  return { state, tabListProps };
}

/** One tab of a `useNativeTabs` list; its own props (an id, the panel it
 * controls, data attributes) are kept beside React Aria's. */
export function NativeTab<K extends string>({ state, tabKey, children, ...props }: {
  state: TabListState<TabItem<K>>;
  tabKey: K;
  children: ReactNode;
} & Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children" | "type" | "role">) {
  const ref = useRef<HTMLButtonElement>(null);
  const { tabProps } = useTab({ key: tabKey }, state, ref);
  return <button {...mergeProps(tabProps, props)} ref={ref} type="button">{children}</button>;
}
