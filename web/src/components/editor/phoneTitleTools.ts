// A page title's part in a phone's editing tools (NativePhoneToolbar): the
// title's field, where its caret stands, and what "@" and a comment do there.
// The title's head (PageHeader, a row page's RichTextCellEditor) gives it;
// the tools of the body beside it in the same page take it.
import type { Editor } from "@tiptap/core";

export type PhoneTitleTools = {
  /** The title's field. */
  editor: Editor;
  /** Writes "@" at the caret, opening the title's "@" menu there. */
  mention(): void;
  /** Starts a new comment on the page (a sheet on a phone), where the page takes one now. */
  comment(): (() => void) | undefined;
  /** The title's chosen words, which its own text model marks (PhoneSelectionTools). */
  words?: PhoneTitleWords;
  /**
   * A database's name in the page, whose tools are its "@" and the
   * keyboard's alone, kept while the keyboard put away leaves its caret
   * there (richCellCaretKept.ts), as Notion's (seen 2026-11-16 in iPad Safari
   * at 834, twice, and on a 402pt iPhone).
   */
  name?: boolean;
};

export type PhoneTitleMark = "bold" | "italic" | "underline" | "strikethrough" | "code";

/** What the title's chosen words hold, and the changes to them its tools make. */
export type PhoneTitleWords = {
  /** Their marks, their text color and background ("mixed" where they differ) and link; null with none chosen. */
  format(): { marks: Record<PhoneTitleMark, boolean>; color: string; background: string; link: string } | null;
  toggle(mark: PhoneTitleMark): void;
  /** A text color or a background, the other kind left as it was. */
  color(token: string): void;
  /** Links them to `href`; "" takes their link off. */
  link(href: string): void;
};

/**
 * A page title's chosen words, which draw in the title's own look: its
 * tools' marks and colors change nothing there, as Notion's (seen 2026-11-14
 * in iPhone Safari: B left the title as it was, a color drew nothing); its
 * "링크" links them, as the title's typed addresses are, Notion's offering it
 * there too.
 */
export function plainTitleWords(editor: Editor): PhoneTitleWords {
  return {
    format: () => ({ marks: { bold: false, italic: false, underline: false, strikethrough: false, code: false },
      color: "default", background: "default_background", link: editor.getAttributes("link").href ?? "" }),
    toggle: () => {}, color: () => {},
    link: href => { if (href) editor.chain().focus().setMark("link", { href }).run(); else editor.chain().focus().unsetMark("link").run(); },
  };
}

const titles = new Map<HTMLElement, PhoneTitleTools>();

/** The title shown in `element` gives the tools these. */
export function registerPhoneTitleTools(element: HTMLElement, tools: PhoneTitleTools) {
  titles.set(element, tools);
  return () => { if (titles.get(element) === tools) titles.delete(element); };
}

/** Whether a database name's caret stays in it with the keyboard put away (richCellCaretKept.ts). */
export function phoneTitleCaretKept(tools: PhoneTitleTools) {
  return Boolean(tools.name) && tools.editor.view.dom.getAttribute("data-caret-kept") === "true";
}

/**
 * The tools of the title in the same page as `body` (an editor's own
 * element), if one shows: a database name's while its caret is in it, else
 * the page's own title's.
 */
export function phoneTitleToolsBeside(body: Element): PhoneTitleTools | null {
  const page = body.closest("[data-page-search-root], [data-row-peek-search-root]");
  if (!page) return null;
  let own: PhoneTitleTools | null = null;
  for (const [element, tools] of titles) {
    if (!element.isConnected || !page.contains(element) || tools.editor.isDestroyed) continue;
    if (tools.name ? tools.editor.isFocused || phoneTitleCaretKept(tools) : !own) {
      if (tools.name) return tools;
      own = tools;
    }
  }
  return own;
}
