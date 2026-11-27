import type { MentionItem } from "./blockPickerModel";
import { displayPreferences } from "@/lib/displayPreferences";
import { dateMentionLabel } from "./dateMentions";

/**
 * The text and attributes a picked @ menu item becomes in the block's text.
 * A person, page or date mention is followed by a space, the caret after it,
 * as Notion's are in the body and comments, a space already there or not
 * (seen 2026-10-27), and so are a database template's "@오늘", "@지금" and
 * "@나" (seen 2026-11-09) and an automation field's chip, so an "@" typed
 * next opens the menu again (seen 2026-11-01).
 */
export function mentionInsertion(item: MentionItem): { text: string; attributes: Record<string, string>; spaced?: true } | null {
  switch (item.kind) {
    case "date": {
      // A new date mention takes its maker's default format.
      const dateFormat = displayPreferences().dateMentionFormat;
      return {
        text: `@${dateMentionLabel(item.date, undefined, dateFormat)}`,
        attributes: {
          mention: "date",
          date: item.date,
          dateFormat,
          ...(item.dateTimeZone ? { dateTimeZone: item.dateTimeZone } : {}),
          ...(item.dateReminder ? { dateReminder: item.dateReminder } : {}),
        },
        spaced: true,
      };
    }
    case "person":
      // The menu marks the person themself "(나)" after their name; the mention keeps the name alone, as Notion's (seen 2026-10-04).
      return { text: `@${item.label}`, attributes: { mention: "person", userId: item.userId }, spaced: true };
    case "template":
      return { text: `@${item.label}`, attributes: { mention: "template", template: item.template }, spaced: true };
    case "automation":
      return item.opens ? null : {
        text: item.chip ?? item.label, attributes: { mention: "automation", automation: item.token, automationType: item.tokenType },
        spaced: true,
      };
    case "page":
      // A database held in a page's body is mentioned as a database, as Notion's "@" makes it (seen 2026-11-10).
      return item.databaseId
        ? { text: item.label, attributes: { mention: "database", databaseId: item.databaseId }, spaced: true }
        : { text: item.label, attributes: { mention: "page", pageId: item.pageId }, spaced: true };
    default:
      return null;
  }
}
