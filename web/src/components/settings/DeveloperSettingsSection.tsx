import { SwitchMark } from "@/components/Switch";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { saveDisplayPreferences, useDisplayPreferences } from "@/lib/displayPreferences";
import { readWorkspaceConnectionPolicies } from "@/lib/workspaceConnectionPolicies";
import { listPersonalAccessTokens, type PersonalAccessToken } from "@/lib/personalAccessTokens";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { useShownChanges } from "@/lib/shownChanges";
import type { Session, Workspace } from "@/types";
import styles from "./DeveloperSettings.module.css";
import { EventWebhookSection } from "./EventWebhookSection";
import { PersonalAccessTokenDialog } from "./PersonalAccessTokenDialog";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";
import { ListNotice } from "./SettingsRowNotice";
import { developerToolsHref, pushRoute } from "@/lib/router";

// How few working tokens left before the limit is said.
const TOKENS_NOTICE_LEFT = 5;

/**
 * The features' "개발자" item, as Notion's (seen 2026-10-02, 2026-10-30):
 * "개발자 UI", whose switch the account keeps and which shows the developer
 * bar and the page menu's "개발자" (developerMode.ts), and "새 토큰" for a
 * personal access token for the open workspace, whose list is the
 * developer tools' (/developers/tokens), as Notion's "토큰은 여기에서
 * 관리하세요" says. The webhooks stay here.
 */
export function DeveloperSettingsSection({ session, workspace, onNavigate }: {
  session: Session; workspace: Workspace;
  /** Closes the settings on the developer tools a link opens. */
  onNavigate?: (path: string) => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [tokens, setTokens] = useState<PersonalAccessToken[] | null>(null);
  const [activeLimit, setActiveLimit] = useState(0);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const display = useDisplayPreferences();
  // The switch turns as it is pressed and is put back with why if the server refuses (lib/shownChanges.ts).
  const pending = useShownChanges<{ developerMode: boolean }>();
  const developerMode = pending.shown.developerMode ?? display.developerMode;
  const [developerError, setDeveloperError] = useState("");
  // A workspace may keep new tokens to its owners; until that is read (or
  // when its read failed) no token is made from here.
  const [ownersOnly, setOwnersOnly] = useState<boolean | null>(null);

  // A read that failed (the tokens, or who may make one) is said with a way to read both again.
  const [readTurn, setReadTurn] = useState(0);
  useEffect(() => {
    setTokens(null);
    setError("");
    setOwnersOnly(null);
    const controller = new AbortController();
    readWorkspaceConnectionPolicies(session, workspace.id, controller.signal)
      .then((policies) => setOwnersOnly(!policies.canMakeTokens))
      .catch((failure) => {
        if (!controller.signal.aborted) setError(settingsErrorMessage(failure, t("connectionPoliciesLoadFailed")));
      });
    listPersonalAccessTokens(session, workspace.id, controller.signal)
      .then((result) => {
        setTokens(result.items);
        setActiveLimit(result.activeLimit);
      })
      .catch((failure) => {
        if (!controller.signal.aborted) setError(settingsErrorMessage(failure, t("personalAccessTokenLoadFailed")));
      });
    return () => controller.abort();
  }, [readTurn, session, workspace.id, t]);

  function changeDeveloperMode() {
    const next = !developerMode;
    setDeveloperError("");
    void pending.change({ developerMode: next }, () => saveDisplayPreferences(session, { developerMode: next }))
      .catch((failure: unknown) => setDeveloperError(settingsErrorMessage(failure, t("developerModeFailed"))));
  }

  // The limit stays out of sight until a few working tokens are left.
  const active = tokens?.filter((token) => token.status === "active").length ?? 0;
  const tokensFull = activeLimit > 0 && active >= activeLimit;
  const tokensNear = activeLimit > 0 && !tokensFull && activeLimit - active <= TOKENS_NOTICE_LEFT;
  return (
    <div data-developer-settings>
      <section className={settingsStyles.section} role="region" aria-label={t("developerUiSection")}>
        <div className={settingsStyles.sectionTitle}>{t("developerUiSection")}</div>
        <div className={settingsStyles.securityRow}>
          <div className={settingsStyles.securityInstruction}>
            <strong>{t("developerModeField")}</strong>
            <span>{t("developerModeDescription")}</span>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={developerMode}
            aria-label={t("developerModeField")}
            data-waiting-note={pending.note("developerMode")}
            data-developer-mode-switch
            onClick={changeDeveloperMode}
          >
            <SwitchMark on={developerMode} />
          </button>
        </div>
        {developerError ? <div className={settingsStyles.errorText} role="alert">{developerError}</div> : null}
      </section>
      <section className={settingsStyles.section} role="region" aria-label={t("personalAccessTokensSection")} data-settings-part="tokens">
        <div className={settingsStyles.sectionTitle}>{t("personalAccessTokensSection")}</div>
        <div className={settingsStyles.securityRow}>
          <div className={settingsStyles.securityInstruction}>
            <strong>{t("personalAccessTokenCreateTitle")}</strong>
            <span>{t("personalAccessTokenCreateDescription")}{" "}
              <a href={developerToolsHref("tokens")} className={styles.link} data-developer-tokens-link
                onClick={(event) => { event.preventDefault(); (onNavigate ?? pushRoute)(developerToolsHref("tokens")); }}>
                {t("developerTools.manageTokens")}</a>.</span>
          </div>
          <button type="button" className={settingsStyles.secondaryButton} disabled={ownersOnly !== false || tokensFull} onClick={() => setCreating(true)}>
            {t("personalAccessTokenNew")}
          </button>
        </div>
        {ownersOnly ? <p className={styles.note} data-personal-access-tokens-owners-only>{t("personalAccessTokenOwnersOnly")}</p> : null}
        {ownersOnly === false && (tokensFull || tokensNear) ? (
          <p className={styles.note} data-personal-access-tokens-capacity={tokensFull ? "full" : "near"}>
            {tokensFull
              ? t("personalAccessTokenFull", { limit: activeLimit })
              : t("personalAccessTokenNear", { left: activeLimit - active, limit: activeLimit })}
          </p>
        ) : null}
        {error ? <ListNotice role="alert" onRetry={() => { setError(""); setReadTurn((turn) => turn + 1); }} data-developer-read-failed>{error}</ListNotice> : null}
      </section>
      <EventWebhookSection session={session} workspace={workspace} />
      {creating ? (
        <PersonalAccessTokenDialog
          session={session}
          workspaceId={workspace.id}
          onCreated={(token) => setTokens((current) => [token, ...(current ?? [])])}
          onClose={() => setCreating(false)}
        />
      ) : null}
    </div>
  );
}
