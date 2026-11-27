import { useId, useLayoutEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { CheckCircleFillIcon } from "@/icons/leafen";
import { notionConnectionName } from "@/lib/notionImportFailure";
import type { Session } from "@/types";
import { BackupOffLine } from "../admin/BackupOffLine";
import NotionTokenGuide from "../NotionTokenGuide";
import styles from "./ImportDialog.module.css";
import { NotionAgentHandoff } from "./NotionAgentHandoffButton";
import { NotionFailureLine } from "./NotionFailureLine";
import own from "./NotionImportAgentFirst.module.css";
import type { NotionImportController } from "./useNotionImportController";

/**
 * Where the Notion window's connection step stands, each with one thing to
 * do (the user, 2026-11-13: "단계마다 할 일 하나"):
 * - "start": no way chosen — the agent's card (NotionAgentHandoff), and
 *   "직접 가져오기" small at the window's foot;
 * - "token": come by the agent's link (`import_screen`, `&via=agent`) or
 *   by hand, or Notion refused the saved token — Notion's steps and the
 *   token's field, saved at the foot;
 * - "saved": the connection is in — one card that says so, and the next
 *   thing: the agent goes on by itself (it waits for the connection, the
 *   migration guide's part 1), or by hand "옮길 항목 고르기" at the foot.
 * What moves and how it ends are the window's later steps (NotionImportPanel).
 */
export type NotionEntryPhase = "start" | "token" | "saved";

export function NotionImportAgentFirst({ controller, phase, byAgent, session, workspaceId, tokenPageUrl, onHanded }: {
  controller: NotionImportController;
  phase: NotionEntryPhase;
  /** The agent's way, not the person's by hand. */
  byAgent: boolean;
  session: Session;
  workspaceId: string;
  /** Notion's page for making a connection's token, as the guide's first scene opens it. */
  tokenPageUrl: string;
  /** The move's words were copied for an agent. */
  onHanded(): void;
}) {
  const { t } = useTranslation("importDialog");
  const { busy, connection, connectionsRead, forgetSavedConnection, notionError, savedConnection, savedConnectionRejected, setTokenValue, token } = controller;
  const saved = connection ?? savedConnection;
  // Why "토큰 저장" failed — refused, Notion not answering, not saved — shows
  // under the token's field, the two brought into view together, and the
  // field takes the focus back to put the token right.
  const failure = phase === "token" ? notionError : "";
  const failureId = useId();
  const field = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  useLayoutEffect(() => {
    if (failure && !busy) input.current?.focus({ preventScroll: true });
  }, [busy, failure]);
  // Until the saved connections are read, neither the token's step nor the saved one shows.
  if (phase !== "start" && !connectionsRead) return <div className={own.agentFirst} data-notion-entry="reading" />;
  if (phase === "start") {
    return (
      <div className={own.agentFirst} data-notion-agent-first data-notion-entry="start">
        <BackupOffLine session={session} place="import" />
        <NotionAgentHandoff session={session} workspaceId={workspaceId} step="start" body={t("handoff.body")}
          move={(origin) => t("handoff.start", { origin, id: workspaceId, firstWords: t("handoff.firstWords"), interpolation: { escapeValue: false } })}
          copiedMove={t("handoff.copiedStart")} onHanded={onHanded} />
      </div>
    );
  }
  if (phase === "saved" && saved) {
    return (
      <div className={own.agentFirst} data-notion-entry="saved">
        <div className={own.saved} data-notion-import-selected-connection>
          <strong><CheckCircleFillIcon size="small" aria-hidden="true" />{t("savedTo", { name: notionConnectionName(t, saved.label) })}</strong>
          {byAgent ? <p data-notion-agent-goes-on>{t("handoff.agentGoesOn")}</p> : null}
          <button type="button" className={own.directLink} data-notion-import-new-token disabled={busy} onClick={forgetSavedConnection}>
            {t("useAnotherToken")}
          </button>
        </div>
      </div>
    );
  }
  // A saved connection whose token Notion no longer takes asks for a new one in its place.
  const rejected = Boolean(saved) && !connection && savedConnectionRejected;
  return (
    <div className={own.agentFirst} data-notion-entry="token" data-notion-import-reconnect={rejected || undefined}>
      {rejected ? (
        <p className={own.head}>
          <strong>{t("reconnectTo", { name: notionConnectionName(t, saved?.label) })}</strong>
          {t("savedTokenRejected")}
        </p>
      ) : byAgent ? <p className={own.head} data-notion-token-stays>{t("handoff.tokenStays")}</p> : null}
      <div className={styles.notionForm} data-notion-steps>
        <div className={styles.tokenIntro}>
          <strong>{t("notionSteps.title")}</strong>
          <a className={styles.tokenPageLink} href={tokenPageUrl} target="_blank" rel="noopener noreferrer" data-notion-token-page>
            {t("openTokenPage")}
          </a>
        </div>
        <ol className={own.steps}>
          {(t("notionSteps.items", { returnObjects: true }) as string[]).map((item) => <li key={item}>{item}</li>)}
        </ol>
        <p>{t("notionSteps.token")}</p>
        <div ref={field} className={own.tokenField}>
          <label className={styles.field}>
            <span>{t("tokenLabel")}</span>
            {/* The step's one field takes the focus as it comes, by a press or by the agent's link. */}
            <input ref={input} className={styles.maskedTokenInput} type="text" autoComplete="off" autoCapitalize="none" spellCheck={false} autoFocus
              placeholder={t("tokenPlaceholder")} value={token} disabled={busy} aria-invalid={failure ? true : undefined}
              aria-describedby={failure ? failureId : undefined} onChange={(event) => setTokenValue(event.target.value)} />
          </label>
          <NotionFailureLine text={failure} id={failureId} className={own.fieldFailure} around={field} />
        </div>
      </div>
      <NotionTokenGuide />
    </div>
  );
}
