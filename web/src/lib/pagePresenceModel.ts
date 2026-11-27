import type {
  PagePresenceAwarenessView,
  PagePresenceConnectionStatus,
  PagePresencePeerView,
  PagePresenceSnapshot,
} from "./pagePresenceView";
import type {
  PageMetaMutationPatch,
  PageRoomMutationChange,
  PageRoomMutationKind,
} from "./pageRoomEvents";

export const PAGE_PRESENCE_AWARENESS_FIELD = "leafenPresence";
const MAX_PAGE_PRESENCE_SELECTED_BLOCKS = 20;
const MAX_PAGE_PRESENCE_MUTATION_IDS = 100;

const PAGE_ROOM_MUTATION_KINDS = new Set<PageRoomMutationKind>([
  "page_meta_changed",
  "database_rows_changed",
  "database_schema_changed",
  "database_views_changed",
  "database_templates_changed",
  "block_structure_changed",
  "comments_changed",
  "permissions_changed",
]);

const PAGE_META_PATCH_KEYS = new Set<keyof PageMetaMutationPatch>([
  "backlinksDisplay",
  "cover",
  "coverPosition",
  "font",
  "fullWidth",
  "icon",
  "iconType",
  "isFavorite",
  "isLocked",
  "lastEditedBy",
  "pageCommentsDisplay",
  "smallText",
  "title",
  "updatedAt",
  "verificationExpiresAt",
  "verifiedAt",
  "verifiedBy",
]);

export type PagePresenceIdentity = {
  color?: string;
  label: string;
  memberId?: string;
  updatedAt?: string;
  userId: string;
};

export type PagePresenceAwarenessState = {
  blockId?: string;
  mode: "editing" | "idle" | "selecting";
  selectedBlockIds: string[];
  textRange?: { end: number; start: number };
  updatedAt: number;
};

export type PagePresenceMutationPulse = PageRoomMutationChange & {
  sequence: number;
};

export type PagePresenceWireState = {
  /** The page is out of sight in its tab: the room hears its changes but draws no face or cursor for it. */
  away?: boolean;
  awareness?: PagePresenceAwarenessState;
  identity: PagePresenceIdentity;
  mutation?: PagePresenceMutationPulse;
  pageId: string;
  sequence: number;
};

export const EMPTY_PAGE_PRESENCE_SNAPSHOT: PagePresenceSnapshot = Object.freeze({
  activeCount: 0,
  awareness: [],
  peers: [],
  recoveryFailed: false,
  status: "idle",
});

function record(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined;
}

function cleanString(value: unknown, maxLength = 512) {
  if (typeof value !== "string") return undefined;
  const clean = value.trim();
  return clean ? clean.slice(0, maxLength) : undefined;
}

function cleanStrings(value: unknown, limit: number) {
  if (!Array.isArray(value)) return undefined;
  const seen = new Set<string>();
  const clean: string[] = [];
  for (const item of value) {
    const id = cleanString(item, 512);
    if (!id || seen.has(id)) continue;
    seen.add(id);
    clean.push(id);
    if (clean.length >= limit) break;
  }
  return clean.length > 0 ? clean : undefined;
}

function cleanSequence(value: unknown) {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0
    ? value
    : undefined;
}

function cleanTextRange(value: unknown) {
  const input = record(value);
  if (!input || typeof input.start !== "number" || typeof input.end !== "number") {
    return undefined;
  }
  if (!Number.isFinite(input.start) || !Number.isFinite(input.end)) return undefined;
  const start = Math.max(0, Math.floor(input.start));
  const end = Math.max(0, Math.floor(input.end));
  return { start: Math.min(start, end), end: Math.max(start, end) };
}

function cleanIdentity(value: unknown): PagePresenceIdentity | undefined {
  const input = record(value);
  const userId = cleanString(input?.userId);
  if (!input || !userId) return undefined;
  return {
    color: cleanString(input.color, 64),
    label: cleanString(input.label) ?? "Guest",
    memberId: cleanString(input.memberId),
    updatedAt: cleanString(input.updatedAt, 128),
    userId,
  };
}

function cleanAwareness(value: unknown): PagePresenceAwarenessState | undefined {
  const input = record(value);
  if (!input) return undefined;
  const mode = input.mode;
  if (mode !== "editing" && mode !== "selecting" && mode !== "idle") return undefined;
  const updatedAt = typeof input.updatedAt === "number" && Number.isFinite(input.updatedAt)
    ? Math.max(0, Math.floor(input.updatedAt))
    : 0;
  return {
    blockId: cleanString(input.blockId),
    mode,
    selectedBlockIds: cleanStrings(
      input.selectedBlockIds,
      MAX_PAGE_PRESENCE_SELECTED_BLOCKS,
    ) ?? [],
    textRange: cleanTextRange(input.textRange),
    updatedAt,
  };
}

function cleanPatch(value: unknown): PageMetaMutationPatch | undefined {
  const input = record(value);
  if (!input) return undefined;
  const patch: Record<string, unknown> = {};
  for (const key of PAGE_META_PATCH_KEYS) {
    if (Object.prototype.hasOwnProperty.call(input, key)) patch[key] = input[key];
  }
  return Object.keys(patch).length > 0 ? patch as PageMetaMutationPatch : undefined;
}

