import { useLayoutEffect, useRef, useState, type ComponentType, type ReactNode } from "react";
import type { Editor as FieldEditor } from "@tiptap/core";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { Editor } from "@tiptap/core";
import { TextSelection, type Transaction } from "@tiptap/pm/state";
import { TouchSheetLayout } from "@/components/shell/AppShell";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { phoneScreen } from "@/lib/screenKind";
import { AddReactionIcon, ArrowLeftToLine, ArrowRightToLine, BoldIcon, CheckIcon, CodeIcon, CommentIcon, DoubleChevronLeft, InlineEquationIcon,
  ItalicIcon, LinkPlusIcon, StrikethroughIcon, UnderlineIcon, type LeafenIconProps } from "@/icons/leafen";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { colorChosenWords, commentOnChosenWords, reactToChosenWords, reactToTitleWords } from "./chosenWordsActions";
import { wordsChosenByPerson } from "./inputMethodChoice";
import { chosenCommentText } from "./nativeTiptapChosenComment";
import { nativeTiptapSelectionContext, nativeTiptapSelectionIntoTable } from "./nativeTiptapSelectionContext";
import { requestNativeTiptapEquation, requestNativeTiptapLinkEdit } from "./nativeTiptapInlineUiEvents";
import { InlineEquationEditor } from "./InlineEquationEditor";
import { PhoneMenuHeader } from "./PhoneMenuHeader";
import { currentStripPanel } from "./stripPanelState";
import { PhoneStripPanel, StripColorMark } from "./PhoneStripPanel";
import { PhoneLinkSheet } from "./PhoneLinkSheet";
import { storedEditorColor } from "./colorMemory";
import { BACKGROUND_COLORS, TEXT_COLORS } from "./selectionToolbarModel";
import { useTextReactionStarter } from "./TextReactions";
import type { PhoneTitleMark, PhoneTitleTools, PhoneTitleWords } from "./phoneTitleTools";
import editorStyles from "./editorStyles.generated.module.css";
import phoneStyles from "./PhoneMenu.module.css";
import styles from "./NativePhoneToolbar.module.css";

/** A tool of a touch screen's line of editing tools (NativePhoneToolbar). */
export type PhoneTool = {
  key: string;
  icon?: ComponentType<LeafenIconProps>;
  /** Words drawn instead of an icon. */
  label?: ReactNode;
  /** Its name where it is not phoneToolbar.<key>. */
  name?: string;
  disabled?: boolean;
  run(button: HTMLElement): void;
  /** It opens a list: a chevron after it. */
  arrow?: boolean;
  /** Tools drawn in one cell together. */
  group?: string;
  flipped?: boolean;
  /** A mark the chosen words hold: drawn blue. */
  pressed?: boolean;
};

const MARKS = [["bold", "B"], ["italic", "i"], ["underline", "U"], ["strikethrough", "S"]] as const;
const STRIP_MARKS = { bold: BoldIcon, italic: ItalicIcon, underline: UnderlineIcon, strikethrough: StrikethroughIcon };
const ENGINE_MARKS: Record<PhoneTitleMark, string> = { bold: "bold", italic: "italic", underline: "underline", strikethrough: "strike", code: "code" };

/**
 * A sheet open for the words from `from` to `to` (in the title's field, or the page's), with their link and their two colors as it opened;
 * `strip`, the colors a phone strip opened in the keyboard's place (stripPanelState.ts).
 */
type Sheet = { kind: "color" | "link" | "equation"; title: PhoneTitleTools | null; from: number; to: number; href: string; text: string; background: string;
  strip?: boolean };

/** The chosen words as their tools read and change them, in a page's text or its title. */
type Words = {
  field: Editor; oneLine: boolean;
  marks: Record<PhoneTitleMark, boolean>; text: string; background: string; link: string;
  toggle(mark: PhoneTitleMark): void; color(token: string): void; setLink(href: string, words?: string): void;
  comment: (() => void) | null; react: ((button: HTMLElement) => void) | null; equation: (() => void) | null;
  nest(lift: boolean): Transaction | null;
};

