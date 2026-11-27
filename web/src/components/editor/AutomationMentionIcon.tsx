import { PropertyTypeIcon } from "@/components/database/PropertyTypeIcon";
import { CalendarIcon, PersonCircleIcon, type LeafenIconSize } from "@/icons/leafen";
import type { PropertyType } from "@/lib/types";

// The run's own people: who ran it and the page's maker.
const RUN_PEOPLE: ReadonlySet<string> = new Set(["executionActor", "pageCreator"]);

/**
 * An automation mention's icon, in its chip and in the "@" menu, as Notion's
 * (seen 2026-11-01): the run's people a person in a circle, filled, where a
 * people property shows two; a time or a date, a date property's too, the
 * dotted calendar, where the property lists draw a date property's plain one
 * (DatePropertyIcon, seen 2026-11-11); a property, a page's field, the page
 * or a variable its kind's icon.
 */
export function AutomationMentionIcon({ token, type, size }: { token: string; type: string; size: LeafenIconSize }) {
  if (RUN_PEOPLE.has(token)) return <PersonCircleIcon size={size} weight="fill" aria-hidden="true" />;
  if (type === "date") return <CalendarIcon size={size} aria-hidden="true" />;
  return <PropertyTypeIcon type={(type || "rich_text") as PropertyType} size={size} />;
}
