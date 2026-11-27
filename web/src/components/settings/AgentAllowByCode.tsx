import { useState } from "react";
import { createPortal } from "react-dom";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { LeafenApiError, request } from "@/apiTransport";
import { ConfirmCard } from "@/components/ConfirmActionDialog";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { styles } from "./workspaceSettingsOverviewStyles";

type Waiting = { code: string; name: string; question: string; effect: string };

// Why the server refused a code or an answer, by the cause it named beside
// its code (mcp_device_connection_refusal.go), in the reader's words — never
// its English (2026-11-23: every refusal read "Couldn't check the code.").
const REFUSED: Record<string, string> = {
  codeUnknown: "agents.allowCodeWrong",
  tooManyWrongCodes: "agents.allowCodeRefused.tooManyWrongCodes",
  noWorkspace: "agents.allowCodeRefused.noWorkspace",
  decidedMeanwhile: "agents.allowCodeRefused.decidedMeanwhile",
  organizationKeysOff: "agents.allowCodeRefused.organizationKeysOff",
  organizationKeyDays: "agents.allowCodeRefused.organizationKeyDays",
  agentNotApproved: "agents.allowCodeRefused.agentNotApproved",
  connectionsFull: "settingsErrors:backend.mcpConnectionsFull",
  httpsRequired: "settingsErrors:backend.mcpConnectionHttpsRequired",
  workspaceUnreachable: "agents.allowCodeRefused.workspaceUnreachable",
};

function refusalText(failure: unknown, t: TFunction) {
  const cause = failure instanceof LeafenApiError ? failure.causeKey : undefined;
  const key = cause ? REFUSED[cause] : undefined;
  return key ? t(key) : settingsErrorMessage(failure, t("agents.allowCodeFailed"));
}

/**
 * An agent on another computer asks to connect with no key and shows its
 * person a code (the user decision of 2026-11-13: Leafen is used in the
 * conversation with an agent). The person allows it in their conversation
 * with an agent already connected, or here — in the phone app's settings,
 * with no agent connected yet: the code shows which agent asked, from where,
 * and what it would reach, and the confirm is their yes. The server decides
 * before it shows done, as for any permission; the press shows at once.
 */
export function AgentAllowByCode({ session }: { session: Session }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [waiting, setWaiting] = useState<Waiting | null>(null);
  const [said, setSaid] = useState("");
  const [error, setError] = useState("");

  async function find() {
    const typed = code.trim();
    if (!typed || busy) return;
    setBusy(true);
    setError("");
    setSaid("");
    try {
      const found = await request<Omit<Waiting, "code">>("/mcp/device-connections/find",
        { method: "POST", body: JSON.stringify({ code: typed }) }, session);
      setWaiting({ ...found, code: typed });
    } catch (failure) {
      setError(refusalText(failure, t));
    } finally {
      setBusy(false);
    }
  }

  async function answer(asked: Waiting, allow: boolean) {
    setWaiting(null);
    setBusy(true);
    try {
      await request<void>("/mcp/device-connections/decide",
        { method: "POST", body: JSON.stringify({ code: asked.code, allow }) }, session);
      setCode("");
      setSaid(t(!allow ? "agents.allowCodeDeclined" : "agents.allowCodeAllowed",
        { name: asked.name }));
    } catch (failure) {
      setError(refusalText(failure, t));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={styles.securityRow} data-agent-allow-code>
      <div className={styles.securityInstruction}>
        <strong>{t("agents.otherComputer")}</strong>
        <span>{t("agents.otherComputerHint")}</span>
        <form className={styles.securityInlineForm} onSubmit={(event) => { event.preventDefault(); void find(); }}>
          <input value={code} inputMode="numeric" autoComplete="one-time-code" placeholder="1234-5678"
            aria-label={t("agents.allowCodeLabel")} onChange={(event) => setCode(event.target.value)} data-agent-allow-code-input />
          <button type="submit" className={styles.secondaryButton} aria-busy={busy} data-agent-allow-code-check>
            {busy ? t("agents.allowCodeChecking") : t("agents.allowCodeCheck")}
          </button>
        </form>
        {said ? <span role="status" data-agent-allow-code-said>{said}</span> : null}
        {error ? <div className={styles.errorText} role="alert">{error}</div> : null}
      </div>
      {waiting ? createPortal((
        // Only an answer closes it: "거절" declines, so the waiting agent hears so.
        <ConfirmCard required message={waiting.question} detail={waiting.effect}
          confirmLabel={t("agents.allowCodeAllow")} cancelLabel={t("agents.allowCodeDecline")}
          markers={{ dialog: { "data-agent-allow-code-ask": "" }, confirm: { "data-agent-allow-code-allow": "" } }}
          onAnswer={(choice) => void answer(waiting, choice !== null)} />
      ), document.body) : null}
    </div>
  );
}
