import { CheckCircleIcon, LockIcon, OpenInNew, UndoIcon, X } from "@/icons/leafen";
"use client";

import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { onLayersChanged } from "@/lib/overlayLayer";
import { overlayRoot } from "@/lib/overlayRoot";
import { logProductError } from "@/lib/safeErrorLog";
import {
  invokeToastAction,
  onToastPlacementChange,
  toastStackSyncClearance,
  type LiveAnnouncement,
  type ToastAction,
  type ToastMessage,
} from "@/lib/toastStackView";

import styles from "./ToastStack.module.css";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * How far the sidebar reaches into the window, read from its slot, the one
 * place its width is set (AppShell: a width set on the shell restyled the
 * whole app): the corner notices stand 12px past it however wide it is
 * dragged and at the window's edge while it is folded or absent (a phone).
 */
function useSidebarReach(active: boolean) {
  const [reach, setReach] = useState(0);
  useEffect(() => {
    if (!active) return undefined;
    let slot: HTMLElement | null = null;
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(() => read());
    function read() {
      const found = document.querySelector<HTMLElement>("[data-sidebar-slot]");
      if (found !== slot) {
        if (slot) observer?.unobserve(slot);
        slot = found;
        if (slot) observer?.observe(slot);
      }
      setReach(slot ? Math.round(slot.getBoundingClientRect().width) : 0);
    }
    read();
    window.addEventListener("resize", read);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", read);
    };
  }, [active]);
  return reach;
}

function visibleModalDialogs() {
  return Array.from(
    document.querySelectorAll<HTMLElement>('[role="dialog"][aria-modal="true"]'),
  ).filter((dialog) => {
    const rect = dialog.getBoundingClientRect();
    const style = window.getComputedStyle(dialog);
    return rect.width > 0
      && rect.height > 0
      && style.display !== "none"
      && style.visibility !== "hidden";
  });
}

function visibleSyncStatusBadge() {
  const badge = document.querySelector<HTMLElement>('[data-testid="sync-status-badge"]');
  if (!badge) return null;
  const rect = badge.getBoundingClientRect();
  const style = window.getComputedStyle(badge);
  return rect.width > 0
    && rect.height > 0
    && style.display !== "none"
    && style.visibility !== "hidden"
    ? badge
    : null;
}

function clearSideLane(stack: HTMLDivElement) {
  stack.style.removeProperty("--toast-stack-inline-start");
  stack.style.removeProperty("--toast-stack-width");
}

function clearSyncClearance(stack: HTMLDivElement) {
  stack.style.removeProperty("--toast-stack-safe-bottom");
}

interface ToastStackPlacementTargets {
  dialog: HTMLElement | null;
  syncBadge: HTMLElement | null;
}

// One place whatever is open, as Notion's: the bottom's middle, over a window
// when one is open (2026-11-05: beside a small window, over the middle of the
// settings window's content otherwise). Over a window too it stands clear of
// the sync status notice at the window's foot, which it covered there
// (2026-11-12, the settings window).
function placeToastStack(stack: HTMLDivElement): ToastStackPlacementTargets {
  const dialog = visibleModalDialogs().at(-1) ?? null;
  if (dialog) stack.dataset.modalPlacement = "footer";
  else delete stack.dataset.modalPlacement;
  clearSideLane(stack);
  const syncBadge = visibleSyncStatusBadge();
  if (!syncBadge) {
    clearSyncClearance(stack);
    return { dialog, syncBadge: null };
  }
  const badgeRect = syncBadge.getBoundingClientRect();
  const stackRect = stack.getBoundingClientRect();
  const safeBottom = toastStackSyncClearance({
    badgeLeft: badgeRect.left,
    badgeRight: badgeRect.right,
    badgeTop: badgeRect.top,
    stackLeft: stackRect.left,
    stackRight: stackRect.right,
    viewportHeight: window.innerHeight,
  });
  if (safeBottom === null) clearSyncClearance(stack);
  else stack.style.setProperty("--toast-stack-safe-bottom", `${safeBottom}px`);
  return { dialog, syncBadge };
}

export interface ToastStackProps {
  announcement?: LiveAnnouncement;
  dismissToast: (id: string) => void;
  toasts: readonly ToastMessage[];
  /** A toast pointed at or focused holds its timer until left. */
  onHold?: (id: string, held: boolean) => void;
}

