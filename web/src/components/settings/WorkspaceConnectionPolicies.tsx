import { SwitchMark } from "@/components/Switch";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { X } from "@/components/icons";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import {
  changeWorkspaceConnectionPolicies,
  readWorkspaceConnectionPolicies,
  type WorkspaceConnectionPolicies,
  type WorkspaceConnectionPolicyChange,
} from "@/lib/workspaceConnectionPolicies";
import type { Session, Workspace } from "@/types";
import styles from "./SettingsItemRows.module.css";
import tags from "./WorkspaceAllowedEmailDomains.module.css";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";
import { ListNotice } from "./SettingsRowNotice";
import { AppSelect } from "@/components/AppSelect";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * The "관리" tab's "사용 권한 관리하기", as Notion's (seen 2026-10-02): who
 * may make personal access tokens — every member or the workspace's owners
 * only, or the owners and chosen groups (the organization's), a member's
 * tokens stopping once the member may not — and whether automations and
 * buttons may send webhooks — and whether members connect their agents to
 * custom MCP servers, and to which (seen 2026-10-13: on by default; the list
 * of servers is what Notion's help describes). Notion's limits on installed
 * and internal connections aren't Leafen's.
 */
export function WorkspaceConnectionPoliciesSection({ session, workspace, headingClassName }: {
  session: Session; workspace: Workspace;
  /** The heading's look where the section sits (the connections 관리 tab's, as Notion's). */
  headingClassName?: string;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [policies, setPolicies] = useState<WorkspaceConnectionPolicies | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [readTurn, setReadTurn] = useState(0);

  useEffect(() => {
    setPolicies(null);
    const controller = new AbortController();
    readWorkspaceConnectionPolicies(session, workspace.id, controller.signal)
      .then(setPolicies)
      .catch((failure) => {
        if (!controller.signal.aborted) setError(settingsErrorMessage(failure, t("connectionPoliciesLoadFailed")));
      });
    return () => controller.abort();
  }, [readTurn, session, t, workspace.id]);

  async function change(next: WorkspaceConnectionPolicyChange) {
    setBusy(true);
    setError("");
    try {
      const changed = await changeWorkspaceConnectionPolicies(session, workspace.id, next);
      setPolicies((current) => ({ ...changed, groupChoices: current?.groupChoices }));
      return true;
    } catch (failure) {
      setError(next.customMcpAllowlist ? t("customMcpAllowlistInvalid") : settingsErrorMessage(failure, t("connectionPoliciesChangeFailed")));
      return false;
    } finally {
      setBusy(false);
    }
  }

  // Not read yet: what failed, with a way to read it again.
  if (!policies) return error ? <ListNotice role="alert" onRetry={() => { setError(""); setReadTurn((turn) => turn + 1); }} data-connection-policies-read-failed>{error}</ListNotice> : null;
  const tokens = policies.personalAccessTokens;
  return (
    <section className={settingsStyles.section} role="region" aria-label={t("connectionPoliciesTitle")} data-connection-policies>
      <div className={headingClassName ? `${settingsStyles.sectionTitle} ${headingClassName}` : settingsStyles.sectionTitle}>{t("connectionPoliciesTitle")}</div>
      <div className={styles.rowStack}>
        <div className={settingsStyles.securityRow}>
          <div className={settingsStyles.securityInstruction}>
            <strong>{t("customMcpServersField")}</strong>
            <span>{t("customMcpServersDescription")}</span>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={policies.customMcpServers}
            aria-label={t("customMcpServersField")}
            disabled={busy || !policies.canManage}
            data-custom-mcp-servers
            onClick={() => void change({ customMcpServers: !policies.customMcpServers })}
          >
            <SwitchMark on={policies.customMcpServers} />
          </button>
        </div>
        {policies.customMcpServers ? (
          <CustomMcpAllowlist allowlist={policies.customMcpAllowlist} disabled={busy || !policies.canManage}
            onChange={(customMcpAllowlist) => change({ customMcpAllowlist })} onEdit={() => setError("")}
            refused={error === t("customMcpAllowlistInvalid")} />
        ) : null}
        <div className={settingsStyles.securityRow}>
          <div className={settingsStyles.securityInstruction}>
            <strong>{t("tokenCreatorsField")}</strong>
            <span>{t(tokens === "owners" ? "tokenCreatorsOwnersDescription"
              : tokens === "owners_and_groups" ? "tokenCreatorsGroupsDescription" : "tokenCreatorsMembersDescription")}</span>
          </div>
          <AppSelect
            aria-label={t("tokenCreatorsField")}
            value={tokens}
            disabled={busy || !policies.canManage}
            data-token-creators
            onChange={(event) => void change({ personalAccessTokens: event.target.value as WorkspaceConnectionPolicies["personalAccessTokens"] })}
          >
            <option value="members">{t("policyAllMembers")}</option>
            <option value="owners">{t("policyOwnersOnly")}</option>
            <option value="owners_and_groups">{t("policyOwnersAndGroups")}</option>
          </AppSelect>
        </div>
        {tokens === "owners_and_groups" ? (
          <fieldset className={styles.groups} aria-label={t("tokenCreatorsGroups")} data-token-creator-groups>
            <legend>{t("tokenCreatorsGroups")}</legend>
            {(policies.groupChoices ?? []).length === 0 ? <span>{t("tokenCreatorsNoGroups")}</span> : null}
            {(policies.groupChoices ?? []).map((group) => {
              const chosen = policies.personalAccessTokenGroups.includes(group.id);
              return (
                <label key={group.id}>
                  <input
                    type="checkbox"
                    checked={chosen}
                    disabled={busy || !policies.canManage}
                    onChange={() => void change({
                      personalAccessTokenGroups: chosen
                        ? policies.personalAccessTokenGroups.filter((id) => id !== group.id)
                        : [...policies.personalAccessTokenGroups, group.id],
                    })}
                  />
                  <span>{group.name}</span>
                </label>
              );
            })}
          </fieldset>
        ) : null}
        <div className={settingsStyles.securityRow}>
          <div className={settingsStyles.securityInstruction}>
            <strong>{t("automationWebhooksField")}</strong>
            <span>{t("automationWebhooksDescription")}</span>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={policies.automationWebhooks}
            aria-label={t("automationWebhooksField")}
            disabled={busy || !policies.canManage}
            data-automation-webhooks
            onClick={() => void change({ automationWebhooks: !policies.automationWebhooks })}
          >
            <SwitchMark on={policies.automationWebhooks} />
          </button>
        </div>
        {error ? <div id="connection-policies-error" className={settingsStyles.errorText} role="alert">{error}</div> : null}
      </div>
    </section>
  );
}

/** The custom MCP servers members may connect, as tags in one field. */
function CustomMcpAllowlist({ allowlist, disabled, onChange, onEdit, refused }: {
  allowlist: string[]; disabled: boolean; onChange(next: string[]): Promise<boolean>;
  /** Typing in the field puts away what the last change said about it. */
  onEdit(): void;
  /** The address typed was refused: the field is marked with the section's red line. */
  refused: boolean;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [draft, setDraft] = useState("");
  async function add() {
    const url = draft.trim();
    if (!url) return;
    if (await onChange([...allowlist, url])) setDraft("");
  }
  return (
    <div className={settingsStyles.securityPanel} data-custom-mcp-allowlist>
      <div className={settingsStyles.securityInstruction}>
        <strong>{t("customMcpAllowlistField")}</strong>
        <span>{t("customMcpAllowlistDescription")}</span>
      </div>
      <div className={tags.domainField}>
        {allowlist.map((url) => (
          <span key={url} className={tags.domainTag} data-custom-mcp-allowed={url}>
            {url}
            <button type="button" aria-label={t("customMcpAllowlistRemove", { url })} disabled={disabled}
              onClick={() => void onChange(allowlist.filter((entry) => entry !== url))}>
              <X size="tiny" aria-hidden="true" />
            </button>
          </span>
        ))}
        <input
          value={draft}
          aria-label={t("customMcpAllowlistField")}
          placeholder={t("customMcpAllowlistPlaceholder")}
          spellCheck={false}
          autoCapitalize="none"
          disabled={disabled}
          onChange={(event) => { setDraft(event.target.value); onEdit(); }}
          onKeyDown={(event) => {
            if (!fieldEnter(event)) return;
            event.preventDefault();
            void add();
          }}
          {...(refused ? { "aria-invalid": true as const, "aria-describedby": "connection-policies-error" } : {})}
        />
      </div>
    </div>
  );
}
