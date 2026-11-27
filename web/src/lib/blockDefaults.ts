import type { Block, BlockContent, BlockType } from "./types";
import { blockSpec } from "./blockSemantics";
import { personValue, storePersonValue } from "./browserStorage";
import { PLAIN_TEXT_CODE_LANGUAGE } from "@/codeLanguages.generated";

// A new code block — "/코드", "```", a line turned into code — takes the
// language its person last chose in a code block's picker on this browser,
// Plain Text until they choose one, as Notion's (seen 2026-11-07: a new block
// began as Plain Text; after Python was chosen in one, the next "/코드" and
// "```" began as Python; Notion keeps it per person on the browser).
const CODE_BLOCK_LANGUAGE = "code-block-language";

export function newCodeBlockLanguage(): string {
  return personValue(CODE_BLOCK_LANGUAGE)?.trim() || PLAIN_TEXT_CODE_LANGUAGE;
}

export function rememberCodeBlockLanguage(language: string): void {
  if (language.trim()) storePersonValue(CODE_BLOCK_LANGUAGE, language.trim());
}

// A callout made in the editor by a person who has given none another look
// is gray with the light bulb, as Notion's "/콜아웃" and "콜아웃 · 전환" make
// it with nothing kept on the browser (measured 2026-11-16 in Chrome at 1440,
// dark, in two tabs with Notion's kept callout look cleared: a gray box,
// rgb(56, 56, 54), and 💡). One with no color is Notion's "default", a box
// drawn with a line. The none seen on 2026-11-07 and the line box seen on
// 2026-11-16 were what those browsers kept, as below.
export const NEW_CALLOUT_COLOR = "gray_background";
export const NEW_CALLOUT_ICON = { icon: "💡", iconType: "emoji" } as const;

// Until its person gives a callout another look: a callout made on the
// screen takes the icon and the color they last gave one on this browser, as
// Notion's (measured 2026-11-15 in Chrome: after a callout's icon was picked,
// the next "/콜아웃" had it, on that page and on another after a reload;
// after it was taken away, the next had none; after a callout was made blue,
// the next was blue — Notion keeps the two per person on the browser, under
// its local preferences calloutBlockBackground and calloutBlockIcon, read
// 2026-11-16).
const CALLOUT_ICON = "callout-block-icon";
const CALLOUT_COLOR = "callout-block-color";
const CALLOUT_ICON_KEYS = ["icon", "iconType", "customEmojiId"] as const;

/** A new callout's color and icon: its person's last, else gray with the light bulb. */
export function newCalloutLook(): Record<string, unknown> {
  let icon: Record<string, unknown> = { ...NEW_CALLOUT_ICON };
  try {
    const kept = JSON.parse(personValue(CALLOUT_ICON) ?? "null") as Record<string, unknown> | null;
    if (kept && typeof kept.icon === "string" && typeof kept.iconType === "string") icon = kept;
  } catch { /* a value that can't be read is none kept */ }
  return { color: personValue(CALLOUT_COLOR) || NEW_CALLOUT_COLOR, ...icon };
}

/** Keeps what a change to a callout's color or icon gives it, for the next one made. */
export function rememberCalloutLook(change: Record<string, unknown>): void {
  if ("color" in change) storePersonValue(CALLOUT_COLOR, typeof change.color === "string" ? change.color : "default");
  if (typeof change.iconType !== "string") return;
  const icon = Object.fromEntries(CALLOUT_ICON_KEYS.filter((key) => typeof change[key] === "string" && change[key]).map((key) => [key, change[key]]));
  storePersonValue(CALLOUT_ICON, JSON.stringify({ icon: "", ...icon }));
}

export function contentForBlockTypeChange(
  textContent: BlockContent,
  previousBlock: Pick<Block, "type" | "content"> | undefined,
  nextType: BlockType
): BlockContent {
  const semantics = blockSpec(nextType);
  if (semantics.typeChangeContent === "clear") return { rich: [] };
  if (semantics.newContent !== "javascript_code") return textContent;
  if (previousBlock?.type !== "code") {
    return { ...textContent, language: newCodeBlockLanguage() };
  }
  if (!previousBlock.content) return textContent;
  return {
    ...previousBlock.content,
    rich: textContent.rich,
  };
}
