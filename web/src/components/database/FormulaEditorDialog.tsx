import { SwitchMark } from "@/components/Switch";
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { DbProperty, Page } from "@/lib/types";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeAround, popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { CheckIcon, ChevronDown, FileText, X } from "@/components/icons";
import { QuestionCircleIcon } from "@/icons/leafen/automationIcons";
import { RichTextCellText } from "./RichTextCellText";
import { formulaEditorPieces } from "./formulaEditorPieces";
import { formulaNameProblems, formulaNamedProblems } from "./formulaDraftProblems";
import { evaluateFormulaRun, formulaDisplay, formulaItemText, type FormulaBounds } from "./formula";
import { FormulaDebugValue } from "./FormulaDebugValue";
import { formulaEmptyCallEnd, formulaEmptyValueCalls } from "./formulaEmptyCalls";
import { formulaUnknownTypeCalls } from "./formulaUnknownCalls";
import { FORMULA_SHOWN_LINES, formulaSyntaxProblems } from "./formulaSyntaxProblems";
import {
  formulaInsert,
  formulaListItemType,
  formulaLoneListItems,
  formulaProblemPlace,
  formulaPropertyChoices,
  formulaTypeProblemsOf,
  formulaPropertyInsert,
  formulaResultEmpty,
  formulaResultType,
  formulaShownOffset,
  formulaStaticResult,
  formulaStaticResultType,
  formulaSuggestions,
  formulaWordAt,
  type FormulaProblem,
  type FormulaScopeName,
} from "./formulaEditorModel";
import { FormulaEditorHelper, type FormulaHelperItem } from "./FormulaEditorHelper";
import { FOOT_PLACEMENT } from "./PropertyCellMenu";
import { FormulaInput, type FormulaInputHandle } from "./FormulaInput";
import { useFormulaDraft } from "./useFormulaDraft";
import { formulaPropertyReferences, resolveFormulaPropertyReference } from "./formulaReferences";
import codeStyles from "./FormulaCode.module.css";
import styles from "./FormulaEditor.module.css";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { pickerWindowBox, pickerWindowLine, usePickerWindow } from "@/lib/usePickerWindow";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { plainTitleOf } from "@/lib/closedMentions";
import { deviceValue, storeDeviceValue } from "@/lib/browserStorage";
import { fieldFocusOnOpen, phoneScreen } from "@/lib/screenKind";
import { PhoneSheet, usePhoneSheetHead } from "@/components/shell/PhoneSheet";
import phoneStyles from "./FormulaEditorPhone.module.css";
import { fieldEscape, imeComposing } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

// Notion's formula editor, opened from a formula cell or its property: the
// formula with its tokens coloured and its properties as chips, its result for a chosen page with the
// result's type — in debug mode each value with its type — what keeps it
// from being read and where, and help with the properties, operators and
// functions to write. Closing it keeps the formula as written, a wrong one
// too, as Notion does; "Discard all changes" goes back to the formula it
// opened with. A formula that runs with names of its own — an automation's —
// has no page to preview it on: as Notion's (seen 2026-10-23) it says only
// whether it reads and of what type it is. Its head has no ✕ but a blue
// "완료", and once the formula changes "모든 변경 사항 취소" and a blue
// "저장", which keeps it and closes the editor — or, from Σ in a value
// window's words, leaves it open with "완료" again (Notion's, seen
// 2026-11-10); Escape keeps it as it closes. On a phone it is a sheet the
// whole screen high, its suggestions alone across it (FormulaEditorHelper
// `phone`), as Notion's (seen 2026-11-14 at 402pt): of its own from a value,
// over a property's "속성 편집" sheet as a whole sheet with its grip.

const NO_NAMES: readonly FormulaScopeName[] = [];
// Debug mode stays as it was last left on this browser, for whoever uses it:
// reopened or reloaded it is as it was, another device starts with it off,
// as Notion keeps it in the browser's storage ("formulas_show_structure",
// seen 2026-11-12).
const DEBUG_MODE_KEY = "leafen:formula-debug-mode";
const ROW_PLACEMENT = { gap: 0, align: "end", alignFallback: true, whole: true } as const;

