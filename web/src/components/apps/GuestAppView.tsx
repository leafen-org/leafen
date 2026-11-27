import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { API_ROOT } from "@/apiTransport";
import { appPlatformOperations, type AppGuestSession } from "@/appPlatformWire.generated";
import { appMaker, appText } from "@/components/admin/administratorAppsApi";
import { TopBar } from "@/components/page/TopBar";
import pageStyles from "@/components/page/PageView.module.css";
import { SharedPageLoading } from "@/components/SharedPageLoading";
import { setDocumentChrome } from "@/lib/documentChrome";
import { useProductToast } from "@/lib/productToast";
import { admissionRefusal, fetchAskingAgain } from "@/lib/serverBusy";
import { serverPath } from "@/lib/serverOrigin";
import { appCallResult, appFrameTheme, bridgeAppFrame, appBridgeRefusal } from "./appFrameBridge";
import { useAppFrameReveal } from "./useAppFrameReveal";
import { AppFrameBox } from "./AppFrameBox";
import { AppFrameStopped } from "./AppFrameStopped";
import styles from "./AppHost.module.css";
import { randomUUID } from "@/lib/browserCrypto";

// A guest's view of an app (docs/app-platform.md 9, T9): someone with a
// guest link opens the app with no account. The link's token rides after "#"
// so no server log holds it; the server trades it for a guest session that
// ends with the link or after two hours. The app's frame reaches only the
// link's room, the organization's temporary store and a short notice —
// nothing of Leafen's data, actions or outside addresses.

type Phase =
  | { kind: "loading" }
  | { kind: "missing" }
  | { kind: "busy" }
  | { kind: "unreachable"; busy?: boolean }
  | { kind: "ended" }
  | { kind: "ready"; app: AppGuestSession };

/** The guest link's token, after "#". */
function guestLinkToken() {
  if (typeof window === "undefined") return "";
  return new URLSearchParams(window.location.hash.slice(1)).get("token") ?? "";
}

