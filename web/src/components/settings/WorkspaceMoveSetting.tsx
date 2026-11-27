import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import { displayDateTime } from "@/lib/displayDate";
import { followAcrossTabs, tellTabs } from "@/lib/followAcrossTabs";
import { productErrorKnownCause, productErrorMessage } from "@/lib/productErrorPresentation";
import { openMovedServer } from "@/lib/profileSwitch";
import { pageHref } from "@/lib/navigation";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { useProductPages } from "@/components/product/productPagesContext";
import type { Session, Workspace } from "@/types";
import {
  workspaceMoveErrorCodes,
  type WorkspaceMoveAccount,
  type WorkspaceMoveCheck,
  type WorkspaceMoveCounts,
  type WorkspaceMoveIn,
  type WorkspaceMovedSummary,
  type WorkspaceMoveOut,
} from "@/workspaceMoveApi";
import { styles } from "./workspaceSettingsOverviewStyles";
import move from "./WorkspaceMoveSetting.module.css";
import { AppSelect } from "@/components/AppSelect";
import { useCopyNotice } from "@/lib/copyNotice";
import { ReadFailed } from "@/components/ReadFailed";

// While a move runs, its state is read again this often.
const STATUS_MS = 2000;
const COUNTS: (keyof WorkspaceMoveCounts)[] = ["pages", "files", "comments"];

type Translate = (key: string, options?: Record<string, unknown>) => string;

/**
 * Moving this workspace to another Leafen server, or one here (the agent
 * decision of 2026-10-19 09:31, its B): the new workspace's owner makes a
 * code there; here its owner checks it, pairs the workspace's people with the
 * accounts there (the same email offered), and moves — the workspace stops
 * taking changes, its pages, files and comments go, and the counts on both
 * sides are shown. Its edit history, and pages only someone else could read,
 * stay here. Leafen copies; the person's outside agent may guide it.
 */
