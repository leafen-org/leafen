import type { ReactNode } from "react";
import { GuideWindowBar } from "../guide/GuideParts";
import css from "./OutsideAddressGuide.module.css";
import styles from "./TailscaleAddressGuide.module.css";
import { GuideQR, LeafenWindow, Phone, TS, TailscaleSignIn, type TailscaleGuideLook } from "./tailscaleAddressGuideScreens";

/** The words the outside address's example draws, read from the catalog. */
export type OutsideGuideWords = {
  /**
   * Where this computer opens Leafen now: its server's host — the page's own
   * in a browser, the local server's in the app, never the app window's
   * own (wails.localhost).
   */
  here: string;
  pageTitle: string;
  unreachable: string;
  doneTitle: string;
  addressQR: string;
  finish: string;
  chatApp: string;
  chatAsk: string;
  chatAnswer: string;
};

/** Leafen's workspace in a computer's window: the sidebar and a page. */
function Workspace({ host, title }: { host: string; title: string }) {
  return (
    <div className={css.window}>
      <GuideWindowBar title={host} />
      <div className={css.workspace}>
        <span className={css.sidebar} aria-hidden="true"><i /><i /><i /><i /></span>
        <span className={css.page}>
          <b>{title}</b>
          <i /><i /><i />
        </span>
      </div>
    </div>
  );
}

/** A chat app on the internet, asking about the person's Leafen. */
function ChatApp({ W }: { W: OutsideGuideWords }) {
  return (
    <div className={css.window}>
      <GuideWindowBar title={W.chatApp} />
      <div className={css.chat}>
        <span className={css.ask}>{W.chatAsk}</span>
        <span className={css.answer}>{W.chatAnswer}</span>
      </div>
    </div>
  );
}

/** Something on a computer beside a phone; a narrow frame keeps the phone. */
function Pair({ left, phone }: { left: ReactNode; phone: ReactNode }) {
  return (
    <div className={css.pair}>
      <div className={css.left}>{left}</div>
      <div className={css.phoneColumn}>{phone}</div>
    </div>
  );
}

// The four scenes, in order: today only this computer opens Leafen and a
// phone can't; a free Tailscale account signs in; Leafen has an address of
// its own; a phone and a chat app on the internet open it there.
export function outsideAddressGuideScreens(look: TailscaleGuideLook, W: OutsideGuideWords): ReactNode[] {
  const address = TS.address.replace(/^https:\/\//u, "");
  return [
    <Pair
      left={<Workspace host={W.here} title={W.pageTitle} />}
      phone={(
        <Phone host={W.here}>
          <span className={css.unreachable}>
            <i aria-hidden="true" />
            {W.unreachable}
          </span>
        </Phone>
      )}
    />,
    <TailscaleSignIn />,
    <LeafenWindow look={look} title={W.doneTitle}>
      <code className={styles.address}>{TS.address}</code>
      <figure className={styles.qr}>
        <GuideQR index={2} />
        <figcaption>{W.addressQR}</figcaption>
      </figure>
      <span className={styles.leafenButton}>{W.finish}</span>
    </LeafenWindow>,
    <Pair
      left={<ChatApp W={W} />}
      phone={(
        <Phone host={address}>
          <span className={css.phonePage}>
            <b>{W.pageTitle}</b>
            <i /><i /><i /><i />
          </span>
        </Phone>
      )}
    />,
  ];
}
