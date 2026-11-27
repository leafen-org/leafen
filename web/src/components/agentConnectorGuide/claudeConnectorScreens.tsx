import type { ReactNode } from "react";
import { GuideCaret, GuideClick, GuideCursor, GuideWindowBar } from "../guide/GuideParts";
import { ScreenIcon, type ScreenIconName } from "./screenIcons";
import dialogCss from "./ClaudeConnectorDialog.module.css";
import css from "./ClaudeConnectorScreens.module.css";

/** Claude's words in the guide's Claude screens (agentConnectorGuide:claude.mock). */
export type ClaudeWords = {
  address: string;
  new: string;
  search: string;
  projects: string;
  artifacts: string;
  customize: string;
  skills: string;
  connectors: string;
  plugins: string;
  yours: string;
  discover: string;
  add: string;
  addCustom: string;
  connector: string;
  type: string;
  status: string;
  web: string;
  connect: string;
  intro: string;
  name: string;
  nameHint: string;
  url: string;
  urlHint: string;
  cancel: string;
  continue: string;
  authentication: string;
  signInNow: string;
  signInWhenNeeded: string;
  noSignIn: string;
  oauthClient: string;
  publishedIdentity: string;
  recommended: string;
  registerAutomatically: string;
  ownClient: string;
  /** "Connected to {name}." with the connector's name in. */
  connected: string;
  /** The connector's name as typed, and the address pasted. */
  connectorName: string;
  mcpUrl: string;
};

const SIDEBAR: ReadonlyArray<[keyof ClaudeWords, ScreenIconName]> = [
  ["new", "plus"],
  ["search", "search"],
  ["projects", "stack"],
  ["artifacts", "shapes"],
];

/**
 * Claude's page around the connectors list (Customize › Connectors, seen in
 * Claude on the web 2026-11-08): the sidebar with Customize, the page's tabs
 * and "+ Add", and the list. `tab` is which tab shows as chosen ("switch":
 * Skills, then Connectors as the cursor presses it); rows go in the list,
 * `menu` opens under "+ Add", and `clicks` are when the cursor presses which.
 */
function ClaudePage({ W, tab, rows, menu, clicks = {}, children }: {
  W: ClaudeWords;
  tab: "connectors" | "switch";
  rows?: ReactNode;
  menu?: ReactNode;
  clicks?: Partial<Record<"customize" | "connectors" | "add", number>>;
  children?: ReactNode;
}) {
  const click = (on: keyof typeof clicks) => (clicks[on] === undefined ? null : <GuideClick at={clicks[on]} />);
  return (
    <div className={css.claude}>
      <GuideWindowBar title={W.address} />
      <div className={css.app}>
        <div className={css.side}>
          <span className={css.logo}>
            <ScreenIcon name="spark" />
            <span>Claude</span>
          </span>
          {SIDEBAR.map(([key, icon]) => (
            <span key={key} className={css.sideItem}>
              <ScreenIcon name={icon} />
              <span>{W[key]}</span>
            </span>
          ))}
          <span className={css.sideItem} data-on="" data-guide-target="customize">
            <ScreenIcon name="case" />
            <span>{W.customize}</span>
            {click("customize")}
          </span>
        </div>
        <div className={css.main}>
          <strong className={css.title}>{W.customize}</strong>
          <div className={css.toolbar}>
            <span className={css.tabs}>
              <span className={tab === "switch" ? css.tabOff : undefined}>{W.skills}</span>
              <span className={tab === "switch" ? css.tabOn : css.tabStill} data-guide-target="connectors">
                {W.connectors}
                {click("connectors")}
              </span>
              <span className={css.wideOnly}>{W.plugins}</span>
            </span>
            <span className={css.segment}>
              <span className={css.tabStill}>{W.yours}</span>
              <span>{W.discover}</span>
            </span>
            <span className={css.add} data-guide-target="add">
              <ScreenIcon name="plus" />
              {W.add}
              <ScreenIcon name="chevron" />
              {click("add")}
              {menu}
            </span>
          </div>
          <div className={tab === "switch" ? `${css.table} ${css.tableLate}` : css.table}>
            <span className={css.head}>
              <span>{W.connector}</span>
              <span className={css.wideOnly}>{W.type}</span>
              <span>{W.status}</span>
            </span>
            {rows}
          </div>
        </div>
      </div>
      {children}
    </div>
  );
}

/** The list's line for the connector just added; `connect` draws its "Connect" button. */
function LeafenRow({ W, connect }: { W: ClaudeWords; connect?: ReactNode }) {
  return (
    <span className={css.listRow}>
      <span className={css.rowName}>
        <i className={css.rowIcon} aria-hidden="true"><ScreenIcon name="globe" /></i>
        {W.connectorName}
      </span>
      <span className={`${css.muted} ${css.wideOnly}`}>{W.web}</span>
      <span>{connect}</span>
    </span>
  );
}

/** "Add custom connector", its first step (name and address) or its second (how it signs in). */
function AddDialog({ W, children }: { W: ClaudeWords; children: ReactNode }) {
  return (
    <div className={dialogCss.backdrop}>
      <div className={dialogCss.dialog}>
        <span className={dialogCss.dialogHead}>
          <strong>{W.addCustom}</strong>
          <ScreenIcon name="close" />
        </span>
        {children}
      </div>
    </div>
  );
}

