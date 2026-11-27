import { SwitchMark } from "@/components/Switch";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { useShownChanges } from "@/lib/shownChanges";
import type { Session, Workspace } from "@/types";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";

/**
 * "애널리틱스" under 일반, as Notion's (seen 2026-10-05): whether the
 * workspace keeps and shows page views. Below it, Leafen's own count of a
 * public page's anonymous visitors, which a page's analytics tab offered
 * before; a page's tab offers neither, as Notion's. A switch turns as it is
 * pressed and is put back with why if the server refuses (lib/shownChanges.ts).
 */
export function WorkspacePageViewAnalytics({ session, workspace, onWorkspaceChange }: {
  session: Session;
  workspace: Workspace;
  onWorkspaceChange: (workspace: Workspace) => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [error, setError] = useState("");
  const pending = useShownChanges<{ analyticsEnabled: boolean; anonymousTrackingEnabled: boolean }>();
  // The workspace as it is when a change goes, not as it was pressed.
  const latest = useRef(workspace);
  latest.current = workspace;
  const enabled = pending.shown.analyticsEnabled ?? workspace.pageAnalyticsEnabled !== false;
  const anonymous = pending.shown.anonymousTrackingEnabled ?? workspace.pageAnalyticsAnonymousTrackingEnabled !== false;

  function change(next: { analyticsEnabled: boolean; anonymousTrackingEnabled: boolean }) {
    setError("");
    void pending.change(next, async () => {
      const saved = await api.updatePageAnalyticsPolicy(session, { workspaceId: workspace.id, ...next });
      onWorkspaceChange({
        ...latest.current,
        pageAnalyticsEnabled: saved.analyticsEnabled,
        pageAnalyticsAnonymousTrackingEnabled: saved.anonymousTrackingEnabled,
      });
    }).catch((failure: unknown) => setError(settingsErrorMessage(failure, t("errUpdatePageViewAnalytics"))));
  }

  return (
    // Under the general settings' "애널리틱스" heading, as Notion's (seen 2026-10-11).
    <div data-workspace-page-view-analytics>
      <div className={settingsStyles.securityRow}>
        <div className={settingsStyles.securityInstruction}>
          <strong>{t("pageViewAnalyticsField")}</strong>
          <span>{t("pageViewAnalyticsDescription", { workspace: workspace.name })}</span>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={enabled}
          aria-label={t("pageViewAnalyticsField")}
          data-waiting-note={pending.note("analyticsEnabled")}
          onClick={() => change({ analyticsEnabled: !enabled, anonymousTrackingEnabled: anonymous })}
        >
          <SwitchMark on={enabled} />
        </button>
      </div>
      <div className={settingsStyles.securityRow}>
        <div className={settingsStyles.securityInstruction}>
          <strong>{t("pageViewAnonymousField")}</strong>
          <span>{t("pageViewAnonymousDescription")}</span>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={enabled && anonymous}
          aria-label={t("pageViewAnonymousField")}
          disabled={!enabled}
          data-waiting-note={pending.note("anonymousTrackingEnabled")}
          onClick={() => change({ analyticsEnabled: enabled, anonymousTrackingEnabled: !anonymous })}
        >
          <SwitchMark on={enabled && anonymous} />
        </button>
      </div>
      {error ? <div className={settingsStyles.errorText} role="alert">{error}</div> : null}
    </div>
  );
}
