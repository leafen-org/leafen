import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Copy } from "@/icons/leafen";
import { type AgentSignInStart, agentSignInApi, isAgentSignInWait } from "@/lib/agentSignInApi";
import { authErrorLine, sayAuthLine, type AuthText } from "@/lib/authErrorPresentation";
import { copyText } from "@/lib/copyText";
import { thisWindowKey } from "@/lib/thisWindowWords";
import type { Session } from "@/types";
import styles from "../AuthGate.module.css";
import agent from "./AgentSignInStep.module.css";
import { isSessionCapacity } from "./authGateRounds";

type Phase = "asking" | "waiting" | "declined" | "expired" | "capacity" | "failed";

// How long "복사했습니다" stays on the button.
const COPIED_FOR = 2_000;
// A poll the server didn't answer is tried again, more slowly.
const LONGEST_WAIT = 15_000;

/**
 * "AI로 로그인", once "에이전트로 로그인" (user decisions 2026-11-16 and
 * 2026-11-22): a short code for the person
 * to give an agent already connected to Leafen, in the sentence to send it;
 * the agent shows them this browser — or Leafen's own app — and approves it,
 * and the one that asked signs in. The code lasts five minutes and is used
 * once: run out or declined, a new one is a press away; leaving the step gives the
 * request up. The person usually waits in the agent's window, so a hidden tab
 * goes on asking, and asks at once as it comes back.
 *
 * The step says it plainly (user decision 2026-11-22: "아래 문장을 복사"
 * named a sentence the screen didn't show): "AI로 로그인", one line, the
 * sentence itself large with one copy button beside it — the code in it, kept
 * whole — and how long and how often it works in a small line under it.
 */
