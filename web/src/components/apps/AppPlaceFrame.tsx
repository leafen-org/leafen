import { type CSSProperties, type MutableRefObject, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { isRefusedForNow } from "@/lib/serverBusy";
import { appPlatformErrorCodes, type AppSession, type AppSessionInput, type UsableApp } from "@/appPlatformWire.generated";
import { useProductPages } from "@/components/product/productPagesContext";
import { pushRoute } from "@/lib/router";
import { serverPath } from "@/lib/serverOrigin";
import type { Session } from "@/types";
import { appCallAnswerer, type AppToken } from "./appCalls";
import type { AppFileSaveTarget } from "./appFileSave";
import { forgetSharedAppSessions, openAppSession, sharedAppSession } from "./appSessions";
import { appFrameTheme, bridgeAppFrame, type AppFrameAnswer, appBridgeRefusal } from "./appFrameBridge";
import { useAppFrameReveal } from "./useAppFrameReveal";
import { AppFrameBox, AppWaitingLine } from "./AppFrameBox";
import { AppFrameStopped } from "./AppFrameStopped";
import { AppUnavailableNotice, useAppAvailability } from "./AppUnavailable";
import { announceAppsChanged } from "@/lib/appsChangedEvents";
import styles from "./AppPlaceFrame.module.css";
import { randomUUID } from "@/lib/browserCrypto";
import { isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";
import { withAppInstalledAgain } from "./useUsableApps";

// An app drawn in a place of a page — one of its blocks, or beside the page
// (docs/app-platform.md 2) — in a frame with no origin of its own, with the
// calls its own screen has, what the place adds and the page it is in. An app
// the person must agree to again opens on its own screen for that; moving
// within the app goes there too.

type Phase = { kind: "loading" } | { kind: "ready"; app: AppSession } | { kind: "consent" } | { kind: "stopped" } | { kind: "failed" }
  | { kind: "unreachable"; busy: boolean };

export function AppPlaceFrame({ session, usable, place, opens, shared = false, title, style, context, extra, port, fileSave }: {
  session: Session;
  usable: UsableApp;
  /** Where the app is drawn, as its context says. */
  place: "block" | "sidePanel" | "propertyCell" | "propertyEditor" | "fileOpener" | "importSource";
  /** The place the session opens, with its block's or editor's key. */
  opens: Omit<AppSessionInput, "tenancyKey">;
  /** One session for many frames of the same place. */
  shared?: boolean;
  title: string;
  style?: CSSProperties;
  /** What the place tells the app beside the app's own context. */
  context: () => Record<string, unknown>;
  extra?: AppFrameAnswer;
  /** The open channel, for the place to tell the app what changed. */
  port: MutableRefObject<MessagePort | null>;
  /** The file a file opener's screen may save. */
  fileSave?: MutableRefObject<AppFileSaveTarget | null>;
}) {
  const { t, i18n } = useTranslation("common");
  const pages = useProductPages();
  const [phase, setPhase] = useState<Phase>({ kind: "loading" });
  const [flooded, setFlooded] = useState(false);
  const [generation, setGeneration] = useState(0);
  // Each opening of the app's session, again after "다시 시도".
  const [opening, setOpening] = useState(0);
  const frameRef = useRef<HTMLIFrameElement>(null);
  const nonceRef = useRef("");
  const tokenRef = useRef<AppToken>({ token: "", expiresAt: 0 });
  const loadsRef = useRef(0);
  // The installs this frame opened again from when a call found them gone:
  // once each, so a refusal that stays ends in the notice, not in a loop.
  const reopenedFrom = useRef(new Set<string>());
  // The latest of what the place adds, for a channel opened earlier.
  const placeRef = useRef({ context, extra, opens });
  placeRef.current = { context, extra, opens };
  // Whether the frame holds what a new frame would lose: a file's window
  // whose app hasn't said it has nothing unsaved.
  const holdsEdits = () => Boolean(fileSave?.current && !fileSave.current.nothingToSave);

  // When the app began opening, for the waiting line (AppFrameBox).
  const loadingSince = useRef(performance.now());
  useEffect(() => {
    let live = true;
    loadingSince.current = performance.now();
    setPhase({ kind: "loading" });
    // An install the page's list still holds may have been removed and the
    // app installed again elsewhere: the app opens as the list reads now.
    withAppInstalledAgain(session, usable, (app) => (shared ? sharedAppSession : openAppSession)(session, app.installId, app.tenancyKey, opens)).then(
      (opened) => {
        // A replaced run leaves the frame's nonce and token alone (see AppHost).
        if (!live) return;
        tokenRef.current = { token: opened.token ?? "", expiresAt: opened.expiresAt ?? 0 };
        nonceRef.current = randomUUID();
        loadsRef.current = 0;
        setPhase({ kind: "ready", app: opened });
      },
      (failure: unknown) => {
        const code = failure instanceof LeafenApiError ? failure.code : "";
        if (!live) return;
        // A server that stayed busy past the transport's own tries (a slow
        // write held the database), or didn't answer, says so and opens the
        // app again once it answers, or at "다시 시도" (2026-11-18: a file's
        // app window said only "앱을 열지 못했습니다." for good).
        if (isServerUnreachable(failure)) setPhase({ kind: "unreachable", busy: isRefusedForNow(failure) });
        else setPhase({ kind: code === appPlatformErrorCodes.consentNeeded ? "consent" : code === appPlatformErrorCodes.appStopped ? "stopped" : "failed" });
      },
    );
    return () => { live = false; };
    // The place is fixed for the frame's life.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opening, session, shared, usable.installId, usable.tenancyKey]);

  const unreachable = phase.kind === "unreachable";
  useEffect(() => (unreachable ? retryWhenServerMayAnswer(() => setOpening((value) => value + 1)) : undefined), [unreachable]);

  // A stopped app turns every open place to the notice at once: the lists of
  // apps are read again past their shared few seconds (useUsableApps), as a
  // block's own write does when it finds the app stopped, and until its place
  // takes over this frame says why in the same notice, with what to do next
  // (2026-11-15: after an emergency stop a side panel said only "관리자만
  // 다시 켤 수 있습니다", with no button, for up to ten seconds).
  const stopped = phase.kind === "stopped";
  useEffect(() => { if (stopped) announceAppsChanged(); }, [stopped]);
  const availability = useAppAvailability(session, { app: usable.appId }, usable.tenancyKey, stopped);

  const app = phase.kind === "ready" ? phase.app : null;
  const { frameStyle, waiting, onChannelReady, onChannelOpened, onFrameLoad } = useAppFrameReveal(nonceRef.current);
  useEffect(() => {
    if (!app) return undefined;
    const answer = appCallAnswerer({
      app, session, tokenRef, t, language: i18n.language, fileSave, place: () => placeRef.current.opens,
      // A file's window holding changes keeps its frame when its install is
      // gone: its calls go on with the app as the list reads now
      // (withAppInstalledAgain), the changes in place; a server that didn't
      // answer fails the call, to be tried again.
      moveInstall: fileSave ? async (from) => {
        if (!holdsEdits()) return null;
        try {
          return await withAppInstalledAgain(session, { ...usable, installId: from },
            (again) => openAppSession(session, again.installId, again.tenancyKey, placeRef.current.opens));
        } catch (failure) {
          if (isServerUnreachable(failure)) throw failure;
          return null;
        }
      } : undefined,
      navigate: (next) => pushRoute(`/apps/${app.slug}${next ? `/${next}` : ""}`),
      notice: (message) => pages?.onStatus?.(message, "default"),
      extra: (method, params) => {
        const own = placeRef.current.extra;
        if (!own) throw appBridgeRefusal("unknownCall");
        return own(method, params);
      },
    });
    return bridgeAppFrame({
      frame: frameRef, nonce: nonceRef.current, port, answer, onReady: onChannelReady, onHello: onChannelOpened, onFlood: () => setFlooded(true),
      onGone: (code) => {
        // An administrator removed the install the frame opened with and
        // installed the app again, in another window or through the API: the
        // frame's next call found its token no longer held or its install
        // gone (2026-11-19: an open block, file window or side panel said
        // "앱을 열지 못했습니다" until it was closed and opened again). It
        // opens again as the list reads now (withAppInstalledAgain), past any
        // session its place shared; an app removed for good, or no longer the
        // person's, ends in the notice as before. A file's window holding
        // changes stays, its changes kept, and says why it can't save
        // (AppFileOpener) — its calls found no install to go on with.
        if (code !== appPlatformErrorCodes.appStopped && holdsEdits()) {
          fileSave?.current?.appGone();
          return "kept";
        }
        if (code !== appPlatformErrorCodes.appStopped && !reopenedFrom.current.has(app.installId)) {
          reopenedFrom.current.add(app.installId);
          forgetSharedAppSessions(app.installId);
          setOpening((value) => value + 1);
          return;
        }
        setPhase({ kind: code === appPlatformErrorCodes.appStopped ? "stopped" : "failed" });
      },
      context: () => ({ locale: i18n.language, theme: appFrameTheme(), path: "", homeUrl: app.homeUrl,
        app: { slug: app.slug, version: app.version }, place, ...(app.administrator ? { administrator: true } : {}), ...placeRef.current.context() }),
    });
    // The channel lives as long as this app's frame.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [app, generation]);

  if (phase.kind !== "ready") {
    return (
      <div className={styles.state} style={style} role="status" data-app-place-state={phase.kind} data-place={place}>
        {phase.kind === "loading" ? <AppWaitingLine since={loadingSince.current} label={t("appPlace.loading")} />
          : phase.kind === "stopped" ? <AppUnavailableNotice availability={availability} session={session}
            compact={place === "block" || place === "propertyCell"} onRetry={() => { announceAppsChanged(); setOpening((value) => value + 1); }} />
          : phase.kind === "unreachable" ? <span>{phase.busy ? t("appPlace.busy") : t("appHost.unreachable")}</span>
          : <span>{phase.kind === "consent" ? t("appPlace.consent") : t("appPlace.failed")}</span>}
        {phase.kind === "unreachable" ? (
          <button type="button" className={styles.action} onClick={() => setOpening((value) => value + 1)} data-app-place-retry>
            {t("appHost.retry")}
          </button>
        ) : null}
        {phase.kind === "consent" ? (
          <button type="button" className={styles.action} onClick={() => pushRoute(`/apps/${usable.slug}`)} data-app-place-consent>
            {t("appPlace.consentAction")}
          </button>
        ) : null}
      </div>
    );
  }
  if (flooded) {
    return (
      <div className={styles.stoppedBox} style={style}>
        <AppFrameStopped onReopen={() => {
          nonceRef.current = randomUUID();
          loadsRef.current = 0;
          setFlooded(false);
          setGeneration((value) => value + 1);
        }} />
      </div>
    );
  }
  const allow = phase.app.device.join("; ");
  // A place the frame fills says the app is opening until it shows; a cell or a block keeps its quiet box.
  const fills = place === "fileOpener" || place === "sidePanel" || place === "importSource";
  const frame = (
    <iframe
      key={generation}
      ref={frameRef}
      className={styles.frame}
      style={fills ? frameStyle : { ...style, ...frameStyle }}
      title={title}
      src={`${serverPath(phase.app.frameUrl)}#leafen=${nonceRef.current}`}
      sandbox="allow-scripts allow-forms"
      allow={allow || undefined}
      referrerPolicy="no-referrer"
      // One document, one hello: a frame that loads another page loses its channel.
      onLoad={() => {
        onFrameLoad();
        loadsRef.current += 1;
        if (loadsRef.current > 1) { port.current?.close(); port.current = null; }
      }}
      data-app-place-frame={place}
    />
  );
  if (!fills) return frame;
  return (
    <AppFrameBox waiting={waiting} since={loadingSince.current} label={t("appPlace.loading")} lineClassName={styles.state} style={style}>
      {frame}
    </AppFrameBox>
  );
}
