"use client";

import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { useProductPages } from "@/components/product/productPagesContext";
import { ChevronDown, LockIcon } from "@/icons/leafen";
import { WarningFillIcon } from "@/icons/leafen/automationIcons";
import { ServiceIcon } from "@/icons/leafen/serviceIcons";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { POPOVER_MARGIN } from "@/lib/popoverPlacement";
import type { Session } from "@/types";
import { AutomationChoicePopover, type AutomationChoice } from "./AutomationChoicePopover";
import styles from "./automationActionEditor.module.css";
import chipStyles from "./automationHeadChip.module.css";
import slackStyles from "./AutomationSlackChannel.module.css";

type SlackChannel = { id: string; name: string; private: boolean };
type SlackConnection = { id: string; channels: SlackChannel[]; incomplete?: boolean; error?: string };

// The server's Slack connections and their channels, read once a workspace
// (automation_slack_channels.go keeps Slack's answer a minute); a read Slack
// stopped short of its last channel is read again the next time a chip asks.
const read = new Map<string, Promise<SlackConnection[]>>();

function readSlackChannels(session: Session, workspaceId: string) {
  let pending = read.get(workspaceId);
  if (!pending) {
    pending = request<{ connections: SlackConnection[] }>(
      `/workspaces/${encodeURIComponent(workspaceId)}/automation-slack-channels`, {}, session,
    ).then((answer) => {
      const connections = answer.connections ?? [];
      if (connections.some((connection) => connection.incomplete)) read.delete(workspaceId);
      return connections;
    }, (error) => { read.delete(workspaceId); throw error; });
    read.set(workspaceId, pending);
  }
  return pending;
}

/** The channels to choose from, null until the server has said; none when it has no Slack connection. */
function useSlackConnections(workspaceHint?: string) {
  const product = useProductPages();
  const session = product?.session;
  const workspaceId = product?.workspaceId || workspaceHint;
  const [connections, setConnections] = useState<SlackConnection[] | null>(null);
  useEffect(() => {
    if (!session || !workspaceId) return;
    let alive = true;
    readSlackChannels(session, workspaceId).then((found) => { if (alive) setConnections(found); }, () => { if (alive) setConnections([]); });
    return () => { alive = false; };
  }, [session, workspaceId]);
  return connections;
}

/**
 * A Slack card's channel in its head, "[Slack 채널 선택] 에 알리기" as Notion's
 * (seen 2026-11-09, 2026-11-15): the channel chosen, or the call to choose
 * one, red with what it lacks once asked for. Pressed, it lists the channels
 * of the server's Slack connections to choose from, as Notion's lists the
 * connected Slack's; with none it says Slack isn't connected, in Notion's
 * "Slack에 연결" box (seen 2026-11-15) — here the server operator's Slack bot
 * stands for Notion's own connecting (automation_slack_channels.go), so the
 * box tells whom to ask in place of Notion's "연결".
 */