export function AgentSignInStep({ busy, onSession, back }: {
  busy: boolean;
  onSession(session: Session): void;
  back(): void;
}) {
  const { t } = useTranslation(["authGate", "common"]);
  const [start, setStart] = useState<AgentSignInStart | null>(null);
  const [phase, setPhase] = useState<Phase>("asking");
  const [error, setError] = useState<AuthText>("");
  const [copied, setCopied] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const pending = useRef<string>("");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const round = useRef(0);
  // The page's way in, read as the answer comes: the step asks once as it opens.
  const signIn = useRef(onSession);
  signIn.current = onSession;

  const poll = useCallback(async (token: string, at: number, replaceOldestSession = false, wait = 0) => {
    clearTimeout(timer.current);
    const again = (after: number) => {
      timer.current = setTimeout(() => void poll(token, at, false, after), after);
    };
    try {
      const answer = await agentSignInApi.poll(token, replaceOldestSession);
      if (round.current !== at) return;
      setNow(Date.now());
      if (!isAgentSignInWait(answer)) {
        pending.current = "";
        signIn.current(answer);
        return;
      }
      if (answer.status === "pending") return again(2_000);
      pending.current = "";
      setPhase(answer.status);
    } catch (caught) {
      if (round.current !== at) return;
      if (isSessionCapacity(caught)) {
        setPhase("capacity");
        return;
      }
      if ((caught as { status?: unknown } | null)?.status) {
        // Used or unknown: the code can't be taken any more.
        pending.current = "";
        setPhase("expired");
        return;
      }
      again(Math.min(LONGEST_WAIT, (wait || 2_000) * 2));
    }
  }, []);

  const ask = useCallback(async () => {
    const at = ++round.current;
    clearTimeout(timer.current);
    if (pending.current) void agentSignInApi.cancel(pending.current).catch(() => undefined);
    pending.current = "";
    setPhase("asking");
    setError("");
    setCopied(false);
    try {
      const started = await agentSignInApi.ask();
      if (round.current !== at) return;
      pending.current = started.token;
      setStart(started);
      setNow(Date.now());
      setPhase("waiting");
      timer.current = setTimeout(() => void poll(started.token, at), started.interval * 1000);
    } catch (caught) {
      if (round.current !== at) return;
      setStart(null);
      setPhase("failed");
      setError(authErrorLine(caught, "auth.deviceCode"));
    }
  }, [poll]);

  useEffect(() => {
    void ask();
    const comeBack = () => {
      if (document.visibilityState === "visible" && pending.current) void poll(pending.current, round.current);
    };
    document.addEventListener("visibilitychange", comeBack);
    return () => {
      document.removeEventListener("visibilitychange", comeBack);
      round.current += 1;
      clearTimeout(timer.current);
      if (pending.current) void agentSignInApi.cancel(pending.current).catch(() => undefined);
      pending.current = "";
    };
  }, [ask, poll]);

  useEffect(() => {
    if (!copied) return undefined;
    const done = setTimeout(() => setCopied(false), COPIED_FOR);
    return () => clearTimeout(done);
  }, [copied]);

  const sentence = start ? t("authGate:agentSignInSentence", { code: start.code }) : "";
  const minutes = start ? Math.max(1, Math.ceil((start.expiresAt - now) / 60_000)) : 0;
  const over = phase === "declined" || phase === "expired" || phase === "failed";
  const shown = start && !over ? start.code : "";
  // The sentence as it is sent, its code drawn whole: a marker stands for it.
  const [before, after = ""] = t("authGate:agentSignInSentence", { code: "\u0001" }).split("\u0001");
  return (
    <div className={styles.form} data-auth-agent-sign-in-step={phase}>
      <div className={styles.notice}>
        <strong>{t("authGate:agentSignInTitle")}</strong>
        {/* "이 브라우저가" or, in Leafen's own app window, "이 앱이" (thisWindowWords.ts). */}
        <p>{t(thisWindowKey("authGate:agentSignInBody", "authGate:agentSignInBodyApp"))}</p>
      </div>
      <div className={agent.sentence} data-agent-sign-in-code={shown || "none"}>
        <p className={agent.words}>{before}<span className={agent.code}>{shown || "···-···-···"}</span>{after}</p>
        {/* Drawn with its code, it takes the focus then: Enter copies. */}
        {shown && phase !== "capacity" ? (
          <button className={`${styles.secondary} ${styles.withIcon} ${agent.copy}`} type="button" disabled={busy} autoFocus
            onClick={() => void copyText(sentence).then((done) => setCopied(done))} data-agent-sign-in-copy>
            <Copy size="small" aria-hidden="true" />
            {/* Both words hold the button's width, so "copied" doesn't move the sentence. */}
            <span className={agent.labels}>
              <span data-shown={!copied}>{t("authGate:agentSignInCopy")}</span>
              <span data-shown={copied}>{t("authGate:agentSignInCopied")}</span>
            </span>
          </button>
        ) : null}
      </div>
      {phase === "capacity" ? (
        <div className={styles.notice} data-auth-agent-capacity>
          <strong>{t("authGate:sessionCapacityTitle")}</strong>
          <p>{t("authGate:sessionCapacityBody")}</p>
          <button className={styles.primary} type="button" disabled={busy}
            onClick={() => { if (start) void poll(start.token, round.current, true); }}>
            {t("authGate:replaceOldestSession")}
          </button>
        </div>
      ) : over ? (
        <>
          <p className={`${styles.error} ${styles.centered}`} role="alert">
            {phase === "failed" ? sayAuthLine(error) : t(phase === "declined" ? "authGate:agentSignInDeclined" : "authGate:agentSignInExpired")}
          </p>
          <button className={styles.primary} type="button" disabled={busy} onClick={() => void ask()} autoFocus data-agent-sign-in-again>
            {t("authGate:agentSignInNewCode")}
          </button>
        </>
      ) : (
        <p className={`${styles.fieldHelp} ${styles.centered}`} role="status" aria-live="polite">
          {start ? t("authGate:agentSignInWaiting", { count: minutes }) : t("authGate:agentSignInAsking")}
        </p>
      )}
      <button className={styles.linkButton} type="button" onClick={back} disabled={busy}>{t("authGate:back")}</button>
    </div>
  );
}
