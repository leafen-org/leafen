import type { GraphValueType } from "@/contentGraph";
import { FileBlank, iconPixels, type LeafenIconSize } from "@/icons/leafen";
import { ClockTypeMark, PersonCircleTypeMark } from "@/icons/leafen/propertyTypeMarks";
import { BuiltInIconGlyph } from "@/components/page/PageIcon";
import { parseNativeIconValue } from "@/components/page/nativeIcons";
import { propertyMarkClass, PropertyTypeIcon } from "./PropertyTypeIcon";

type PropertyIconSource = {
  type: GraphValueType | string;
  config?: unknown;
};

/** The built-in icon a property was given (config.icon, "name:color"), if any. */
export function propertyOwnIcon(property: PropertyIconSource) {
  const icon = (property.config as { icon?: unknown } | null | undefined)?.icon;
  return typeof icon === "string" ? parseNativeIconValue(icon) : undefined;
}

/**
 * A property's icon wherever the property is named — the table head, a
 * database page's properties, the filter and property lists: the icon it was
 * given, as Notion lets each property have one (seen 2026-09-30), or else its
 * type's icon. A given icon's box keeps its size in a row as the type's
 * does, never taking the room a row's name grows into (2026-11-14: the
 * layout builder's lists pushed "작업 유형" to the row's middle; Notion's
 * mark is a 20 box, the name 8 after it, whichever icon it is).
 */
export function PropertyIcon({ property, size = "small" }: { property: PropertyIconSource; size?: LeafenIconSize }) {
  const mark = (property.config as { mark?: unknown } | null | undefined)?.mark;
  if (mark === "page" || mark === "clock" || mark === "person") return <PropertyMark mark={mark} size={size} />;
  const icon = propertyOwnIcon(property);
  if (!icon) return <PropertyTypeIcon type={property.type as GraphValueType} size={size} />;
  return (
    <span aria-hidden="true" className={propertyMarkClass(size)} style={{ display: "inline-flex", flex: "none" }} data-property-icon={`${icon.name}:${icon.color}`}>
      {/* Notion's default gray and light gray sit in the place's icon color, as the type icons beside
          it (seen 2026-11-06 on a Sprints database's two rollups; 2026-11-15 a task tracker's "노력 수준"
          and "작업 유형", gray, drawn in the head's icoSec as its other icons); another color is its own. */}
      <BuiltInIconGlyph icon={icon} size={iconPixels(size)} color={icon.color === "lightgray" || icon.color === "gray" ? "currentColor" : undefined} />
    </span>
  );
}

/** What a property made in memory may name as its mark (config.mark) in place of its type's. */
export type PropertyMarkName = "page" | "clock" | "person";

/**
 * A filled mark that stands for a property where its type's would not, as
 * the library's columns (Notion's library, seen 2026-11-14: its "필터" and
 * "정렬" lists, pills and settings draw the marks its table heads do — a
 * filled page for the name, a filled clock for the times, a filled person
 * for who made or last edited a page — not "Aa" or a clock's outline), the
 * clock and person being the time and person types' own (propertyTypeMarks).
 */
export function PropertyMark({ mark, size = "small" }: { mark: PropertyMarkName; size?: LeafenIconSize }) {
  const className = propertyMarkClass(size);
  if (mark === "page") return <FileBlank size={size} weight="fill" className={className} aria-hidden="true" />;
  if (mark === "person") return <PersonCircleTypeMark size={size} className={className} aria-hidden="true" />;
  return <ClockTypeMark size={size} className={className} aria-hidden="true" />;
}
