import { useLayoutEffect, useRef, type FocusEvent } from "react";

/**
 * A name field that opens holding a value Leafen suggested — a friendly name,
 * the device's user name, the name an inviter wrote — keeps that value chosen
 * while it has focus, so typing replaces it and Enter keeps it, as a rename
 * field opens with the name chosen. Once the person types, the field is theirs.
 */
export function useChosenSuggestion(value: string) {
  const ref = useRef<HTMLInputElement>(null);
  const typed = useRef(false);
  // A suggestion that arrives after the field took focus (an invitation's
  // name) is chosen too.
  useLayoutEffect(() => {
    const field = ref.current;
    if (field && !typed.current && document.activeElement === field) field.setSelectionRange(0, field.value.length);
  }, [value]);
  return {
    ref,
    onFocus: (event: FocusEvent<HTMLInputElement>) => {
      if (!typed.current) event.currentTarget.setSelectionRange(0, event.currentTarget.value.length);
    },
    typed: () => { typed.current = true; },
  };
}
