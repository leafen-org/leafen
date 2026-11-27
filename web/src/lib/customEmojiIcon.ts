import { useEffect } from "react";
import { loadSession } from "@/lib/authSessionStore";
import { LeafenApiError } from "@/apiTransport";
import { getCustomEmoji, getWorkspaceCustomEmoji } from "@/lib/customEmoji";
import { resolveWorkspaceFileUrl } from "@/lib/fileUrls";
import { useStateUnlessSame } from "@/lib/useStateUnlessSame";

// A workspace emoji's picture file, read once a reader and kept for the
// screens that show it. Text that names the emoji without its workspace (a
// comment's, a database cell's) reads it by its id alone.

const MAX_CUSTOM_EMOJI_ICON_CACHE = 512;
/** What a reader sees of a workspace emoji: its picture file, its name now, and whether it was deleted. */
export type CustomEmojiShown = { fileId: string; name: string; gone: boolean };
const NOTHING: CustomEmojiShown = { fileId: "", name: "", gone: false };
const iconCache = new Map<string, { shown?: CustomEmojiShown; pending?: Promise<CustomEmojiShown> }>();
// The last name each emoji was read under, kept past a rename's or a
// deletion's fresh read: a deleted emoji's tip names it as it was last, as
// Notion's (seen 2026-11-07).
const lastNames = new Map<string, string>();
const listeners = new Map<string, Set<() => void>>();

function cacheKey(actorId: string, workspaceId: string, emojiId: string) {
  return `${actorId}\0${workspaceId}\0${emojiId}`;
}

function trimCache() {
  while (iconCache.size > MAX_CUSTOM_EMOJI_ICON_CACHE) {
    const oldest = iconCache.keys().next().value;
    if (typeof oldest !== "string") return;
    iconCache.delete(oldest);
  }
}

// An emoji the server no longer has: text that used it marks it deleted, as
// Notion's (seen 2026-11-07); any other failure leaves the text's own name.
const deleted = (error: unknown) => error instanceof LeafenApiError && error.code === "LEAFEN_CUSTOM_EMOJI_NOT_FOUND";

// An emoji ID as Leafen gives one; text an app wrote keeps any other as it
// was written (notion_missing_custom_emoji.go), no emoji's.
const EMOJI_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const GONE: CustomEmojiShown = { ...NOTHING, gone: true };

function loadIconShown(key: string, workspaceId: string, emojiId: string) {
  if (!EMOJI_ID.test(emojiId)) return Promise.resolve(GONE);
  const current = iconCache.get(key);
  if (current?.shown) return Promise.resolve(current.shown);
  if (current?.pending) return current.pending;
  const session = loadSession();
  if (!session) return Promise.resolve(NOTHING);
  const pending = (workspaceId ? getWorkspaceCustomEmoji(session, workspaceId, emojiId) : getCustomEmoji(session, emojiId))
    // A deleted one comes with the name it last had, kept by the server, as Notion's (seen 2026-11-07).
    .then((emoji): CustomEmojiShown => emoji.deleted ? { ...GONE, name: emoji.name } : { fileId: emoji.fileId, name: emoji.name, gone: false })
    .catch((error): CustomEmojiShown => ({ ...NOTHING, name: lastNames.get(key) ?? "", gone: deleted(error) }))
    .then((shown) => {
      if (shown.name) {
        lastNames.delete(key);
        lastNames.set(key, shown.name);
        for (const oldest of lastNames.keys()) {
          if (lastNames.size <= MAX_CUSTOM_EMOJI_ICON_CACHE) break;
          lastNames.delete(oldest);
        }
      }
      iconCache.delete(key);
      iconCache.set(key, { shown });
      trimCache();
      return shown;
    });
  iconCache.set(key, { pending });
  trimCache();
  return pending;
}

export function invalidateWorkspaceCustomEmojiIcon(workspaceId: string, emojiId: string) {
  for (const key of iconCache.keys()) {
    if (key.endsWith(`\0${workspaceId}\0${emojiId}`) || key.endsWith(`\0\0${emojiId}`)) iconCache.delete(key);
  }
  for (const [key, callbacks] of listeners) {
    if (!key.endsWith(`\0${workspaceId}\0${emojiId}`) && !key.endsWith(`\0\0${emojiId}`)) continue;
    for (const callback of callbacks) callback();
  }
}

/** An emoji's picture address, its name now and whether it was deleted, for the signed-in reader. */
export async function customEmojiShown(emojiId: string, workspaceId = "") {
  const actorId = loadSession()?.user.id ?? "";
  if (!actorId || !emojiId) return { url: "", name: "", gone: false };
  const shown = await loadIconShown(cacheKey(actorId, workspaceId, emojiId), workspaceId, emojiId);
  const url = shown.fileId ? await resolveWorkspaceFileUrl(shown.fileId).catch(() => "") : "";
  return { url, name: shown.name, gone: shown.gone };
}

/** A workspace emoji as its reader sees it now, kept up as it is renamed, replaced or deleted. */
export function useWorkspaceCustomEmoji(workspaceId?: string, emojiId?: string): CustomEmojiShown {
  // Text with no workspace emoji reads nothing, not even the sign-in, and
  // showing what is already shown (nothing, or the emoji kept here) queues
  // no update.
  const actorId = emojiId ? loadSession()?.user.id ?? "" : "";
  const key = actorId && emojiId ? cacheKey(actorId, workspaceId ?? "", emojiId) : "";
  const [shown, setShown] = useStateUnlessSame(() => key ? iconCache.get(key)?.shown ?? NOTHING : NOTHING);

  useEffect(() => {
    if (!key || !emojiId) {
      setShown(NOTHING);
      return;
    }
    let active = true;
    const refresh = () => {
      void loadIconShown(key, workspaceId ?? "", emojiId).then((next) => {
        if (active) setShown(next);
      });
    };
    const callbacks = listeners.get(key) ?? new Set<() => void>();
    callbacks.add(refresh);
    listeners.set(key, callbacks);
    setShown(iconCache.get(key)?.shown ?? NOTHING);
    refresh();
    return () => {
      active = false;
      callbacks.delete(refresh);
      if (callbacks.size === 0) listeners.delete(key);
    };
  }, [emojiId, key, workspaceId]);

  return shown;
}

export function useWorkspaceCustomEmojiFileId(workspaceId?: string, emojiId?: string) {
  return useWorkspaceCustomEmoji(workspaceId, emojiId).fileId;
}
