import i18next from "i18next";
import { BLOCK_TEXT_ROOT_PREFIX, replaceBlockTextRange } from "@/lib/blockTextEditing";
import type { GraphBlock } from "@/contentGraph";
import { copyText } from "@/lib/copyText";
import { rememberCodeBlockLanguage } from "@/lib/blockDefaults";
import { blockItemText } from "./blockItemLabels";
import { codeFormattable, formatCode } from "./codeFormat";
import { codeLanguageOptions, PLAIN_TEXT_CODE_LANGUAGE } from "./codeLanguages";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";

type Notify = (message: string, tone?: "default" | "success" | "error") => void;

/**
 * A code block's own rows in its menu, as Notion's (seen 2026-10-11): copy
 * its code, wrap its lines, and its language, as its own toolbar sets them;
 * then, for the languages Notion formats, "코드 포맷팅" (seen 2026-10-13),
 * one undo step.
 */
export function codeMenu(adapter: NativeEditorGraphAdapter, block: GraphBlock, onDone: () => void, notify: Notify) {
  const stored = typeof block.content.language === "string" ? block.content.language.trim() : "";
  const wrap = block.content.wrap === true;
  const update = (change: Record<string, unknown>) => {
    const current = adapter.graph.blocks.find((candidate) => candidate.id === block.id);
    if (current && !adapter.readOnly) adapter.updateBlock(current, change);
  };
  return {
    onCopy: () => {
      onDone();
      const text = adapter.collaborationDocument?.getText(BLOCK_TEXT_ROOT_PREFIX + block.id).toString() ?? "";
      void copyText(text).then((copied) => notify(String(i18next.t(copied ? "blockHandle:notifications.copiedToClipboard" : "blockHandle:notifications.couldNotCopy")),
        copied ? "success" : "error"));
    },
    wrap,
    onToggleWrap: () => { update({ wrap: !wrap }); onDone(); },
    onFormat: codeFormattable(stored) && !adapter.readOnly ? () => {
      onDone();
      const document = adapter.collaborationDocument;
      if (!document) return;
      const text = document.getText(BLOCK_TEXT_ROOT_PREFIX + block.id);
      const before = text.toString();
      void formatCode(stored, before).then((formatted) => {
        // A peer's change in the meantime keeps its own text.
        if (formatted !== before && text.toString() === before) replaceBlockTextRange(document, block.id, 0, before.length, formatted);
      }).catch(() => notify(String(i18next.t("blockHandle:notifications.couldNotFormatCode")), "error"));
    } : undefined,
    language: {
      formats: codeLanguageOptions(blockItemText, stored).map(({ value, label }) => [value, label] as const),
      active: stored || PLAIN_TEXT_CODE_LANGUAGE,
      onSelect: (language: string) => { update({ language }); rememberCodeBlockLanguage(language); onDone(); },
    },
  };
}
