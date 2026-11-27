import { importAssetContentType, type ImportAssetUploader } from "./importReferences";
import { isServerUnreachable } from "./serverReach";
import { uploadWorkspaceFile } from "./storage";

/** Uploads an asset of an imported document as a workspace file. */
export function workspaceImportAssetUploader(workspaceId: string): ImportAssetUploader {
  return async (name, bytes) => {
    const type = importAssetContentType(name);
    try {
      const uploaded = await uploadWorkspaceFile(new File([bytes as BlobPart], name, { type }), "imports", { workspaceId });
      return { kind: type.startsWith("image/") ? "image" : "file", fileId: uploaded.fileId, name: uploaded.name || name };
    } catch (error) {
      // A file the server turns away, such as active web content, is left
      // out; one it didn't answer for waits with the import.
      if (isServerUnreachable(error)) throw error;
      return null;
    }
  };
}