/**
 * The line of tools chosen words take on a touch screen, in the caret's
 * tools' place: a phone's strip as the Notion Android app's (tools(title,
 * true)), a wider screen's as Notion's in a browser (seen 2026-11-14 in
 * iPhone Safari at 402, a line's words, a heading's, a list item's, a code
 * block's, the title's and words across two lines): 댓글 · 리액션 · B · i ·
 * U · S · 내어쓰기 · 들여쓰기 · 코드 · 링크 · √x · 색, the keyboard at the
 * end, a mark the words hold blue. Across lines the indents, the link and √x
 * are dimmed, and in the title the indents. "링크" and "색" open a sheet over
 * the page (SelectionSheet); the rest act at once, the words staying chosen.
 */
export function usePhoneChosenWords(editor: Editor, adapter: NativeEditorGraphAdapter, nest: (lift: boolean) => Transaction | null) {
  const { t } = useTranslation(["editor", "selectionToolbar"]);
  const reaction = useTextReactionStarter();
  const [sheet, setSheet] = useState<Sheet | null>(null);
  const usable = (field: Editor) => !field.isDestroyed && !editor.isDestroyed && adapter.isCurrent() && !adapter.readOnly;

  function bodyWords(): Words {
    const state = editor.state, { from, to } = state.selection;
    const oneLine = Boolean(nativeTiptapSelectionContext(state) ?? nativeTiptapSelectionIntoTable(state));
    const chosen = Boolean(chosenCommentText(state));
    const marks = Object.fromEntries(Object.entries(ENGINE_MARKS).map(([mark, name]) => [mark, editor.isActive(name)])) as Words["marks"];
    return { field: editor, oneLine, marks, link: editor.getAttributes("link").href ?? "",
      text: editor.getAttributes("nativeColor").stringValue ?? "default",
      background: editor.getAttributes("nativeBackground").stringValue ?? "default_background",
      toggle: mark => { editor.chain().focus().toggleMark(ENGINE_MARKS[mark]).run(); },
      color: token => colorChosenWords(editor, token, { writable: true }),
      // At a caret (a phone strip's "Aa"), the link goes in as its own words: the page's name or the address.
      setLink: (href, named) => {
        if (href && editor.state.selection.empty) editor.chain().focus().insertContent({ type: "text", text: named || href, marks: [{ type: "link", attrs: { href } }] }).run();
        else if (href) editor.chain().focus().setMark("link", { href }).run(); else editor.chain().focus().unsetMark("link").run();
      },
      comment: chosen ? () => { commentOnChosenWords(editor, adapter.pageId); } : null,
      react: chosen && reaction ? button => { reactToChosenWords(editor, adapter.pageId, reaction, button); } : null,
      equation: oneLine ? () => requestNativeTiptapEquation(editor, from, to) : null, nest };
  }
  // The title's words take the title's own marks, a link, a comment and a
  // reaction on the page as its caret's tools do and Notion's (seen
  // 2026-11-14: the reaction the page's own, under its title); their
  // equation opens in its own window (tools).
  function titleWords(title: PhoneTitleTools, words: PhoneTitleWords): Words | null {
    const format = words.format();
    if (!format) return null;
    const comment = title.comment();
    return { field: title.editor, oneLine: true, marks: format.marks, link: format.link,
      text: format.color === "mixed" ? "default" : format.color, background: format.background === "mixed" ? "default_background" : format.background,
      toggle: words.toggle, color: words.color, setLink: words.link, comment: comment ?? null,
      react: comment && reaction ? button => { reactToTitleWords(title.editor, adapter.pageId, reaction, button); } : null,
      equation: null, nest: () => null };
  }
  const wordsOf = (title: PhoneTitleTools | null) => title ? title.words ? titleWords(title, title.words) : null : bodyWords();

  function tools(title: PhoneTitleTools | null, strip = false): PhoneTool[] {
    const words = wordsOf(title);
    if (!words) return [];
    const open = (kind: Sheet["kind"]) => {
      const { from, to } = words.field.state.selection;
      setSheet({ kind, title, from, to, href: words.link, text: words.text, background: words.background,
        strip: kind === "color" && currentStripPanel()?.kind === "color" });
    };
    const nestRun = (lift: boolean) => () => { const tr = words.nest(lift); if (tr) editor.view.dispatch(tr); };
    const { text, background } = words;
    // A title's words become an equation in its own window, as Notion's (seen
    // 2026-11-14: the words in its field, "완료" making them one).
    const equation = title ? equationIn(title.editor) ? () => open("equation") : null : words.equation;
    const comment = adapter.session ? [{ key: "comment", icon: CommentIcon, disabled: !words.comment, run: () => words.comment?.() }] : [];
    const react = reaction ? [{ key: "reaction", icon: AddReactionIcon, name: t("selectionToolbar:reaction"), disabled: !words.react,
      run: (button: HTMLElement) => words.react?.(button) }] : [];
    const tint = { ...(text !== "default" ? { color: `var(--c-${text})` } : {}),
      ...(background !== "default_background" ? { background: `var(--c-bg-${background.replace(/_background$/u, "")})` } : {}) };
    // A phone's strip, as the Notion Android app's (2026-11-21, a word chosen
    // by a long press): the color's "A" first, 댓글 · 리액션 · B · I · U · S ·
    // 링크 · 코드 · √x, each its mark, a mark held on its grey tile; the app's
    // "«" (unseen what it does) and AI left out.
    if (strip) return [
      { key: "color", label: <span className={styles.colorMark} style={tint} aria-hidden="true">A</span>, name: t("phoneToolbar.color"), run: () => open("color") },
      ...comment, ...react, ...stripMarks(words.marks, mark => words.toggle(mark)),
      // A page's linked words edit their link (NativeTiptapLinkPill's PhoneLinkEdit), as the app's (seen 2026-11-25).
      { key: "link", icon: LinkPlusIcon, name: t("selectionToolbar:marks.link"), disabled: !words.oneLine, pressed: Boolean(words.link),
        run: () => (!title && words.link ? requestNativeTiptapLinkEdit(editor) : open("link")) },
      { key: "code", icon: CodeIcon, name: t("selectionToolbar:marks.code"), pressed: words.marks.code, run: () => words.toggle("code") },
      { key: "equation", icon: InlineEquationIcon, name: t("selectionToolbar:marks.equation"), disabled: !equation, run: () => equation?.() },
    ];
    return [
      ...comment, ...react,
      ...MARKS.map(([key, letter]) => ({ key, label: letter, name: t(`selectionToolbar:marks.${key}`), pressed: words.marks[key],
        run: () => words.toggle(key) })),
      { key: "outdent", icon: ArrowLeftToLine, group: "chosenNest", disabled: !words.oneLine || !words.nest(true), run: nestRun(true) },
      { key: "indent", icon: ArrowRightToLine, group: "chosenNest", disabled: !words.oneLine || !words.nest(false), run: nestRun(false) },
      { key: "code", label: t("selectionToolbar:marks.code"), pressed: words.marks.code, run: () => words.toggle("code") },
      { key: "link", label: <span className={styles.linkWord}>{t("selectionToolbar:marks.link")}</span>, disabled: !words.oneLine, run: () => open("link") },
      { key: "equation", icon: InlineEquationIcon, name: t("selectionToolbar:marks.equation"), disabled: !equation, run: () => equation?.() },
      { key: "color", label: <span className={styles.colorWord} style={tint}>{t("phoneToolbar.color")}</span>, run: () => open("color") },
    ];
  }

  /**
   * A phone strip's "Aa" at a caret in the page, as the Notion Android app's
   * (2026-11-21): « · the color's "A" · B · I · U · S · 링크 · 코드 · √x, the
   * keyboard staying up. A mark pressed holds for what is typed next (a
   * letter typed after B went in bold, and the line's tools came back); its
   * link goes in as words; √x opens an empty equation at the caret. The "A"
   * opens the line's colors, in the keyboard's place where a browser can put
   * it away (stripPanelState.ts), over the screen elsewhere.
   */
  function caretTools(back: () => void, color: () => void): PhoneTool[] {
    const words = bodyWords();
    const { from } = editor.state.selection;
    return [
      { key: "back", icon: DoubleChevronLeft, run: back },
      { key: "color", label: <span className={styles.colorMark} aria-hidden="true">A</span>, name: t("phoneToolbar.color"), run: color },
      ...stripMarks(words.marks, mark => words.toggle(mark)),
      { key: "link", icon: LinkPlusIcon, name: t("selectionToolbar:marks.link"), disabled: !words.oneLine,
        run: () => setSheet({ kind: "link", title: null, from, to: from, href: "", text: words.text, background: words.background }) },
      { key: "code", icon: CodeIcon, name: t("selectionToolbar:marks.code"), pressed: words.marks.code, run: () => words.toggle("code") },
      { key: "equation", icon: InlineEquationIcon, name: t("selectionToolbar:marks.equation"), disabled: !words.equation, run: () => words.equation?.() },
    ];
  }

  function stripMarks(marks: Words["marks"], toggle: (mark: PhoneTitleMark) => void): PhoneTool[] {
    return MARKS.map(([key]) => ({ key, icon: STRIP_MARKS[key], name: t(`selectionToolbar:marks.${key}`), pressed: marks[key], run: () => toggle(key) }));
  }

  // The words are chosen again, their field's focus back (a sheet takes it
  // while open), and then changed — after the press that closed the sheet,
  // so the keyboard stays down and the tools stand alone, as Notion's (seen
  // 2026-11-14 in iPhone Safari: after "취소", a color or a page linked);
  // given back in the press, the focus brought the keyboard up again. On a
  // phone a link sheet or an equation's field, which keep the keyboard up
  // while open, give it back in the press, the keyboard staying up with the
  // words' strip, as the Notion Android app's (seen 2026-11-25 on a Galaxy
  // A90: after "취소" and after "완료").
  function close(change?: (words: Words) => void) {
    const was = sheet;
    setSheet(null);
    const field = was?.title?.editor ?? editor;
    if (!was || !usable(field)) return;
    if (was.kind !== "color" && phoneScreen()) field.view.focus();
    requestAnimationFrame(() => {
      if (!usable(field)) return;
      const size = field.state.doc.content.size;
      field.chain().focus().setTextSelection({ from: Math.min(was.from, size), to: Math.min(was.to, size) }).run();
      const words = change && wordsOf(was.title);
      if (words) change(words);
    });
  }
  // A title's equation takes the place of its words.
  const makeEquation = (tex: string) => close(words => {
    const atom = words.field.schema.nodes.nativeAtom;
    if (!atom || !tex.trim()) return;
    const { from, to } = words.field.state.selection;
    words.field.chain().focus().command(({ tr }) => {
      tr.replaceWith(from, to, atom.create({ text: tex, payload: JSON.stringify({ equation: tex }) }));
      return true;
    }).run();
  });
  const sheetShown = sheet?.kind === "equation" ? <InlineEquationEditor anchor={wordsPlace(sheet)} initial={sheetWords(sheet)}
    onPreview={() => {}} onDone={makeEquation} onCancel={() => close()} />
    : sheet?.strip ? <ColorPanel onClose={() => close()} onColor={token => close(words => words.color(token))} />
    : sheet ? <SelectionSheet sheet={sheet} adapter={adapter} onClose={() => close()}
    onColor={token => close(words => words.color(token))} onLink={(href, named) => close(words => words.setLink(href, named))} /> : null;
  return { tools, caretTools, sheet: sheetShown };
}

