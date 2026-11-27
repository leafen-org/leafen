import type { ContentGraph, GraphOperation } from "@/contentGraph";
import { newId } from "@/lib/ids";
import type { SelectOption } from "@/lib/types";
import { newOptionColor } from "./colors";
import { optionSortOf, sortedOptions } from "./selectOptionSort";

/**
 * An option's name holds no comma, as Notion's: its fields leave the comma
 * out as it is typed or pasted (seen 2026-11-26 in Chrome: "Errands,
 * groceries" was offered as "Errands groceries", and ", garden" typed into
 * an option's name kept "Home garden"). The server refuses one in a name it
 * is sent new, saying the column and the option (database_option_names.go);
 * a name an option already has keeps its comma until it is renamed.
 */

/** A name as an option's field keeps it: without its commas. */
export function optionNameWithoutCommas(text: string): string {
  return text.replace(/,/gu, "");
}

/** Takes the commas out of an option's name field as they come in, its caret kept where it was. */
export function dropOptionNameCommas(field: HTMLInputElement) {
  const value = field.value;
  if (!value.includes(",")) return;
  const caret = field.selectionStart ?? value.length;
  const before = optionNameWithoutCommas(value.slice(0, caret));
  field.value = before + optionNameWithoutCommas(value.slice(caret));
  field.setSelectionRange(before.length, before.length);
}

/**
 * An option's name typed, as the change of its property saving it makes —
 * its settings `config`, written over the property as `graph` draws it —
 * kept on the device at each key (useTypedName); null when its name stays.
 */
export function optionRenameChange(graph: ContentGraph, propertyId: string, config: object | null | undefined): GraphOperation[] | null {
  const property = config ? graph.properties.find(({ id }) => id === propertyId) : undefined;
  return property && config ? [{ kind: "upsertProperty", property: { ...property, config: { ...config } } }] : null;
}

/** A new option named so — its commas left out — in a color of its own, as a group added to a view makes one. */
export function newNamedOption(name: string): SelectOption {
  return { id: newId(), name: optionNameWithoutCommas(name).trim(), color: newOptionColor() };
}

/**
 * A property's options with one renamed as a group's name field saves it
 * (GroupNameField): trimmed, its commas left out, in the place the
 * property's "정렬" gives it (selectOptionSort.ts); null when the name stays
 * or is left empty, which keeps it.
 */
export function optionsRenamed(config: unknown, optionId: string, typed: string): SelectOption[] | null {
  const options = (config as { options?: SelectOption[] } | null | undefined)?.options ?? [];
  const name = optionNameWithoutCommas(typed).trim();
  const current = options.find((option) => option.id === optionId);
  if (!current || !name || name === current.name) return null;
  return sortedOptions(options.map((option) => option.id === optionId ? { ...option, name } : option), optionSortOf(config));
}

/** The names a group renamed may not take: the other options' (a group named so is refused, as a new group is). */
export function otherOptionNames(config: unknown, optionId: string): string[] {
  const options = (config as { options?: SelectOption[] } | null | undefined)?.options ?? [];
  return options.flatMap((option) => option.id === optionId ? [] : [option.name]);
}

/** Whether a property's settings hold the option of that id (a group renamed from its chip is one). */
export function optionNamed(config: unknown, optionId: string): boolean {
  return ((config as { options?: SelectOption[] } | null | undefined)?.options ?? []).some((option) => option.id === optionId);
}