export function GuestAppView() {
  const { t, i18n } = useTranslation(["common", "serverConsole"]);
  const { notify } = useProductToast();
  const [token] = useState(guestLinkToken);
  const [phase, setPhase] = useState<Phase>({ kind: "loading" });
  const [attempt, setAttempt] = useState(0);
  const frameRef = useRef<HTMLIFrameElement>(null);
  // A frame stopped for flooding Leafen with calls, and how often it was opened again.
  const [flooded, setFlooded] = useState(false);
  const [generation, setGeneration] = useState(0);
  const nonceRef = useRef("");
  const { frameStyle, waiting, onChannelReady, onChannelOpened, onFrameLoad } = useAppFrameReveal(nonceRef.current);
  // When the page began opening, for the waiting line (AppFrameBox).
  const [openedAt] = useState(() => performance.now());
  const portRef = useRef<MessagePort | null>(null);

  useEffect(() => {
    let live = true;
    void (async () => {
      try {
        // Asked again while the server refuses it for now (lib/serverBusy.ts).
        const response = await fetchAskingAgain(() => fetch(serverPath(`${API_ROOT}${appPlatformOperations.openGuestSession.path}`), {
          method: appPlatformOperations.openGuestSession.method, headers: { "content-type": "application/json" }, body: JSON.stringify({ token }),
        }));
        if (!live) return;
        // The server busy for a moment (a slow write held the database) or
        // short of memory isn't the guests' limit: it says so, with a way to
        // ask again (2026-11-18; 2026-11-23, a 429 of its memory's).
        const refusedForNow = await admissionRefusal(response);
        if (!live) return;
        if (refusedForNow) { setPhase({ kind: "unreachable", busy: true }); return; }
        if (response.status === 503 || response.status === 429) { setPhase({ kind: "busy" }); return; }
        if (!response.ok) { setPhase({ kind: "missing" }); return; }
        const opened = await response.json() as AppGuestSession;
        // A replaced run leaves the frame's nonce alone (see AppHost).
        if (!live) return;
        nonceRef.current = randomUUID();
        setPhase({ kind: "ready", app: opened });
      } catch {
        // No answer from the server isn't a broken link (2026-10-17).
        if (live) setPhase({ kind: "unreachable" });
      }
    })();
    return () => { live = false; };
  }, [token, attempt]);

  const app = phase.kind === "ready" ? phase.app : null;
  const name = app ? appText(app.name, i18n.language) : "";

  useEffect(() => {
    setDocumentChrome({ title: name || "Leafen", plain: true });
  }, [name]);

  // The session ends with the link or after two hours.
  useEffect(() => {
    if (!app) return undefined;
    const ends = window.setTimeout(() => setPhase({ kind: "ended" }), Math.max(0, app.expiresAt - Date.now()));
    return () => window.clearTimeout(ends);
  }, [app]);

  useEffect(() => {
    if (!app) return undefined;
    const call = async <T,>(path: string, body: unknown) => {
      const response = await fetch(serverPath(`${API_ROOT}${path}`), {
        method: "POST", headers: { "x-leafen-guest": app.guestToken, "content-type": "application/json" }, body: JSON.stringify(body),
      });
      // Refused before the link's time is up, the session was ended — the link
      // cut, guests turned off, the app stopped or removed — and says so as an
      // opening refused does, not that the time ran out (2026-11-09).
      if (response.status === 401) setPhase(Date.now() >= app.expiresAt ? { kind: "ended" } : { kind: "missing" });
      return appCallResult<T>(response);
    };
    const answer = async (method: string, params: Record<string, unknown>): Promise<unknown> => {
      if (method === "room") {
        // Only the link's room; the frame joins it with y-websocket.
        if (String(params.name ?? app.room) !== app.room) throw appBridgeRefusal("notForGuests");
        return call(appPlatformOperations.guestRoomTicket.path, {});
      }
      if (method === "kv") {
        const stored = await call<{ result?: unknown }>(appPlatformOperations.guestKV.path, params);
        return stored?.result ?? null;
      }
      if (method === "notice") {
        notify(String(params.message ?? "").slice(0, 200), "default");
        return null;
      }
      if (method === "navigate") return null;
      throw appBridgeRefusal("notForGuests");
    };
    return bridgeAppFrame({
      frame: frameRef, nonce: nonceRef.current, port: portRef, answer, onReady: onChannelReady, onHello: onChannelOpened, onFlood: () => setFlooded(true),
      context: () => ({ locale: i18n.language, theme: appFrameTheme(), path: "", app: { slug: app.slug, version: app.version },
        guest: { room: app.room, expiresAt: app.expiresAt } }),
    });
    // The channel lives as long as this app's frame.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [app, generation]);

  // Opening a stopped frame again starts it with a fresh channel.
  const reopen = () => {
    nonceRef.current = randomUUID();
    setFlooded(false);
    setGeneration((value) => value + 1);
  };

  if (phase.kind === "loading") return <SharedPageLoading />;
  if (phase.kind !== "ready") {
    const [title, body] = phase.kind === "ended" ? [t("appGuest.endedTitle"), t("appGuest.ended")]
      : phase.kind === "busy" ? [t("appGuest.missingTitle"), t("appGuest.busy")]
        : phase.kind === "unreachable" ? [t("appHost.unreachableTitle"), phase.busy ? t("appPlace.busy") : t("appHost.unreachable")]
          : [t("appGuest.missingTitle"), t("appGuest.missing")];
    return (
      <div data-app-guest={phase.kind}>
        <TopBar title={t("appGuest.pageTitle")} standalone />
        <div className={pageStyles.missing} role="alert">
          <strong>{title}</strong>
          <p>{body}</p>
          {phase.kind === "unreachable" ? (
            <button type="button" className={styles.retry} onClick={() => { setPhase({ kind: "loading" }); setAttempt((value) => value + 1); }}>
              {t("appHost.retry")}
            </button>
          ) : null}
        </div>
      </div>
    );
  }
  return (
    <div className={styles.guestPage} data-app-guest="ready">
      <div className={styles.host} data-app-host={phase.app.slug}>
        <TopBar title={name} standalone />
        <div className={styles.bar}>
          <span className={styles.badge}>{t("appGuest.badge")}</span>
          <span className={styles.name}>{((maker) => maker.builtin ? t("serverConsole:apps.trust.builtin") : t("appGuest.by", { developer: maker.developer }))(
            appMaker(phase.app.developer.name, phase.app.builtin, i18n.language))}</span>
        </div>
        {flooded ? <AppFrameStopped onReopen={reopen} /> : (
          <AppFrameBox className={styles.frameBox} waiting={waiting} since={openedAt} label={t("appHost.loading")} lineClassName={styles.state}>
          <iframe
            key={generation}
            ref={frameRef}
            className={styles.frame}
            title={name}
            style={frameStyle}
            src={`${serverPath(phase.app.frameUrl)}#leafen=${nonceRef.current}`}
            sandbox="allow-scripts allow-forms"
            referrerPolicy="no-referrer"
            onLoad={onFrameLoad}
            data-app-frame
          />
          </AppFrameBox>
        )}
      </div>
    </div>
  );
}
