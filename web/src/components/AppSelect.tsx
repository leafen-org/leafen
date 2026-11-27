import { Children, Fragment, isValidElement, useContext, type ReactElement, type ReactNode } from "react";
import { usePendingChoice } from "@/lib/pendingChoice";
import { NotionSelect, type NotionSelectOption, type NotionSelectSearch } from "@/components/database/NotionSelect";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { SettingsTouchChoice } from "@/components/settings/SettingsTouchChoice";
import { InSettingsContext } from "@/components/settings/settingsSurface";
import selectStyles from "@/components/database/NotionSelect.module.css";
import styles from "./AppSelect.module.css";

/** What a change hands its handler: the chosen value where a native select's
 * event carries it, so a form keeps its handler. */
export type AppSelectChange = { target: { value: string }; currentTarget: { value: string } };

type OptionProps = { value?: string | number; disabled?: boolean; children?: ReactNode;
  "data-description"?: string; "data-trigger-label"?: string; "data-group"?: string };

function textOf(node: ReactNode): string {
  if (node === null || node === undefined || typeof node === "boolean") return "";
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (isValidElement<{ children?: ReactNode }>(node)) return textOf(node.props.children);
  return "";
}

function optionsOf(children: ReactNode): NotionSelectOption[] {
  return Children.toArray(children).flatMap((child): NotionSelectOption[] => {
    if (!isValidElement(child)) return [];
    if (child.type === Fragment) return optionsOf((child as ReactElement<{ children?: ReactNode }>).props.children);
    if (child.type !== "option") return [];
    const props = (child as ReactElement<OptionProps>).props;
    const label = textOf(props.children);
    // An option's `data-description` shows under its name in the list, as a closed choice names only the value;
    // `data-trigger-label` is what a closed choice says instead of its name, `data-group` the heading it falls under.
    const description = props["data-description"], triggerLabel = props["data-trigger-label"], group = props["data-group"];
    return [{ value: String(props.value ?? label), label, disabled: Boolean(props.disabled), ...(description ? { description } : {}),
      ...(triggerLabel ? { triggerLabel } : {}), ...(group ? { group } : {}) }];
  });
}

/**
 * A choice drawn as the app's own menu, as Notion's dialogs offer theirs
 * (2026-10-16: the repeat and automation dialogs showed the browser's own
 * pickers). It reads a native select's <option>s and hands its change
 * handler the same `target.value`, so a form moves to it by its tag alone.
 */
