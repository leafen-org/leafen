import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AuthSession } from "@/authWire.generated";
import { enterpriseSAMLErrorCodes } from "@/enterpriseSAMLWire.generated";
import { useDeviceCode } from "@/lib/useDeviceCode";
import { useQRCode } from "@/lib/useQRCode";
import { LocalAddressHint } from "@/components/product/LocalAddressHint";
import type { Session } from "@/types";
import { styles } from "./accountSettingsStyles";
import devices from "./AccountDevices.module.css";
import { imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * "새 기기 로그인" above the account's devices (account_device_codes.go): a QR
 * of the server's address with a one-time code, for a phone or another
 * computer to scan, and the same code to type under the sign-in screen's
 * "코드로 로그인". A new code comes before the last runs out; closing
 * ends it. Once a device joins, the panel says so and the list shows it;
 * once that device is signed out (`sessions`, the account's sign-ins as the
 * list has them), the panel closes rather than say it is still signed in.
 * Standing, it is the workspace menu's "새 기기 로그인" window's body: the
 * panel alone, open while the window is, a new code once a joined device
 * is signed out.
 */
export function AccountAddDevice({ session, sessions, onToggle, standing = false }: {
  session: Session;
  /** The account's sign-ins as the list has them, read again while it is open. */
  sessions: readonly AuthSession[] | null;
  /** Opening or closing the panel: what the last device action said is put away. */
  onToggle?: () => void;
  /** The panel alone, always open, in a window of its own. */
  standing?: boolean;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [opened, setOpen] = useState(false);
  const open = standing || opened;
  // The sign-ins the joined device made, empty until one joins.
  const [joinedAs, setJoinedAs] = useState<string[]>([]);
  const joined = joinedAs.length > 0;
  const { code, failed } = useDeviceCode(session, open && !joined);
  const picture = useQRCode(code?.url ?? "", 176);
  const [now, setNow] = useState(() => Date.now());
  // The sign-ins there were when the panel opened: a device that joins is a new one among them.
  const seen = useRef<Set<string> | null>(null);

  useEffect(() => {
    if (!open || joined) return undefined;
    const tick = setInterval(() => setNow(Date.now()), 1_000);
    return () => clearInterval(tick);
  }, [open, joined]);

  useEffect(() => {
    if (!open || joined || !sessions) return;
    // Opened before the list was read: what there was is the first read.
    seen.current ??= new Set(sessions.map((item) => item.id));
    const known = seen.current;
    const added = sessions.filter((item) => item.registered && !known.has(item.id)).map((item) => item.id);
    if (added.length) setJoinedAs(added);
  }, [joined, open, sessions]);

  // Gone from the list, the joined device was signed out.
  useEffect(() => {
    if (!joinedAs.length || !sessions) return;
    if (!joinedAs.some((id) => sessions.some((item) => item.id === id))) {
      setJoinedAs([]);
      setOpen(false);
    }
  }, [joinedAs, sessions]);

  const minutes = code ? Math.max(0, Math.ceil((code.expiresAt - now) / 60_000)) : 0;
  // Escape in the open panel or on its button puts the panel away first and
  // gives its button the keys. Notion has no such panel; its nearest, a
  // settings dropdown, closes alone under Escape, its button ringed (2026-11-15,
  // Chrome, no script before the action; 기본 설정 › 테마).
  const toggle = useRef<HTMLButtonElement>(null);
  return (
    <div className={devices.addDevice} data-account-add-device={open ? (joined ? "joined" : "open") : "closed"}
      onKeyDown={(event) => {
        if (standing || !open || event.key !== "Escape" || event.defaultPrevented || imeComposing(event)) return;
        event.preventDefault();
        event.stopPropagation();
        setOpen(false);
        onToggle?.();
        toggle.current?.focus();
      }}>
      {standing ? null : <div className={styles.securityRow}>
        <div className={styles.securityInstruction}><strong>{t("addDevice")}</strong><span>{t("addDeviceDesc")}</span></div>
        <button ref={toggle} type="button" className={styles.secondaryButton} aria-expanded={open}
          onClick={() => {
            seen.current = sessions ? new Set(sessions.map((item) => item.id)) : null;
            setJoinedAs([]);
            setOpen((value) => !value);
            onToggle?.();
          }}>
          {open ? t("addDeviceClose") : t("addDevice")}
        </button>
      </div>}
      {open ? (
        <div className={devices.addDevicePanel} role="group" aria-label={t("addDevice")}>
          {joined ? <p className={devices.addDeviceJoined} role="status">{t("addDeviceJoined")}</p> : failed ? (
            // Single sign-on only isn't a failure here: the new device signs in with the company account.
            (failed as { code?: unknown }).code === enterpriseSAMLErrorCodes.required
              ? <p className={devices.addDeviceJoined} role="status" data-account-device-sso>{t("addDeviceSSO")}</p>
              // It tries again by itself; nothing for the person to reload.
              : <p className={styles.errorText} role="alert">{t("addDeviceRetrying")}</p>
          ) : (
            <>
              {/* The QR, and under it the host of the address it opens (as the phone offer's, SecondDeviceOffer). */}
              <div className={devices.addDeviceScan}>
                <div className={devices.addDeviceQR} aria-hidden={!picture}>
                  {picture ? <img src={picture} alt={t("addDeviceQRAlt")} width={176} height={176} /> : null}
                </div>
                {code ? <p className={devices.addDeviceHost} data-account-device-host>{qrHost(code.url)}</p> : null}
              </div>
              <div className={devices.addDeviceSteps}>
                <p className={devices.addDeviceLead}>{t("addDeviceScan")}</p>
                <p>{t("addDeviceScanApp")}</p>
                <p>{t("addDeviceType")}</p>
                <code className={devices.addDeviceCode} data-account-device-code>{code?.code ?? "····-····-····"}</code>
                {code ? <p className={styles.organizationMeta}>{t("addDeviceExpires", { count: minutes })}</p> : null}
                {code ? <LocalAddressHint session={session} address={code.url} className={styles.organizationMeta} /> : null}
              </div>
            </>
          )}
        </div>
      ) : null}
    </div>
  );
}

/** The host of the address the QR opens. */
function qrHost(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return "";
  }
}
