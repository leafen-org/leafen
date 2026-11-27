import { i18next } from "@/i18n";

/** The ID prefix of a Notion person an import kept before anyone here was matched to them. */
export const NOTION_USER_PREFIX = "notion-user:";

/**
 * What a Notion person an import kept, not matched to anyone here yet, is
 * called: a Notion user named by a short code that tells two of them apart;
 * "" for any other ID.
 */
export function notionUserPlaceholder(id: string) {
  if (!id.startsWith(NOTION_USER_PREFIX) || id.length === NOTION_USER_PREFIX.length) return "";
  let hash = 0x811c9dc5;
  for (let index = NOTION_USER_PREFIX.length; index < id.length; index += 1) {
    hash ^= id.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193);
  }
  const identifier = (hash >>> 0).toString(36).toUpperCase().padStart(7, "0");
  return i18next.t("peopleDirectory:notionUser", { identifier });
}
