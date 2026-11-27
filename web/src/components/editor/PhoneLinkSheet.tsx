import { useId, useLayoutEffect, useRef, useState, type KeyboardEvent } from "react";
import { flushSync } from "react-dom";
import { useTranslation } from "react-i18next";
import { Trash } from "@/icons/leafen";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import { SearchClearButton } from "@/components/PageActionSearch";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { appleTouchDevice } from "@/lib/iosViewport";
import { pageHref } from "@/lib/navigation";
import { pageIdFromPageHref } from "@/lib/pageLinks";
import { usePageNames } from "@/lib/pageNames";
import { phoneScreen } from "@/lib/screenKind";
import { normalizePastedLink } from "./editorInputModel";
import { SelectionToolbarLinkPicker, type SelectionLinkPages } from "./SelectionToolbarLinkPicker";
import styles from "./NativePhoneToolbar.module.css";
import editStyles from "./PhoneLinkSheet.module.css";
import rowStyles from "./SelectionToolbarLinkRows.module.css";

/** A link's address and words as its edit sheet closes: an empty address takes the link off. */
export type PhoneLinkEdit = { href: string; words: string };

/**
 * A phone's "링크" sheet, as the Notion Android app's (seen 2026-11-25 on a
 * Galaxy A90, from a page's chosen words and a table text cell's strip): a
 * sheet from the foot on the soft colour, "취소" at its head's end, a field
 * to paste an address or find a page in a white round box, and the pages
 * lately visited in one under it. On a phone its field takes the caret as it
 * opens, the keyboard staying up; a choice closes it with the address and,
 * for a page, its name.
 * Over linked words (`edit`) it edits their link, as the app's own over a
 * link — its strip's "링크" and its preview's "편집" alike: "페이지 또는 URL"
 * holding the address, "링크 제목" holding the words with the caret at their
 * end, and "링크 제거"; however it closes, the words typed are kept, as the
 * app's after "취소" (seen 2026-11-25, a letter typed into the words).
 * Words typed into its "페이지 또는 URL" find pages there and leave the card
 * alone on the sheet, "링크 제목" and "링크 제거" gone until the field is
 * empty again, even words that are its address again; a page or the address
 * chosen there stands in the field, the link taking it however the sheet
 * closes, the other two come back and the caret goes to the words' end, the
 * keyboard staying up; with its field empty it lists no pages lately visited
 * (seen 2026-11-25 on a Galaxy A90, the app, three openings: typing, the
 * address cut and typed again, emptied by ⓧ and by ⌫, a page chosen twice
 * and the address once, a letter typed after each choice).
 * Each field holding words shows "입력 값 지우기" at its end, which empties
 * it and leaves the caret in it, the keyboard up (seen 2026-11-25, the search
 * field and the edit's two, the one not being written too).
 * On an Android phone each field's keyboard key is "다음", as the app's and
 * its Chrome page's (seen 2026-11-25 on a Galaxy A90: the search field, empty
 * and holding words, and both of an edit's; a property's name and a sheet's
 * search keep "이동"): pressed in an edit's address it takes the caret to the
 * words' end, the keyboard staying up, as the app's; anywhere else it leaves
 * the caret where it is and chooses nothing — the app's sends it to a field
 * off the screen or to the page under the sheet, where letters typed are
 * lost, as its "입력 값 지우기" does (an approved difference). An iPhone's
 * keyboard keeps its own key (unmeasured).
 */
