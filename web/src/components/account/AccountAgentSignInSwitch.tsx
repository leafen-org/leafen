import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { SwitchMark } from "@/components/Switch";
import { agentSignInApi } from "@/lib/agentSignInApi";
import { useShownChanges } from "@/lib/shownChanges";
import type { Session } from "@/types";
import { styles } from "./accountSettingsStyles";

/**
 * "에이전트로 로그인 승인" (user decision 2026-11-16): whether an agent
 * connected as this person may approve the code on a sign-in screen,
 * signing a new device in. On until the person turns it off; only their own
 * sign-in changes it (an agent can't turn it back on). It shows its new
 * value at once, as the account's other switches do (lib/shownChanges.ts).
 */
export function AccountAgentSignInSwitch({ session }: { session: Session }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [failed, setFailed] = useState(false);
  const pending = useShownChanges<{ allowed: boolean }>();
  useEffect(() => {
    const reading = new AbortController();
    agentSignInApi.readSetting(session, reading.signal).then((setting) => setAllowed(setting.allowed), () => undefined);
    return () => reading.abort();
  }, [session]);
  const shown = pending.shown.allowed ?? allowed;
  const change = (next: boolean) => {
    setFailed(false);
    void pending.change({ allowed: next }, () => agentSignInApi.setSetting(session, next))
      .then((setting) => setAllowed(setting.allowed), () => setFailed(true));
  };
  return (
    <>
      <div className={styles.securityRow} data-account-agent-sign-in>
        <div className={styles.securityInstruction}>
          <strong>{t("agentSignInSetting")}</strong>
          <span>{t("agentSignInSettingDesc")}</span>
        </div>
        <button type="button" role="switch" aria-checked={shown === true} aria-label={t("agentSignInSetting")}
          disabled={allowed === null} data-waiting-note={pending.note("allowed")} onClick={() => change(!shown)}>
          <SwitchMark on={shown === true} />
        </button>
      </div>
      {failed ? <div className={styles.notice} role="alert">{t("agentSignInSettingFailed")}</div> : null}
    </>
  );
}