/** Whether a field takes an equation: a title's, which holds them as its words. */
function equationIn(field: FieldEditor) {
  return Boolean(field.schema.nodes.nativeAtom);
}

/** Where a sheet's words stand, and what they read. */
function wordsPlace(sheet: Sheet) {
  const field = (sheet.title?.editor)!;
  const start = field.view.coordsAtPos(sheet.from), end = field.view.coordsAtPos(sheet.to);
  return { top: Math.min(start.top, end.top), bottom: Math.max(start.bottom, end.bottom), left: start.left, right: Math.max(end.right, start.left) };
}
function sheetWords(sheet: Sheet) {
  return sheet.title ? sheet.title.editor.state.doc.textBetween(sheet.from, sheet.to, "") : "";
}

/** Whether the selection holds words a person chose (wordsChosenByPerson): text, or an inline part (a mention, an equation) as one. */
export function chosenWords(editor: Editor) {
  const { selection, doc } = editor.state;
  return selection instanceof TextSelection && wordsChosenByPerson(editor.view)
    && doc.textBetween(selection.from, selection.to, "", node => node.isInline ? "￼" : "") !== "";
}

/**
 * The chosen words' "색" or "링크", as Notion's (seen 2026-11-14 in iPhone
 * Safari at 402), "취소" at its head's end. "텍스트 색" covers the screen as
 * the tools' other lists do (PhoneMenu.module.css): 최근 사용, 텍스트 색상
 * and 배경 색상, the words' own two ticked and their boxes ringed.
 * "링크" is the phone's link sheet (PhoneLinkSheet). A choice closes it, the
 * words still chosen.
 */
