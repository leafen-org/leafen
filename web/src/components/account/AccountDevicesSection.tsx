import { type ReactNode, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { authBounds, type AuthSession } from "@/authWire.generated";
import { ArrowDown, Copy, InfoIcon } from "@/icons/leafen";
import { DeviceMobileIcon, LaptopIcon } from "@/icons/leafen/deviceIcons";
import { copyText } from "@/lib/copyText";
import { activeDateLocale } from "@/lib/i18n";
import { useOptionalProductToast } from "@/lib/productToast";
import { HoverTip } from "@/components/IconTipButton";
import type { Session } from "@/types";
import { AccountAddDevice } from "./AccountAddDevice";
import { AccountAgentSignInSwitch } from "./AccountAgentSignInSwitch";
import { AccountOpenInBrowser } from "./AccountOpenInBrowser";
import { styles } from "./accountSettingsStyles";
import { EmptyLineReadFailed } from "@/components/settings/SettingsRowNotice";
import devices from "./AccountDevices.module.css";
import type { useAccountSecurity } from "./useAccountSecurity";

/** Notion shows three devices, then "기기 N개 더 로드". */
const FIRST_SHOWN = 3;
const DEVICES_REREAD_MS = 3_000;
/** How few sign-ins left before the limit is said. */
const SESSIONS_NOTICE_LEFT = 3;

/** The system a sign-in's browser names, as Notion's device table names a device. */
export function sessionDevice(userAgent: string): { name: string; mobile: boolean } | null {
  if (/iPhone|iPad|iPod/u.test(userAgent)) return { name: "iOS", mobile: true };
  if (/Android/u.test(userAgent)) return { name: "Android", mobile: true };
  if (/Macintosh|Mac OS X/u.test(userAgent)) return { name: "macOS", mobile: false };
  if (/Windows/u.test(userAgent)) return { name: "Windows", mobile: false };
  if (/CrOS/u.test(userAgent)) return { name: "ChromeOS", mobile: false };
  if (/Linux/u.test(userAgent)) return { name: "Linux", mobile: false };
  return null;
}

function lastUse(item: AuthSession) {
  return item.lastUsedAt || item.createdAt;
}

function formatLastUse(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(activeDateLocale(), {
    dateStyle: "long",
    timeStyle: "short",
  }).format(date);
}

/**
 * The account's "기기", as Notion's (seen 2026-10-12): "모든 기기에서
 * 로그아웃" signs out every sign-in but this one, and the table lists this
 * device first, then the rest by last use, each with its own "로그아웃".
 * Where Notion names a place, Leafen names the address the sign-in came
 * from; it looks nothing up elsewhere.
 */
export function AccountDevicesSection({ session, security, notices, onClickCapture }: {
  session: Session;
  security: ReturnType<typeof useAccountSecurity>;
  /** The security notices, shown here after a device was signed out. */
  notices: ReactNode;
  /** Where the security notices show once a row here is focused. */
  onClickCapture?: () => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [expanded, setExpanded] = useState(false);
  const sessions = [...security.snapshot?.sessions ?? []]
    .sort((a, b) => Number(b.current) - Number(a.current) || lastUse(b).localeCompare(lastUse(a)));
  const shown = expanded ? sessions : sessions.slice(0, FIRST_SHOWN);
  const signingOutAll = security.busy === "sessions:others";
  const { refreshSessions } = security;
  useEffect(() => {
    const read = () => { if (document.visibilityState !== "hidden") void refreshSessions(); };
    const timer = setInterval(read, DEVICES_REREAD_MS);
    document.addEventListener("visibilitychange", read);
    return () => { clearInterval(timer); document.removeEventListener("visibilitychange", read); };
  }, [refreshSessions]);
  // A full account asks, at the next sign-in, to sign out the device used
  // longest ago; nearing it, the list says so.
  const sessionsLeft = authBounds.sessionLimit - sessions.length;
  return (
    <section className={styles.section} role="region" aria-label={t("devicesSection")} data-account-devices onClickCapture={onClickCapture}>
      {/* Hints in the app's own dark tip, as Notion's "기기 ⓘ" and "마지막 활동 ⓘ" (seen 2026-11-06) —
          the browser's title tip came late and looked like no other. */}
      <div className={styles.sectionTitle}>
        <HoverTip label={t("devicesSectionHint")} placement="top" className={devices.titleHint}>
          {t("devicesSection")}<InfoIcon size="small" aria-label={t("devicesSectionHint")} />
        </HoverTip>
      </div>
      {notices}
      {/* A device action's line ("세션을 종료했습니다.") goes once the next one starts (2026-11-07: it stayed beside "새 기기가 로그인했습니다"). */}
      <AccountAddDevice session={session} sessions={security.snapshot ? sessions : null} onToggle={security.dismissNotice} />
      <AccountOpenInBrowser session={session} />
      <AccountAgentSignInSwitch session={session} />
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}><strong>{t("signOutAllDevices")}</strong><span>{t("signOutAllDevicesDesc")}</span></div>
        <button type="button" className={devices.softDanger} onClick={() => void security.revokeOtherSessions()}
          disabled={security.loading || signingOutAll || !sessions.some((item) => !item.current)}>
          {t("signOutAllDevices")}
        </button>
      </div>
      {security.snapshot && sessionsLeft <= SESSIONS_NOTICE_LEFT ? (
        <p className={styles.organizationMeta} data-account-devices-capacity={sessionsLeft <= 0 ? "full" : "near"}>
          {sessionsLeft <= 0
            ? t("devicesFull", { limit: authBounds.sessionLimit })
            : t("devicesNear", { count: sessions.length, limit: authBounds.sessionLimit })}
        </p>
      ) : null}
      <table className={devices.devices} aria-busy={security.loading}>
        <thead>
          <tr>
            <th scope="col">{t("deviceName")}</th>
            <th scope="col">
              <HoverTip label={t("deviceLastActiveHint")} placement="top" className={devices.lastActive}>
                {t("deviceLastActive")}<InfoIcon size="tiny" aria-label={t("deviceLastActiveHint")} />
              </HoverTip>
            </th>
            <th scope="col">{t("deviceLocation")}</th>
            <th scope="col" aria-label={t("deviceSignOut")} />
          </tr>
        </thead>
        <tbody>
          {shown.map((item) => {
            const device = sessionDevice(item.userAgent ?? "");
            const Icon = device?.mobile ? DeviceMobileIcon : LaptopIcon;
            return (
              <tr key={item.id} data-account-session={item.current ? "current" : "other"}>
                <td title={item.userAgent || undefined}>
                  <span className={devices.deviceName}>
                    <Icon size="regular" aria-hidden="true" />
                    <span>
                      <span>{device?.name ?? t("unknownDevice")}</span>
                      {item.current ? <span className={devices.thisDevice}>{t("thisDevice")}</span> : null}
                      {item.registered ? <span className={devices.registered}>{t("deviceRegistered")}</span> : null}
                    </span>
                  </span>
                </td>
                <td>{item.current ? t("deviceActiveNow") : formatLastUse(lastUse(item))}</td>
                <td title={item.ip || undefined}>{item.country || t("unknownLocation")}</td>
                <td>
                  {item.current ? null : (
                    <button type="button" className={devices.rowButton} onClick={() => void security.revokeSession(item)}
                      disabled={security.busy === `session:${item.id}` || signingOutAll}>
                      {t("deviceSignOut")}
                    </button>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {!expanded && sessions.length > FIRST_SHOWN ? (
        <button type="button" className={devices.more} onClick={() => setExpanded(true)}>
          <ArrowDown size="small" aria-hidden="true" />{t("loadMoreDevices", { count: sessions.length - FIRST_SHOWN })}
        </button>
      ) : null}
      {/* DB-IP asks for a link where its results show (its license terms, 2026-11-05). */}
      {shown.some((item) => item.country) ? (
        <p className={devices.locationCredit}><a href="https://db-ip.com" target="_blank" rel="noopener noreferrer">{t("deviceLocationCredit")}</a></p>
      ) : null}
      {/* A read that failed says so, not that there are no devices, with a way to read again (it also tries again quietly). */}
      {!security.loading && !security.snapshot ? (
        <EmptyLineReadFailed onRetry={() => void security.refresh()} data-account-devices-read-failed>{t("devicesFailed")}</EmptyLineReadFailed>
      ) : null}
      {!security.loading && security.snapshot && !sessions.length ? <div className={styles.emptyState}>{t("noActiveSessions")}</div> : null}
    </section>
  );
}

/** "사용자 ID", as Notion's: the account's ID and a button that copies it. */
export function AccountUserIdSection({ userId }: { userId: string }) {
  const { t } = useTranslation(["workspaceSettingsDialog", "blockHandle"]);
  const toast = useOptionalProductToast();
  return (
    <section className={styles.section} role="region" aria-label={t("accountId")} data-account-user-id>
      <div className={styles.sectionTitle}>{t("accountId")}</div>
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}><span>{t("accountId")}</span></div>
        <span className={devices.userId} data-row-end>
          <code>{userId}</code>
          <button type="button" className={devices.copy} aria-label={t("copyAccountId")}
            onClick={() => void copyText(userId).then((copied) => {
              toast?.notify(copied ? t("blockHandle:notifications.copiedToClipboard") : t("clipboardCopyFailed"), copied ? "success" : "error");
            })}>
            <Copy size="small" aria-hidden="true" />
          </button>
        </span>
      </div>
    </section>
  );
}