export function FormulaEditorDialog({ prop, props, pagesById, propsByDb, bounds, rowId, anchor, above = false, fromRow = false, fresh = false, keepOpen = false, names = NO_NAMES, resultProblem, onSave, onClose }: {
  prop: DbProperty;
  props: DbProperty[];
  pagesById: Record<string, Page>;
  propsByDb?: Record<string, DbProperty[]>;
  bounds: FormulaBounds;
  rowId?: string;
  /**
   * What it hangs from: the foot of a cell's value where a cell opens it (PropertyCellMenu.tsx `valueCellFoot`), or
   * the value an automation sets — a page line's field, a card, the value window (Notion's, seen 2026-11-10).
   */
  anchor?: PopoverRect | null;
  /** Over its anchor rather than under it, as from a value window's "Σ 사용자 지정 수식 작성 ›" (Notion's, seen 2026-11-10). */
  above?: boolean;
  /**
   * Its anchor is a property's "수식 편집" row: it hangs under the row, its end on the row's end, as Notion's from a
   * column's "속성 편집 › 수식 편집" (seen 2026-11-07 and 2026-11-11, a right column's and a left one's alike).
   */
  fromRow?: boolean;
  /**
   * What it opens with isn't the value yet — the words of a page line Σ makes a formula — so closing keeps it
   * unchanged too, as Notion's "완료" makes `"12"` the line's formula (seen 2026-11-10).
   */
  fresh?: boolean;
  /**
   * An automation's "저장" keeps it open, "완료" again, as Notion's from Σ in a value window's words; from a card,
   * a page line, the value window's "Σ … ›" line or a variable it saves and closes (seen 2026-11-10).
   */
  keepOpen?: boolean;
  /** Names the formula may use where it runs, as an automation's message (formulaEditorModel.ts). */
  names?: readonly FormulaScopeName[];
  /** What it gives that where it sets a value can't take, as an automation's date a number (automationValueTypes.ts). */
  resultProblem?: (formula: string) => FormulaProblem | null;
  onSave: (formula: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation(["formulaEditor", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const inputRef = useRef<FormulaInputHandle>(null), caretRef = useRef<HTMLElement>(null);
  const helperKeys = useRef<((event: KeyboardEvent) => void) | null>(null);
  const setHelperKeys = useCallback((handler: ((event: KeyboardEvent) => void) | null) => { helperKeys.current = handler; }, []);
  const previewMenu = useRef<HTMLDivElement>(null), previewList = useRef<HTMLDivElement>(null), previewInput = useRef<HTMLInputElement>(null);
  const pickerRef = useRef<HTMLButtonElement>(null);
  const saved = prop.config?.formula ?? "";
  const { kept, draft, setDraft, keepDraft, noteUnchangedStep } = useFormulaDraft(saved);
  const [cursor, setCursor] = useState(saved.length);
  const [helperOpen, setHelperOpen] = useState(true);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [previewRowId, setPreviewRowId] = useState(rowId);
  const [debug, setDebug] = useState(() => deviceValue(DEBUG_MODE_KEY) === "true");
  const [phone] = useState(phoneScreen);
  const overSheet = usePhoneSheetHead(null);

  const rows = useMemo(() => Object.values(pagesById)
    .filter((page) => page.parentType === "database" && page.parentId === prop.databaseId && !page.inTrash && !page.archivedAt)
    .sort((left, right) => left.position - right.position), [pagesById, prop.databaseId]);
  const row = rows.find((candidate) => candidate.id === previewRowId) ?? rows[0] ?? null;
  const draftProp = useMemo(() => ({ ...prop, config: { ...prop.config, formula: draft } }), [prop, draft]);

  // What keeps it from being read, after the problems of what it reads (formulaSyntaxProblems.ts).
  const reading = useMemo(() => formulaSyntaxProblems(draft), [draft]), syntax = reading.problems.length > 0;
  const warnings = useMemo(() => formulaNameProblems(draft, props, bounds, new Set(names.map((name) => name.name)), reading),
    [draft, props, bounds, names, reading]);
  // Values whose types don't fit are named as Notion names them, among the names it doesn't know.
  const typeProblems = useMemo(() => reading.alone ? [] : formulaTypeProblemsOf(draft, props, propsByDb, names), [draft, props, propsByDb, names, reading]);
  // Read, it may still give what its value can't take, over the whole formula (Notion's, seen 2026-11-10).
  const result = useMemo(() => syntax || !draft.trim() ? null : resultProblem?.(draft) ?? null, [draft, syntax, resultProblem]);
  const preview = useMemo(() => {
    if (!row || syntax || typeProblems.length || !draft.trim()) return null;
    try {
      const { value, run, own } = evaluateFormulaRun({ row, prop: draftProp, props, pagesById, propsByDb, bounds });
      return { value, run, own, display: formulaDisplay(row, draftProp, bounds, untitledPageDisplayTitle("page"), () => value) };
    } catch {
      return null;
    }
  }, [row, syntax, typeProblems, draft, draftProp, props, pagesById, propsByDb, bounds, t]);
  // A prop("Name") naming a property or page field is written as a chip, a page's too (formulaEditorPieces.ts).
  const piecesOf = (text: string) => formulaEditorPieces(text, props, names, propsByDb);
  const unpreviewed = names.length > 0;
  const pieces = useMemo(() => piecesOf(draft), [draft, props, names, propsByDb]);
  // Notion writes where the problem is after its words, as "[start,end]", counting a chip as one
  // letter. Every problem is underlined in the formula, the first three on their lines (FORMULA_SHOWN_LINES).
  // What kept it from working out on the previewed page, named as Notion's
  // preview names it in red with its place, when nothing else is named
  // (formulaRunError.ts); one of a formula it reads keeps that formula's place.
  const run = useMemo((): FormulaProblem | null => {
    if (!preview?.run) return null;
    const lead = preview.own ? draft.length - draft.trimStart().length : 0;
    const at = (offset: number | undefined) => offset === undefined ? undefined : offset + lead;
    return { key: preview.run.key, values: preview.run.values, start: at(preview.run.start), end: at(preview.run.end), raw: !preview.own };
  }, [preview, draft]);
  const problems = useMemo(() => {
    const named = formulaNamedProblems({ reading, names: warnings, types: typeProblems, result, draft, props, propsByDb });
    return !named.length && run ? [run] : named;
  }, [reading, warnings, typeProblems, result, run, draft, props, propsByDb]);
  const problem = problems[0] ?? null;
  const shown = (offset: number | undefined) => offset === undefined ? undefined : formulaShownOffset(pieces, offset);
  // In debug mode a result comes with a line under it, in amber, for each
  // call given a value whose type can't be known where it takes one type
  // (formulaUnknownCalls.ts), else for each call given a value that may be
  // empty (formulaEmptyCalls.ts), its place marked too, as Notion's (seen
  // 2026-11-12: with the one, the other doesn't show). The first show under
  // the formula's problems too, after their red lines, where the second,
  // which a result gives, don't (seen 2026-11-14: abs(foo) - "b" shows foo
  // not defined [4,7], the type problem [0,14], then the amber line that
  // abs() may be given a value that isn't a number [0,8]; abs(prop("n")) - "b"
  // its red line alone). Its "?" to Notion's help is
  // left out, its room kept.
  const knownNames = useMemo(() => new Set(names.map((name) => name.name)), [names]);
  const emptyCalls = useMemo((): { start: number; end: number; warning: true; line: string; kind: string }[] => {
    if (!debug || (!preview && !problems.length)) return [];
    const unknown = formulaUnknownTypeCalls(draft, props, knownNames);
    if (unknown.length) {
      return unknown.map((call) => ({ start: call.start, end: call.kind === "target" ? call.end : formulaEmptyCallEnd(draft, pieces, call.end),
        warning: true, kind: call.kind === "target" ? "unknownTarget" : "unknownArgument",
        line: t(call.kind === "target" ? "unknownTargetCall" : "unknownArgumentCall", { type: call.type, name: call.name }) }));
    }
    if (problems.length) return [];
    return formulaEmptyValueCalls(draft, props).map(({ start, end }) => ({ start, end: formulaEmptyCallEnd(draft, pieces, end), warning: true,
      kind: "emptyValue", line: t("emptyValueCall") }));
  }, [debug, preview, problems.length, draft, props, pieces, knownNames, t]);
  const marks = useMemo(() => emptyCalls.length ? [...problems, ...emptyCalls] : problems, [problems, emptyCalls]);
  // After a page name's "." a property may follow too, as Notion says there (seen 2026-10-23).
  const afterPage = problem?.key === "method" && names.some((name) => name.page
    && new RegExp(`(?:^|[^A-Za-z0-9_])${name.name}\\s*\\.\\s*$`, "u").test(draft));
  // Their words as Notion's keep a particle pair as written ("Nope은(는)", seen 2026-11-03).
  // Its place follows in a deeper red, as Notion's (seen 2026-11-14, dark).
  const errors = problems.slice(0, FORMULA_SHOWN_LINES).map((item) => ({
    words: t(`errors.${afterPage && item === problem ? "pageMethod" : item.key}`, {
      token: item.token, name: item.name, left: item.left, right: item.right, type: item.type,
      expected: item.expected, given: item.given, ...item.values, postProcess: [],
    }),
    place: formulaProblemPlace(item.raw ? item : { ...item, start: shown(item.start), end: shown(item.end) }),
  }));
  const error = errors.length > 0;
  const errorLines = error ? <span className={styles.error} role="alert">{errors.map((line, index) => <span key={index}>
    <span>{line.words}</span>{line.place ? <span className={styles.errorPlace}>{line.place}</span> : null}
  </span>)}</span> : null;
  // With no page to run it on it is checked alone: valid, of its type, a list's by its items' ('유형:블록 목록', Notion's, 2026-11-21).
  const validLine = (type: string) => <><span className={styles.resultValue}><span className={styles.valid}>{t("valid")}</span></span>
    <span className={styles.typeBadge}>{t("typeLabel", { type })}</span></>;
  const alone = !row && !unpreviewed ? formulaStaticResult(draft, props, names, propsByDb) : null;
  const aloneName = !alone ? "" : alone.type === "list" && alone.items && !["unknown", "undefined"].includes(alone.items)
    ? t(`listTypes.${alone.items}`) : t(`types.${alone.type}`);
  // A formula reading its own property is of a type not known, as Notion's (seen 2026-11-02).
  const selfReading = [...formulaPropertyReferences(draft)].some((name) => resolveFormulaPropertyReference(props, name)?.id === prop.id);
  // A result with nothing in it is named by the formula's own type, as Notion's
  // "출력 값 없음" is (seen 2026-11-12: [].first().upper() 텍스트, [].first()
  // 알 수 없음, [] 목록, an empty number 숫자, an empty people property 사람 목록).
  const empty = preview ? formulaResultEmpty(preview.value) : false;
  const shape = empty && !selfReading ? formulaStaticResult(draft, props, names, propsByDb) : null;
  const resultType = preview ? (selfReading ? "unknown" : shape ? shape.type : formulaResultType(preview.value)) : null;
  const shapeItems = shape?.items && shape.items !== "unknown" ? shape.items : shape ? formulaLoneListItems(draft, props) : undefined;
  const itemType = shape ? shapeItems ?? null : preview ? formulaListItemType(preview.value) : null;
  const typeName = resultType === "list" && itemType ? t(`listTypes.${itemType}`) : resultType ? t(`types.${resultType}`) : null;

  const choices = useMemo(() => formulaPropertyChoices(props, prop), [props, prop]);
  const groups = useMemo(() => formulaSuggestions(draft, cursor, choices, names), [draft, cursor, choices, names]);

  // A modal layer over the cell it opened from: Escape closes its picker,
  // then it, once nothing opened over it takes the key.
  const escapeRef = useRef<() => void>(() => undefined);
  const { zIndex, onKeyDown: layerKeys } = useOverlayLayer(!phone, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], restoreFocus: true, onEscape: () => escapeRef.current(),
  });

  // The editor takes focus once, when it opens, what is typed meanwhile going in (focusOpenedField).
  const formulaField = () => dialogRef.current?.querySelector<HTMLElement>("[data-formula-input]");
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => focusOpenedField(formulaField, { place: () => inputRef.current?.focus(saved.length) }), []);

  function close() {
    if (draft !== kept || (fresh && draft.trim())) onSave(draft);
    else if (rowId !== undefined || fromRow) noteUnchangedStep(prop.databaseId, prop.id);
    onClose();
  }
  escapeRef.current = close;

  function keep() {
    onSave(draft);
    if (!keepOpen) {
      onClose();
      return;
    }
    keepDraft();
    focusOpenedField(formulaField, { place: () => inputRef.current?.focus(cursor) });
  }

  // Closing the row list puts focus back on its button, inside the editor.
  function closePicker() {
    setPickerOpen(false);
    setSearch("");
  }

  function edit(text: string, at: number) {
    setDraft(text);
    setCursor(at);
    focusOpenedField(formulaField, { place: () => inputRef.current?.focus(at) });
  }

  // A suggestion or a tab is written as typing is, so Undo takes a
  // suggestion back with the name typed for it; "모든 변경 취소" and other
  // whole changes stay Undo steps of their own.
  function type(text: string, at: number, completing = false) {
    inputRef.current?.replace(text, at, completing);
    edit(text, at);
  }

  function insert(item: FormulaHelperItem) {
    const text = item.kind === "name" ? item.name.name : item.kind === "property" ? formulaPropertyInsert(item.property) : item.entry.insert;
    const next = formulaInsert(draft, cursor, text);
    type(next.text, next.cursor, Boolean(formulaWordAt(draft, cursor).word));
  }

  function onEditorKeyDown(event: KeyboardEvent) {
    // The editor marks Escape handled itself (ProseMirror does), so the layer
    // never hears it; here it closes the editor as Escape does elsewhere.
    if (fieldEscape(event) && !pickerOpen) {
      event.preventDefault(); close();
      return;
    }
    // As Notion's (seen 2026-10-08): the arrows and Enter work the suggestions
    // wherever the caret is, and Tab picks one only for a name being typed;
    // elsewhere Tab writes a tab. Other keys, and Shift with them, remain with
    // Tiptap and the parent modal.
    if (pickerOpen || event.shiftKey || imeComposing(event)) return;
    const typing = Boolean(formulaWordAt(draft, cursor).word);
    if (event.key === "Tab" && !(helperOpen && typing && groups.length)) {
      event.preventDefault();
      type(draft.slice(0, cursor) + "\t" + draft.slice(cursor), cursor + 1);
      return;
    }
    if (!helperOpen || !groups.length) return;
    if (!["Enter", "ArrowDown", "ArrowUp", "Tab"].includes(event.key)) return;
    helperKeys.current?.(event);
  }

  // The rows to preview on, found while the picker is open alone and once for each search, its list drawn around what it shows (usePickerWindow).
  const shownRows = useMemo(() => !pickerOpen ? []
    : rows.filter(candidate => (candidate.title || "").toLowerCase().includes(search.trim().toLowerCase())), [pickerOpen, rows, search]);
  const previewPicker = useNativeAutocomplete({ items: shownRows, query: search, onQuery: setSearch,
    inputRef: previewInput, listRef: previewList, popoverRef: previewMenu, label: t("previewSearch"), open: pickerOpen,
    commitTab: false, keyOf: candidate => candidate.id, textOf: candidate => plainTitleOf(candidate) || untitledPageDisplayTitle(candidate),
    onPick: candidate => { setPreviewRowId(candidate.id); closePicker(); } });
  const previewLines = usePickerWindow({ scrollRef: previewMenu, open: pickerOpen, count: shownRows.length, keyAt: (index) => shownRows[index].id,
    sizeAt: () => 28, gap: 1, focused: previewPicker.active });
  const previewOption = (candidate: Page) => (
    <NativePickerOption key={candidate.id} state={previewPicker.state} itemKey={candidate.id}
      aria-selected={candidate.id === row?.id} className={styles.pickerItem}>
      {({ labelProps }) => <>
      <span className={styles.pickerIcon} aria-hidden="true"><FileText size="tiny" /></span>
      <span {...labelProps} className={styles.pickerName}>{plainTitleOf(candidate) || untitledPageDisplayTitle(candidate)}</span>
      {candidate.id === row?.id ? <span className={styles.pickerCheck} aria-hidden="true"><CheckIcon size="small" /></span> : null}
      </>}
    </NativePickerOption>
  );
  useOverlayLayer(pickerOpen, { kind: "picker", ref: previewMenu, inside: () => [pickerRef.current],
    onEscape: closePicker, onOutsidePress: closePicker });
  // From the foot of the cell it opens from, as the date's window, as Notion's
  // (seen 2026-11-08: from 653,238.4 under a 37px row's cell at 654,202.4 and
  // from 653,339.4 under a 100px row's, both moved up to end 12px over an 823
  // window's foot; over a low row's cell, its foot on the cell's).
  // An automation's hangs from its value the same way: under it, or over a value window from its "Σ … ›" line, moved
  // into the window 12px from its edges (seen 2026-11-10: from a page line's field at 456..486 at 377,191 in a
  // 1129×779 window; from a value window at 335..542 at its left, 12px from the top).
  // From a property's "수식 편집" row it hangs under the row, its end on the row's end, moved up into the window 12px
  // from its foot where it is taller than the room under the row (Notion's, seen 2026-11-11 in a 679 window: under rows
  // at 246..274, ending at 953 and at 1160, at 213,91 and 420,91, 740×576).
  // A phone's sheet takes the whole screen, hung from nothing.
  const anchoredPlace = usePopoverPlacement(dialogRef, anchor && !phone ? (size, viewport) => placeAround(anchor, size, viewport,
    fromRow ? ROW_PLACEMENT : above ? { ...FOOT_PLACEMENT, prefer: "above" } : FOOT_PLACEMENT) : null, [anchor, above, fromRow, phone], undefined, false);

  // The editor opens from inside a cell; its clicks and keys stay its own.
  const contain = { onClick: stop, onMouseDown: stop, onPointerDown: stop, onKeyDown: stop };
  const section = (
      <section
        ref={dialogRef}
        className={phone ? phoneStyles.editor : styles.dialog}
        style={phone ? undefined : anchor ? popoverPlacementStyle(anchoredPlace) : { top: 72 }}
        data-anchored={anchor && !phone ? true : undefined}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        data-formula-editor
        onKeyDown={(event) => {
          // Its keys stay its own (the cell under it hears none), so it hears Escape here.
          layerKeys(event);

        }}
      >
        <header className={styles.header}>
          <h2 id={titleId}>{t("title")}</h2>
          <button type="button" className={styles.iconButton} aria-label={t("helperToggle")} aria-pressed={helperOpen}
            onClick={() => setHelperOpen((open) => !open)}><QuestionCircleIcon size="small" aria-hidden="true" /></button>
          <span className={styles.headerSpacer} />
          {draft !== kept ? (
            <button type="button" className={styles.discardButton} data-formula-discard
              onClick={() => edit(kept, kept.length)}>{t("discard")}</button>
          ) : null}
          {unpreviewed ? (
            <button type="button" className={styles.keepButton} data-formula-keep={draft !== kept ? "save" : "done"}
              onClick={draft !== kept ? keep : close}>{t(draft !== kept ? "common:actions.save" : "common:actions.done")}</button>
          ) : (
            <button type="button" className={styles.closeButton} aria-label={t("common:actions.close")} data-formula-close onClick={close}>
              <X size="tiny" aria-hidden="true" />
            </button>
          )}
        </header>
        <div className={codeStyles.editorWrap}>
          <FormulaInput
            ref={inputRef}
            caretRef={caretRef}
            value={draft}
            piecesOf={piecesOf}
            label={t("title")}
            placeholder={t("placeholder")}
            invalid={error}
            problems={marks}
            onChange={(text, at) => {
              setDraft(text);
              setCursor(at);
            }}
            onCursor={setCursor}
            onKeyDown={onEditorKeyDown}
          />
        </div>
        {unpreviewed ? (
          <div className={styles.preview} data-formula-syntax-check>
            <div className={styles.previewRow}><span>{t("syntaxCheck")}</span></div>
            <div className={styles.previewResults} aria-live="polite"><div className={styles.previewResult}>
              {errorLines ?? validLine(t(`types.${formulaStaticResultType(draft, props, names)}`))}
            </div></div>
          </div>
        ) : <div className={styles.preview}>
          <div className={styles.previewRow}>
            <span className={styles.previewLabel}>{t("preview")}</span>
            {/* With no rows it reads "페이지 선택" and finds none, as Notion's (seen 2026-11-21 twice). */}
            <span className={styles.pickerWrap} data-formula-preview-rows={row ? undefined : "none"}>
              <button ref={pickerRef} type="button" className={styles.picker} aria-haspopup="listbox" aria-expanded={pickerOpen}
                onClick={() => (pickerOpen ? closePicker() : setPickerOpen(true))}>
                {row ? <span className={styles.pickerIcon} aria-hidden="true"><FileText size="tiny" /></span> : null}
                <span>{row ? plainTitleOf(row) || untitledPageDisplayTitle(row) : t("pickPreview")}</span>
                <ChevronDown size="tiny" aria-hidden="true" className={styles.pickerChevron} />
              </button>
              {pickerOpen ? (
                <div ref={previewMenu} className={styles.pickerMenu}>
                  <input {...previewPicker.inputProps} ref={previewInput} className={styles.pickerSearch} placeholder={t("previewSearch")} autoFocus={fieldFocusOnOpen()} />
                  <div {...previewPicker.listBoxProps} ref={previewList} className={styles.pickerList}>
                    {!shownRows.length ? <span className={`${styles.pickerItem} ${styles.muted}`}>{t("noResult")}</span> : previewLines.windowed ? (
                      <div ref={previewLines.linesRef} role="presentation" style={pickerWindowBox(previewLines.height)}>
                        {previewLines.lines.map((line) => <div key={line.key} {...pickerWindowLine(line, previewLines.measure)}>
                          {previewOption(shownRows[line.index])}</div>)}
                      </div>
                    ) : shownRows.map(previewOption)}
                  </div>
                </div>
              ) : null}
            </span>
            <button type="button" role="switch" aria-checked={debug} className={styles.debugSwitch} data-formula-debug
              onClick={() => { setDebug(!debug); storeDeviceValue(DEBUG_MODE_KEY, debug ? null : "true"); }}>
              <SwitchMark on={debug} />
              <span>{t("debugMode")}</span>
            </button>
          </div>
          <div className={styles.previewResults} aria-live="polite">
          <div className={styles.previewResult}>
            {errorLines ?? (!row ? validLine(aloneName) : preview && empty ? (
              // Debug mode or not, as Notion's.
              <>
                <span className={styles.resultValue}><span className={styles.muted}>{t("noOutput")}</span></span>
                {typeName ? <span className={styles.typeBadge}>{t("typeLabel", { type: typeName })}</span> : null}
              </>
            ) : preview ? (
              <>
                <span className={styles.resultValue} data-one-line={debug ? undefined : ""} data-debug={debug ? "" : undefined}>
                  {debug ? (
                    <FormulaDebugValue value={preview.value} checkboxLabel={t("types.checkbox")}
                      text={(item) => formulaItemText(item, bounds, untitledPageDisplayTitle("page"))} />
                  ) : preview.display.checked !== undefined
                    ? <input type="checkbox" checked={preview.display.checked} disabled aria-label={t(`types.checkbox`)} />
                    : preview.display.spans ? <RichTextCellText spans={preview.display.spans} interactive={false} />
                      : preview.display.text || <span className={styles.muted}>{t("noResult")}</span>}
                </span>
                {typeName ? <span className={styles.typeBadge}>{t("typeLabel", { type: typeName })}</span> : null}
              </>
            ) : !draft.trim() ? (
              // As Notion's: an empty formula gives nothing, of a type not known.
              <>
                <span className={styles.resultValue}><span className={styles.muted}>{t("noOutput")}</span></span>
                <span className={styles.typeBadge}>{t("typeLabel", { type: t("types.unknown") })}</span>
              </>
            ) : null)}
          </div>
          {emptyCalls.slice(0, FORMULA_SHOWN_LINES).map((call, index) => (
            <div key={index} className={styles.emptyCall} data-formula-warning={call.kind}>
              <span>{call.line}</span>
              <span className={styles.emptyCallPlace}>{formulaProblemPlace({ key: "emptyValueCall", start: shown(call.start), end: shown(call.end) })}</span>
            </div>
          ))}
          </div>
        </div>}
        {helperOpen ? (
          <FormulaEditorHelper groups={groups} names={names} edit={`${cursor}:${draft}`} ownerRef={caretRef}
            phone={phone ? (overSheet ? "grouped" : "flat") : undefined} onCaretReady={setHelperKeys} onInsert={insert} />
        ) : null}
      </section>
  );
  if (phone) return (
    <PhoneSheet title={t("title")} head={false} plain size={overSheet ? "full" : "screen"} onClose={close}
      data={{ "data-formula-sheet": overSheet ? "grouped" : "flat" }}>
      <div className={phoneStyles.contain} {...contain}>{section}</div>
    </PhoneSheet>
  );
  const dialog = (
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} {...contain}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} onClick={close} />
      {section}
    </div>
  );
  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}

function stop(event: { stopPropagation: () => void }) {
  event.stopPropagation();
}
