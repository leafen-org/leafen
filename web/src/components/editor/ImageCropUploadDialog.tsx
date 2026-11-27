import { useEffect, useRef, useState } from "react";
import type { GraphBlock } from "@/contentGraph";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { ImageCropDialog } from "./ImageCropDialog";
import { cropImageFile } from "./cropImageFile";
import { useMediaBlockUpload } from "./mediaBlockUpload";
import { blockItemText } from "./blockItemLabels";
import { blockUploadProgressLabel } from "./fileDrop";
import type { ImageCrop } from "./imageCropModel";

export function ImageCropUploadDialog({ adapter, block, src, resolveUrl, onClose }: {
  adapter: NativeEditorGraphAdapter; block: GraphBlock; src: string; resolveUrl(): Promise<string>; onClose(): void;
}) {
  const upload = useMediaBlockUpload(adapter, block, "image");
  const latest = useRef({ adapter, block }); latest.current = { adapter, block };
  const controller = useRef<AbortController | null>(null);
  const [preparing, setPreparing] = useState(false);
  useEffect(() => () => controller.current?.abort(), []);
  async function save(crop: ImageCrop) {
    if (preparing || upload.progress) return;
    const task = new AbortController(); controller.current = task; setPreparing(true); upload.setError("");
    const source = String(block.content.fileId ?? block.content.url ?? "");
    const context = latest.current.adapter;
    try {
      const file = await cropImageFile(await resolveUrl(), crop, String(block.content.name ?? "image.png"), task.signal);
      const current = latest.current.adapter.graph.blocks.find(candidate => candidate.id === block.id);
      const editor = latest.current.adapter;
      if (!task.signal.aborted && !editor.readOnly && editor.pageId === context.pageId && editor.workspaceId === context.workspaceId
        && editor.session?.token === context.session?.token && current?.type === "image"
        && String(current.content.fileId ?? current.content.url ?? "") === source)
        upload.upload(file);
    } catch { if (!task.signal.aborted) upload.setError(blockItemText("image.cropFailed")); }
    finally { if (!task.signal.aborted) setPreparing(false); }
  }
  return <ImageCropDialog src={src} initial={null} onClose={onClose} onSave={crop => { void save(crop); }}
    busy={preparing || Boolean(upload.progress)} error={upload.error}
    status={upload.progress ? blockUploadProgressLabel(upload.progress) : preparing ? blockItemText("common.uploading") : ""} />;
}
