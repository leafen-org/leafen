import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { GlobeIcon, McpTokenIcon, UserPlusIcon, X } from "@/icons/leafen";
import { outsideAddress } from "@/lib/addressReach";
import { hearTabs } from "@/lib/followAcrossTabs";
import type { PublicRuntimeConfig } from "@/lib/runtimeConfigContract";
import { fetchRuntimeConfig, subscribeRuntimeConfig } from "@/lib/runtimeConfigService";
import { readServerAdministrator } from "@/lib/serverAdministrator";
import { serverOrigin } from "@/lib/serverOrigin";
import type { SettingsSection } from "@/components/settings/settingsWindowModel";
import type { Session, Workspace } from "@/types";
import {
  cardsStillToCome, handleSidebarCard, putAwayGoneCards, sidebarCardNow,
  type SidebarCardFacts, type SidebarCardId,
} from "./sidebarCards";
import styles from "./SidebarCardSlot.module.css";

type Config = { outsideAddress: boolean };

// The window coming back reads the settings again unless they were read this short while ago.
const COME_BACK_READ_WITHIN = 5000;

const comingBack = (read: () => void) => {
  const comeBack = () => { if (document.visibilityState === "visible") read(); };
  window.addEventListener("focus", comeBack);
  return () => window.removeEventListener("focus", comeBack);
};

/**
 * The sidebar's card place over its foot (sidebarCards.ts: one card at a
 * time, in order, 24 hours empty after one is handled), each card drawn as
 * Notion's "멤버 초대" card (seen 2026-11-22 in Chrome at 1440, dark, in three
 * workspaces its owner holds): 16px in from the sidebar's sides and over the
 * foot's line, whatever the list holds. It reads only what the cards still to
 * come ask: whether this person administers the server, the server's outside
 * address (the runtime config), and whether this person has an agent
 * connected — as it shows, as the window comes back from elsewhere, and as
 * they change in this window: the settings read anew (refreshRuntimeConfig —
 * the address turned on or off) and the connected agents an agents screen
 * reads, so a card whose reason went goes at once (2026-11-22: the address
 * card stayed after the address was turned on, till a reload).
 */
export function SidebarCardSlot({ session, workspace, onInvite, onOpenSettings }: {
  session: Session;
  workspace: Workspace | null;
  onInvite: () => void;
  onOpenSettings: (section: SettingsSection) => void;
}) {
  const { t } = useTranslation(["sidebar", "workspaceSettingsDialog"]);
  const userId = session.user.id;
  const [handled, setHandled] = useState(0);
  const [administrator, setAdministrator] = useState<boolean | null>(null);
  const [config, setConfig] = useState<Config | null>(null);
  const [agentConnected, setAgentConnected] = useState<boolean | null>(null);
  const coming = cardsStillToCome(userId, workspace);
  const asksAdministrator = coming.includes("outside-address");
  const asksAgents = coming.includes("connect-agent");
  useEffect(() => {
    if (!asksAdministrator) return undefined;
    let live = true;
    void readServerAdministrator(session).then((value) => { if (live) setAdministrator(value); });
    return () => { live = false; };
  }, [asksAdministrator, session]);
  useEffect(() => {
    if (!asksAdministrator) return undefined;
    let live = true;
    const take = (read: PublicRuntimeConfig) => {
      // The address agents are given (AgentConnect), judged as everywhere (lib/addressReach.ts).
      const outside = outsideAddress(read.publicAddress || serverOrigin());
      if (live) setConfig({ outsideAddress: outside });
    };
    // A read that failed stands in with defaults: it says nothing of the cards.
    const read = (within?: number) => void fetchRuntimeConfig(within === undefined ? false : { within })
      .then(({ config: next, degradation, unreachable }) => { if (!degradation && !unreachable) take(next); }, () => undefined);
    read();
    const stop = subscribeRuntimeConfig(take);
    const stopComingBack = comingBack(() => read(COME_BACK_READ_WITHIN));
    return () => { live = false; stop(); stopComingBack(); };
  }, [asksAdministrator]);
  useEffect(() => {
    if (!asksAgents) return undefined;
    const controller = new AbortController();
    const read = () => void api.listMCPConnections(session, undefined, 1, controller.signal)
      .then((page) => setAgentConnected(page.activeConnections > 0), () => undefined);
    read();
    // What an agents screen of this person's reads of their active connections (useConnectedAgents).
    const stopHearing = hearTabs(`mcp-connections:${session.user.id}`, (value) => {
      if (!controller.signal.aborted && Array.isArray(value)) setAgentConnected(value.length > 0);
    });
    const stopComingBack = comingBack(read);
    return () => { controller.abort(); stopHearing(); stopComingBack(); };
  }, [asksAgents, session]);
  const facts: SidebarCardFacts = { administrator, outsideAddress: config?.outsideAddress ?? null, agentConnected };
  useEffect(() => {
    putAwayGoneCards(userId, workspace, { administrator, outsideAddress: config?.outsideAddress ?? null, agentConnected });
  }, [userId, workspace, administrator, config?.outsideAddress, agentConnected]);
  const id = sidebarCardNow(userId, workspace, facts);
  if (!id || !workspace) return null;
  const handle = (how: "press" | "dismiss") => {
    handleSidebarCard(userId, workspace, id, how);
    setHandled(handled + 1);
  };
  const cards: Record<SidebarCardId, { icon: ReactNode; title: string; body: string; open: () => void }> = {
    "outside-address": { icon: <GlobeIcon size="regular" aria-hidden="true" />, title: t("workspaceSettingsDialog:agents.outsideChoose"),
      body: t("sidebar:outsideAddressCardBody"), open: () => onOpenSettings("server-tunnel") },
    "connect-agent": { icon: <McpTokenIcon size="regular" aria-hidden="true" />, title: t("sidebar:connectAgent"),
      body: t("sidebar:agentCardBody"), open: () => onOpenSettings("connections") },
    "invite-members": { icon: <UserPlusIcon size="regular" aria-hidden="true" />, title: t("sidebar:inviteMembers"),
      body: t("sidebar:inviteCardBody"), open: onInvite },
  };
  const card = cards[id];
  // The invitation keeps its marks from before it took a line in sidebarCards.ts.
  const invite = id === "invite-members";
  return (
    <div className={styles.area} data-sidebar-card={id} data-sidebar-invite-card={invite ? "" : undefined}>
      <div className={styles.card}>
        <button type="button" className={styles.open} data-sidebar-card-open data-sidebar-invite-open={invite ? "" : undefined}
          onClick={() => { handle("press"); card.open(); }}>
          <span className={styles.icon}>{card.icon}</span>
          <span className={styles.text}>
            <span className={styles.title}>{card.title}</span>
            <span className={styles.body}>{card.body}</span>
          </span>
        </button>
        <button type="button" className={styles.dismiss} aria-label={t("sidebar:cardDismiss")}
          data-sidebar-card-dismiss data-sidebar-invite-dismiss={invite ? "" : undefined} onClick={() => handle("dismiss")}>
          <X size="tiny" aria-hidden="true" />
        </button>
      </div>
    </div>
  );
}
