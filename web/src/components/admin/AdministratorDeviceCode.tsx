import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AccountDeviceCode } from "../../authWire.generated";
import { api } from "../../api";
import type { Session } from "../../types";
import { readAndFollow } from "@/lib/followAcrossTabs";
import { useQRCode } from "@/lib/useQRCode";
import devices from "../account/AccountDevices.module.css";
import { administratorUserStyles as userStyles } from "./administratorUserManagementStyles";
import { useCopyNotice } from "@/lib/copyNotice";

/**
 * A member's one-time device code an administrator made
 * (administrator_device_codes.go): the code, the address it opens and its
 * QR, for the administrator to hand to that person only, laid out as the
 * person's own "새 기기 로그인" (2026-10-26: the code ran on at the end of the
 * sentence in small type and broke at its hyphen). Once a device of
 * that account comes in, or the ten minutes pass, the code is no longer
 * shown as one to use (2026-10-26: it stayed after the member came in).
 */
export function AdministratorDeviceCode({ session, accountId, label, code }: {
  session: Session;
  accountId: string;
  label: string;
  code: AccountDeviceCode;
}) {
  const { t } = useTranslation("serverConsole");
  const copy = useCopyNotice();
  const [state, setState] = useState<"waiting" | "used" | "expired">("waiting");
  const picture = useQRCode(state === "waiting" ? code.url : "", 176);

  // A device that joins shows up among the account's devices: read once now,
  // then one of the person's tabs in sight asks for all of them, and a tab
  // behind asks nothing (followAcrossTabs).
  useEffect(() => {
    if (state !== "waiting") return undefined;
    const controller = new AbortController();
    let seen: Set<string> | null = null;
    const stop = readAndFollow(`administrator-account-devices:${session.user.id}:${accountId}`, 3_000, true,
      () => api.readAdministratorAccountDevices(session, accountId, controller.signal).then(({ devices }) => devices.map((device) => device.id)),
      (value) => {
        if (!Array.isArray(value)) return;
        if (!seen) seen = new Set(value as string[]);
        else if ((value as string[]).some((id) => !seen?.has(id))) setState("used");
      });
    // The code ends when it expires, asked or not.
    const ends = window.setTimeout(() => setState((current) => current === "waiting" ? "expired" : current), Math.max(0, code.expiresAt - Date.now()));
    return () => { stop(); window.clearTimeout(ends); controller.abort(); };
  }, [state, code.expiresAt, session, accountId]);

  if (state !== "waiting") {
    return (
      <div className={userStyles.notice} data-tone="neutral" data-administrator-device-code={state} role="status">
        {t(state === "used" ? "users.deviceCodeUsed" : "users.deviceCodeExpired", { email: label })}
      </div>
    );
  }
  return (
    <div className={devices.addDevicePanel} data-administrator-device-code="waiting">
      <div className={devices.addDeviceQR} aria-hidden={!picture}>
        {picture ? <img src={picture} alt={t("users.deviceCodeQRAlt")} width={176} height={176} /> : null}
      </div>
      <div className={devices.addDeviceSteps}>
        <p>{t("users.deviceCodeIssued", { email: label })}</p>
        <code className={devices.addDeviceCode}>{code.code}</code>
        {code.url ? (
          <div>
            <button type="button" className={userStyles.memberAction} onClick={() => void copy(code.url)}>
              {t("users.deviceCodeCopy")}
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
