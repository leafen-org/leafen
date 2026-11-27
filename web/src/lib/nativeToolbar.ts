import { type FocusEvent, type KeyboardEvent, type RefObject, useEffect, useRef } from "react";
import { useToolbar } from "react-aria";

// Whether the latest key pressed was Tab, for focus coming into a toolbar.
let tabbing = false;
let listeners = 0;
function track(event: Event) {
  tabbing = event instanceof KeyboardEvent && event.key === "Tab";
}

/**
 * react-aria's toolbar (arrow keys between its controls, Tab out of it), with
 * focus that comes back in by Tab going to the control Tab left, as its own;
 * focus a press or the code puts on a control stays there (2026-11-06: in a
 * database's toolbar, pressing "정렬" after "필터", or closing 정렬's list with
 * Escape, put the focus on "필터"; Notion's stays on 정렬).
 */
export function useNativeToolbar(props: Parameters<typeof useToolbar>[0], ref: RefObject<HTMLElement | null>) {
  const { toolbarProps } = useToolbar(props, ref);
  const lastLeft = useRef<HTMLElement | null>(null);
  useEffect(() => {
    if (listeners++ === 0) {
      window.addEventListener("keydown", track, true);
      window.addEventListener("pointerdown", track, true);
    }
    return () => {
      if (--listeners > 0) return;
      window.removeEventListener("keydown", track, true);
      window.removeEventListener("pointerdown", track, true);
    };
  }, []);
  const { onKeyDownCapture, onFocusCapture, onBlurCapture: _keptByFocus, ...rest } = toolbarProps;
  // One inside another toolbar is a group of it, handling no keys of its own.
  if (!onFocusCapture) return { toolbarProps };
  return {
    toolbarProps: {
      ...rest,
      onKeyDownCapture: (event: KeyboardEvent<HTMLElement>) => {
        const active = document.activeElement;
        if (event.key === "Tab" && active instanceof HTMLElement && ref.current?.contains(active)) lastLeft.current = active;
        onKeyDownCapture?.(event);
      },
      onFocusCapture: (event: FocusEvent<HTMLElement>) => {
        if (event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget)) return;
        const back = lastLeft.current;
        lastLeft.current = null;
        if (tabbing && back && back !== event.target && back.isConnected && ref.current?.contains(back)) back.focus();
      },
    },
  };
}
