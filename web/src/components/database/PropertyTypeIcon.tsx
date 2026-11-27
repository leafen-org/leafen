import { type LeafenIconSize } from "@/icons/leafen";
import { ListResultIcon, QuestionMarkIcon } from "@/icons/leafen/automationIcons";
import {
  AtTypeMark, ButtonTypeMark, CheckboxTypeMark, ClockTypeMark, FileTypeMark, FormulaTypeMark, MultiSelectTypeMark, NumberTypeMark,
  NumeroTypeMark, PeopleTypeMark, PersonCircleTypeMark, PhoneTypeMark, PlaceTypeMark, RelationTypeMark, RollupTypeMark, StatusTypeMark, TextTypeMark,
  TitleTypeMark, UrlTypeMark, VerificationTypeMark,
} from "@/icons/leafen/propertyTypeMarks";
import type { GraphValueType } from "../../contentGraph";
import { Copy, DatePropertyIcon, EyeIcon, SelectIcon } from "../icons";
import styles from "./PropertyTypeIcon.module.css";

type RetainedPropertyType =
  | "title"
  | "rich_text"
  | "number"
  | "select"
  | "multi_select"
  | "status"
  | "date"
  | "person"
  | "checkbox"
  | "url"
  | "email"
  | "phone"
  | "files"
  | "created_time"
  | "last_edited_time"
  | "created_by"
  | "last_edited_by"
  | "relation"
  | "rollup"
  | "formula"
  | "unique_id"
  | "button"
  | "location"
  | "verification"
  | "last_visited_time"
  | "place";

/**
 * A formula name's mark on its chips where no property's is its: a list's
 * "[ ]" and "?" for one of a type that can't be told — an automation
 * variable's, as Notion's chips show them (seen 2026-11-21).
 */
export type FormulaValueMark = "value_list" | "value_unknown";

/**
 * The class that draws a property's mark at Notion's size where it stands in
 * a 20px place ("regular"): Notion's 16px box drawn 1.2 times, 19.2, about
 * the place's middle (PropertyTypeIcon.module.css) — its type's mark, an icon
 * it was given or a mark standing in for it alike.
 */
export function propertyMarkClass(size: LeafenIconSize): string | undefined {
  return size === "regular" ? styles.regular : undefined;
}

/**
 * The class for the 28px button by a property's name field — the column
 * menu's head, a database page's "속성 추가" tile, the view settings' "새
 * 속성": the regular mark in it drawn 16.8, Notion's 14px box at 1.2
 * (PropertyTypeIcon.module.css).
 */
export const propertyNameButtonClass = styles.nameButton;

export function PropertyTypeIcon({
  type,
  size = "small",
}: {
  type: RetainedPropertyType | GraphValueType | FormulaValueMark;
  size?: LeafenIconSize;
}) {
  // Each kind's mark as Notion's type list, column heads and pills draw it, drawn after where Notion's puts its ink
  // (propertyTypeMarks), the same in every place a property's type is marked; a select and a date are drawn to
  // Notion's place in their box already, and the kind Notion has not (last visited) is an eye.
  const p = { size, className: propertyMarkClass(size), "aria-hidden": true } as const;
  if (type === "title") return <TitleTypeMark {...p} />;
  if (type === "rich_text") return <TextTypeMark {...p} />;
  if (type === "number") return <NumberTypeMark {...p} />;
  if (type === "select") return <SelectIcon {...p} />;
  if (type === "multi_select") return <MultiSelectTypeMark {...p} />;
  if (type === "status") return <StatusTypeMark {...p} />;
  if (type === "date") return <DatePropertyIcon {...p} />;
  if (type === "person") return <PeopleTypeMark {...p} />;
  if (type === "created_by" || type === "last_edited_by") return <PersonCircleTypeMark {...p} />;
  if (type === "checkbox") return <CheckboxTypeMark {...p} />;
  if (type === "files") return <FileTypeMark {...p} />;
  if (type === "url") return <UrlTypeMark {...p} />;
  if (type === "relation") return <RelationTypeMark {...p} />;
  if (type === "rollup") return <RollupTypeMark {...p} />;
  if (type === "formula") return <FormulaTypeMark {...p} />;
  if (type === "email") return <AtTypeMark {...p} />;
  if (type === "phone") return <PhoneTypeMark {...p} />;
  if (type === "unique_id") return <NumeroTypeMark {...p} />;
  if (type === "created_time" || type === "last_edited_time") return <ClockTypeMark {...p} />;
  if (type === "button") return <ButtonTypeMark {...p} />;
  if (type === "location" || type === "place") return <PlaceTypeMark {...p} />;
  if (type === "verification") return <VerificationTypeMark {...p} />;
  if (type === "last_visited_time") return <EyeIcon {...p} />;
  if (type === "value_list") return <ListResultIcon size={size} className={p.className} aria-hidden />;
  if (type === "value_unknown") return <QuestionMarkIcon size={size} className={p.className} aria-hidden />;
  return <Copy {...p} />;
}
