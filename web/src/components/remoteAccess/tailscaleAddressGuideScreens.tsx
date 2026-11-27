import type { ReactNode } from "react";
import { GuideClick, GuideCursor, GuideWindowBar } from "../guide/GuideParts";
import styles from "./TailscaleAddressGuide.module.css";
import pages from "./TailscaleAddressGuidePages.module.css";
import { pageMarkSource } from "@/lib/pageMark";

/** Leafen's own words drawn in the guide, read from its screens' catalog. */
export type TailscaleGuideWords = {
  title: string;
  loginOpened: string;
  /** The allow step as this client shows it: opened by the app, or the button's to open in a browser. */
  allow: string;
  allowButton: string;
  openAgain: string;
  signInQR: string;
  allowQR: string;
  fact: string;
  back: string;
  later: string;
  doneTitle: string;
  addressQR: string;
  tailnetName: string;
  finish: string;
};

/** The first run's full-window step, or the dialog Settings opens. */
export type TailscaleGuideLook = "screen" | "dialog";

// Tailscale's pages are in English only, drawn as they are: its sign-in
// from the QR sign-in picture in Tailscale's docs ("Add a device using a QR
// code"), and its page that turns on HTTPS and Funnel from the consent page
// pictured for Serve ("Tailscale Serve"), both seen 2026-11-08. The docs show
// no picture of Funnel's own page, so its title and button are that
// template's with Funnel's name. The address is the docs' example tailnet.
// The outside address's example before choosing (outsideAddressGuideScreens)
// draws with the same parts.
export const TS = {
  host: "login.tailscale.com",
  loginLead: "Log in to connect a device to your tailnet.",
  email: "Enter your email...",
  signIn: "Sign in", // screen words: another service's
  or: "OR",
  providers: ["Google", "Microsoft", "GitHub", "Apple", "passkey"] as const,
  qrLink: "Alternatively, use a QR code.",
  account: "you@example.com",
  consentTitle: "Start using Funnel",
  consentLead: "Tailscale Funnel lets you route traffic from the internet to services running on your Tailscale devices.",
  https: "HTTPS certificates",
  httpsBody: "Allow users to provision HTTPS certificates for their devices. The names of those devices will be written to a public ledger.",
  funnel: "Tailscale Funnel",
  funnelBody: "Turning it on will edit the tailnet policy file to give Funnel access to all Members.",
  enable: "Enable Funnel",
  address: "https://leafen.pango-lin.ts.net",
};

// A drawn QR code: the three finder squares and a fixed scatter of modules
// from `seed` — no format or error correction, so no camera reads it.
function qrPath(seed: number): string {
  let state = seed;
  const next = () => {
    state = (Math.imul(state, 1103515245) + 12345) >>> 0;
    return state / 4294967296;
  };
  const finder = (x: number, y: number) => `M${x} ${y}h7v7h-7zM${x + 1} ${y + 1}v5h5v-5zM${x + 2} ${y + 2}h3v3h-3z`;
  const parts = [finder(0, 0), finder(14, 0), finder(0, 14)];
  for (let y = 0; y < 21; y += 1) {
    for (let x = 0; x < 21; x += 1) {
      const inFinder = (x < 8 && y < 8) || (x > 12 && y < 8) || (x < 8 && y > 12);
      if (!inFinder && next() < 0.48) parts.push(`M${x} ${y}h1v1h-1z`);
    }
  }
  return parts.join("");
}

const QR_PATHS = [qrPath(7), qrPath(19), qrPath(43)];

export function GuideQR({ index }: { index: number }) {
  return (
    <svg className={styles.qrCode} viewBox="-2 -2 25 25" aria-hidden="true">
      <path d={QR_PATHS[index]} fillRule="evenodd" />
    </svg>
  );
}

/** Leafen's window: the first run's panel under the mark, or the dialog over the app. */
export function LeafenWindow({ look, title, children }: { look: TailscaleGuideLook; title: string; children: ReactNode }) {
  return (
    <>
      <GuideWindowBar title="Leafen" />
      {look === "screen" ? (
        <div className={styles.leafen} data-look="screen">
          <span className={styles.brand}>
            <img src={pageMarkSource()} alt="" />
            Leafen
          </span>
          <strong className={styles.leafenTitle}>{title}</strong>
          {children}
        </div>
      ) : (
        <div className={styles.appBehind}>
          <div className={styles.leafen} data-look="dialog">
            <span className={styles.dialogClose} aria-hidden="true">✕</span>
            <strong className={styles.leafenTitle}>{title}</strong>
            {children}
          </div>
        </div>
      )}
    </>
  );
}

