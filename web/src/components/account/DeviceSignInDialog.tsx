import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { AuthSession } from "@/authWire.generated";
import dialogs from "@/components/settings/DeveloperSettings.module.css";
import { SettingsFormDialog } from "@/components/settings/SettingsFormDialog";
import type { Session } from "@/types";
import { AccountAddDevice } from "./AccountAddDevice";

const SESSIONS_REREAD_MS = 3_000;

/**
 * "새 기기 로그인" from the workspace menu (2026-11-12: the code lay three
 * levels down, Settings → 계정 → 새 기기 등록): the account's QR and
 * one-time code at once, as Settings › 계정 shows them, saying so when the
 * new device has signed in. Its own read of the account's sign-ins tells.
 */
export function DeviceSignInDialog({ session, onClose }: { session: Session; onClose: () => void }) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const done = useRef<HTMLButtonElement>(null);
  const sessions = useAccountSessions(session);
  return (
    <SettingsFormDialog title={t("workspaceSettingsDialog:addDevice")} busy={false} wide initialFocus={done} onClose={onClose}>
      <div className={dialogs.dialogBody} data-device-sign-in-window>
        <p className={dialogs.dialogNote}>{t("workspaceSettingsDialog:addDeviceDesc")}</p>
        <AccountAddDevice session={session} sessions={sessions} standing />
        <div className={dialogs.dialogActions}>
          <button ref={done} type="button" className={dialogs.primary} onClick={onClose}>{t("workspaceSettingsDialog:addDeviceClose")}</button>
        </div>
      </div>
    </SettingsFormDialog>
  );
}

function useAccountSessions(session: Session): AuthSession[] | null {
  const [sessions, setSessions] = useState<AuthSession[] | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    const read = () => {
      if (document.visibilityState === "hidden") return;
      void api.listAuthSessions(session, controller.signal).then((list) => {
        if (!controller.signal.aborted) setSessions(list);
      }, () => undefined);
    };
    read();
    const timer = window.setInterval(read, SESSIONS_REREAD_MS);
    return () => { controller.abort(); window.clearInterval(timer); };
  }, [session]);
  return sessions;
}
