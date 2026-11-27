import { administratorBounds } from "@/administratorWire.generated";

// A backup place off this machine and the fields it takes, in one table:
// Settings › Backups, the first screen's restore and the keys typed beside
// an agent's restore name them alike and ask for them in the same order
// (2026-11-22: the restore asked for "리전" and "시크릿 키" after the
// bucket where Settings asks for "지역" before it and "비밀 키", and a person
// copying their storage from one screen to the other was lost). The words
// are Settings' own (serverConsole:schedule.*); the agent tools' field
// descriptions say the same (setup_restore_agent_mcp.go).

export type BackupPlaceFieldName =
  | "webdavUrl" | "webdavUsername" | "webdavPassword"
  | "s3Endpoint" | "s3Region" | "s3Bucket" | "s3Prefix" | "s3AccessKey" | "s3SecretKey";

export type BackupPlaceField = {
  name: BackupPlaceFieldName;
  /** Its name, a translation key. */
  label: string;
  type: "url" | "text" | "password";
  /** What it shows while empty: a translation key, or text as it is. */
  placeholder?: { key: string } | { text: string };
  maxLength: number;
};

/** A WebDAV server's and S3-compatible storage's names among the places. */
export const BACKUP_PLACE_TARGET_LABELS = {
  webdav: "serverConsole:schedule.targetWebdav",
  s3: "serverConsole:schedule.targetS3",
} as const;

/** The fields of a WebDAV server and of S3-compatible storage, in their order. */
export const BACKUP_PLACE_FIELDS: Record<"webdav" | "s3", readonly BackupPlaceField[]> = {
  webdav: [
    { name: "webdavUrl", label: "serverConsole:schedule.webdavUrl", type: "url",
      placeholder: { key: "serverConsole:schedule.webdavUrlPlaceholder" }, maxLength: administratorBounds.scheduledBackupWebDAVURLLength },
    { name: "webdavUsername", label: "serverConsole:schedule.webdavUsername", type: "text",
      maxLength: administratorBounds.scheduledBackupWebDAVUsernameLength },
    { name: "webdavPassword", label: "serverConsole:schedule.webdavPassword", type: "password",
      placeholder: { key: "serverConsole:schedule.webdavPasswordNone" }, maxLength: administratorBounds.scheduledBackupPasswordMaximum },
  ],
  s3: [
    { name: "s3Endpoint", label: "serverConsole:schedule.s3Endpoint", type: "url",
      placeholder: { key: "serverConsole:schedule.s3EndpointPlaceholder" }, maxLength: administratorBounds.scheduledBackupWebDAVURLLength },
    { name: "s3Region", label: "serverConsole:schedule.s3Region", type: "text",
      placeholder: { text: "us-east-1" }, maxLength: administratorBounds.scheduledBackupS3FieldLength },
    { name: "s3Bucket", label: "serverConsole:schedule.s3Bucket", type: "text", maxLength: administratorBounds.scheduledBackupS3FieldLength },
    { name: "s3Prefix", label: "serverConsole:schedule.s3Prefix", type: "text",
      placeholder: { key: "serverConsole:schedule.s3PrefixPlaceholder" }, maxLength: administratorBounds.scheduledBackupS3FieldLength },
    { name: "s3AccessKey", label: "serverConsole:schedule.s3AccessKey", type: "text", maxLength: administratorBounds.scheduledBackupS3FieldLength },
    { name: "s3SecretKey", label: "serverConsole:schedule.s3SecretKey", type: "password", maxLength: administratorBounds.scheduledBackupPasswordMaximum },
  ],
};

/** The S3 path-style switch, after the storage's fields. */
export const BACKUP_PLACE_PATH_STYLE = "serverConsole:schedule.s3PathStyle";

/** One field's name, as a translation key. */
export function backupPlaceFieldLabel(name: BackupPlaceFieldName): string {
  return [...BACKUP_PLACE_FIELDS.webdav, ...BACKUP_PLACE_FIELDS.s3].find((field) => field.name === name)?.label ?? name;
}

/** What a field shows while empty, in the words `t` gives. */
export function backupPlacePlaceholder(field: BackupPlaceField, t: (key: string) => unknown): string | undefined {
  if (!field.placeholder) return undefined;
  return "key" in field.placeholder ? String(t(field.placeholder.key)) : field.placeholder.text;
}
