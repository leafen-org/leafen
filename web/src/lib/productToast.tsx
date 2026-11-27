"use client";

import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import i18next from "i18next";
import { ToastStack } from "@/components/ToastStack";
import { newId } from "./ids";
import { refusedForNowLately } from "./serverBusy";
import type { LiveAnnouncement, ToastAction, ToastMessage } from "./toastStackView";

type ProductToastTone = NonNullable<ToastMessage["tone"]>;

// Every toast stays five seconds unless what says it sets its own time, with
// a button or without, an error or not, as Notion's snackbar (its web client
// read 2026-11-16: `durationMs ?? 5e3` for the one shown, 360 of 371 callers
// leaving it; seen the same day, "링크가 복사되었지만…" with its button gone
// after 5.27 s, and a failure snackbar after 5.27 s).
const TOAST_LIFETIME_MS = 5_000;

type ProductToastContextValue = {
  dismissToast: (id: string) => void;
  /** Several actions show in order; the last one sets how long it stays. One
   * kept stays until it is dismissed by what said it (a state that lasts). */
  notify: (message: string, tone?: ProductToastTone, action?: ToastAction | readonly ToastAction[], icon?: ToastMessage["icon"], detail?: string, person?: ToastMessage["person"], kept?: boolean) => void;
  /** Closes the toast showing this message, if one does (a message shows once: notify replaces it). */
  dismissMessage: (message: string) => void;
  /** Takes away the failures still showing, as when the sign-in they came from ends. */
  dismissErrors: () => void;
  /** Says a message to a screen reader only, nothing drawn, as Notion's
   * "1개 블록 삭제됨" when blocks go (seen 2026-11-08). */
  announce: (message: string) => void;
};

const ProductToastContext = createContext<ProductToastContextValue | null>(null);
// The stack the app has mounted, for what is drawn outside the app's tree
// (the Mac app's right-press menu, components/desktop/DesktopTextMenu.tsx).
let mountedStack: ProductToastContextValue | null = null;

export function ProductToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);
  const [announcement, setAnnouncement] = useState<LiveAnnouncement | undefined>();
  const announce = useCallback((message: string) => setAnnouncement({ id: newId(), message }), []);
  const dismissalTimers = useRef(new Map<string, ReturnType<typeof setTimeout>>());
  // The timed toasts, each with its time on screen.
  const timed = useRef(new Map<string, number>());

  const dismissToast = useCallback((id: string) => {
    const timer = dismissalTimers.current.get(id);
    if (timer) clearTimeout(timer);
    dismissalTimers.current.delete(id);
    timed.current.delete(id);
    setToasts((current) => current.filter((toast) => toast.id !== id));
  }, []);

  const schedule = useCallback((id: string, lifetime: number) => {
    const timer = dismissalTimers.current.get(id);
    if (timer) clearTimeout(timer);
    dismissalTimers.current.set(id, setTimeout(() => dismissToast(id), lifetime));
  }, [dismissToast]);

  // A toast under the pointer or holding focus waits for it to leave, then
  // starts its whole time again, as Notion's.
  const holdToast = useCallback((id: string, held: boolean) => {
    const lifetime = timed.current.get(id);
    if (lifetime === undefined) return;
    if (!held) { schedule(id, lifetime); return; }
    const timer = dismissalTimers.current.get(id);
    if (timer) clearTimeout(timer);
    dismissalTimers.current.delete(id);
  }, [schedule]);

  const show = useCallback((
    message: string,
    tone: ProductToastTone = "default",
    actions?: ToastAction | readonly ToastAction[],
    icon?: ToastMessage["icon"],
    detail?: string,
    person?: ToastMessage["person"],
    kept = false,
  ) => {
    const id = newId();
    const list: readonly ToastAction[] = actions === undefined ? [] : "label" in actions ? [actions] : actions;
    const action = list.at(-1), leadingActions = list.slice(0, -1);
    setToasts((current) => [
      ...current.filter((toast) => toast.message !== message),
      { id, message, tone, action, ...(leadingActions.length ? { leadingActions } : {}), ...(icon ? { icon } : {}), ...(detail ? { detail } : {}), ...(person ? { person } : {}) },
    ].slice(-4));
    const lifetime = kept || action?.autoDismissMs === null ? undefined : action?.autoDismissMs ?? TOAST_LIFETIME_MS;
    if (lifetime !== undefined) {
      timed.current.set(id, lifetime);
      schedule(id, lifetime);
    }
  }, [schedule]);

  // A failure told just as the server refused a request for now — after
  // asking it again long enough — says beside why, whatever its own words
  // (lib/serverBusy.ts); a change kept here to send later tells no failure.
  const notify = useCallback<ProductToastContextValue["notify"]>((message, tone, ...rest) => {
    show(message, tone, ...rest);
    const busy = i18next.t("common:serverBusy");
    if (tone === "error" && message !== busy && refusedForNowLately()) show(busy, "error");
  }, [show]);

  const dismissMessage = useCallback((message: string) => {
    setToasts((current) => {
      for (const toast of current) {
        if (toast.message !== message) continue;
        const timer = dismissalTimers.current.get(toast.id);
        if (timer) clearTimeout(timer);
        dismissalTimers.current.delete(toast.id);
        timed.current.delete(toast.id);
      }
      return current.filter((toast) => toast.message !== message);
    });
  }, []);

  const dismissErrors = useCallback(() => {
    setToasts((current) => {
      for (const toast of current) {
        if (toast.tone !== "error") continue;
        const timer = dismissalTimers.current.get(toast.id);
        if (timer) clearTimeout(timer);
        dismissalTimers.current.delete(toast.id);
        timed.current.delete(toast.id);
      }
      return current.filter((toast) => toast.tone !== "error");
    });
  }, []);

  useEffect(() => () => {
    for (const timer of dismissalTimers.current.values()) clearTimeout(timer);
    dismissalTimers.current.clear();
  }, []);

  const value = useMemo(() => ({ announce, dismissErrors, dismissMessage, dismissToast, notify }), [announce, dismissErrors, dismissMessage, dismissToast, notify]);
  useEffect(() => {
    mountedStack = value;
    return () => { if (mountedStack === value) mountedStack = null; };
  }, [value]);

  return (
    <ProductToastContext.Provider value={value}>
      {children}
      <ToastStack dismissToast={dismissToast} toasts={toasts} onHold={holdToast} announcement={announcement} />
    </ProductToastContext.Provider>
  );
}

/** The app's toast stack for what is drawn outside its tree; none before the app is drawn. */
export function appToastStack() {
  return mountedStack;
}

/** The toast stack when one is mounted; editors on published pages have none. */
export function useOptionalProductToast() {
  return useContext(ProductToastContext);
}

export function useProductToast() {
  const value = useContext(ProductToastContext);
  if (!value) throw new Error("useProductToast must be used inside ProductToastProvider.");
  return value;
}