function SelectionSheet({ sheet, adapter, onClose, onColor, onLink }: {
  sheet: Sheet; adapter: NativeEditorGraphAdapter;
  onClose(): void; onColor(token: string): void; onLink(href: string, named?: string): void;
}) {
  if (sheet.kind === "color") return <ColorSheet sheet={sheet} onClose={onClose} onColor={onColor} />;
  return <PhoneLinkSheet href={sheet.href} onClose={onClose} onLink={onLink}
    source={adapter.session ? { session: adapter.session, workspaceId: adapter.workspaceId } : undefined} />;
}

/** A color's name in the chosen words' lists: "회색 텍스트", "회색 배경", "기본 배경". */
function useColorName() {
  const { t } = useTranslation(["editor", "blockHandle"]);
  return (token: string) => token === "default_background" ? t("blockHandle:colorMenu.defaultBackground")
    : token.endsWith("_background") ? t(`blockHandle:colors.${token}`) : t("blockHandle:menu.textColorNamed", { color: t(`blockHandle:colors.${token}`) });
}

/**
 * The chosen words' "A" on a phone strip, in the keyboard's place as the
 * app's colors (PhoneStripPanel): 최근 사용 where one is, 텍스트 색상, 배경 색상.
 */
function ColorPanel({ onClose, onColor }: { onClose(): void; onColor(token: string): void }) {
  const { t } = useTranslation(["editor", "blockHandle"]);
  const colorName = useColorName();
  const [recent] = useState(() => storedEditorColor());
  const card = (token: string, key = token) => ({ key, icon: <StripColorMark color={token} />, label: colorName(token), run: () => onColor(token),
    data: { "data-chosen-color": token } });
  return <PhoneStripPanel kind="color" label={t("phoneToolbar.textColor")} onClose={onClose} sections={[
    ...(recent ? [{ label: t("blockHandle:colorMenu.recent"), cards: [card(recent, `recent:${recent}`)] }] : []),
    { label: t("blockHandle:colorMenu.textColor"), cards: TEXT_COLORS.map(color => card(color.token)) },
    { label: t("blockHandle:colorMenu.backgroundColor"), cards: BACKGROUND_COLORS.map(color => card(color.token)) },
  ]} />;
}