/** The Tailscale step while it waits: what to do, its button and the QR code, and the ways out. */
function waitingStep(look: TailscaleGuideLook, W: TailscaleGuideWords, text: string, button: string, qr: number, caption: string) {
  return (
    <LeafenWindow look={look} title={W.title}>
      <span className={styles.leafenText}>{text}</span>
      <div className={styles.handoff}>
        <span className={styles.leafenButton}>{button}</span>
        <figure className={styles.qr} data-pulse="">
          <GuideQR index={qr} />
          <figcaption>{caption}</figcaption>
        </figure>
      </div>
      <span className={styles.leafenFact}>{W.fact}</span>
      <span className={styles.exits}>
        <span>{W.back}</span>
        <span>{W.later}</span>
      </span>
    </LeafenWindow>
  );
}

/** A phone, its status bar, and the browser's address when it shows a page. */
export function Phone({ host, camera, children }: { host?: string; camera?: boolean; children: ReactNode }) {
  return (
    <div className={styles.phoneStage}>
      <div className={styles.phone}>
        <div className={styles.phoneScreen} data-camera={camera ? "" : undefined}>
          <span className={styles.statusBar}>
            <b>9:41</b>
            <i className={styles.island} />
            <span className={styles.signal} />
          </span>
          {host ? <span className={styles.urlBar}>{host}</span> : null}
          {children}
        </div>
      </div>
    </div>
  );
}

function TailscaleMark() {
  return (
    <span className={pages.tsMark}>
      <i className={pages.tsDots} aria-hidden="true">
        {[0, 0, 0, 1, 1, 1, 0, 1, 0].map((on, index) => (
          <b key={index} data-on={on ? "" : undefined} />
        ))}
      </i>
      tailscale
    </span>
  );
}

function ProviderIcon({ name }: { name: (typeof TS.providers)[number] }) {
  return <i className={pages.providerIcon} data-provider={name} aria-hidden="true" />;
}

/** Tailscale's sign-in on the phone, its first account tapped. */
export function TailscaleSignIn() {
  return (
    <Phone host={TS.host}>
      <div className={pages.tsPage}>
        <TailscaleMark />
        <span className={pages.tsLead}>{TS.loginLead}</span>
        <span className={pages.tsInput}>{TS.email}</span>
        <span className={pages.tsPrimary}>{TS.signIn}</span>
        <span className={pages.tsOr}>{TS.or}</span>
        {TS.providers.map((name, index) => (
          <span key={name} className={pages.tsProvider} data-tapped={index === 0 ? "" : undefined}>
            <ProviderIcon name={name} />
            {/* screen words: another service's */ name === "passkey" ? "Sign in with a passkey" : `Sign in with ${name}`}
            {index === 0 ? <GuideClick at={1700} /> : null}
          </span>
        ))}
        <span className={pages.tsFoot}>{TS.qrLink}</span>
      </div>
    </Phone>
  );
}

// The six scenes, in order: Leafen opens Tailscale and shows the QR code, the
// phone's camera reads it, Tailscale's sign-in, Leafen's step to allow the
// address, Tailscale's page that turns on HTTPS and Funnel, and the address.
export function tailscaleAddressGuideScreens(look: TailscaleGuideLook, W: TailscaleGuideWords): ReactNode[] {
  return [
    waitingStep(look, W, W.loginOpened, W.openAgain, 0, W.signInQR),
    <Phone camera>
      <div className={styles.viewfinder}>
        <span className={styles.monitor}>
          <span className={styles.monitorQR}>
            <GuideQR index={0} />
            <i className={styles.focusBox} />
          </span>
        </span>
        <span className={styles.cameraLink}>
          {TS.host}
          <GuideClick at={2000} />
        </span>
        <i className={styles.shutter} />
      </div>
    </Phone>,
    <TailscaleSignIn />,
    waitingStep(look, W, W.allow, W.allowButton, 1, W.allowQR),
    <Phone host={TS.host}>
      <div className={pages.tsPage} data-consent="">
        <span className={pages.tsHead}>
          <TailscaleMark />
          <span className={pages.tsAccount}>
            {TS.account}
            <i aria-hidden="true" />
          </span>
        </span>
        <strong className={pages.tsTitle}>{TS.consentTitle}</strong>
        <span className={pages.tsLead}>{TS.consentLead}</span>
        {[[TS.https, TS.httpsBody], [TS.funnel, TS.funnelBody]].map(([name, body]) => (
          <span key={name} className={pages.tsOption}>
            <i className={pages.tsCheck} aria-hidden="true" />
            <span>
              <b>{name}</b>
              <span>{body}</span>
            </span>
          </span>
        ))}
        <span className={pages.tsEnable} data-tapped="">
          {TS.enable}
          <GuideClick at={1800} />
        </span>
      </div>
    </Phone>,
    <LeafenWindow look={look} title={W.doneTitle}>
      <code className={styles.address}>{TS.address}</code>
      <figure className={styles.qr}>
        <GuideQR index={2} />
        <figcaption>{W.addressQR}</figcaption>
      </figure>
      <span className={styles.leafenNote}>{W.tailnetName}</span>
      <span className={styles.leafenButton}>
        {W.finish}
        <GuideCursor path={[{ at: 500, x: -140, y: -70 }, { at: 1400, x: 0, y: 0 }]} clickAt={1800} />
      </span>
    </LeafenWindow>,
  ];
}
