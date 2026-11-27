import { useEffect, useRef, useState, type ComponentPropsWithoutRef } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import i18next from "i18next";
import { MissingEmojiIcon } from "@/icons/leafen";
import { customEmojiNeverHad } from "@/lib/closedMentions";
import { customEmojiShown, useWorkspaceCustomEmoji } from "@/lib/customEmojiIcon";
import { useInnerHtml } from "@/lib/innerHtml";
import { filePreviewWidth, useWorkspaceFileUrl } from "@/lib/fileUrls";
import { publicEmojiPictureUrl, usePublicFileUrl } from "@/lib/publicFileUrls";
import { nativeWorkspaceFileLocator } from "@/lib/workspaceFileTransport";
import styles from "./InlineCustomEmoji.module.css";

/**
 * A workspace's emoji in a line of text, as Notion's: its picture at the
 * text's size, its ":name:" while the picture loads. Pointed at, it shows
 * larger with its name as it is now (renamed, the new one); deleted, or never
 * the workspace's, a dashed question mark whose tip says so with its last
 * name (Notion's, seen 2026-11-07 and 2026-11-10). A published page's visitor
 * sees the picture by the token its text carries.
 */
export function InlineCustomEmoji({ workspaceId, emojiId, name, publicToken }: {
  workspaceId?: string; emojiId: string; name: string; publicToken?: string;
}) {
  const publicFileUrl = usePublicFileUrl();
  const publicUrl = publicToken && publicFileUrl?.emoji ? publicFileUrl.emoji(publicToken) : "";
  // One the workspace never had is known without asking, by any reader.
  const neverHad = !publicUrl && customEmojiNeverHad(emojiId, name);
  const shown = useWorkspaceCustomEmoji(workspaceId, publicUrl || neverHad ? undefined : emojiId);
  const fileUrl = useWorkspaceFileUrl(shown.fileId ? nativeWorkspaceFileLocator(shown.fileId) : "", ["data:image/"], { width: filePreviewWidth(32) });
  const url = publicUrl || fileUrl;
  const label = shown.name ? `:${shown.name}:` : name;
  // A picture that doesn't load (a visitor's token ran out) shows its name.
  const [failed, setFailed] = useState("");
  if (shown.gone || neverHad) return <MissingCustomEmoji emojiId={emojiId} name={label} />;
  return url && failed !== url
    // Pointing at it shows it larger with its name (iconTips).
    ? <img className={styles.emoji} src={url} alt={label} data-icon-tip={label} data-tip-image={url} draggable={false} data-inline-custom-emoji={emojiId}
      onError={() => setFailed(url)} />
    : <span data-inline-custom-emoji={emojiId}>{label}</span>;
}

const deletedNote = () => i18next.t("emojiPicker:custom.deleted");

// The words of an emoji the workspace never had, kept by its ID from text an
// app wrote (notion_missing_custom_emoji.go): no name to show, so its tip
// says only "(삭제됨)", as Notion's (seen 2026-11-10).
const UNNAMED = ":?:";
const shownName = (name: string) => (name === UNNAMED ? "" : name);
const goneLabel = (name: string) => (shownName(name) ? `${name} ${deletedNote()}` : deletedNote());

function MissingCustomEmoji({ emojiId, name }: { emojiId: string; name: string }) {
  return (
    <span className={styles.missing} role="img" aria-label={goneLabel(name)} data-icon-tip={shownName(name)} data-tip-note={deletedNote()}
      data-inline-custom-emoji={emojiId} data-custom-emoji-gone="">
      <MissingEmojiIcon pixels={20} aria-hidden="true" />
    </span>
  );
}

const EMOJI_SELECTOR = '[data-mention="custom_emoji"][data-custom-emoji-id]:not([data-custom-emoji-shown])';

/**
 * Shows the pictures of the workspace emoji in text drawn as HTML outside
 * React (a cell editor's, a title's field), each in place of its ":name:"
 * once it is read; a published page's visitor sees one by the token its
 * text carries, as InlineCustomEmoji shows it.
 */
export function showCustomEmojiPictures(root: HTMLElement) {
  for (const element of root.querySelectorAll<HTMLElement>(EMOJI_SELECTOR)) {
    element.dataset.customEmojiShown = "";
    const emojiId = element.dataset.customEmojiId ?? "";
    const name = element.textContent ?? "";
    const publicUrl = publicEmojiPictureUrl(element.dataset.customEmojiPublic ?? "");
    const read = publicUrl ? Promise.resolve({ url: publicUrl, name: "", gone: false })
      : customEmojiNeverHad(emojiId, name) ? Promise.resolve({ url: "", name: "", gone: true }) : customEmojiShown(emojiId);
    void read.then(({ url, name: now, gone }) => {
      if (!element.isConnected) return;
      const label = now ? `:${now}:` : name;
      if (gone) {
        const missing = document.createElement("span");
        missing.className = styles.missing;
        missing.setAttribute("role", "img");
        missing.setAttribute("aria-label", goneLabel(label));
        missing.dataset.iconTip = shownName(label);
        missing.dataset.tipNote = deletedNote();
        missing.dataset.inlineCustomEmoji = emojiId;
        missing.dataset.customEmojiGone = "";
        missing.innerHTML = renderToStaticMarkup(<MissingEmojiIcon pixels={20} aria-hidden="true" />);
        element.replaceChildren(missing);
        return;
      }
      if (!url) return;
      const image = document.createElement("img");
      image.className = styles.emoji;
      image.src = url;
      image.alt = label;
      image.dataset.iconTip = label;
      image.dataset.tipImage = url;
      image.draggable = false;
      image.dataset.inlineCustomEmoji = emojiId;
      element.replaceChildren(image);
    });
  }
}

/**
 * A span of markup, set again only when it changes (useInnerHtml), whose
 * workspace emoji show their pictures — those of a mentioned page's title
 * too (richtext.ts).
 */
export function HtmlSpanWithCustomEmoji({ html, ...props }: Omit<ComponentPropsWithoutRef<"span">, "children" | "dangerouslySetInnerHTML"> & { html: string }) {
  const ref = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    if (ref.current && html.includes("data-custom-emoji-id")) showCustomEmojiPictures(ref.current);
  }, [html]);
  return <span ref={ref} {...props} dangerouslySetInnerHTML={useInnerHtml(html)} />;
}
