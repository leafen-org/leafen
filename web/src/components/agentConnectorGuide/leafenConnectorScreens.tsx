import type { ReactNode } from "react";
import { Trans } from "react-i18next";
import { GuideCaret, GuideCursor, GuideWindowBar } from "../guide/GuideParts";
import css from "./LeafenConnectorScreens.module.css";

/** Leafen's own words in its screens, read from the catalogs the screens themselves use. */
export type LeafenWords = {
  /** The outside address's host, in the sign-in window's bar. */
  host: string;
  mcpUrl: string;
  appRow: string;
  appHint: string;
  copyAddress: string;
  copied: string;
  codeTitle: string;
  codeHint: string;
  codeLabel: string;
  continueLabel: string;
  scanCode: string;
  consentTitle: string;
  consentAccess: string;
  consentLegend: string;
  deny: string;
  allow: string;
  workspace: string;
  /** The app asking to connect, as the allow screen names it. */
  client: string;
};

// The code a signed-in device's 새 기기 로그인 shows, as the code field's own example.
const CODE = "1234-5678-9012";

function Mark({ size }: { size: number }) {
  return <img className={css.mark} src="/mark-128.png" alt="" width={size} height={size} />;
}

/**
 * Leafen's screens in a connector guide, drawn as Leafen draws them and in its
 * words: the connector row's "Copy address" (Settings › Agents and the first
 * run's agents step), the sign-in by a code a signed-in device gives (the
 * outside address is another site to the browser, so it may ask), and the
 * MCP allow screen. They follow Leafen's own light and dark theme.
 */
export function leafenScreens(W: LeafenWords): { copy: ReactNode; signIn: ReactNode; allow: ReactNode } {
  return {
    copy: (
      <div className={css.leafen}>
        <GuideWindowBar title="Leafen" />
        <div className={css.settings}>
          <div className={css.row}>
            <div className={css.rowText}>
              <strong>{W.appRow}</strong>
              <span className={css.code}>{W.mcpUrl}</span>
              <span className={css.hint}>{W.appHint}</span>
            </div>
            <span className={css.button} data-guide-target="copy">
              {W.copyAddress}
              <GuideCursor path={[{ at: 300, x: -150, y: 70 }, { at: 1100, x: 0, y: 0 }]} clickAt={1400} />
            </span>
          </div>
        </div>
        <span className={css.toast} role="presentation">{W.copied}</span>
      </div>
    ),
    signIn: (
      <div className={css.leafen}>
        <GuideWindowBar title={W.host} />
        <div className={css.page}>
          <div className={css.auth}>
            <span className={css.brand}>
              <Mark size={22} />
              Leafen
            </span>
            <strong className={css.authTitle}>{W.codeTitle}</strong>
            <span className={css.authHint}>{W.codeHint}</span>
            <span className={css.label}>{W.codeLabel}</span>
            <span className={css.input}>
              <span className={css.typed}>{CODE}</span>
              <GuideCaret />
            </span>
            <span className={css.primary} data-guide-target="continue">
              {W.continueLabel}
              <GuideCursor path={[{ at: 300, x: 120, y: 60 }, { at: 1500, x: 120, y: 60 }, { at: 2100, x: 0, y: 0 }]} clickAt={2400} />
            </span>
            <span className={css.secondary}>{W.scanCode}</span>
          </div>
        </div>
      </div>
    ),
    allow: (
      <div className={css.leafen}>
        <GuideWindowBar title={W.host} />
        <div className={`${css.page} ${css.consentPage}`}>
          <div className={css.card}>
            <Mark size={24} />
            <strong className={css.cardTitle}>{W.consentTitle}</strong>
            <span className={css.intro}>
              <Trans i18nKey="authGate:mcpConsent.chooseWorkspaces" values={{ client: W.client }}
                components={{ client: <b /> }} />
            </span>
            <span className={css.note}><span>{W.consentAccess}</span></span>
            <span className={css.legend}>{W.consentLegend}</span>
            <span className={css.workspace}>
              <i className={css.checkbox} aria-hidden="true" />
              {W.workspace}
            </span>
            <span className={css.actions}>
              <span className={css.deny}>{W.deny}</span>
              <span className={css.allow} data-guide-target="allow">
                {W.allow}
                <GuideCursor path={[{ at: 300, x: -170, y: -110 }, { at: 1200, x: 0, y: 0 }]} clickAt={1500} />
              </span>
            </span>
          </div>
        </div>
      </div>
    ),
  };
}
