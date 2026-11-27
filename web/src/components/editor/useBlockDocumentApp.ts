import { useTranslation } from "react-i18next";
import type { GraphBlock } from "@/contentGraph";
import { appText } from "@/components/admin/administratorAppsApi";
import { fileOpenersFor, type AppOpenedFile } from "@/components/apps/AppFileOpener";
import { openAppFile } from "@/components/apps/appFileOpening";
import { useUsableAppList } from "@/components/apps/useUsableApps";
import { useProductPages } from "@/components/product/productPagesContext";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";

/** A file block's file as an app opens it: editing while the person may change the page. */
export function blockAppFile(adapter: NativeEditorGraphAdapter, block: GraphBlock): AppOpenedFile {
  return {
    workspaceId: adapter.workspaceId, pageId: adapter.pageId, blockId: block.id, fileId: String(block.content.fileId ?? ""),
    name: String(block.content.name ?? ""), contentType: String(block.content.contentType ?? ""), size: Number(block.content.size ?? 0),
    canEdit: !adapter.readOnly,
    version: block.content.sha256 ? `sha256:${String(block.content.sha256)}` : `file:${String(block.content.fileId ?? "")}`,
  };
}

/**
 * The document app for a file block's format, when the person has one, and
 * opening the file with it: one app per format opens it, to edit or, without
 * edit access or on a phone, to read (document apps decision of 2026-10-10,
 * 11 — 2026-10-16: a reader, or anyone at a phone's width, had no block menu
 * to open an HWP with, and its name opened a blank tab).
 */
export function useBlockDocumentApp(adapter: NativeEditorGraphAdapter, block: GraphBlock): { name: string; open: () => void } | undefined {
  const fileId = typeof block.content.fileId === "string" ? block.content.fileId : "";
  const usable = useUsableAppList(adapter.session, Boolean(fileId));
  const organizationId = useProductPages()?.organizationId;
  const { i18n } = useTranslation();
  const session = adapter.session;
  if (!fileId || !session) return undefined;
  const found = fileOpenersFor(usable.apps, String(block.content.name ?? ""), String(block.content.contentType ?? ""), organizationId)[0];
  if (!found) return undefined;
  return {
    name: appText(found.opener.name, i18n.language) || appText(found.app.name, i18n.language),
    open: () => openAppFile({ session, ...found, file: blockAppFile(adapter, block) }),
  };
}
