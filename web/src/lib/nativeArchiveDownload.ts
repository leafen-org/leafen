import { i18next } from "@/i18n";
import { newId } from "./ids";
import {
  NATIVE_ARCHIVE_MIME,
  type ArchiveWriteSink,
} from "./nativeArchiveFormat";
import { triggerBrowserFileDownload } from "./browserFileDownload";

export async function pipeArchiveResponseToSink(response: Response, sink: ArchiveWriteSink) {
  if (!response.body) throw new Error("Archive response did not include a readable stream.");
  const reader = response.body.getReader();
  try {
    while (true) {
      const next = await reader.read();
      if (next.done) break;
      await sink.write(next.value);
    }
    await sink.close();
  } catch (error) {
    await reader.cancel(error).catch(() => undefined);
    await sink.abort(error).catch(() => undefined);
    throw error;
  }
}

interface FileSystemWritableLike {
  write(data: BufferSource | Blob | string): Promise<void>;
  close(): Promise<void>;
  abort(reason?: unknown): Promise<void>;
}

interface FileSystemFileHandleLike {
  createWritable(): Promise<FileSystemWritableLike>;
  getFile?(): Promise<File>;
}

interface FileSystemDirectoryHandleLike {
  getFileHandle(name: string, options: { create: true }): Promise<FileSystemFileHandleLike>;
  removeEntry(name: string): Promise<void>;
}

function writableSink(writable: FileSystemWritableLike): ArchiveWriteSink {
  return {
    write: (chunk) => writable.write(Uint8Array.from(chunk).buffer),
    close: () => writable.close(),
    abort: (reason) => writable.abort(reason),
  };
}

export interface StreamedResponseFileType {
  description: string;
  mimeType: string;
  extensions: string[];
}

/**
 * Saves a download's stream, answering once it is saved or the save window
 * was closed without saving ("cancelled"): a caller says it downloaded only
 * on "saved". A save that fails throws.
 */
export async function saveStreamedResponse(
  response: Response,
  suggestedName: string,
  fileType: StreamedResponseFileType,
): Promise<"saved" | "cancelled"> {
  const storageWithDirectory = navigator.storage as StorageManager & {
    getDirectory?: () => Promise<FileSystemDirectoryHandleLike>;
  };
  if (storageWithDirectory?.getDirectory) {
    const root = await storageWithDirectory.getDirectory();
    const tempName = `.leafen-export-${newId()}.tmp`;
    const handle = await root.getFileHandle(tempName, { create: true });
    try {
      await pipeArchiveResponseToSink(response, writableSink(await handle.createWritable()));
      const file = await handle.getFile?.();
      if (!file) throw new Error("The browser could not reopen the completed archive.");
      const saved = await triggerBrowserFileDownload(file, suggestedName, {
        revokeAfterMs: 60_000,
        cleanup: () => root.removeEntry(tempName),
      });
      if (saved === "failed") throw new Error("The file couldn't be saved on this device.");
      return saved;
    } catch (error) {
      await root.removeEntry(tempName).catch(() => undefined);
      throw error;
    }
  }
  const globalWithPicker = globalThis as typeof globalThis & {
    showSaveFilePicker?: (options: Record<string, unknown>) => Promise<FileSystemFileHandleLike>;
  };
  if (globalWithPicker.showSaveFilePicker) {
    let writable: FileSystemWritableLike;
    try {
      const handle = await globalWithPicker.showSaveFilePicker({
        suggestedName,
        types: [{
          description: fileType.description,
          accept: { [fileType.mimeType]: fileType.extensions },
        }],
      });
      writable = await handle.createWritable();
    } catch (error) {
      await response.body?.cancel(error).catch(() => undefined);
      throw error;
    }
    await pipeArchiveResponseToSink(response, writableSink(writable));
    return "saved";
  }
  await response.body?.cancel().catch(() => undefined);
  throw new Error("This browser cannot save a streamed download. Use a current Chrome, Edge, or Safari release.");
}

export async function saveNativeArchiveResponse(response: Response, suggestedName: string) {
  return saveStreamedResponse(response, suggestedName, {
    description: i18next.t("common:fileTypes.leafenArchive"),
    mimeType: NATIVE_ARCHIVE_MIME,
    extensions: [".leafen.zip"],
  });
}
