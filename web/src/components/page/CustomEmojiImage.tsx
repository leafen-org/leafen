import type { WorkspaceCustomEmoji } from "@/lib/customEmoji";
import { filePreviewWidth, useWorkspaceFileUrl } from "@/lib/fileUrls";
import { nativeWorkspaceFileLocator } from "@/lib/workspaceFileTransport";

/** A workspace emoji's picture, from its workspace file. */
export function CustomEmojiImage({ emoji }: { emoji: WorkspaceCustomEmoji }) {
  const url = useWorkspaceFileUrl(nativeWorkspaceFileLocator(emoji.fileId), ["data:image/"], { width: filePreviewWidth(32) });
  return url ? <img src={url} alt="" loading="lazy" /> : <span aria-hidden="true" />;
}
