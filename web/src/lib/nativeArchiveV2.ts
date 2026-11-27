import {
  NATIVE_ARCHIVE_DESCRIPTOR_PATH,
  NATIVE_ARCHIVE_ENTITIES_PATH,
  NATIVE_ARCHIVE_FILES_DATA_PATH,
  NATIVE_ARCHIVE_FILES_INDEX_PATH,
  NATIVE_ARCHIVE_FORMAT,
  NATIVE_ARCHIVE_FORMAT_VERSION,
  NATIVE_ARCHIVE_LIMITS,
  type NativeArchiveStreamDescriptor,
  type NativeArchiveV2Descriptor,
  type NativeArchiveV2Entity,
  type NativeArchiveV2File,
  type ParsedNativeArchiveV2,
} from "./nativeArchiveFormat";
import {
  boundedString,
  canonicalJson,
  exactInteger,
  hashBlob,
  isRecord,
  nativeArchiveError,
  parseZipDirectory,
  readEntry,
  type ParsedZipDirectory,
  type ZipDirectoryEntry,
} from "./nativeArchiveZip";

const SHA256_HEX = /^[0-9a-f]{64}$/;
const FILE_ID = /^[A-Za-z0-9_-]{15}$/;
const FILE_MARKER_PREFIX = "leafen-archive://files/";
const ENTITY_KINDS = new Set(["page", "block", "dataSource", "view", "property", "row", "ygoDocument", "databaseContainer", "blockTrash", "customEmoji", "rowPageBlock", "rowBlockTrash", "teamspace", "databaseAutomation"]);
const SUPPORTED_ARCHIVE_VERSIONS = new Set([2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, NATIVE_ARCHIVE_FORMAT_VERSION]);

function validateStream(
  value: unknown,
  label: string,
  path: string,
  entry: ZipDirectoryEntry,
): NativeArchiveStreamDescriptor {
  if (!isRecord(value) || value.path !== path) throw nativeArchiveError(`${label} stream path is invalid.`);
  const bytes = exactInteger(value.bytes, `${label} stream bytes`);
  const records = value.records === undefined ? undefined : exactInteger(value.records, `${label} stream records`);
  const sha256 = boundedString(value.sha256, `${label} stream digest`, 64);
  if (!SHA256_HEX.test(sha256) || bytes !== entry.bytes) throw nativeArchiveError(`${label} stream metadata does not match its ZIP entry.`);
  return { path, bytes, sha256, ...(records === undefined ? {} : { records }) };
}