export function WorkspaceMoveSetting({ session, workspace }: { session: Session; workspace: Workspace }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const copy = useCopyNotice();
  const [moveIn, setMoveIn] = useState<WorkspaceMoveIn | null>(null);
  const [moveOut, setMoveOut] = useState<WorkspaceMoveOut | null>(null);
  const [pasted, setPasted] = useState("");
  const [check, setCheck] = useState<WorkspaceMoveCheck | null>(null);
  const [pairs, setPairs] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useAttemptNotice<string>(pasted);
  const mounted = useRef(true);

  // Either move's state that could not be read: the part says so with a way to read again.
  const [readFailed, setReadFailed] = useState(false);
  const load = useCallback((signal?: AbortSignal) => {
    const failed = () => { if (mounted.current && !signal?.aborted) setReadFailed(true); };
    api.workspaceMoveIn(session, workspace.id, signal).then((value) => { if (mounted.current) setMoveIn(value); }, failed);
    api.workspaceMoveOut(session, workspace.id, signal).then((value) => { if (mounted.current) setMoveOut(value); }, failed);
  }, [session, workspace.id]);

  useEffect(() => {
    mounted.current = true;
    const controller = new AbortController();
    load(controller.signal);
    return () => { mounted.current = false; controller.abort(); };
  }, [load]);

  const sending = moveOut?.state === "sending";
  const receiving = moveIn?.state === "waiting" || moveIn?.state === "receiving"
    || (moveIn?.state === "importing" && moveIn.jobStatus !== "completed" && moveIn.jobStatus !== "failed");
  // One of the person's tabs in sight asks for all of them, and a tab behind
  // asks nothing (followAcrossTabs).
  useEffect(() => {
    if (!sending && !receiving) return undefined;
    const controller = new AbortController();
    const topic = `workspace-move:${session.user.id}:${workspace.id}`;
    const stop = followAcrossTabs(topic, STATUS_MS, () => Promise.allSettled([
      api.workspaceMoveIn(session, workspace.id, controller.signal), api.workspaceMoveOut(session, workspace.id, controller.signal),
    ]).then(([moveIn, moveOut]) => tellTabs(topic, {
      moveIn: moveIn.status === "fulfilled" ? moveIn.value : undefined,
      moveOut: moveOut.status === "fulfilled" ? moveOut.value : undefined,
    })), (value) => {
      const told = value as { moveIn?: WorkspaceMoveIn; moveOut?: WorkspaceMoveOut } | undefined;
      if (!mounted.current || !told) return;
      if (told.moveIn) setMoveIn(told.moveIn);
      if (told.moveOut) setMoveOut(told.moveOut);
    });
    return () => { controller.abort(); stop(); };
  }, [sending, receiving, session, workspace.id]);

  async function act<T>(action: () => Promise<T>, apply: (value: T) => void) {
    setBusy(true);
    setError("");
    try {
      const value = await action();
      if (mounted.current) apply(value);
    } catch (cause) {
      if (mounted.current) setError(moveMessage(cause, t));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  if (!moveIn || !moveOut) return readFailed ? (
    <section className={styles.section} aria-label={t("move.title")} data-workspace-move data-settings-part="move">
      <div className={styles.sectionTitle}>{t("move.title")}</div>
      <ReadFailed message={t("move.readFailed")} onRetry={() => { setReadFailed(false); load(); }} />
    </section>
  ) : null;
  const moved = moveOut.state === "moved";
  const empty = moveIn.state !== "none" || moveIn.pages === 0;
  // A move here that failed leaves nothing, and another can start: both ways stay open.
  const moveInFailed = moveIn.state === "importing" && (moveIn.jobStatus === "failed" || moveIn.jobStatus === "canceled" || moveIn.jobStatus === "cancelled");
  const sendForm = !moved && !sending && moveIn.pages > 0 && (moveIn.state === "none" || moveInFailed);

  return (
    <section className={styles.section} aria-label={t("move.title")} data-workspace-move data-settings-part="move">
      <div className={styles.sectionHeader}>
        <div>
          <div className={styles.sectionTitle}>{t("move.title")}</div>
          <div className={styles.organizationMeta}>{t("move.meta")}</div>
        </div>
      </div>

      {moved ? (
        <div className={styles.notice} data-tone="neutral" data-workspace-move-out="moved">
          <p>{t("move.moved", { address: moveOut.movedTo })}</p>
          {moveOut.report ? <CountTable here={moveOut.report.here} there={moveOut.report.there} t={t} /> : null}
          {moveOut.report && moveOut.report.there.pages < moveOut.report.here.pages ? <p>{t("move.pagesLeft")}</p> : null}
          <p>{t("move.movedHelp")}</p>
          <div className={styles.sectionActions}>
            <button type="button" className={styles.primaryButton} onClick={() => openMovedServer(moveOut.movedTo ?? "")} data-workspace-move-open>
              {t("move.openNew")}
            </button>
          </div>
        </div>
      ) : sending ? (
        <div className={styles.notice} data-tone="neutral" data-workspace-move-out="sending" role="status">
          <p>{t(`move.step.${moveOut.step || "export"}`, { address: moveOut.movedTo })}</p>
        </div>
      ) : sendForm ? (
        <div className={`${styles.securityRow} ${move.row}`} data-workspace-move-out={moveOut.state}>
          <div className={styles.securityInstruction}>
            <strong>{t("move.outTitle")}</strong>
            {moveOut.state === "failed" ? <span className={styles.errorText}>
              {moveOut.errorCode === workspaceMoveErrorCodes.alreadyHere ? t("move.alreadyThere") : failedMoveText(t("move.failed"), moveOut.errorCode)}</span>
              : <span>{t("move.outHelp")}</span>}
            <label className={`${styles.field} ${move.code}`}>
              <span>{t("move.codeLabel")}</span>
              <textarea rows={3} value={pasted} disabled={busy} placeholder="leafen-workspace-move-1:…" data-workspace-move-code
                onChange={(event) => { setPasted(event.target.value); setCheck(null); if (error) setError(""); }}
                {...(error === t("move.codeInvalid") || error === t("move.sameServer") ? { "aria-invalid": true as const, "aria-describedby": "workspace-move-error" } : {})} />
            </label>
            {error ? <div id="workspace-move-error" className={move.error} role="alert">{error}</div> : null}
            {check ? (
              <div className={styles.securityInstruction} data-workspace-move-pairs>
                <span>{t("move.pairsHelp", { address: check.to })}</span>
                {check.people.map((person) => (
                  <label key={person.id} className={move.pair} data-workspace-move-person={person.id}>
                    <span>{personLabel(person)}</span>
                    <MovePairSelect label={personLabel(person)} accounts={check.accounts} value={pairs[person.id] ?? ""} disabled={busy}
                      onChange={(account) => setPairs((current) => ({ ...current, [person.id]: account }))} />
                  </label>
                ))}
                <span>{t("move.stays")}</span>
                <span>{t("move.notCarried")}</span>
              </div>
            ) : null}
          </div>
          <div className={styles.sectionActions}>
            {check ? (
              <>
                <button type="button" className={styles.dangerButton} disabled={busy} data-workspace-move-start
                  onClick={() => void act(() => api.startWorkspaceMoveOut(session, workspace.id, pasted.trim(), pairs), (value) => {
                    setMoveOut(value); setCheck(null); setPasted("");
                  })}>
                  {t("move.start")}
                </button>
                <button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => setCheck(null)}>
                  {t("move.cancel")}
                </button>
              </>
            ) : (
              <button type="button" className={styles.secondaryButton} disabled={busy || !pasted.trim()} data-workspace-move-check
                onClick={() => void act(() => api.checkWorkspaceMoveOut(session, workspace.id, pasted.trim()), (value) => {
                  setCheck(value);
                  setPairs(Object.fromEntries(value.people.filter((person) => person.suggestion).map((person) => [person.id, person.suggestion as string])));
                })}>
                {t("move.check")}
              </button>
            )}
          </div>
        </div>
      ) : null}

      {error && !sendForm ? <div id="workspace-move-error" className={move.error} role="alert">{error}</div> : null}
      {!moved && !sending && !empty ? (
        <div className={`${styles.securityRow} ${move.row}`} data-workspace-move-in="not-empty">
          <div className={styles.securityInstruction}>
            <strong>{t("move.inTitle")}</strong>
            <span>{t("move.inNeedsEmpty")}</span>
          </div>
        </div>
      ) : null}

      {/* What a move here left to do (the decision of 2026-11-05 19:41): invite the people
          who weren't paired with an account here, and turn public again what was public there. */}
      {moveIn.moved ? <WorkspaceMovedFollowUp session={session} workspaceId={workspace.id} moved={moveIn.moved} /> : null}

      {!moved && !sending && empty ? (
        <div className={`${styles.securityRow} ${move.row}`} data-workspace-move-in={moveIn.state}>
          <div className={styles.securityInstruction}>
            <strong>{t("move.inTitle")}</strong>
            {moveIn.state === "waiting" ? (
              <>
                <label className={`${styles.field} ${move.code}`}>
                  <span>{t("move.inCodeLabel")}</span>
                  <textarea readOnly rows={3} value={moveIn.code ?? ""} onFocus={(event) => event.currentTarget.select()} data-workspace-move-in-code />
                </label>
                <span>{t("move.inCodeHelp", { time: formatTime(moveIn.expiresAt ?? "") })}</span>
              </>
            ) : moveIn.state === "receiving" || (moveIn.state === "importing" && moveIn.jobStatus !== "completed") ? (
              <span role="status">{!moveInFailed ? t("move.receiving")
                : moveIn.errorCode === workspaceMoveErrorCodes.alreadyHere ? t("move.alreadyHere") : failedMoveText(t("move.inFailed"), moveIn.errorCode)}</span>
            ) : moveIn.state === "importing" ? (
              <span>{t("move.movedIn", { pages: moveIn.pages, files: moveIn.files, comments: moveIn.comments })}</span>
            ) : (
              <span>{t("move.inHelp")}</span>
            )}
          </div>
          <div className={styles.sectionActions}>
            {moveIn.state === "waiting" ? (
              <>
                <button type="button" className={styles.secondaryButton} onClick={() => void copy(moveIn.code ?? "")}>
                  {t("move.copy")}
                </button>
                <button type="button" className={styles.secondaryButton} disabled={busy} data-workspace-move-in-close
                  onClick={() => void act(() => api.closeWorkspaceMoveIn(session, workspace.id), setMoveIn)}>
                  {t("move.cancel")}
                </button>
              </>
            ) : moveIn.state === "none" || moveInFailed ? (
              <button type="button" className={styles.secondaryButton} disabled={busy} data-workspace-move-in-open
                onClick={() => void act(() => api.openWorkspaceMoveIn(session, workspace.id), setMoveIn)}>
                {t("move.inOpen")}
              </button>
            ) : null}
          </div>
        </div>
      ) : null}
    </section>
  );
}

function CountTable({ here, there, t }: { here: WorkspaceMoveCounts; there: WorkspaceMoveCounts; t: Translate }) {
  return (
    <ul data-workspace-move-counts>
      {COUNTS.map((key) => (
        <li key={key} data-workspace-move-count={key}>
          {t(`move.count.${key}`)}: {here[key] === there[key]
            ? t(there[key] === 0 ? "move.countNone" : "move.countSame", { count: there[key] })
            : t("move.countDiffers", { there: there[key], here: here[key] })}
        </li>
      ))}
    </ul>
  );
}

function formatTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : displayDateTime(date);
}