function ColorSheet({ sheet, onClose, onColor }: { sheet: Sheet; onClose(): void; onColor(token: string): void }) {
  const { t } = useTranslation(["editor", "blockHandle"]);
  const ref = useRef<HTMLDivElement>(null);
  const layer = useOverlayLayer(true, { kind: "dialog", ref, onEscape: onClose });
  const [recent] = useState(() => storedEditorColor());
  // The page's focus goes, and the keyboard with it, while the sheet shows.
  useLayoutEffect(() => { (document.activeElement as HTMLElement | null)?.blur?.(); }, []);
  const title = t("phoneToolbar.textColor");
  const colorName = useColorName();
  const row = (token: string, key = token) => {
    const own = token.endsWith("_background") ? sheet.background === token : sheet.text === token;
    return <button type="button" key={key} className={phoneStyles.row} data-chosen-color={token}
      role="menuitemradio" aria-checked={own} onClick={() => onColor(token)}>
      <span className={`${editorStyles.colorSwatch} ${phoneStyles.swatch}`} data-color={token} data-chosen={own && key === token || undefined} aria-hidden="true"
        style={token.endsWith("_background") && token !== "default_background" ? { color: `var(--c-${token.replace(/_background$/u, "")})` } : undefined}>
        {token.endsWith("_background") ? null : "A"}</span>
      <span>{colorName(token)}</span>
      {own && key === token ? <CheckIcon size="small" className={phoneStyles.check} aria-hidden="true" /> : null}
    </button>;
  };
  const group = (label: string, rows: ReactNode[]) => <div className={phoneStyles.section}>
    <div className={phoneStyles.label}>{label}</div>
    <div className={phoneStyles.rows} role="group" aria-label={label}>{rows}</div>
  </div>;
  return createPortal(<TouchSheetLayout><div ref={ref} role="menu" aria-label={title}
    className={`${phoneStyles.menu} ${phoneStyles.list}`} data-phone-chosen-sheet="color" style={{ zIndex: layer.zIndex }}>
    <PhoneMenuHeader title={title} onClose={onClose} />
    {recent ? group(t("blockHandle:colorMenu.recent"), [row(recent, `recent:${recent}`)]) : null}
    {group(t("blockHandle:colorMenu.textColor"), TEXT_COLORS.map(color => row(color.token)))}
    {group(t("blockHandle:colorMenu.backgroundColor"), BACKGROUND_COLORS.map(color => row(color.token)))}
  </div></TouchSheetLayout>, document.body);
}
