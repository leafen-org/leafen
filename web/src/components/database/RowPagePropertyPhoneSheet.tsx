import { useState, type ReactNode } from "react";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { PhoneSheetTick } from "./TablePropertyPhoneSheet";

/** A database page property menu's row as its phone sheet draws it. */
export type RowPagePhoneRow = {
  key: string; label: string; icon?: ReactNode; next?: boolean; disabled?: boolean; danger?: boolean;
  /** A new group starts at it. */
  groupStart: boolean;
  /** What opens over the sheet: its choices, or the property's settings. */
  opens?: "settings" | "choices";
  run(): void;
};

/** A row's choices, a half sheet over the menu with the chosen one ticked. */
export type RowPagePhoneChoices = {
  key: string; title: string; value: string;
  choices: readonly { value: string; label: string; disabled?: boolean }[];
  onChoose(value: string): void;
};

/**
 * A property's menu on a database page on a phone, as Notion's sheet (seen
 * 2026-11-11 at 402pt in iOS Safari): half the screen high under the
 * property's name, its rows in white groups — "이름 바꾸기", "속성 편집",
 * "댓글"; "속성 표시 여부 ›", "속성 복제", "속성 삭제"; "레이아웃 사용자 지정".
 * "이름 바꾸기" and "속성 편집" turn the same sheet into the property's
 * settings, "속성 편집", its name not focused: as high as the menu was —
 * half the screen, or whole where the menu was pushed up — with no "‹", the
 * shade or a drag closing it all (Notion's, seen 2026-11-14 at 402pt for a
 * number, a select, a relation and a new text property: one sheet at 367 of
 * 714, 10 once pushed up). A row with "›" opens its choices over it.
 */
export function RowPagePropertyPhoneSheet({ propertyId, title, settingsTitle, rows, choices, settings, settingsOpen, onSettings, onClose }: {
  propertyId: string;
  title: string;
  settingsTitle: string;
  rows: readonly RowPagePhoneRow[];
  choices: readonly RowPagePhoneChoices[];
  /** The property's settings, drawn while they are open. */
  settings: ReactNode;
  settingsOpen: boolean;
  onSettings(open: boolean): void;
  onClose(): void;
}) {
  const [choosing, setChoosing] = useState<string | null>(null);
  const groups: RowPagePhoneRow[][] = [];
  for (const row of rows) {
    if (!groups.length || row.groupStart) groups.push([row]);
    else groups[groups.length - 1].push(row);
  }
  const open = choices.find((candidate) => candidate.key === choosing);
  return (
    <>
      <PhoneSheet size="half" title={settingsOpen ? settingsTitle : title} onClose={onClose}
        data={settingsOpen ? { "data-phone-property-settings": propertyId } : { "data-row-property-menu": propertyId, "data-phone-property-sheet": "" }}>
        {settingsOpen ? settings : groups.map((group) => (
          <PhoneSheetGroup key={group[0].key}>
            {group.map((row) => (
              <PhoneSheetRow key={row.key} icon={row.icon} label={row.label} more={Boolean(row.next)} disabled={row.disabled} danger={row.danger}
                onPress={() => {
                  if (row.opens === "settings") onSettings(true);
                  else if (row.opens === "choices") setChoosing(row.key);
                  else row.run();
                }} data={{ "data-row-property-menu-item": row.key }} />
            ))}
          </PhoneSheetGroup>
        ))}
      </PhoneSheet>
      {open ? (
        <PhoneSheet size="half" title={open.title} onClose={() => setChoosing(null)} data={{ "data-phone-property-choices": open.key }}>
          <PhoneSheetGroup>
            {open.choices.map((choice) => (
              <PhoneSheetRow key={choice.value} label={choice.label} more={false} disabled={choice.disabled}
                value={choice.value === open.value ? <PhoneSheetTick /> : undefined}
                onPress={() => { setChoosing(null); open.onChoose(choice.value); }} data={{ "data-phone-choice": choice.value }} />
            ))}
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
    </>
  );
}