export function cleanPagePresenceMutation(
  value: unknown,
  pageId: string,
): PagePresenceMutationPulse | undefined {
  const input = record(value);
  const kind = input?.kind;
  const sequence = cleanSequence(input?.sequence);
  if (
    !input ||
    input.pageId !== pageId ||
    typeof kind !== "string" ||
    !PAGE_ROOM_MUTATION_KINDS.has(kind as PageRoomMutationKind) ||
    sequence === undefined
  ) return undefined;
  return {
    blockIds: cleanStrings(input.blockIds ?? input.affectedIds, MAX_PAGE_PRESENCE_MUTATION_IDS),
    databaseId: cleanString(input.databaseId),
    kind: kind as PageRoomMutationKind,
    pageId,
    patch: cleanPatch(input.patch),
    propertyIds: cleanStrings(input.propertyIds, MAX_PAGE_PRESENCE_MUTATION_IDS),
    reason: cleanString(input.reason, 512),
    revision: typeof input.revision === "number" && Number.isFinite(input.revision)
      ? input.revision
      : undefined,
    rowIds: cleanStrings(input.rowIds, MAX_PAGE_PRESENCE_MUTATION_IDS),
    sequence,
    targetPageId: cleanString(input.targetPageId),
    updatedAt: cleanString(input.updatedAt, 128),
    viewIds: cleanStrings(input.viewIds, MAX_PAGE_PRESENCE_MUTATION_IDS),
  };
}

export function cleanPagePresenceState(
  value: unknown,
  pageId: string,
): PagePresenceWireState | undefined {
  const outer = record(value);
  const input = record(outer?.[PAGE_PRESENCE_AWARENESS_FIELD]) ?? outer;
  const identity = cleanIdentity(input?.identity);
  const sequence = cleanSequence(input?.sequence);
  if (!input || input.pageId !== pageId || !identity || sequence === undefined) return undefined;
  return {
    ...(input.away === true ? { away: true } : {}),
    awareness: cleanAwareness(input.awareness),
    identity,
    mutation: cleanPagePresenceMutation(input.mutation, pageId),
    pageId,
    sequence,
  };
}

/**
 * A reader's presence as the server passes it on (collaboration_reader_presence.go):
 * no one named, only the page and whether it is in sight.
 */
function readerInSight(value: unknown, pageId: string) {
  const input = record(record(value)?.[PAGE_PRESENCE_AWARENESS_FIELD]);
  return input?.reader === true && input.pageId === pageId && input.away !== true;
}

export function pagePresenceColor(userId: string) {
  const colors = ["#d9730d", "#0f7b6c", "#337ea9", "#9065b0", "#c14c8a", "#548164"];
  let hash = 0;
  for (let index = 0; index < userId.length; index += 1) {
    hash = (hash * 31 + userId.charCodeAt(index)) >>> 0;
  }
  return colors[hash % colors.length];
}

export function pagePresenceTerminalStatus(code: number): PagePresenceConnectionStatus {
  return code === 4401 ? "auth_lost" : "kicked";
}

function peer(clientId: number, state: PagePresenceWireState, currentUserId: string) {
  const identity = state.identity;
  return {
    color: identity.color ?? pagePresenceColor(identity.userId),
    isCurrent: identity.userId === currentUserId,
    label: identity.label,
    memberId: identity.memberId ?? `ygo:${clientId}`,
    updatedAt: identity.updatedAt,
    userId: identity.userId,
  } satisfies PagePresencePeerView;
}

export function projectPagePresenceSnapshot(
  states: ReadonlyMap<number, unknown>,
  pageId: string,
  currentUserId: string,
  status: PagePresenceConnectionStatus,
  recoveryFailed: boolean,
): PagePresenceSnapshot {
  const selected = new Map<string, { clientId: number; state: PagePresenceWireState }>();
  const away = new Set<string>();
  let readers = 0;
  for (const [clientId, raw] of states) {
    if (readerInSight(raw, pageId)) readers += 1;
    const state = cleanPagePresenceState(raw, pageId);
    // Only those with the page in sight are here, as in Notion: a person
    // whose page goes behind another tab leaves the others' bar at once and
    // comes back as they look again (seen 2026-11-13, 0.16 seconds each way).
    if (state?.away) away.add(state.identity.userId);
    if (!state || state.away) continue;
    const previous = selected.get(state.identity.userId);
    if (!previous || state.sequence > previous.state.sequence) {
      selected.set(state.identity.userId, { clientId, state });
    }
  }
  const entries = [...selected.values()];
  const peers = entries
    .map(({ clientId, state }) => peer(clientId, state, currentUserId))
    .sort((left, right) => Number(right.isCurrent) - Number(left.isCurrent) ||
      left.label.localeCompare(right.label));
  const awareness: PagePresenceAwarenessView[] = entries.flatMap(({ clientId, state }) => {
    const current = state.awareness;
    if (!current || current.mode === "idle" || state.identity.userId === currentUserId) return [];
    return [{
      ...peer(clientId, state, currentUserId),
      blockId: current.blockId,
      mode: current.mode,
      selectedBlockIds: current.selectedBlockIds,
      textRange: current.textRange,
      updatedAt: current.updatedAt,
    }];
  }).sort((left, right) => right.updatedAt - left.updatedAt);
  for (const userId of selected.keys()) away.delete(userId);
  return { activeCount: peers.length, awareness, away: [...away], peers, readers, recoveryFailed, status };
}
