import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type {
  AdministratorAccountDevice, AdministratorAccountDevices as Devices, AdministratorAccountJoinKind, AdministratorManagedAccount,
} from "../../administratorWire.generated";
import { api } from "../../api";
import { DeviceMobileIcon, LaptopIcon } from "@/icons/leafen/deviceIcons";
import { activeDateLocale } from "@/lib/i18n";
import { newId } from "../../lib/ids";
import { userErrorText } from "@/lib/settingsErrors";
import type { Session } from "../../types";
import { sessionDevice } from "../account/AccountDevicesSection";
import devices from "../account/AccountDevices.module.css";
import { confirmAction } from "../ConfirmActionDialog";
import { ReadFailed } from "../ReadFailed";
import { administratorUserStyles as userStyles } from "./administratorUserManagementStyles";
import { accountChangedMeanwhile } from "./administratorAccountFailures";

const joinedVia: Record<AdministratorAccountJoinKind, string> = {
  workspace_email: "users.joinedViaWorkspaceEmail",
  page_email: "users.joinedViaPageEmail",
  workspace_name: "users.joinedViaWorkspaceName",
  workspace_link: "users.joinedViaWorkspaceLink",
  teamspace_link: "users.joinedViaTeamspaceLink",
  allowed_domain: "users.joinedViaAllowedDomain",
  page_link: "users.joinedViaPageLink",
};

function formatWhen(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long", timeStyle: "short" }).format(date);
}

/**
 * An account's way in and its devices, for the server's administrator
 * (account_joins.go): which invitation it came in by, from whom and when,
 * and each signed-in device with its last use and address, ended one at a
 * time at once (the multi-server decision's 2026-10-20 change).
 */
export function AdministratorAccountDevices({ session, account, onAccount, onStale }: {
  session: Session;
  account: AdministratorManagedAccount;
  onAccount(account: AdministratorManagedAccount): void;
  /** The account changed elsewhere meanwhile: the list reads it again (administratorAccountFailures.ts). */
  onStale(): void;
}) {
  const { t } = useTranslation("serverConsole");
  const { t: ts } = useTranslation("workspaceSettingsDialog");
  const [shown, setShown] = useState<Devices | null>(null);
  const [busy, setBusy] = useState("");
  const [said, setSaid] = useState("");
  const [failed, setFailed] = useState("");
  // A failed read of the way in and the devices, said where they show (in the
  // loading line's place until they were read once) with a way to read again;
  // a device that could not be ended is `failed`.
  const [readFailed, setReadFailed] = useState("");

  const load = useCallback((signal?: AbortSignal) => api.readAdministratorAccountDevices(session, account.id, signal)
    .then((read) => { setShown(read); setReadFailed(""); },
      (cause) => { if (!signal?.aborted) setReadFailed(userErrorText(cause, t("users.devicesFailed"))); }),
  [account.id, session, t]);
  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  async function revoke(device: AdministratorAccountDevice) {
    const name = sessionDevice(device.userAgent)?.name ?? ts("unknownDevice");
    if (!await confirmAction({ message: t("users.confirmRevokeDevice", { device: name, email: account.email || account.name }),
      detail: t("users.confirmRevokeDeviceDetail"), confirmLabel: t("users.revokeDevice"), danger: true })) return;
    setBusy(device.id);
    setFailed("");
    try {
      const result = await api.revokeAdministratorAccountSession(session, account.id, device.id, {
        requestId: newId(), expectedRevision: account.revision,
      });
      onAccount(result.account);
      setSaid(t("users.deviceRevoked"));
      await load();
    } catch (cause) {
      if (accountChangedMeanwhile(cause)) onStale();
      else setFailed(userErrorText(cause, t("users.actionFailed")));
    } finally {
      setBusy("");
    }
  }

  const join = shown?.join;
  const readAgain = readFailed ? <ReadFailed message={readFailed} onRetry={() => { setReadFailed(""); void load(); }} /> : null;
  return (
    <div className={userStyles.notice} data-tone="neutral" data-administrator-account-devices={account.id}>
      <div data-administrator-account-join>
        {join ? t(joinedVia[join.kind], {
          workspace: join.workspaceName || t("users.deletedWorkspace"),
          inviter: join.inviterName || t("users.unknownInviter"),
          when: formatWhen(join.joinedAt),
        }) : shown ? t("users.notInvited") : readAgain ?? t("users.loading")}
      </div>
      {shown?.devices.length ? (
        <table className={devices.devices}>
          <thead>
            <tr>
              <th scope="col">{ts("deviceName")}</th>
              <th scope="col">{ts("deviceLastActive")}</th>
              <th scope="col">{ts("deviceLocation")}</th>
              <th scope="col" aria-label={t("users.revokeDevice")} />
            </tr>
          </thead>
          <tbody>
            {shown.devices.map((device) => {
              const kind = sessionDevice(device.userAgent);
              const Icon = kind?.mobile ? DeviceMobileIcon : LaptopIcon;
              return (
                <tr key={device.id} data-administrator-account-device={device.id}>
                  <td title={device.userAgent || undefined}>
                    <span className={devices.deviceName}>
                      <Icon size="regular" aria-hidden="true" />
                      <span>
                        <span>{kind?.name ?? ts("unknownDevice")}</span>
                        {device.registered ? <span className={devices.registered}>{ts("deviceRegistered")}</span> : null}
                      </span>
                    </span>
                  </td>
                  <td>{formatWhen(device.lastUsedAt || device.createdAt)}</td>
                  <td>{device.ip || ts("unknownLocation")}</td>
                  <td>
                    <button type="button" className={devices.rowButton} disabled={Boolean(busy)} onClick={() => void revoke(device)}>
                      {t("users.revokeDevice")}
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      ) : null}
      {shown && !shown.devices.length ? <div>{t("users.noDevices")}</div> : null}
      {shown ? readAgain : null}
      <div className={userStyles.live} data-error={failed ? "true" : undefined} role={failed ? "alert" : "status"} aria-live="polite">{failed || said}</div>
    </div>
  );
}
