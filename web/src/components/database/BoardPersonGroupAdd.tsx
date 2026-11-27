import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { scheduleOrganizationPeopleTypeahead } from "@/lib/typeaheadSearch";
import type { OrganizationProfile } from "@/lib/types";
import { NativePickerOption } from "@/lib/nativeAutocomplete";
import { confirmAction } from "../ConfirmActionDialog";
import { Plus } from "../icons";
import { personLabel } from "./people";
import { CellMenuPortal } from "./PropertyCellMenu";
import { organizationProfilePersonLabel, personOptionMatches } from "./PropertyPeopleCell";
import { PeoplePickerEmpty } from "./PeoplePickerEmpty";
import { capturePropertyCellRequest, type PropertyCellEnvironment } from "./propertyCellTypes";
import styles from "./databaseSheet";
import { useGroupAddPicker } from "./useGroupAddPicker";
import { SelfMark } from "@/components/people/SelfMark";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

/**
 * A view grouped by a person: "+ 신규 그룹" — after a board's columns, under a
 * table's, list's or gallery's last group (seen 2026-11-12) — lists the
 * workspace's people, the reader first as "〈이름〉(나)", searched as typed,
 * and the one picked gets a group of their own (a board's column, after the
 * group for no one), empty and kept though empty groups hide; one with a group already says
 * "그룹이 이미 존재합니다.", the list staying open; with no one to list it says
 * "결과 없음", offering no invite (Notion's, seen 2026-11-12). The list hangs
 * from the button's foot, its left on the button's, with no gap between;
 * its keys are useGroupAddPicker's and the focus after it ViewGroupAdd's.
 */
export function BoardPersonGroupAdd({ environment, className, keepsFocus = false, has, onPick }: {
  environment: PropertyCellEnvironment;
  className: string;
  /** Whether its button takes no focus when pressed, its closing giving the focus back to what held it (ViewGroupAdd). */
  keepsFocus?: boolean;
  has(personId: string): boolean;
  onPick(personId: string): void;
}) {
  const { t } = useTranslation(["boardView", "propertyCell", "tableView"]);
  const { snapshot, operations, context } = environment;
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [found, setFound] = useState<OrganizationProfile[]>([]);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const currentId = snapshot.userId || "local-user";
  const searchKey = query.trim().toLowerCase();

  useEffect(() => {
    if (!open) return;
    return focusOpenedField(() => searchRef.current);
  }, [open]);

  useEffect(() => {
    if (!open || !snapshot.organization?.id) return;
    const request = capturePropertyCellRequest(context);
    const cancel = scheduleOrganizationPeopleTypeahead({ organizationId: snapshot.organization.id, query: searchKey, limit: 12 }, {
      search: (input, options) => operations.searchPeople(input, options, context),
      onResult: (result) => { if (request.isCurrent()) setFound(result.people ?? []); },
      onError: () => { if (request.isCurrent()) setFound([]); },
    });
    return () => { request.abort(); cancel(); };
  }, [context, open, operations, searchKey, snapshot.organization?.id]);

  const people = useMemo(() => {
    const listed = new Map<string, string>();
    const add = (id: string | undefined | null, label: string, description: string) => {
      const personId = id?.trim();
      if (personId && !listed.has(personId) && personOptionMatches(label, description, personId, searchKey)) listed.set(personId, label);
    };
    add(currentId, personLabel(currentId, snapshot.userId ?? undefined, false), t("propertyCell:you"));
    for (const profile of [...found, ...snapshot.organizationProfiles]) {
      add(profile.userId, organizationProfilePersonLabel(profile), profile.email?.trim() ?? "");
    }
    return [...listed].slice(0, 12).map(([id, label]) => ({ id, label }));
  }, [currentId, found, searchKey, snapshot.organizationProfiles, snapshot.userId, t]);

  function close() {
    setOpen(false);
    setQuery("");
  }

  const picker = useGroupAddPicker({ open, rows: people, query, onQuery: setQuery, onClose: close, inputRef: searchRef, listRef,
    label: t("propertyCell:searchOrganizationPeople"), onPick: (person) => {
      if (has(person.id)) { void confirmAction({ message: t("tableView:groupExists"), confirmLabel: t("tableView:groupExistsOk"), cancelLabel: null }); return; }
      close();
      onPick(person.id);
    } });

  return (
    <>
      <button ref={triggerRef} type="button" className={className} aria-haspopup="dialog" aria-expanded={open}
        {...(keepsFocus ? { "data-press-keeps-focus": "" } : {})} onClick={() => setOpen(true)}>
        <Plus size="small" aria-hidden="true" />{t("boardView:newGroup")}
      </button>
      {open && (
        <CellMenuPortal triggerRef={triggerRef} onClose={close} ariaLabel={t("boardView:newGroup")} className={styles.peopleMenu}
          backdropClassName={styles.menuBackdrop} width={240} gap={0} halfHigh
          onKeyDown={(event) => { if (event.key === "Escape") { event.preventDefault(); close(); } }}>
          {/* A box of its own, not a property picker's strip, as Notion's (seen 2026-11-12). */}
          <div className={styles.peopleSearchBox}>
            <input {...picker.inputProps} ref={searchRef} className={styles.peopleSearch} type="text"
              placeholder={t("propertyCell:searchPersonOrGroup")} aria-label={t("propertyCell:searchOrganizationPeople")} />
          </div>
          <div {...picker.listBoxProps} ref={listRef} className={styles.peopleList}>
            {people.length === 0 ? <PeoplePickerEmpty query={query} />
              : people.map((person) => (
                <NativePickerOption key={person.id} itemKey={person.id} state={picker.state} tabIndex={-1} className={styles.peopleOption}>
                  {({ labelProps }) => <>
                    <PersonAvatar className={styles.personAvatar} userId={person.id} name={person.label} size={20} />
                    <span className={styles.peopleOptionText}>
                      <span {...labelProps}>{person.label}{person.id === currentId ? <SelfMark className={styles.peopleOptionMe} text={t("propertyCell:youMark")} /> : null}</span>
                    </span>
                  </>}
                </NativePickerOption>
              ))}
          </div>
        </CellMenuPortal>
      )}
    </>
  );
}
