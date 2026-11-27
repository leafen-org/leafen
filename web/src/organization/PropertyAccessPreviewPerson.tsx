import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useGroupAddPicker } from "@/components/database/useGroupAddPicker";
import { SearchClearButton } from "@/components/PageActionSearch";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { SelfMark } from "@/components/people/SelfMark";
import { CheckIcon, ChevronDown } from "@/icons/leafen";
import { NativePickerOption } from "@/lib/nativeAutocomplete";
import { directoryPeople, personName } from "@/lib/peopleDirectory";
import { popoverRectOf, type PopoverRect } from "@/lib/popoverPlacement";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { AccessPopover } from "./PropertyAccessParts";
import styles from "./PropertyAccessPreviewPerson.module.css";

/**
 * The preview's "다음 사용자로 미리보기", as Notion's (read 2026-11-18 in
 * Chrome at 1440, dpr 1, dark): a 32px button across the preview, outlined and
 * 8 round, 12 in — the person's 20px face, 8 before the name and the reader's
 * grey "(나)" 4 after it, and a grey 14px ⌄ at the end, shaded under the
 * pointer. It opens a 258px menu 4 under it at its start: a 28px search
 * ("사용자 검색", its ⓧ once typed) 12 in and 8 down, then the people 1px apart
 * in 28px rows 8 in — the face 8 before the name, the reader first and as
 * "〈이름〉(나)", a ✓ at the end of the one previewed; "사용자를 찾을 수
 * 없습니다." when the search finds no one. Its keys are a person picker's
 * (useGroupAddPicker); Escape gives the focus back to the button.
 */
export function PreviewPersonPicker({ selfId, value, onChange }: { selfId: string; value: string; onChange(userId: string): void }) {
  const { t } = useTranslation(["propertyAccess", "peopleProfiles"]);
  const [anchor, setAnchor] = useState<PopoverRect | null>(null);
  const [query, setQuery] = useState("");
  const inputRef = useRef<HTMLInputElement>(null), listRef = useRef<HTMLDivElement>(null);
  const people = useMemo(() => {
    const all = directoryPeople().map((person) => ({ id: person.userId, label: person.displayName || personName(person.userId) }));
    return [...all.filter((person) => person.id === selfId), ...all.filter((person) => person.id !== selfId)];
  }, [selfId]);
  const needle = query.trim().toLocaleLowerCase();
  const rows = needle ? people.filter((person) => person.label.toLocaleLowerCase().includes(needle)) : people;
  const close = () => { setAnchor(null); setQuery(""); };
  const picker = useGroupAddPicker({ open: Boolean(anchor), rows, query, onQuery: setQuery, onClose: close, inputRef, listRef,
    label: t("previewAs"), onPick: (person) => { onChange(person.id); close(); } });
  const chosen = people.find((person) => person.id === value);
  const self = (id: string) => (id === selfId ? <SelfMark className={styles.self} text={t("peopleProfiles:selfMark")} /> : null);

  return <>
    <button type="button" className={styles.button} aria-label={t("previewAs")} aria-haspopup="dialog" aria-expanded={Boolean(anchor)}
      data-access-preview-person={value} onClick={(event) => (anchor ? close() : setAnchor(popoverRectOf(event.currentTarget)))}>
      <span className={styles.who}>
        <PersonAvatar userId={value} name={chosen?.label ?? personName(value)} size={20} />
        <span className={styles.name}>{chosen?.label ?? personName(value)}</span>{self(value)}
      </span>
      <ChevronDown size="tiny" trim glyphWidth={9.83} aria-hidden="true" />
    </button>
    {anchor ? (
      <AccessPopover anchor={anchor} onClose={close} width={258} className={styles.menu} restoreFocus>
        <div className={styles.searchBox}>
          <label className={styles.search}>
            <input {...picker.inputProps} ref={inputRef} type="text" autoFocus={fieldFocusOnOpen()} placeholder={t("previewSearch")}
              aria-label={t("previewSearch")} data-access-preview-search />
            <SearchClearButton value={query} inputRef={inputRef} onClear={() => setQuery("")} />
          </label>
        </div>
        <div {...picker.listBoxProps} ref={listRef} className={styles.list} data-access-preview-people>
          {rows.length === 0 ? <div className={styles.empty}>{t("previewNoPeople")}</div> : rows.map((person) => (
            <NativePickerOption key={person.id} itemKey={person.id} state={picker.state} tabIndex={-1} className={styles.row}
              data-access-preview-person-row={person.id}>
              {({ labelProps }) => <>
                <PersonAvatar userId={person.id} name={person.label} size={20} />
                <span {...labelProps} className={styles.rowName}><span>{person.label}</span>{self(person.id)}</span>
                {person.id === value ? <CheckIcon size="small" aria-hidden="true" /> : null}
              </>}
            </NativePickerOption>
          ))}
        </div>
      </AccessPopover>
    ) : null}
  </>;
}
