import { useRef } from "react";

type Cleanup = () => void;

/**
 * One stable ref callback per key, so a registration made on an element (a
 * drag source, a drop target) lives as long as that element rather than
 * being torn down and redone on every render. `register` runs once when the
 * element attaches; its cleanup runs when the element goes. It should read
 * changing state through a ref.
 */
export function useKeyedRefs<K>(register: (key: K, element: HTMLElement) => Cleanup | void) {
  const latest = useRef(register);
  latest.current = register;
  const refs = useRef(new Map<K, (element: HTMLElement | null) => Cleanup | undefined>());
  return (key: K) => {
    let ref = refs.current.get(key);
    if (!ref) {
      ref = (element: HTMLElement | null) => element ? latest.current(key, element) ?? undefined : undefined;
      refs.current.set(key, ref);
    }
    return ref;
  };
}