/** What stopped a move, after what it means for the workspace, when the server says. */
function failedMoveText(what: string, code?: string) {
  const known = code && code !== workspaceMoveErrorCodes.failed ? productErrorKnownCause(code, "workspaceImport.job") : "";
  return known ? `${what} ${known}` : what;
}

/** A person by name and address, whichever they have. */
/** The most accounts a person's list draws at once; more are found by searching. */
const PAIR_LIST_LIMIT = 100;

/**
 * A person's account there, chosen from a list searched by name or email: a
 * server's accounts may be thousands, and each person's list draws only its
 * choice and the first matches (2026-11-08: a 5,051-account organization drew
 * every account in every person's list).
 */
function MovePairSelect({ label, accounts, value, disabled, onChange }: {
  label: string;
  accounts: WorkspaceMoveAccount[];
  value: string;
  disabled: boolean;
  onChange: (account: string) => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [query, setQuery] = useState("");
  const shown = useMemo(() => {
    const words = query.trim().toLocaleLowerCase();
    const matches = words ? accounts.filter((account) => personLabel(account).toLocaleLowerCase().includes(words)) : accounts;
    const first = matches.slice(0, PAIR_LIST_LIMIT);
    const chosen = value && !first.some((account) => account.id === value) ? accounts.find((account) => account.id === value) : undefined;
    return { list: chosen ? [chosen, ...first] : first, more: matches.length > PAIR_LIST_LIMIT };
  }, [accounts, query, value]);
  const search = t("move.searchAccounts");
  return (
    <AppSelect look="setting" aria-label={label} value={value} disabled={disabled}
      search={{ ariaLabel: search, placeholder: search, match: () => true, onQueryChange: setQuery }}
      onChange={(event) => onChange(event.target.value)}>
      <option value="">{t("move.notPaired")}</option>
      {shown.list.map((account) => <option key={account.id} value={account.id}>{personLabel(account)}</option>)}
      {shown.more ? <option value="more" disabled>{t("move.moreAccounts")}</option> : null}
    </AppSelect>
  );
}

function personLabel({ name, email }: { name?: string; email?: string }) {
  return [name, email !== name ? email : ""].filter(Boolean).join(" · ");
}

function moveMessage(error: unknown, t: Translate) {
  if (error instanceof LeafenApiError) {
    if (error.code === workspaceMoveErrorCodes.codeInvalid) return t("move.codeInvalid");
    if (error.code === workspaceMoveErrorCodes.sameServer) return t("move.sameServer");
    if (error.code === workspaceMoveErrorCodes.notEmpty) return t("move.notEmpty");
    if (error.code === workspaceMoveErrorCodes.failed) return t("move.requestFailed");
  }
  return productErrorMessage(error, "workspace.settings.update");
}

function WorkspaceMovedFollowUp({ session, workspaceId, moved }: { session: Session; workspaceId: string; moved: WorkspaceMovedSummary }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [invited, setInvited] = useState<Record<string, "busy" | "done" | "failed">>({});
  const invite = async (email: string) => {
    setInvited((current) => ({ ...current, [email]: "busy" }));
    try {
      await api.addWorkspaceMemberByEmail(session, workspaceId, { email, role: "member" });
      setInvited((current) => ({ ...current, [email]: "done" }));
    } catch {
      setInvited((current) => ({ ...current, [email]: "failed" }));
    }
  };
  const pages = [...moved.links, ...moved.sites.filter((site) => !moved.links.some((link) => link.pageId === site.pageId))];
  // An untitled page as the page list names it — a full-page database by its kind — else a page's name.
  const listed = useProductPages()?.pages;
  const untitled = (pageId: string) => untitledPageDisplayTitle(listed?.find(({ id }) => id === pageId) ?? "page");
  return (
    <div className={`${styles.securityRow} ${move.row}`} data-workspace-moved-follow-up>
      <div className={styles.securityInstruction}>
        {moved.people.length ? <>
          <strong>{t("move.movedPeopleTitle", { count: moved.people.length })}</strong>
          <span>{t("move.movedPeopleHelp")}</span>
          {moved.people.map((person) => (
            <div key={person.email} className={move.pair} data-workspace-moved-person>
              <span>{[person.name, person.email].filter(Boolean).join(" · ")}</span>
              <button type="button" className={styles.secondaryButton} disabled={invited[person.email] === "busy" || invited[person.email] === "done"}
                onClick={() => void invite(person.email)}>
                {invited[person.email] === "done" ? t("move.movedInvited") : invited[person.email] === "failed" ? t("move.movedInviteAgain") : t("move.movedInvite")}
              </button>
            </div>
          ))}
        </> : null}
        {pages.length ? <>
          <strong>{t("move.movedPublicTitle", { count: pages.length })}</strong>
          <span>{t("move.movedPublicHelp")}</span>
          {pages.map((page) => (
            <a key={page.pageId} className={move.pair} href={pageHref(page.pageId)} data-workspace-moved-public>
              {page.title || untitled(page.pageId)}
            </a>
          ))}
        </> : null}
      </div>
    </div>
  );
}
