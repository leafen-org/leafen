import { type ContextType, type ReactNode, useContext, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { Editor } from "@tiptap/core";
import { TextSelection } from "@tiptap/pm/state";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import type { DateMentionChange } from "@/lib/collaborativeMentions";
import type { TextSpan } from "@/lib/types";
import { replaceCellRange } from "../database/richTextCellModel";
import type { BlockPickerSnapshot, MentionItem } from "./blockPickerModel";
import { blockItemText } from "./blockItemLabels";
import { MentionMenu } from "./MentionMenu";
import { FieldPagesContext } from "./mentionSnapshotContext";
import { BlockMoveToDialog } from "./BlockMoveToDialog";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { useProductPages } from "@/components/product/productPagesContext";
import { makeAnchoredSubPage, makePageUnder } from "./anchoredSubPage";
import { InviteAndMentionPanel, inviteMentionDoneText } from "../page/InviteAndMentionPanel";
import { popoverRectAt } from "@/lib/popoverPlacement";
import { mentionInsertion } from "./mentionInsertion";
import { fieldMenuTrigger, type FieldMenu } from "./textBlockInputModel";
import { InlineEmojiMenu } from "./InlineEmojiMenu";
import { nativeTiptapCaretCoords } from "./nativeTiptapMenuAnchor";
import { registerMentionQuery, showMentionQuery } from "./mentionQuery";
import { changedDateAtomAttrs, pmOffsetAt, readNativeInlineMetadata } from "./nativeTiptapInline";
import { DateMentionEditor, type DateMentionTarget } from "./DateMentionEditor";
import { dateMentionTarget } from "./textBlockMentionClicks";
import { invitedPersonMentions, offerMentionInvite } from "./mentionInvite";
import { i18next } from "@/i18n";
import { useOptionalProductToast } from "@/lib/productToast";
import type { Session } from "@/types";
import { editorHoldsPageFocus } from "./editorHoldsPageFocus";

type MenuAnchor = { left: number; top: number; bottom: number };

/**
 * Where a field's menu stands, as a page line's (NativeTiptapMenus): an "@"'s
 * by the caret right after its "@", a page link's by its "[[" or "+", an
 * emoji's 3px past its ":" — by its opener, not by the caret where its query
 * grows (Notion's, seen 2026-11-12).
 */
function openerAnchor(editor: Editor, trigger: FieldMenu): MenuAnchor {
  const opener = 1 + pmOffsetAt(editor.state.doc.firstChild!.content, trigger.start);
  try {
    if (trigger.kind === "mention") return nativeTiptapCaretCoords(editor, opener + 1);
    const at = editor.view.coordsAtPos(opener);
    return trigger.kind === "emoji" ? { left: at.left + 3, top: at.top, bottom: at.bottom } : at;
  } catch {
    // A place the field no longer has: the caret's.
    return editor.view.coordsAtPos(editor.state.selection.head);
  }
}

/** A field of words, mentions and equations its menus write into: a database cell, a page's title. */
export type MenuField = {
  editor: Editor;
  read(): TextSpan[];
  selection(): { start: number; end: number };
  replace(spans: readonly TextSpan[], start: number, end?: number, remote?: boolean): void;
};

/** A menu its owner draws in place of the page's, as an automation message's (AutomationMentionMenu). */
export type FieldMentionMenuRenderer = (menu: { anchor: MenuAnchor; query: string; pick(item: MentionItem): void; close(): void }) => ReactNode;

/**
 * The "@" menu of a field outside a page body's blocks, the body's own menu
 * (MentionMenu), and what follows from it: a date mention's window, a page
 * made from the menu, an invite. Its "@" follows the page's rule
 * (mentionMenuTrigger): typed at the start, after a space or a bracket it opens, and
 * all typed after it — spaces too — is its query until a line break, the
 * "@" goes or it is closed. `lineRules` gives it ":", "[[" and "+" too, as a
 * line's (a database cell); `commentMention` a comment box's dates and grey
 * hint. The field calls `updated` as its words or caret change.
 */
export function useFieldMenus(options: {
  field: () => MenuField | null;
  /** The field once made: the "@" and its query sit in a pale pill while their menu is open. */
  core: MenuField | null;
  /** What the @ menu offers; none opens no menu. */
  snapshot: BlockPickerSnapshot | null | undefined;
  lineRules?: boolean;
  commentMention?: boolean;
  /** The page a menu's new page goes under, and its date mentions' page. */
  pageId?: string;
  /**
   * The field of that page a new sub-page made from the "@" hangs on, as
   * Notion's (anchoredSubPage.ts): its title, or a database page's cell.
   */
  anchor?: string;
  /** The page whose sharing the "@" menu's invite opens. */
  invitePage?: string;
  /**
   * Who writes: the "@" menu marks the page's guests, restricted members and
   * the people it doesn't open to, and asks to invite one picked (mentionInvite.ts).
   */
  session?: Session;
  /** Where the pill is drawn: a page's own words, or a cell's. */
  place: "page" | "cell" | "comment";
  renderMentionMenu?: FieldMentionMenuRenderer;
  /** Pages made from the menu: the page's editing, where the field is outside it. */
  fieldPages?: ContextType<typeof FieldPagesContext>;
}) {
  const latest = useRef(options); latest.current = options;
  const toast = useOptionalProductToast();
  const productPages = useProductPages();
  const contextPages = useContext(FieldPagesContext);
  const fieldPages = options.fieldPages ?? contextPages;
  const [menu, setMenu] = useState<(FieldMenu & { anchor: MenuAnchor }) | null>(null);
  const [date, setDate] = useState<DateMentionTarget | null>(null);
  const [addPage, setAddPage] = useState<{ trigger: NonNullable<typeof menu>; title: string } | null>(null);
  // "초대" opens "초대와 멘션" by the "@", the name typed in its field (InviteAndMentionPanel);
  // the "@" and what was typed after it become whom it invited (mentionInvited).
  const [inviting, setInviting] = useState<{ name: string; pageId: string; anchor: MenuAnchor; trigger: FieldMenu } | null>(null);
  const menuRef = useRef(menu); menuRef.current = menu;
  const opened = useRef<{ kind: FieldMenu["kind"]; start: number } | null>(null);
  const before = useRef({ length: 0, at: 0 });
  const [menus] = useState(() => ({
    /**
     * The field's words or caret changed; `remap` moves a place in them
     * through a change from elsewhere (a title following another window),
     * which the open "@" moves with and which isn't typing.
     */
    updated(remap?: (offset: number) => number) {
      const current = latest.current.field();
      if (!current || current.editor.isDestroyed) return;
      const { editor } = current, at = current.selection();
      const text = current.read().map(span => span.text).join("");
      if (remap && opened.current) opened.current = { ...opened.current, start: remap(opened.current.start) };
      // What was just put in: a key's letter, or a word an input method or a
      // correction put in at once (fieldMenuTrigger reads it as a line does).
      const grew = text.length - before.current.length;
      const typed = !remap && grew > 0 && at.start === before.current.at + grew ? text.slice(before.current.at, at.start) : "";
      before.current = { length: text.length, at: at.start };
      const { lineRules, commentMention, snapshot, renderMentionMenu } = latest.current;
      const rules = lineRules ? "all" : commentMention ? "links" : "mention";
      const trigger = (snapshot || renderMentionMenu) && editorHoldsPageFocus(editor) && editor.state.selection.empty
        ? fieldMenuTrigger(opened.current, text, at.start, typed, rules) : null;
      opened.current = trigger ? { kind: trigger.kind, start: trigger.start } : null;
      setMenu(trigger ? { ...trigger, anchor: openerAnchor(editor, trigger) } : null);
    },
    close() {
      opened.current = null;
      setMenu(null);
    },
    /** A date mention pressed opens its window, as in a page's text; whether it did. */
    datePressed(mention: HTMLElement, root: HTMLElement) {
      const target = dateMentionTarget(mention, root);
      if (target) { menus.close(); setDate(target); }
      return Boolean(target);
    },
    /** Writes "@" at the caret, opening the @ menu there. */
    mention() {
      const current = latest.current.field();
      if (!current || current.editor.isDestroyed) return;
      const spans = current.read(), at = current.selection(), text = spans.map(span => span.text).join("");
      const cue = at.start > 0 && !/\s/u.test(text[at.start - 1] ?? "") ? " @" : "@";
      try {
        current.replace(replaceCellRange(spans, at.start, at.end, [{ text: cue }]), at.start + cue.length);
      } catch (error) { if (!(error instanceof RangeError)) throw error; }
      opened.current = { kind: "mention", start: at.start + cue.length - 1 };
      menus.updated();
    },
  }));
  const { core } = options;
  useEffect(() => core ? registerMentionQuery(core.editor, latest.current.place) : undefined, [core]);
  useEffect(() => {
    if (!core || core.editor.isDestroyed) return;
    const content = core.editor.state.doc.firstChild!.content;
    showMentionQuery(core.editor, menu && menu.kind !== "emoji" ? { from: 1 + pmOffsetAt(content, menu.start), to: 1 + pmOffsetAt(content, menu.end) } : null);
  }, [core, menu]);
  // A page made from the menu goes in as its mention, as a line's does, in
  // the press's own frame (makePageUnder).
  async function makePage(destination: string, trigger: NonNullable<typeof menu>, title: string) {
    if (!fieldPages) return;
    const { moveContext, workspaceId } = fieldPages;
    try {
      const page = await makePageUnder({ move: moveContext, outbox: productPages?.mutationOutbox, parentId: destination, workspaceId, title });
      const current = latest.current.field(), words = [{ text: page.title || title, mention: "page" as const, pageId: page.id }, { text: " " }];
      if (!current || current.editor.isDestroyed) return;
      if (!current.read().map(span => span.text).join("").slice(trigger.start, trigger.end).endsWith(trigger.query)) {
        moveContext.onStatus?.(blockItemText("mention.pageCreatedWithoutMention"), "default");
        return;
      }
      current.replace(replaceCellRange(current.read(), trigger.start, trigger.end, words), trigger.start + words[0].text.length + 1);
    } catch {
      moveContext.onStatus?.(blockItemText("mention.addPageFailed"), "error");
    }
  }
  // A sub-page made under the field's own page hangs on the field, its
  // mention in at once, as a line's (makeAnchoredSubPage); false where it
  // can't, for makePage.
  function makeFieldPage(trigger: NonNullable<typeof menu>, title: string) {
    const { anchor, pageId } = latest.current;
    if (!anchor || !pageId || !fieldPages) return false;
    return makeAnchoredSubPage({ move: fieldPages.moveContext, outbox: productPages?.mutationOutbox, parentId: pageId,
      workspaceId: fieldPages.workspaceId, title, anchor, write: (page) => {
        const current = latest.current.field();
        if (!current || current.editor.isDestroyed
          || !current.read().map(span => span.text).join("").slice(trigger.start, trigger.end).endsWith(trigger.query)) return false;
        const words = [{ text: page.title || untitledPageDisplayTitle(page), mention: "page" as const, pageId: page.id }, { text: " " }];
        try {
          current.replace(replaceCellRange(current.read(), trigger.start, trigger.end, words), trigger.start + words[0].text.length + 1);
        } catch (error) {
          if (!(error instanceof RangeError)) throw error;
          return false;
        }
        return true;
      } });
  }
  function pickMention(item: MentionItem) {
    const trigger = menuRef.current, current = latest.current.field(), insertion = mentionInsertion(item);
    const { invitePage, pageId, lineRules } = latest.current;
    menus.close();
    if (item.kind === "invite") { if (invitePage && trigger) setInviting({ name: item.name, pageId: invitePage, anchor: trigger.anchor, trigger }); return; }
    if (trigger && item.kind === "sub_page" && pageId) { if (!makeFieldPage(trigger, item.title)) void makePage(pageId, trigger, item.title); return; }
    if (trigger && item.kind === "add_page") { setAddPage({ trigger, title: item.title }); return; }
    if (!trigger || !current || !insertion || current.editor.isDestroyed) return;
    const { dateReminder: reminder, ...attributes } = insertion.attributes;
    if (lineRules && reminder !== undefined) Object.assign(attributes, { dateReminder: reminder });
    const text = current.read().map(span => span.text).join("");
    if (!text.slice(trigger.start, trigger.end).endsWith(trigger.query)) return;
    const space = insertion.spaced ? [{ text: " " }] : [];
    try {
      current.replace(replaceCellRange(current.read(), trigger.start, trigger.end,
        [{ text: insertion.text, ...attributes }, ...space]), trigger.start + insertion.text.length + space.length);
    } catch (error) { if (!(error instanceof RangeError)) throw error; }
    // Someone the page doesn't open to is offered an invite, as from a page's text (mentionInvite.ts).
    const { session } = latest.current;
    if (item.kind === "person" && session && invitePage && toast) {
      void offerMentionInvite({ session, pageId: invitePage, userId: item.userId, name: item.label, notify: toast.notify });
    }
  }
  // Whom an "@" invited is mentioned where the "@" was, as Notion's says
  // "〈주소〉님을 초대했습니다." and mentions them (seen 2026-11-12).
  async function mentionInvited(trigger: FieldMenu, emails: string[]) {
    toast?.notify(inviteMentionDoneText(emails), "success");
    const session = latest.current.session ?? fieldPages?.moveContext.session;
    const workspaceId = fieldPages?.workspaceId ?? latest.current.snapshot?.workspaceId;
    if (!session || !workspaceId) return;
    const words = await invitedPersonMentions(session, workspaceId, emails).catch(() => {
      toast?.notify(i18next.t("topBar:inviteMention.notPlaced"), "error");
      return [];
    });
    const current = latest.current.field();
    if (!words.length || !current || current.editor.isDestroyed) return;
    if (!current.read().map(span => span.text).join("").slice(trigger.start, trigger.end).endsWith(trigger.query)) return;
    try {
      current.replace(replaceCellRange(current.read(), trigger.start, trigger.end, words),
        trigger.start + words.reduce((length, span) => length + span.text.length, 0));
    } catch (error) { if (!(error instanceof RangeError)) throw error; }
  }
  // The date its window was opened on: the field's so-many-th date mention.
  function dateAtom(ordinal: number) {
    const current = latest.current.field();
    if (!current || current.editor.isDestroyed) return null;
    const field = current.editor.state.doc.firstChild!;
    for (let index = 0, seen = 0, position = 1; index < field.childCount; position += field.child(index).nodeSize, index += 1) {
      const node: ProseMirrorNode = field.child(index);
      if (node.type.name === "nativeAtom" && readNativeInlineMetadata(node.attrs.payload).mention === "date" && seen++ === ordinal) {
        return { editor: current.editor, node, position };
      }
    }
    return null;
  }
  // Each change shows at once, the window staying open, as the page's.
  function changeDate(change: DateMentionChange) {
    const atom = date && dateAtom(date.ordinal);
    if (atom) atom.editor.view.dispatch(atom.editor.state.tr.setNodeMarkup(atom.position, undefined, changedDateAtomAttrs(atom.node.attrs, change), atom.node.marks));
  }
  // "삭제" takes the date out, the writing going on where it stood.
  function removeDate() {
    const atom = date && dateAtom(date.ordinal);
    setDate(null);
    if (!atom) return;
    const tr = atom.editor.state.tr.delete(atom.position, atom.position + 1);
    atom.editor.view.dispatch(tr.setSelection(TextSelection.create(tr.doc, atom.position)));
    atom.editor.view.focus();
  }
  function pickEmoji(words: TextSpan) {
    const trigger = menuRef.current, current = latest.current.field();
    menus.close();
    if (!trigger || !current || current.editor.isDestroyed) return;
    try {
      current.replace(replaceCellRange(current.read(), trigger.start, trigger.end, [words]), trigger.start + words.text.length);
    } catch (error) { if (!(error instanceof RangeError)) throw error; }
  }
  const { snapshot, renderMentionMenu, commentMention, lineRules, pageId, invitePage } = options;
  const element = <>
    {menu?.kind === "emoji" ? <InlineEmojiMenu anchor={menu.anchor} query={menu.query} onClose={menus.close} onPick={emoji => pickEmoji({ text: emoji })}
      custom={snapshot?.organizationId ? { workspaceId: snapshot.organizationId, pageId: pageId ?? "",
        onPick: emoji => pickEmoji({ text: `:${emoji.name}:`, mention: "custom_emoji", customEmojiId: emoji.id }) } : undefined} />
      : menu && renderMentionMenu ? renderMentionMenu({ anchor: menu.anchor, query: menu.query, pick: pickMention, close: menus.close })
      : menu && snapshot ? <MentionMenu anchor={menu.anchor} pageId={invitePage ?? pageId} query={menu.query} session={options.session}
        mode={menu.kind === "page-link" ? "page_link" : "mention"} newPageFirst={menu.newPageFirst}
        newPages={Boolean((lineRules || options.fieldPages) && fieldPages && pageId)} invite={Boolean(invitePage)}
      snapshot={snapshot} onPick={pickMention} onClose={menus.close} comment={commentMention} docked={options.place === "page"} /> : null}
    {/* A cell's date reminds where its @ can (lineRules); a comment's never does. */}
    {date && typeof document !== "undefined" ? createPortal(<DateMentionEditor target={date} reminders={Boolean(lineRules)}
      onChange={changeDate} onClose={() => setDate(null)} onRemove={removeDate} />, document.body) : null}
    {addPage && fieldPages ? <BlockMoveToDialog blockId={pageId ?? ""} sourcePageId={pageId ?? ""} workspaceId={fieldPages.workspaceId}
      pages={fieldPages.moveContext.pages} session={fieldPages.moveContext.session} anchor={{ ...addPage.trigger.anchor, right: addPage.trigger.anchor.left }}
      title={blockItemText("mention.addPageTitle")} placeholder={blockItemText("mention.addPagePlaceholder")}
      sourcePage="always" doneMessage={null} failedMessage={blockItemText("mention.addPageFailed")}
      onChooseDestination={destination => makePage(destination, addPage.trigger, addPage.title)} onClose={() => setAddPage(null)}
      onPageChange={fieldPages.moveContext.onPageChange} onStatus={fieldPages.moveContext.onStatus} /> : null}
    {inviting && (options.session ?? fieldPages?.moveContext.session) ? <InviteAndMentionPanel session={(options.session ?? fieldPages?.moveContext.session)!}
      pageId={inviting.pageId} invite={inviting.name} anchor={() => popoverRectAt(inviting.anchor)} byLine onBack={() => setInviting(null)} onClose={() => setInviting(null)}
      onInvited={emails => void mentionInvited(inviting.trigger, emails)} /> : null}
  </>;
  return { ...menus, menu, element };
}
