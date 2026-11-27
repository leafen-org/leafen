import { type ReactNode, useRef } from "react";
import { createFocusManager } from "@react-aria/focus";
import { Radio, RadioGroup } from "react-aria-components";
import { imeComposing } from "./fieldKeyBoundary";

/** Aria owns radio press, roving focus and arrow selection. Native enums
 * and writes remain controlled by the caller. Home/End retain the native
 * boundary shortcut through the public focus manager, without DOM indexes. */
export function NativeRadioChoices<T extends string>({ options, value, onChange, label, children,
  disabled = false, className, itemClassName, marker }: {
  options: readonly T[]; value: string; onChange(value: T): void; label: string;
  children(value: T): ReactNode; disabled?: boolean; className?: string; itemClassName?: string;
  marker?(value: T): Record<string, string>;
}) {
  const ref = useRef<HTMLDivElement>(null);
  return <div ref={ref} style={{ display: "contents" }} onKeyDownCapture={event => {
    if (disabled || event.defaultPrevented || imeComposing(event) || !["Home", "End"].includes(event.key)) return;
    event.preventDefault(); event.stopPropagation();
    const manager = createFocusManager(ref, { accept: node => node instanceof HTMLInputElement && node.type === "radio" });
    const target = event.key === "Home" ? manager.focusFirst() : manager.focusLast();
    if (target instanceof HTMLInputElement && options.includes(target.value as T)) onChange(target.value as T);
  }}>
    <RadioGroup value={value} onChange={next => { if (options.includes(next as T)) onChange(next as T); }}
      aria-label={label} orientation="horizontal" isDisabled={disabled} className={className}>
      {options.map(option => <Radio key={option} value={option} className={itemClassName}
        data-segmented-option data-active={value === option ? "true" : undefined} {...marker?.(option)}>
        {children(option)}
      </Radio>)}
    </RadioGroup>
  </div>;
}