export function AppSelect({ value, onChange, children, disabled, className, autoFocus, look = "field", placeholder, deselectable, menuWidth, menuHeight, menuAlign, menuGap, unmarked, plain, settingsList, search,
  "aria-label": ariaLabel, "aria-invalid": invalid, "aria-describedby": describedBy, ...data }: {
  value: string | number | undefined;
  /** A field (the default), its value and a chevron in a dialog row, or that with a thin border in a settings row. */
  look?: "field" | "value" | "setting";
  onChange?: (change: AppSelectChange) => void;
  children?: ReactNode;
  disabled?: boolean;
  className?: string;
  autoFocus?: boolean;
  /** Shown, greyed, while no option or a blank one is chosen. */
  placeholder?: string;
  /** Choosing the chosen option again leaves none chosen (the value ""). */
  deselectable?: boolean;
  /** A settings row's list is 250px at its button's end, as Notion's (seen 2026-10-30); a list of its own width or edge says so. A field's list is at least this wide (its button's width otherwise). */
  menuWidth?: number;
  /** A list that stands higher than Notion's usual settings list. */
  menuHeight?: number;
  menuAlign?: "start" | "end";
  /** A list this far from its button rather than 4px: a field's or value's `menuWidth` wide (Notion's new token window: 8, seen 2026-11-13), a settings row's (Notion's language list: 0, seen 2026-11-14). */
  menuGap?: number;
  /** A long list's search field, as NotionSelect's. */
  search?: NotionSelectSearch;
  /** The list marks no choice and opens at its top (NotionSelect's). */
  unmarked?: boolean;
  /** A plain list right under it, unticked (NotionSelect's), at its button's end with `menuAlign`; "spaced" 4px under, unlit;
   *  "ticked" as "spaced" with its choice ticked. */
  plain?: boolean | "spaced" | "ticked";
  /** A field's list drawn as Notion's menus: 28px rows a pixel apart 4px in, 14px words, a 16px check (NotionSelect's). */
  settingsList?: boolean;
  "aria-label"?: string;
  "aria-invalid"?: boolean | "true" | "false";
  "aria-describedby"?: string;
  /** A select's data attributes and class stay on the choice's wrapper, which sits where the select did. */
  [data: `data-${string}`]: string | boolean | undefined;
}) {
  // A choice made while its list waits for the server shows at once, and
  // the list says so after ten seconds (lib/pendingChoice.ts); a list that
  // stays free while its choice goes says so through its own note
  // (lib/shownChanges.ts).
  const pending = usePendingChoice(value === undefined ? "" : String(value), disabled);
  const invalidLook = invalid === true || invalid === "true";
  // A field takes the select's own field look, which a look of its own would set aside; a wrong one keeps it.
  const buttonClassName = [look === "field" ? (invalidLook ? selectStyles.fieldLook : "") : styles[`${look}Button`], invalidLook ? styles.invalidButton : ""]
    .filter(Boolean).join(" ");
  // A settings row's choices on a touch screen, a phone's or an iPad's, rise
  // on a sheet as Notion's there (SettingsTouchChoice), and so does any
  // choice in the settings window, whatever its look: a role, a filter, a
  // form's field (2026-11-15: the window's other choices kept the desktop's
  // list beside their button).
  const inSettings = useContext(InSettingsContext);
  const layout = useSidebarLayout(true), touchSheet = layout.touch && (look === "setting" || inSettings);
  return (
    <span {...data} className={[look === "field" ? "" : styles[look], className].filter(Boolean).join(" ") || undefined} data-app-select
      data-value={value === undefined ? "" : String(value)} data-invalid={invalidLook ? "true" : undefined} data-phone-choice={layout.phone && look === "setting" ? "" : undefined}
      data-waiting-note={pending.note ?? data["data-waiting-note"]}>
      {touchSheet ? <SettingsTouchChoice ariaLabel={ariaLabel ?? ""} value={pending.shown} options={optionsOf(children)} disabled={disabled}
        placeholder={placeholder} search={search} buttonClassName={buttonClassName || undefined} describedBy={describedBy}
        onChange={(next) => { pending.pick(next); onChange?.({ target: { value: next }, currentTarget: { value: next } }); }} /> : <NotionSelect ariaLabel={ariaLabel ?? ""} value={pending.shown} options={optionsOf(children)} disabled={disabled}
        buttonClassName={buttonClassName || undefined} describedBy={describedBy} autoFocus={autoFocus} placeholder={placeholder}
        deselectable={deselectable} unmarked={unmarked} plain={plain} settingsList={settingsList} search={search} onChange={(next) => { pending.pick(next); onChange?.({ target: { value: next }, currentTarget: { value: next } }); }}
        {...(look === "setting" ? {
          settingsRow: true, menuAlign: menuAlign ?? "end",
          // Notion's settings lists are 4px under the button (its language list 0) and at most 70% of the window high.
          menuSize: { width: menuWidth ?? 250, gap: menuGap ?? 4, maxHeight: menuHeight ?? Math.round((typeof window === "undefined" ? 900 : window.innerHeight) * 0.7) },
        } : {
          ...(menuGap !== undefined
            ? { menuSize: { width: menuWidth ?? 220, gap: menuGap, maxHeight: Math.round((typeof window === "undefined" ? 900 : window.innerHeight) * 0.7) } }
            : menuWidth ? { menuMinWidth: menuWidth } : {}),
          ...(menuAlign ? { menuAlign } : {}),
        })} />}
    </span>
  );
}

/**
 * Several choices drawn as the app's own menu, each toggled from a list that
 * stays open, where a form had a native multiple select. Its <option>s are
 * read as AppSelect reads them; a chosen value that is no longer an option
 * stays an option, marked by its own <option>.
 */
export function AppMultiSelect({ values, onChange, children, disabled, className, placeholder, "aria-label": ariaLabel,
  "aria-invalid": invalid, ...data }: {
  values: readonly string[];
  onChange: (values: string[]) => void;
  children?: ReactNode;
  disabled?: boolean;
  className?: string;
  /** Shown while nothing is chosen. */
  placeholder?: string;
  "aria-label"?: string;
  "aria-invalid"?: boolean | "true" | "false";
  [data: `data-${string}`]: string | boolean | undefined;
}) {
  return (
    <span {...data} className={className || undefined} data-app-select data-app-multi-select
      data-invalid={invalid === true || invalid === "true" ? "true" : undefined}>
      <NotionSelect ariaLabel={ariaLabel ?? ""} value="" options={optionsOf(children)} disabled={disabled}
        placeholder={placeholder ?? ""} multiple={{ values, onChange }} onChange={() => undefined} />
    </span>
  );
}