function validateDescriptor(value: unknown, directory: ParsedZipDirectory): NativeArchiveV2Descriptor {
  if (!isRecord(value) || value.format !== NATIVE_ARCHIVE_FORMAT
    || typeof value.formatVersion !== "number" || !SUPPORTED_ARCHIVE_VERSIONS.has(value.formatVersion)) {
    throw nativeArchiveError("archive descriptor format or version is unsupported.");
  }
  const scope = value.scope;
  if (!isRecord(scope)
    || !(scope.kind === "workspace" || scope.kind === "subtree")
    || !Array.isArray(scope.rootIds)
    || scope.rootIds.some((id) => typeof id !== "string" || id.length < 1 || id.length > 255)
    || (scope.kind === "subtree" && scope.rootIds.length !== 1)) {
    throw nativeArchiveError("archive scope is invalid.");
  }
  if (!isRecord(value.source)) throw nativeArchiveError("archive source is invalid.");
  boundedString(value.source.workspaceId, "archive source workspace id", 255);
  if (value.source.workspaceName !== undefined) boundedString(value.source.workspaceName, "archive source workspace name", 2000);
  if (!isRecord(value.counts)) throw nativeArchiveError("archive counts are invalid.");
  for (const key of [
    "pages", "blocks", "databases", "dataSources", "dbProperties", "dbViews",
    "databaseRows", "ygoDocuments", "dbTemplates", "comments",
  ]) exactInteger(value.counts[key], `archive ${key} count`);
  if (value.formatVersion >= 5) exactInteger(value.counts.databaseContainers, "archive databaseContainers count");
  if (value.formatVersion >= 6) exactInteger(value.counts.blockTrashTombstones, "archive blockTrashTombstones count");
  if (value.formatVersion >= 7) exactInteger(value.counts.customEmojis, "archive customEmojis count");
  if (value.formatVersion >= 8) {
    exactInteger(value.counts.rowPageBlocks, "archive rowPageBlocks count");
    exactInteger(value.counts.rowBlockTrashTombstones, "archive rowBlockTrashTombstones count");
  }
  if (value.formatVersion >= 10) exactInteger(value.counts.teamspaces, "archive teamspaces count");
  if (value.formatVersion >= 11) exactInteger(value.counts.dbAutomations, "archive dbAutomations count");
  if (value.counts.databases !== value.counts.dataSources) throw nativeArchiveError("archive database counts do not match.");
  if (!isRecord(value.files) || value.files.included !== true) throw nativeArchiveError("archive files summary is invalid.");
  exactInteger(value.files.count, "archive file count");
  exactInteger(value.files.bytes, "archive file bytes");
  exactInteger(value.files.strippedReferences, "archive stripped references");
  if (!isRecord(value.streams)) throw nativeArchiveError("archive streams are invalid.");
  const entitiesEntry = directory.byName.get(NATIVE_ARCHIVE_ENTITIES_PATH);
  const fileIndexEntry = directory.byName.get(NATIVE_ARCHIVE_FILES_INDEX_PATH);
  const fileDataEntry = directory.byName.get(NATIVE_ARCHIVE_FILES_DATA_PATH);
  if (!entitiesEntry || !fileIndexEntry || !fileDataEntry) throw nativeArchiveError("archive stream entry is missing.");
  const streams = {
    entities: validateStream(value.streams.entities, "entity", NATIVE_ARCHIVE_ENTITIES_PATH, entitiesEntry),
    fileIndex: validateStream(value.streams.fileIndex, "file-index", NATIVE_ARCHIVE_FILES_INDEX_PATH, fileIndexEntry),
    fileData: validateStream(value.streams.fileData, "file-data", NATIVE_ARCHIVE_FILES_DATA_PATH, fileDataEntry),
  };
  return { ...value, streams } as unknown as NativeArchiveV2Descriptor;
}

async function* readNdjson(blob: Blob, label: string): AsyncGenerator<unknown> {
  const decoder = new TextDecoder("utf-8", { fatal: true });
  let carry = "";
  for (let offset = 0; offset < blob.size; offset += NATIVE_ARCHIVE_LIMITS.readerWindowBytes) {
    const bytes = new Uint8Array(await blob.slice(offset, Math.min(blob.size, offset + NATIVE_ARCHIVE_LIMITS.readerWindowBytes)).arrayBuffer());
    try {
      carry += decoder.decode(bytes, { stream: offset + bytes.byteLength < blob.size });
    } catch {
      throw nativeArchiveError(`${label} stream is not valid UTF-8.`);
    }
    while (true) {
      const newline = carry.indexOf("\n");
      if (newline < 0) break;
      const line = carry.slice(0, newline);
      carry = carry.slice(newline + 1);
      if (!line) throw nativeArchiveError(`${label} stream contains an empty record.`);
      try {
        yield JSON.parse(line);
      } catch {
        throw nativeArchiveError(`${label} stream contains invalid JSON.`);
      }
    }
  }
  if (carry !== "") throw nativeArchiveError(`${label} stream is not newline terminated.`);
}

function validateEntity(value: unknown): NativeArchiveV2Entity {
  if (!isRecord(value) || typeof value.kind !== "string" || !ENTITY_KINDS.has(value.kind) || !isRecord(value.value)) {
    throw nativeArchiveError("entity stream record is invalid.");
  }
  const serialized = JSON.stringify(value.value);
  if (serialized.includes(FILE_MARKER_PREFIX)) {
    const remainder = serialized.replace(/leafen-archive:\/\/files\/[A-Za-z0-9_-]{15}/g, "");
    if (remainder.includes(FILE_MARKER_PREFIX)) throw nativeArchiveError("entity stream contains a malformed file marker.");
  }
  return value as unknown as NativeArchiveV2Entity;
}

