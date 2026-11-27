"use client";
import { workspaceMembershipBounds } from "@/workspaceMembershipWire.generated";
import { useAttemptNotice } from "@/lib/useNoticeFor";

import { type FormEvent, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { WorkspaceIconGlyph, workspaceIconLetter } from "@/components/page/PageIcon";
import { ReadFailed } from "@/components/ReadFailed";
import { i18next } from "@/i18n";
import { newId } from "@/lib/ids";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import type { Workspace } from "@/lib/types";
import type { JoinableWorkspace } from "@/lib/workspaceAllowedEmailDomains";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useClosedForLink } from "@/lib/productWindows";
import { X } from "./icons";
import styles from "./WorkspaceCreateDialog.module.css";

export type WorkspaceCreateChoice = "blank" | "notion" | "leafen" | "move";

// An organization that verified the creator's email domain may keep new
// workspaces to its owners and admins (Identity → 도메인 관리).
export function workspaceCreateErrorMessage(error: unknown): string {
  if (error instanceof LeafenApiError && error.code === "LEAFEN_WORKSPACE_CREATION_RESTRICTED") {
    return i18next.t("workspaceCreateDialog:restricted");
  }
  return productErrorMessage(error, "workspace.create");
}

export function WorkspaceCreateDialog({
  initialChoice = "blank",
  onClose,
  onCreate,
  loadJoinable,
  onJoin,
}: {
  /** What it starts from as it opens: the import screen asks for a Leafen file's. */
  initialChoice?: WorkspaceCreateChoice;
  onClose: () => void;
  /**
   * Creates the workspace (choice !== "blank" skips the starter pages) and
   * resolves with the created workspace; the caller owns navigation and the
   * follow-up import dialog.
   */
  onCreate: (input: {
    workspaceId: string;
    name: string;
    choice: WorkspaceCreateChoice;
  }) => Promise<Workspace>;
  /** The workspaces open to the person's email domain, offered to join first. */
  loadJoinable?: (signal: AbortSignal) => Promise<JoinableWorkspace[]>;
  /** Joins one, after which the caller opens it and closes the dialog. */
  onJoin?: (workspaceId: string) => Promise<void>;
}) {
  const { t } = useTranslation(["workspaceCreateDialog", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const createIdentityRef = useRef<{ intent: string; workspaceId: string } | null>(null);
  const [name, setName] = useState("");
  const [choice, setChoice] = useState<WorkspaceCreateChoice>(initialChoice);
  const [busy, setBusy] = useState(false);
  // Making or joining moves the app's address itself: a link meanwhile leaves
  // the window to its own flow, which closes it once done.
  const working = useRef(false);
  const work = (on: boolean) => { working.current = on; setBusy(on); };
  const [error, setError] = useAttemptNotice<string>(name, choice);
  const [joinable, setJoinable] = useState<JoinableWorkspace[]>([]);
  const [joining, setJoining] = useState<string | null>(null);

  const [joinableFailed, setJoinableFailed] = useState(false);
  const [joinableAttempt, setJoinableAttempt] = useState(0);
  useEffect(() => {
    if (!loadJoinable) return;
    const controller = new AbortController();
    setJoinableFailed(false);
    loadJoinable(controller.signal)
      .then((workspaces) => { if (!controller.signal.aborted) setJoinable(workspaces); })
      // Unread, the workspaces this person could join instead say so.
      .catch(() => { if (!controller.signal.aborted) setJoinableFailed(true); });
    return () => controller.abort();
  }, [joinableAttempt, loadJoinable]);

  async function join(workspaceId: string) {
    if (busy || !onJoin) return;
    work(true);
    setJoining(workspaceId);
    setError(null);
    try {
      await onJoin(workspaceId);
    } catch (err) {
      // A server that doesn't add members says so whatever is typed.
      const disabled = err instanceof LeafenApiError && err.code === "LEAFEN_WORKSPACE_MEMBER_ADD_DISABLED";
      setError(disabled ? t("workspaceCreateDialog:join.disabled") : t("workspaceCreateDialog:join.failed"), { stands: disabled });
      work(false);
      setJoining(null);
    }
  }

  // A link going to a place here closes it as Escape does (closeWindowsForLink), unless it is making or joining.
  useClosedForLink(() => { if (!working.current) onClose(); });

  // A modal layer: the page stays still, Escape closes it while nothing
  // opened over it takes the key, and the focus goes back to its opener.
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); },
  });

  // The name takes the keys as soon as the window shows, and again after the
  // layer settles in case it moved the focus.
  useLayoutEffect(() => {
    nameRef.current?.focus();
    const frame = window.requestAnimationFrame(() => nameRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    work(true);
    setError(null);
    const normalizedName = name.trim();
    const intent = JSON.stringify([normalizedName, choice]);
    if (createIdentityRef.current?.intent !== intent) {
      createIdentityRef.current = { intent, workspaceId: newId() };
    }
    try {
      await onCreate({
        workspaceId: createIdentityRef.current.workspaceId,
        name: normalizedName,
        choice,
      });
    } catch (err) {
      setError(workspaceCreateErrorMessage(err), { stands: err instanceof LeafenApiError && err.code === "LEAFEN_WORKSPACE_CREATION_RESTRICTED" });
      work(false);
    }
  }

  const options: Array<{ key: WorkspaceCreateChoice; title: string; body: string }> = [
    {
      key: "blank",
      title: t("workspaceCreateDialog:options.blank.title"),
      body: t("workspaceCreateDialog:options.blank.body"),
    },
    {
      key: "notion",
      title: t("workspaceCreateDialog:options.notion.title"),
      body: t("workspaceCreateDialog:options.notion.body"),
    },
    {
      key: "leafen",
      title: t("workspaceCreateDialog:options.leafen.title"),
      body: t("workspaceCreateDialog:options.leafen.body"),
    },
    {
      key: "move",
      title: t("workspaceCreateDialog:options.move.title"),
      body: t("workspaceCreateDialog:options.move.body"),
    },
  ];

  const dialog = (
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }}>
      <button
        type="button"
        className={styles.backdrop}
        onClick={() => {
          if (!busy) onClose();
        }}
        disabled={busy}
        tabIndex={-1}
        aria-label={t("common:actions.close")}
      />
      <section
        ref={dialogRef}
        className={styles.dialog}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-busy={busy || undefined}
      >
        <header className={styles.header}>
          <h2 id={titleId}>{t("workspaceCreateDialog:title")}</h2>
          <button
            type="button"
            className={styles.close}
            onClick={onClose}
            disabled={busy}
            aria-label={t("common:actions.close")}
          >
            <X size="regular" aria-hidden="true" />
          </button>
        </header>
        <form className={styles.body} onSubmit={submit}>
          {joinable.length ? (
            <>
              <span className={styles.label}>{t("workspaceCreateDialog:join.title")}</span>
              <ul className={styles.joinList} data-joinable-workspaces>
                {joinable.map((item) => (
                  <li key={item.id} className={styles.joinRow} data-joinable-workspace={item.id}>
                    <WorkspaceIconGlyph icon={item.icon} size={22} fallback={workspaceIconLetter(item.name)} />
                    <span className={styles.joinText}>
                      <strong>{item.name}</strong>
                      <span>{t("workspaceCreateDialog:join.members", { count: item.members })}</span>
                    </span>
                    <button
                      type="button"
                      className={styles.secondary}
                      disabled={busy}
                      onClick={() => void join(item.id)}
                    >
                      {joining === item.id ? t("workspaceCreateDialog:join.joining") : t("workspaceCreateDialog:join.join")}
                    </button>
                  </li>
                ))}
              </ul>
            </>
          ) : joinableFailed ? (
            <ReadFailed message={t("workspaceCreateDialog:join.loadFailed")} onRetry={() => setJoinableAttempt((current) => current + 1)} />
          ) : null}
          <label className={styles.label} htmlFor="workspace-create-name">
            {t("workspaceCreateDialog:name.label")}
          </label>
          <input
            id="workspace-create-name"
            ref={nameRef}
            className={styles.nameInput}
            value={name}
            placeholder={t("workspaceCreateDialog:name.placeholder")}
            maxLength={workspaceMembershipBounds.creationNameRunes}
            disabled={busy}
            onChange={(event) => setName(event.target.value)}
          />
          <span className={styles.label}>{t("workspaceCreateDialog:startLabel")}</span>
          <div
            className={styles.options}
            role="radiogroup"
            aria-label={t("workspaceCreateDialog:startLabel")}
          >
            {options.map((option) => (
              <button
                key={option.key}
                type="button"
                className={styles.option}
                role="radio"
                aria-checked={choice === option.key}
                data-active={choice === option.key ? "true" : undefined}
                disabled={busy}
                onClick={() => setChoice(option.key)}
              >
                <span className={styles.optionTitle}>{option.title}</span>
                <span className={styles.optionBody}>{option.body}</span>
              </button>
            ))}
          </div>
          {error ? (
            <p className={styles.error} role="alert">
              {error}
            </p>
          ) : null}
          <div className={styles.actions}>
            <button type="button" className={styles.cancel} onClick={onClose} disabled={busy}>
              {t("common:actions.cancel")}
            </button>
            <button type="submit" className={styles.primary} disabled={busy}>
              {busy ? t("workspaceCreateDialog:creating") : t("workspaceCreateDialog:create")}
            </button>
          </div>
        </form>
      </section>
    </div>
  );

  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}