function Radio({ state }: { state: "on" | "off" | "turnsOn" | "turnsOff" }) {
  const look = { on: dialogCss.radioOn, off: "", turnsOn: dialogCss.radioTurnsOn, turnsOff: dialogCss.radioTurnsOff }[state];
  return <i className={`${dialogCss.radio} ${look}`} aria-hidden="true" />;
}

/**
 * The guide's Claude screens: Customize › Connectors and "+ Add" › "Add custom
 * connector", the dialog's two steps, the list with "Connect", and Claude
 * saying it connected. Claude's own light and dark colors, following the app's.
 */
export function claudeScreens(W: ClaudeWords): Record<"open" | "address" | "auth" | "connect" | "done", ReactNode> {
  return {
    open: (
      <ClaudePage W={W} tab="switch" clicks={{ customize: 550, connectors: 1350, add: 2250 }} menu={(
        <span className={css.menu}>
          <span className={css.menuItem} data-guide-target="addCustom">
            {W.addCustom}
            <GuideCursor
              clickAt={3150}
              path={[
                { at: 0, on: "customize", x: 40, y: 30 },
                { at: 450, on: "customize", x: 4, y: 2 },
                { at: 700, on: "customize", x: 4, y: 2 },
                { at: 1250, on: "connectors", x: 2, y: 1 },
                { at: 1500, on: "connectors", x: 2, y: 1 },
                { at: 2150, on: "add", x: 0, y: 1 },
                { at: 2400, on: "add", x: 0, y: 1 },
                { at: 3050, x: 0, y: 0 },
              ]}
            />
          </span>
        </span>
      )} />
    ),
    address: (
      <ClaudePage W={W} tab="connectors">
        <AddDialog W={W}>
          <span className={dialogCss.intro}>{W.intro}</span>
          <span className={dialogCss.field}>
            <span className={dialogCss.typedName}>{W.connectorName}</span>
          </span>
          <span className={dialogCss.hint}>{W.nameHint}</span>
          <span className={`${dialogCss.field} ${dialogCss.fieldFocus}`}>
            <span className={dialogCss.placeholder}>{W.url}</span>
            <span className={dialogCss.pasted}>{W.mcpUrl}</span>
            <GuideCaret />
          </span>
          <span className={`${dialogCss.hint} ${dialogCss.clamp}`}>{W.urlHint}</span>
          <span className={dialogCss.footer}>
            <span className={dialogCss.secondary}>{W.cancel}</span>
            <span className={`${dialogCss.primary} ${dialogCss.primaryLate}`} data-guide-target="continue">
              {W.continue}
              <GuideCursor path={[{ at: 300, x: -150, y: -60 }, { at: 1500, x: -150, y: -60 }, { at: 2200, x: 0, y: 0 }]} clickAt={2500} />
            </span>
          </span>
        </AddDialog>
      </ClaudePage>
    ),
    auth: (
      <ClaudePage W={W} tab="connectors">
        <AddDialog W={W}>
          <span className={dialogCss.section}>{W.authentication}</span>
          <span className={dialogCss.option}><Radio state="on" />{W.signInNow}</span>
          <span className={dialogCss.option}><Radio state="off" />{W.signInWhenNeeded}</span>
          <span className={dialogCss.option}><Radio state="off" />{W.noSignIn}</span>
          <span className={dialogCss.section}>{W.oauthClient}</span>
          <span className={dialogCss.option}>
            <Radio state="turnsOff" />
            {W.publishedIdentity}
            <em className={dialogCss.badge}>{W.recommended}</em>
          </span>
          <span className={dialogCss.option} data-guide-target="register">
            <Radio state="turnsOn" />
            {W.registerAutomatically}
            <GuideClick at={1100} />
          </span>
          <span className={dialogCss.option}><Radio state="off" />{W.ownClient}</span>
          <span className={dialogCss.footer}>
            <span className={dialogCss.secondary}>{W.cancel}</span>
            <span className={dialogCss.primary} data-guide-target="addConnector">
              {W.add}
              <GuideCursor
                clickAt={2600}
                path={[
                  { at: 0, on: "register", x: 60, y: -40 },
                  { at: 900, on: "register", x: -30, y: 1 },
                  { at: 1400, on: "register", x: -30, y: 1 },
                  { at: 2300, x: 0, y: 0 },
                ]}
              />
            </span>
          </span>
        </AddDialog>
      </ClaudePage>
    ),
    connect: (
      <ClaudePage W={W} tab="connectors" rows={
        <LeafenRow W={W} connect={(
          <span className={css.connect}>
            {W.connect}
            <GuideCursor path={[{ at: 300, x: -170, y: 80 }, { at: 1200, x: 0, y: 0 }]} clickAt={1500} />
          </span>
        )} />
      } />
    ),
    done: (
      <ClaudePage W={W} tab="connectors" rows={<LeafenRow W={W} />}>
        <span className={css.toast}>
          <ScreenIcon name="check" />
          {W.connected}
        </span>
      </ClaudePage>
    ),
  };
}
