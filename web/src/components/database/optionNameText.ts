import { newId } from "@/lib/ids";
import type { SelectOption } from "@/lib/types";
import { newOptionColor } from "./colors";

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

/** A new option named so — its commas left out — in a color of its own, as a group added to a view makes one. */
export function newNamedOption(name: string): SelectOption {
  return { id: newId(), name: optionNameWithoutCommas(name).trim(), color: newOptionColor() };
}