export function AutomationSlackChannelChip({ connectionId, channelId, number, warn = false, workspaceId, onChoose }: {
  connectionId: string; channelId: string; number: number;
  /** Whether a missing channel shows red, on an action asked for, and what it says then. */
  warn?: boolean;
  workspaceId?: string;
  onChoose(connectionId: string, channelId: string): void;
}) {
  const { t } = useTranslation("databaseView");
  const connections = useSlackConnections(workspaceId);
  const [open, setOpen] = useState(false);
  const chip = useRef<HTMLButtonElement>(null);
  const known = connections?.find((connection) => connection.id === connectionId)?.channels.find((channel) => channel.id === channelId);
  const shown = channelId.trim() ? `#${known?.name ?? channelId.trim()}` : t("databaseView:slackChannelChoose");
  const many = (connections?.length ?? 0) > 1;
  const choices: AutomationChoice[] = (connections ?? []).flatMap((connection) => connection.channels.map((channel) => ({
    key: `${connection.id}\u0000${channel.id}`,
    text: channel.name,
    icon: channel.private ? <LockIcon size="small" /> : <span className={slackStyles.hash}>#</span>,
    chosen: connection.id === connectionId && channel.id === channelId,
    ...(many ? { group: connection.id } : {}),
  })));
  const unreadable = (connections ?? []).length > 0 && (connections ?? []).every((connection) => connection.error);
  // Slack stopped answering before a bot's last channel: those read are to choose from, and it says more weren't.
  const incomplete = (connections ?? []).some((connection) => connection.incomplete);
  return (
    <>
      <button ref={chip} type="button" className={`${styles.targetButton} ${chipStyles.chip}`} aria-label={t("databaseView:channelForAction", { number })}
        aria-invalid={warn || undefined} aria-expanded={open} data-automation-slack-channel
        onClick={() => setOpen((current) => !current)}>
        {warn ? <WarningFillIcon size="tiny" aria-hidden="true" /> : null}<span>{shown}</span><ChevronDown size="tiny" aria-hidden="true" />
      </button>
      {open && connections !== null && connections.length > 0 ? (
        <AutomationChoicePopover choices={choices} label={t("databaseView:channelForAction", { number })} trigger={chip.current}
          title={incomplete ? t("databaseView:slackChannelsIncomplete") : undefined}
          searchPlaceholder={t("databaseView:slackChannelSearch")} emptyText={t(unreadable ? "databaseView:slackChannelsUnreadable" : "databaseView:slackChannelNone")}
          onPick={(key) => { const [connection, channel] = key.split("\u0000"); onChoose(connection, channel); }}
          onClose={() => setOpen(false)} />
      ) : null}
      {open && connections !== null && connections.length === 0 ? <SlackNotConnected trigger={chip.current} onClose={() => setOpen(false)} /> : null}
    </>
  );
}

/**
 * Notion's "Slack에 연결" box (seen 2026-11-15, 1440 dark): 266px wide, its end
 * at the chip's end, Slack's mark, a 16px title and 13px grey words centred,
 * then a full-width 32px button and "취소" under it.
 */
function SlackNotConnected({ trigger, onClose }: { trigger: HTMLElement | null; onClose(): void }) {
  const { t } = useTranslation("databaseView");
  const ref = useRef<HTMLDivElement>(null);
  usePopoverAppear(true, ref, () => trigger);
  const layer = useOverlayLayer(true, { kind: "menu", ref, onEscape: onClose, onOutsidePress: onClose, inside: () => [trigger],
    returnFocusRef: { current: trigger } });
  const [place, setPlace] = useState<CSSProperties>({ visibility: "hidden" });
  useLayoutEffect(() => {
    const rect = trigger?.getBoundingClientRect();
    const width = ref.current?.offsetWidth ?? 266;
    const height = ref.current?.offsetHeight ?? 0;
    if (!rect) return;
    const left = Math.max(POPOVER_MARGIN, Math.min(rect.right - width, window.innerWidth - width - POPOVER_MARGIN));
    const below = rect.bottom + height + POPOVER_MARGIN <= window.innerHeight;
    setPlace(below ? { top: rect.bottom, left } : { top: Math.max(POPOVER_MARGIN, rect.top - height), left });
  }, [trigger]);
  useLayoutEffect(() => { ref.current?.querySelector<HTMLButtonElement>("button")?.focus({ preventScroll: true }); }, []);
  if (typeof document === "undefined") return null;
  return createPortal(
    <div ref={ref} role="dialog" aria-label={t("databaseView:slackConnectTitle")} className={slackStyles.connect}
      style={{ ...place, zIndex: layer.zIndex }} onKeyDown={layer.onKeyDown} data-automation-slack-connect>
      <ServiceIcon service="slack" name="Slack" size="large" className={slackStyles.mark} aria-hidden="true" />
      <strong className={slackStyles.title}>{t("databaseView:slackConnectTitle")}</strong>
      <p className={slackStyles.words}>{t("databaseView:slackConnectNotConfigured")}</p>
      <button type="button" className={slackStyles.close} onClick={onClose}>{t("common:actions.close")}</button>
    </div>,
    document.body,
  );
}
