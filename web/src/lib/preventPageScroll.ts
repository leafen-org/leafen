import { useLayoutEffect } from "react";
import { usePreventScroll } from "react-aria";
import { appleTouchDevice } from "./iosViewport";
import { watchFieldHomeEnd } from "./fieldHomeEnd";

// The page under a modal layer doesn't scroll while it is open. React Aria's
// usePreventScroll does that, but on an iPhone or iPad it lays a style of
// `* { overscroll-behavior: contain }` into the head as the layer opens and
// takes it out as it closes, and each works out the style of the whole page
// again (2026-11-13 in iPhone Safari, a page of 38k elements: 294 ms of
// style and 90 ms of layout as the phone's search opened, some 425 ms as it
// closed). There the same is done here without that style: what scrolls in a
// layer keeps its scrolling to itself by a rule laid down once
// (styles.css). Otherwise as React Aria's preventScrollMobileWebKit
// (react-aria 3.x, Apache License 2.0); elsewhere React Aria's own.

const appleTouch = appleTouchDevice();
// That rule also takes the Mac's Home and End from a layer's fields in
// Chrome: they move the caret again from here, where every layer comes.
watchFieldHomeEnd();
let holding = 0;
let release: (() => void) | null = null;

/** Keeps the page from scrolling while `isDisabled` is false (React Aria's usePreventScroll). */
export function usePreventPageScroll({ isDisabled }: { isDisabled: boolean }) {
  usePreventScroll({ isDisabled: isDisabled || appleTouch });
  useLayoutEffect(() => {
    if (isDisabled || !appleTouch) return undefined;
    if (holding++ === 0) release = holdPage();
    return () => {
      if (--holding > 0) return;
      release?.();
      release = null;
    };
  }, [isDisabled]);
}

// Input types that bring no soft keyboard.
const NO_KEYBOARD = new Set(["checkbox", "radio", "range", "color", "file", "image", "button", "submit", "reset"]);
function opensKeyboard(target: EventTarget | null) {
  return (target instanceof HTMLInputElement && !NO_KEYBOARD.has(target.type)) || target instanceof HTMLTextAreaElement
    || (target instanceof HTMLElement && target.isContentEditable);
}

function scrolls(node: Element | null, overflowing = false) {
  if (!node) return false;
  const style = window.getComputedStyle(node);
  const root = document.scrollingElement || document.documentElement;
  let can = /(auto|scroll)/.test(style.overflow + style.overflowX + style.overflowY);
  if (node === root && style.overflow !== "hidden") can = true;
  if (can && overflowing) can = node.scrollHeight !== node.clientHeight || node.scrollWidth !== node.clientWidth;
  return can;
}

function scrollParent(node: Element, overflowing = false): Element {
  let parent: Element | null = scrolls(node, overflowing) ? node.parentElement : node;
  while (parent && !scrolls(parent, overflowing)) parent = parent.parentElement;
  return parent || document.scrollingElement || document.documentElement;
}

function holdPage(): () => void {
  const root = document.documentElement;
  // Overflow hidden, so lists that scroll a choice into view scroll only what holds it.
  const overflow = root.style.overflow;
  root.style.overflow = "hidden";
  let scrollable: Element | null = null;
  let touchMoves = false;
  const onTouchStart = (event: TouchEvent) => {
    const target = event.target instanceof Element ? event.target : null;
    if (!target) return;
    scrollable = scrolls(target) ? target : scrollParent(target, true);
    touchMoves = false;
    // Text chosen under the finger, a range input and a field's chosen text are dragged.
    const selection = window.getSelection();
    if (selection && !selection.isCollapsed && selection.containsNode(target, true)) touchMoves = true;
    if (event.composedPath().some((node) => node instanceof HTMLInputElement && node.type === "range")) touchMoves = true;
    if ((target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) && target.selectionStart !== null
      && target.selectionEnd !== null && target.selectionStart < target.selectionEnd && document.activeElement === target) touchMoves = true;
  };
  // The page doesn't move, nor does a layer's scroll pass on to it at either end.
  const onTouchMove = (event: TouchEvent) => {
    if (event.touches.length === 2 || touchMoves) return;
    if (!scrollable || scrollable === document.documentElement || scrollable === document.body) {
      event.preventDefault();
      return;
    }
    if (scrollable.scrollHeight === scrollable.clientHeight && scrollable.scrollWidth === scrollable.clientWidth) event.preventDefault();
  };
  // A field the focus moves to is shown without scrolling the page; with the
  // keyboard's Done the focus goes to what holds the field, not the page.
  const onBlur = (event: FocusEvent) => {
    const target = event.target, next = event.relatedTarget;
    if (next instanceof HTMLElement && opensKeyboard(next)) {
      next.focus({ preventScroll: true });
      showWhenReady(next, opensKeyboard(target));
    } else if (!next && target instanceof Element) {
      target.parentElement?.closest<HTMLElement>("[tabindex]")?.focus({ preventScroll: true });
    }
  };
  const focus = HTMLElement.prototype.focus;
  Reflect.defineProperty(HTMLElement.prototype, "focus", {
    configurable: true,
    writable: true,
    value: function (this: HTMLElement, options?: FocusOptions) {
      const keyboardUp = opensKeyboard(document.activeElement);
      focus.call(this, { ...options, preventScroll: true });
      if (!options?.preventScroll) showWhenReady(this, keyboardUp);
    },
  });
  const capture = { passive: false, capture: true };
  document.addEventListener("touchstart", onTouchStart, capture);
  document.addEventListener("touchmove", onTouchMove, capture);
  document.addEventListener("blur", onBlur, true);
  return () => {
    root.style.overflow = overflow;
    document.removeEventListener("touchstart", onTouchStart, capture);
    document.removeEventListener("touchmove", onTouchMove, capture);
    document.removeEventListener("blur", onBlur, true);
    Reflect.defineProperty(HTMLElement.prototype, "focus", { configurable: true, writable: true, value: focus });
  };
}

function showWhenReady(target: HTMLElement, keyboardUp: boolean) {
  const viewport = window.visualViewport;
  // With the keyboard already up the field is shown at once; else once it has come up.
  if (keyboardUp || !viewport) show(target);
  else viewport.addEventListener("resize", () => show(target), { once: true });
}

// Centred in what shows of each scroll that holds it, short of the page itself.
function show(target: HTMLElement) {
  const root = document.scrollingElement || document.documentElement;
  const viewport = window.visualViewport;
  let next: Element | null = target;
  while (next && next !== root) {
    const scrollable = scrollParent(next);
    if (scrollable !== document.documentElement && scrollable !== document.body && scrollable !== next) {
      const box = scrollable.getBoundingClientRect(), rect = next.getBoundingClientRect();
      if (rect.top < box.top || rect.bottom > box.top + next.clientHeight) {
        const bottom = viewport ? Math.min(box.bottom, viewport.offsetTop + viewport.height) : box.bottom;
        const by = rect.top - box.top - ((bottom - box.top) / 2 - rect.height / 2);
        scrollable.scrollTo({ top: Math.max(0, Math.min(scrollable.scrollHeight - scrollable.clientHeight, scrollable.scrollTop + by)), behavior: "smooth" });
      }
    }
    next = scrollable.parentElement;
  }
}