export function ToastStack({
  announcement,
  dismissToast,
  toasts,
  onHold,
}: ToastStackProps) {
  const { t } = useTranslation(["toastStack", "common"]);
  // "실행 취소" leads with Notion's turning-back arrow, 6px before the words
  // (seen 2026-10-31), but not beside another action, as its move notice
  // (seen 2026-11-03); every undo toast says it in the same words.
  const undoLabel = t("toastStack:undo");
  const actionLabel = (label: string) => label === undoLabel
    ? <><UndoIcon size="small" aria-hidden="true" />{label}</> : label;
  // "열기" leads with Notion's ↗ box wherever a toast offers it, as its
  // button's and its sprint completion's (seen 2026-11-08); each says it in
  // the same words.
  const openLabel = t("toastStack:open");
  const markedLabel = (action: ToastAction, plain: ReactNode) => action.icon === "none" ? action.label
    : action.icon === "open" || (!action.icon && action.label === openLabel)
      ? <><OpenInNew size="small" aria-hidden="true" />{action.label}</>
      : action.icon === "undo" ? <><UndoIcon size="small" aria-hidden="true" />{action.label}</> : plain;
  const stackRef = useRef<HTMLDivElement>(null);
  const layerRef = useRef<HTMLDivElement>(null);
  const showing = toasts.length > 0;

  // While a toast shows it keeps clear of a window opened over the page and
  // of the sync status badge, measured again as those come and go: the
  // app's layers opening and closing (onLayersChanged), the body's own
  // layers, what changes in the box at its end every layer of the app is
  // drawn in (overlayRoot) but in a page's text there (a peek's), and the
  // badge, which says when it comes and goes. Watching all of the body
  // measured the page after each change of its lines, laid out again in each
  // edit's frame of a long page while a toast showed (2026-11-14), and noted
  // each change while none did.
  useLayoutEffect(() => {
    if (!showing || typeof window === "undefined" || typeof MutationObserver === "undefined") return;

    let frame = 0;
    let observedDialog: HTMLElement | null = null;
    let observedStack: HTMLDivElement | null = null;
    let observedSyncBadge: HTMLElement | null = null;
    const resizeObserver = typeof ResizeObserver === "undefined"
      ? null
      : new ResizeObserver(() => schedule());

    function measure() {
      frame = 0;
      const stack = stackRef.current, layer = layerRef.current;
      // Over an open window the toasts and notices stand above it (the
      // layer says so for ToastStack.module.css): a rule on the body that
      // asked for a window there marked the body once any toast had shown,
      // and each line made after restyled the whole page (2026-11-14: 9,000
      // elements twice an Enter on a long page, CPU ×2).
      const notices = layer?.querySelector<HTMLElement>("[data-toast-notices]") ?? null;
      if (!stack) {
        layer?.toggleAttribute("data-over-window", !!notices && visibleModalDialogs().length > 0);
        resizeObserver?.disconnect();
        observedDialog = null;
        observedStack = null;
        observedSyncBadge = null;
        return;
      }
      const { dialog, syncBadge } = placeToastStack(stack);
      layer?.toggleAttribute("data-over-window", !!dialog);
      if (
        dialog === observedDialog
        && stack === observedStack
        && syncBadge === observedSyncBadge
      ) return;
      resizeObserver?.disconnect();
      resizeObserver?.observe(stack);
      if (dialog) resizeObserver?.observe(dialog);
      if (syncBadge) resizeObserver?.observe(syncBadge);
      observedDialog = dialog;
      observedStack = stack;
      observedSyncBadge = syncBadge;
    }

    function schedule() {
      if (frame) return;
      frame = window.requestAnimationFrame(measure);
    }

    const mutationObserver = new MutationObserver((records) => {
      if (records.some((record) => !(record.target instanceof Element ? record.target : record.target.parentElement)
        ?.closest('[contenteditable="true"]'))) schedule();
    });
    mutationObserver.observe(document.body, { childList: true });
    mutationObserver.observe(overlayRoot(), {
      attributes: true,
      attributeFilter: ["aria-modal", "class", "hidden", "role"],
      childList: true,
      subtree: true,
    });
    const badge = onToastPlacementChange(schedule), layers = onLayersChanged(schedule);
    window.addEventListener("resize", schedule);
    schedule();

    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      mutationObserver.disconnect();
      resizeObserver?.disconnect();
      badge(); layers();
      window.removeEventListener("resize", schedule);
    };
  }, [showing]);

  // A toast that explains itself is Notion's corner notice; the rest stack in the middle.
  const notices = toasts.filter((toast) => toast.detail);
  const plain = toasts.filter((toast) => !toast.detail);
  const sidebarReach = useSidebarReach(notices.length > 0);

  // A window that keeps the page behind it still leaves what a toast says and
  // offers within reach (2026-11-05: over the first run its buttons were dead),
  // so the element is there before any window opens.
  return (
    <div ref={layerRef} data-toast-layer data-react-aria-top-layer="true" style={{ display: "contents" }}>
      {announcement && (
        <div
          key={announcement.id}
          className={styles.announcement}
          data-toast-announcement
          data-react-aria-top-layer=""
          role="status"
          aria-live="polite"
          aria-atomic="true"
        >
          {announcement.message}
        </div>
      )}
      {notices.length > 0 && (
        <div className={styles.notices} data-toast-notices data-react-aria-top-layer="" role="status" aria-live="polite" aria-atomic="false"
          style={{ insetInlineStart: sidebarReach + 12 }}>
          {notices.map((toast) => (
            <div key={toast.id} className={styles.notice} data-tone={toast.tone ?? "default"}
              onPointerEnter={() => onHold?.(toast.id, true)} onPointerLeave={() => onHold?.(toast.id, false)}
              onFocus={() => onHold?.(toast.id, true)}
              onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) onHold?.(toast.id, false); }}>
              {toast.tone !== "error" && toast.icon !== "none" ? <span className={styles.noticeMark} aria-hidden="true"><CheckCircleIcon size="small" /></span> : null}
              <div className={styles.noticeBody}>
                <span className={styles.noticeText}>
                  <strong>{toast.message}</strong>
                  {toast.person ? (
                    <span className={styles.noticePerson}>
                      <PersonAvatar userId={toast.person.userId} name={toast.person.name} size={20} />
                      <span className={styles.noticePersonName}>{toast.person.name}</span>
                    </span>
                  ) : null}
                  {toast.detail}
                </span>
                {toast.action ? (
                  <span className={styles.noticeActions}>
                    {/* An action before the last reads as words, Notion's "무시" before its ringed "초대" (seen 2026-11-07). */}
                    {toast.leadingActions?.map((leading) => (
                      <button key={leading.label} type="button" className={styles.noticeAction} data-plain onClick={() => {
                        dismissToast(toast.id);
                        void invokeToastAction(leading.onClick, (error) => logProductError("warn", "toast.action", error));
                      }}>{leading.label}</button>
                    ))}
                    <button type="button" className={styles.noticeAction} onClick={() => {
                      dismissToast(toast.id);
                      void invokeToastAction(toast.action!.onClick, (error) => logProductError("warn", "toast.action", error));
                    }}>{toast.action.label}</button>
                  </span>
                ) : null}
              </div>
              <button type="button" className={styles.noticeClose} aria-label={t("toastStack:dismiss")} onClick={() => dismissToast(toast.id)}>
                <X size="tiny" aria-hidden="true" />
              </button>
            </div>
          ))}
        </div>
      )}
      {plain.length > 0 && (
        <div
          ref={stackRef}
          className={styles.stack}
          data-toast-stack
          data-react-aria-top-layer=""
          role="status"
          aria-live="polite"
          aria-atomic="false"
        >
          {plain.map((toast) => {
            const action = toast.action;
            // An error shows its close button; another toast closes when
            // clicked.
            const closeButton = !action && toast.tone === "error";
            const clickToClose = !action && !closeButton;
            return (
              <div
                key={toast.id}
                className={styles.toast}
                data-tone={toast.tone ?? "default"}
                data-icon={toast.icon}
                role={clickToClose ? "button" : undefined}
                tabIndex={clickToClose ? 0 : undefined}
                onClick={clickToClose ? () => dismissToast(toast.id) : undefined}
                onPointerEnter={() => onHold?.(toast.id, true)}
                onPointerLeave={() => onHold?.(toast.id, false)}
                onFocus={() => onHold?.(toast.id, true)}
                onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) onHold?.(toast.id, false); }}
                onKeyDown={(event) => {
                  if (!clickToClose || (!fieldEnter(event) && event.key !== " ")) return;
                  event.preventDefault();
                  dismissToast(toast.id);
                }}
              >
                {toast.icon === "lock" ? <LockIcon size="small" className={styles.icon} aria-hidden="true" /> : null}
                {toast.icon === "progress" ? <span className={styles.progress} aria-hidden="true" /> : null}
                <span className={styles.message}>
                  {toast.message}
                </span>
                {closeButton && (
                  <button
                    type="button"
                    className={styles.dismiss}
                    aria-label={t("toastStack:dismiss")}
                    onClick={() => dismissToast(toast.id)}
                  >
                    <X size="small" aria-hidden="true" />
                  </button>
                )}
                {action && (
                  <>
                    {toast.icon === "progress" ? <span className={styles.actionLine} aria-hidden="true" /> : null}
                    {toast.leadingActions?.map((leading) => (
                      <button key={leading.label} type="button" className={styles.action}
                        onClick={(event) => {
                          event.stopPropagation();
                          dismissToast(toast.id);
                          void invokeToastAction(leading.onClick, (error) => {
                            logProductError("warn", "toast.action", error);
                          });
                        }}>
                        {markedLabel(leading, actionLabel(leading.label))}
                      </button>
                    ))}
                    <button
                      type="button"
                      className={styles.action}
                      onClick={(event) => {
                        event.stopPropagation();
                        dismissToast(toast.id);
                        void invokeToastAction(action.onClick, (error) => {
                          logProductError("warn", "toast.action", error);
                        });
                      }}
                    >
                      {markedLabel(action, toast.leadingActions?.length ? action.label : actionLabel(action.label))}
                    </button>
                    {(action.dismissible ?? action.autoDismissMs === null) && (
                      <button
                        type="button"
                        className={styles.dismiss}
                        aria-label={t("toastStack:dismiss")}
                        onClick={(event) => {
                          event.stopPropagation();
                          dismissToast(toast.id);
                        }}
                      >
                        <X size="small" aria-hidden="true" />
                      </button>
                    )}
                  </>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
