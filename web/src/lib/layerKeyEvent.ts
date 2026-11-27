import type { KeyboardEvent as ReactKeyboardEvent } from "react";

/** A key a layer answers: a DOM keyboard event or a React one, read field by field. */
export type LayerKey = {
  key: string; defaultPrevented: boolean; preventDefault(): void;
  target?: EventTarget | null; stopPropagation?: () => void;
  nativeEvent?: KeyboardEvent; isComposing?: boolean;
  shiftKey?: boolean; altKey?: boolean; ctrlKey?: boolean; metaKey?: boolean;
};

// A DOM keyboard event needs the same public prop shape as a React event.
// This forwards the original event, never dispatches a second input event.
export function keyboardPropsEvent(event: LayerKey, root: HTMLElement, virtualTarget = false): ReactKeyboardEvent<HTMLElement> {
  const native = event.nativeEvent ?? event as KeyboardEvent;
  const onPage = !event.target || event.target === document.body || event.target === document.documentElement;
  return {
    key: event.key, code: native.code, altKey: native.altKey, ctrlKey: native.ctrlKey, metaKey: native.metaKey,
    shiftKey: native.shiftKey, repeat: native.repeat, nativeEvent: native, currentTarget: root,
    target: onPage || virtualTarget ? root : event.target, defaultPrevented: event.defaultPrevented,
    type: "keydown", timeStamp: native.timeStamp, bubbles: native.bubbles, cancelable: native.cancelable,
    isTrusted: native.isTrusted, eventPhase: native.eventPhase,
    preventDefault: () => event.preventDefault(), stopPropagation: () => event.stopPropagation?.(),
    isDefaultPrevented: () => event.defaultPrevented, isPropagationStopped: () => false,
    getModifierState: (key: string) => native.getModifierState?.(key) ?? false, persist() {},
  } as ReactKeyboardEvent<HTMLElement>;
}