function validateFileLine(value: unknown, expectedOffset: number, previousID: string): Omit<NativeArchiveV2File, "blob"> {
  if (!isRecord(value)) throw nativeArchiveError("file-index record is invalid.");
  const id = boundedString(value.id, "file-index id", 32);
  const offset = exactInteger(value.offset, "file-index offset");
  const bytes = exactInteger(value.bytes, "file-index bytes", 1);
  const sha256 = boundedString(value.sha256, "file-index digest", 64);
  const scope = boundedString(value.scope, "file-index scope", 16);
  if (!FILE_ID.test(id) || id <= previousID || offset !== expectedOffset || !SHA256_HEX.test(sha256)
    || !["icons", "covers", "uploads"].includes(scope)) {
    throw nativeArchiveError("file-index ordering, offset, digest, or scope is invalid.");
  }
  return {
    id,
    offset,
    name: boundedString(value.name, "file-index name", 255),
    contentType: boundedString(value.contentType, "file-index content type", 255),
    scope: scope as NativeArchiveV2File["scope"],
    bytes,
    sha256,
  };
}

async function verifyStream(file: File, entry: ZipDirectoryEntry, descriptor: NativeArchiveStreamDescriptor, label: string) {
  const digest = await hashBlob(file.slice(entry.dataStart, entry.dataEnd), entry.bytes);
  if (digest.crc32 !== entry.crc32 || digest.sha256 !== descriptor.sha256) {
    throw nativeArchiveError(`${label} stream digest does not match.`);
  }
}

function entityReader(file: File, entry: ZipDirectoryEntry, descriptor: NativeArchiveV2Descriptor) {
  return async (visit: (entity: NativeArchiveV2Entity) => void | Promise<void>) => {
    const counts = new Map<string, number>();
    let records = 0;
    for await (const raw of readNdjson(file.slice(entry.dataStart, entry.dataEnd), "entity")) {
      const entity = validateEntity(raw);
      counts.set(entity.kind, (counts.get(entity.kind) ?? 0) + 1);
      records++;
      await visit(entity);
    }
    const expected = descriptor.streams.entities.records ?? -1;
    if (records !== expected
      || (counts.get("page") ?? 0) !== descriptor.counts.pages
      || (counts.get("block") ?? 0) !== descriptor.counts.blocks
      || (counts.get("dataSource") ?? 0) !== descriptor.counts.dataSources
      || (counts.get("view") ?? 0) !== descriptor.counts.dbViews
      || (counts.get("property") ?? 0) !== descriptor.counts.dbProperties
      || (counts.get("row") ?? 0) !== descriptor.counts.databaseRows
      || (counts.get("ygoDocument") ?? 0) !== descriptor.counts.ygoDocuments
      || (counts.get("databaseContainer") ?? 0) !== (descriptor.counts.databaseContainers ?? 0)
      || (counts.get("blockTrash") ?? 0) !== (descriptor.counts.blockTrashTombstones ?? 0)
      || (counts.get("customEmoji") ?? 0) !== (descriptor.counts.customEmojis ?? 0)
      || (counts.get("rowPageBlock") ?? 0) !== (descriptor.counts.rowPageBlocks ?? 0)
      || (counts.get("rowBlockTrash") ?? 0) !== (descriptor.counts.rowBlockTrashTombstones ?? 0)
      || (counts.get("teamspace") ?? 0) !== (descriptor.counts.teamspaces ?? 0)
      || (counts.get("databaseAutomation") ?? 0) !== (descriptor.counts.dbAutomations ?? 0)) {
      throw nativeArchiveError("entity stream counts do not match the descriptor.");
    }
    return records;
  };
}

