import { SwitchMark } from "@/components/Switch";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { settingsErrorMessage, writeUncertainMark } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { styles } from "@/components/settings/workspaceSettingsOverviewStyles";

type GuestMFAWorkspace = { id: string; name: string; required: boolean };
/** A window of the workspaces, how many of all require it, and the next window's cursor. */
type GuestMFA = { workspaces: GuestMFAWorkspace[]; required: number; nextCursor?: string };

const guestMFAPath = (organizationId: string) => `/organizations/${encodeURIComponent(organizationId)}/guest-mfa`;

/**
 * Notion's MFA enforcement (its help, read 2026-10-10): the organization's
 * owner chooses which workspaces require their guests to use 2-step
 * verification; members are not asked.
 */
export function OrganizationGuestMFA({ session, organizationId }: { session: Session; organizationId: string }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [workspaces, setWorkspaces] = useState<GuestMFAWorkspace[] | null>(null);
  const [requiredCount, setRequiredCount] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Every window of the organization's workspaces, each shown as it comes.
  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      let read: GuestMFAWorkspace[] = [];
      let cursor = "";
      do {
        const value = await request<GuestMFA>(guestMFAPath(organizationId) + (cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""),
          { signal: controller.signal }, session);
        if (controller.signal.aborted) return;
        read = [...read, ...value.workspaces];
        setWorkspaces(read);
        setRequiredCount(value.required);
        cursor = value.nextCursor ?? "";
      } while (cursor);
    })().catch((failure) => {
      if (!controller.signal.aborted) setError(settingsErrorMessage(failure, t("guestMFAFailed")));
    });
    return () => controller.abort();
  }, [organizationId, session, t]);

  async function change(workspaceId: string, required: boolean) {
    if (!workspaces) return;
    setBusy(true);
    setError("");
    try {
      const next = await request<GuestMFA>(guestMFAPath(organizationId), {
        method: "PUT", body: JSON.stringify({ workspaceId, required }),
      }, session);
      const changed = new Map(next.workspaces.map((workspace) => [workspace.id, workspace]));
      setWorkspaces((current) => (current ?? []).map((workspace) => changed.get(workspace.id) ?? workspace));
      setRequiredCount(next.required);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("guestMFAFailed")));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={styles.organizationPolicyBlock} data-guest-mfa>
      <div className={styles.organizationSubhead}>
        <span>{t("guestMFATitle")}</span>
        {workspaces ? <strong>{requiredCount}</strong> : null}
      </div>
      <p className={styles.organizationMeta}>{t("guestMFADescription")}</p>
      {workspaces ? (
        <div className={styles.sharingPolicyGrid} aria-busy={busy}>
          {workspaces.map((workspace) => (
            <button
              key={workspace.id}
              type="button"
              className={styles.sharingPolicyToggle}
              aria-pressed={workspace.required}
              disabled={busy}
              onClick={() => void change(workspace.id, !workspace.required)}
            >
              <span>{workspace.name}</span>
              <SwitchMark on={workspace.required} />
            </button>
          ))}
        </div>
      ) : null}
      {/* Told under the switches, not above them: a box there moved what was pressed (2026-11-06). */}
      {error ? <div className={styles.notice} role="alert" {...writeUncertainMark(error)}>{error}</div> : null}
    </div>
  );
}