export function PhoneLinkSheet({ href, source, edit, onClose, onLink }: {
  href: string;
  /** Where its pages are found, if anywhere. */
  source?: SelectionLinkPages;
  /** The linked words it edits, and what takes their link and words as it closes. */
  edit?: { words: string; onDone(change: PhoneLinkEdit): void };
  onClose(): void;
  onLink(href: string, named?: string): void;
}) {
  const { t } = useTranslation(["selectionToolbar", "common"]);
  const inputRef = useRef<HTMLInputElement>(null), wordsRef = useRef<HTMLInputElement>(null), linkRef = useRef<HTMLDivElement>(null);
  // An edit's link as it stands, a page or address chosen on the sheet included.
  const [link, setLink] = useState(href);
  // A page's address is left out of the edit's field, which shows the page until it is pressed (SelectionToolbarLinkPicker).
  const pageLink = edit && pageIdFromPageHref(link) ? link : undefined;
  const shown = pageLink ? "" : link;
  const [value, setValue] = useState(shown);
  // Whether an edit's address field was typed into since the sheet opened or a choice was made there.
  const [typed, setTyped] = useState(false);
  const finding = !edit || typed && value.trim() !== "";
  const [words, setWords] = useState(edit?.words ?? "");
  const pageNames = usePageNames();
  const wordsId = useId();
  // A wider touch screen's lets the focus go, the keyboard with it, as Notion's in iPad Safari (seen 2026-11-14).
  useLayoutEffect(() => {
    if (phoneScreen()) caretToEnd(edit ? wordsRef.current : inputRef.current);
    else (document.activeElement as HTMLElement | null)?.blur?.();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  // The keyboard's "다음" on an Android phone, and what it does (above), for every field of the sheet.
  const nextKey = phoneScreen() && !appleTouchDevice() ? "next" : undefined;
  const onNextKey = nextKey ? (event: KeyboardEvent<HTMLDivElement>) => {
    if (!(event.target instanceof HTMLInputElement) || !fieldEnter(event)) return;
    event.preventDefault();
    event.stopPropagation();
    if (edit && !finding && event.target === inputRef.current) caretToEnd(wordsRef.current);
  } : undefined;
  // An edit's address as typed: one left as it was stays as written, words that are no address keep the link.
  const kept = () => value.trim() === shown.trim() ? link : normalizePastedLink(value) || link;
  const done = (next: string) => edit?.onDone({ href: next, words: words.trim() ? words : edit.words });
  const close = edit ? () => done(kept()) : onClose;
  // A choice in an edit's field stands there and gives the words their field back, the caret at their end, in the press.
  const choose = (next: string) => {
    flushSync(() => { setLink(next); setValue(pageIdFromPageHref(next) ? "" : next); setTyped(false); });
    if (phoneScreen()) caretToEnd(wordsRef.current);
  };
  return <PhoneSheet title={t("selectionToolbar:marks.link")} onClose={close} sheetRef={linkRef}
    action={{ label: t("common:actions.cancel"), onPress: close }} data={{ "data-phone-chosen-sheet": "link" }}>
    <div className={edit ? editStyles.edit : undefined}>
      {edit ? <div className={editStyles.label}>{t("selectionToolbar:linkEditor.pageOrUrl")}</div> : null}
      <div className={`${styles.linkField} ${editStyles.clearable}`} onKeyDownCapture={onNextKey}>
        <SelectionToolbarLinkPicker inputRef={inputRef} value={value} onValue={next => { setValue(next); setTyped(true); }} enterKeyHint={nextKey}
          popoverRef={linkRef} onClose={close} pageIconSize={22} source={source} current={finding ? undefined : value} pageLink={pageLink}
          placeholder={edit ? t("selectionToolbar:linkEditor.editPlaceholder") : undefined}
          onPage={page => (edit ? choose(pageHref(page.id)) : onLink(pageHref(page.id), pageNames.title(page)))}
          onAddress={() => {
            const next = normalizePastedLink(value);
            if (edit) { if (next) choose(next); } else if (next || !value.trim()) onLink(next);
          }} />
        <SearchClearButton value={value} inputRef={inputRef} className={editStyles.clear} onClear={() => setValue("")} />
      </div>
      {edit && !finding ? <>
        <label className={editStyles.label} htmlFor={wordsId}>{t("selectionToolbar:linkEditor.title")}</label>
        <div className={`${styles.linkField} ${editStyles.clearable}`} onKeyDownCapture={onNextKey}>
          <input ref={wordsRef} id={wordsId} value={words} onChange={event => setWords(event.target.value)} enterKeyHint={nextKey} />
          <SearchClearButton value={words} inputRef={wordsRef} className={editStyles.clear} onClear={() => setWords("")} />
        </div>
        <button type="button" className={`${editStyles.remove} ${styles.linkRow}`} onClick={() => done("")}>
          <span className={rowStyles.tbLinkIcon} aria-hidden="true" data-link-part="icon"><Trash size="wide" /></span>
          <span data-link-part="title">{t("selectionToolbar:linkEditor.removeLink")}</span>
        </button>
      </> : null}
    </div>
  </PhoneSheet>;
}

function caretToEnd(field: HTMLInputElement | null) {
  field?.focus();
  field?.setSelectionRange(field.value.length, field.value.length);
}
