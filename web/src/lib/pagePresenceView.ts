import type { TFunction } from "i18next";

export type PagePresenceConnectionStatus =
  | "auth_lost"
  | "connected"
  | "connecting"
  | "disconnected"
  | "idle"
  | "kicked"
  | "reconnecting";

export interface PagePresencePeerView {
  color: string;
  isCurrent: boolean;
  label: string;
  memberId: string;
  updatedAt?: string;
  userId: string;
}

export interface PagePresenceAwarenessView {
  blockId?: string;
  color: string;
  label: string;
  memberId?: string;
  mode: "editing" | "selecting";
  selectedBlockIds: string[];
  textRange?: { end: number; start: number };
  updatedAt: number;
  userId: string;
}

export interface PagePresenceSnapshot {
  activeCount: number;
  awareness: PagePresenceAwarenessView[];
  /** Others in the room with the page out of sight (away), whom the peers leave out. */
  away?: readonly string[];
  peers: PagePresencePeerView[];
  /** Others with the page in sight who may only read or comment it: counted, never named (the server sends no more). */
  readers?: number;
  recoveryFailed: boolean;
  status: PagePresenceConnectionStatus;
}

/** Said in the screen's language, as the indicator's title and its name to a screen reader. */
export function pagePresenceStatusLabel(
  status: PagePresenceConnectionStatus,
  activeCount: number,
  t: TFunction,
) {
  if (status === "connected") return t("topBar:presenceConnected", { people: activeCount });
  if (status === "connecting") return t("topBar:presenceConnecting");
  if (status === "reconnecting") return t("topBar:presenceReconnecting");
  if (status === "auth_lost") return t("topBar:presenceAuthLost");
  if (status === "kicked") return t("topBar:presenceDisconnected");
  return t("topBar:presenceOffline");
}

export function pagePresenceAwarenessLabel(
  awareness: readonly PagePresenceAwarenessView[],
  t: TFunction,
) {
  const first = awareness[0];
  if (!first) return "";
  const doing = t(first.mode === "selecting" ? "topBar:presenceSelecting" : "topBar:presenceEditing", { name: first.label });
  const suffix = awareness.length > 1 ? ` +${awareness.length - 1}` : "";
  return `${doing}${suffix}`;
}