function fileReader(
  file: File,
  indexEntry: ZipDirectoryEntry,
  dataEntry: ZipDirectoryEntry,
  descriptor: NativeArchiveV2Descriptor,
) {
  return async (visit: (entry: NativeArchiveV2File) => void | Promise<void>) => {
    let expectedOffset = 0;
    let previousID = "";
    let records = 0;
    for await (const raw of readNdjson(file.slice(indexEntry.dataStart, indexEntry.dataEnd), "file-index")) {
      const item = validateFileLine(raw, expectedOffset, previousID);
      const blob = file.slice(dataEntry.dataStart + item.offset, dataEntry.dataStart + item.offset + item.bytes, item.contentType);
      const digest = await hashBlob(blob, item.bytes);
      if (digest.sha256 !== item.sha256) throw nativeArchiveError(`file ${item.id} digest does not match.`);
      await visit({ ...item, blob });
      expectedOffset += item.bytes;
      previousID = item.id;
      records++;
    }
    if (records !== (descriptor.streams.fileIndex.records ?? -1)
      || records !== descriptor.files.count
      || expectedOffset !== descriptor.files.bytes
      || expectedOffset !== dataEntry.bytes) {
      throw nativeArchiveError("file stream counts or byte totals do not match the descriptor.");
    }
    return records;
  };
}

export async function parseNativeArchiveV2(
  file: File,
  suppliedDirectory?: ParsedZipDirectory,
): Promise<ParsedNativeArchiveV2> {
  const directory = suppliedDirectory ?? await parseZipDirectory(file);
  const expectedPaths = new Set<string>([
    NATIVE_ARCHIVE_ENTITIES_PATH,
    NATIVE_ARCHIVE_FILES_INDEX_PATH,
    NATIVE_ARCHIVE_FILES_DATA_PATH,
    NATIVE_ARCHIVE_DESCRIPTOR_PATH,
  ]);
  if (directory.entries.length !== expectedPaths.size || directory.entries.some((entry) => !expectedPaths.has(entry.name))) {
    throw nativeArchiveError("archive v2 must contain exactly its four owned stream entries.");
  }
  const descriptorEntry = directory.byName.get(NATIVE_ARCHIVE_DESCRIPTOR_PATH);
  const entitiesEntry = directory.byName.get(NATIVE_ARCHIVE_ENTITIES_PATH);
  const fileIndexEntry = directory.byName.get(NATIVE_ARCHIVE_FILES_INDEX_PATH);
  const fileDataEntry = directory.byName.get(NATIVE_ARCHIVE_FILES_DATA_PATH);
  if (!descriptorEntry || !entitiesEntry || !fileIndexEntry || !fileDataEntry) throw nativeArchiveError("archive v2 entry is missing.");
  const descriptorRead = await readEntry(file, descriptorEntry, NATIVE_ARCHIVE_LIMITS.readerWindowBytes, "archive descriptor");
  const descriptor = validateDescriptor(canonicalJson(descriptorRead.bytes, "archive descriptor"), directory);
  await verifyStream(file, entitiesEntry, descriptor.streams.entities, "entity");
  await verifyStream(file, fileIndexEntry, descriptor.streams.fileIndex, "file-index");
  await verifyStream(file, fileDataEntry, descriptor.streams.fileData, "file-data");
  const readEntities = entityReader(file, entitiesEntry, descriptor);
  const readFiles = fileReader(file, fileIndexEntry, fileDataEntry, descriptor);
  await readEntities(() => undefined);
  await readFiles(() => undefined);
  return { formatVersion: NATIVE_ARCHIVE_FORMAT_VERSION, source: file, descriptor, readEntities, readFiles };
}

/** The source a bundle's descriptor names (`source.workspaceId`), read without the rest of it. */
export async function nativeArchiveSourceId(file: File): Promise<string> {
  const directory = await parseZipDirectory(file);
  const entry = directory.byName.get(NATIVE_ARCHIVE_DESCRIPTOR_PATH);
  if (!entry) throw nativeArchiveError("archive v2 entry is missing.");
  const read = await readEntry(file, entry, NATIVE_ARCHIVE_LIMITS.readerWindowBytes, "archive descriptor");
  const descriptor = canonicalJson(read.bytes, "archive descriptor");
  if (!isRecord(descriptor) || !isRecord(descriptor.source)) throw nativeArchiveError("archive source is invalid.");
  return boundedString(descriptor.source.workspaceId, "archive source workspace id", 255);
}
