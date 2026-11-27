import {
  workspaceExportBounds,
  workspaceExportConstants,
} from "../workspaceExportWire.generated";

export const NATIVE_ARCHIVE_FORMAT = workspaceExportConstants.archiveFormat;
export const NATIVE_ARCHIVE_FORMAT_VERSION = workspaceExportConstants.archiveFormatVersion;
export const NATIVE_ARCHIVE_ENTITIES_PATH = workspaceExportConstants.entitiesPath;
export const NATIVE_ARCHIVE_FILES_INDEX_PATH = workspaceExportConstants.filesIndexPath;
export const NATIVE_ARCHIVE_FILES_DATA_PATH = workspaceExportConstants.filesDataPath;
export const NATIVE_ARCHIVE_DESCRIPTOR_PATH = workspaceExportConstants.descriptorPath;
export const NATIVE_ARCHIVE_MIME = workspaceExportConstants.archiveMime;

export const NATIVE_ARCHIVE_LIMITS = Object.freeze({
  maxPathBytes: 240,
  maxPathDepth: 3,
  readerWindowBytes: workspaceExportBounds.readerWindowBytes,
  heavyConcurrency: workspaceExportBounds.heavyConcurrency,
});

export interface NativeArchiveStreamDescriptor {
  path: string;
  records?: number;
  bytes: number;
  sha256: string;
}

export interface NativeArchiveV2Descriptor {
  format: typeof NATIVE_ARCHIVE_FORMAT;
  formatVersion: number;
  generatedAt: string;
  scope: { kind: "workspace" | "subtree"; rootIds: string[] };
  source: { workspaceId: string; workspaceName?: string };
  counts: Record<string, number>;
  files: { included: true; count: number; bytes: number; strippedReferences: number };
  streams: {
    entities: NativeArchiveStreamDescriptor;
    fileIndex: NativeArchiveStreamDescriptor;
    fileData: NativeArchiveStreamDescriptor;
  };
}

export interface NativeArchiveV2Entity {
  kind: "page" | "block" | "dataSource" | "view" | "property" | "row" | "ygoDocument" | "databaseContainer" | "blockTrash" | "customEmoji" | "rowPageBlock" | "rowBlockTrash";
  value: Record<string, unknown>;
}

export interface NativeArchiveV2File {
  id: string;
  offset: number;
  name: string;
  contentType: string;
  scope: "icons" | "covers" | "uploads";
  bytes: number;
  sha256: string;
  blob: Blob;
}

export interface ParsedNativeArchiveV2 {
  formatVersion: number;
  source: File;
  descriptor: NativeArchiveV2Descriptor;
  readEntities(visit: (entity: NativeArchiveV2Entity) => void | Promise<void>): Promise<number>;
  readFiles(visit: (file: NativeArchiveV2File) => void | Promise<void>): Promise<number>;
}

export type ParsedNativeArchive = ParsedNativeArchiveV2;

export interface ArchiveWriteSink {
  write(chunk: Uint8Array): Promise<void>;
  close(): Promise<void>;
  abort(reason?: unknown): Promise<void>;
}
