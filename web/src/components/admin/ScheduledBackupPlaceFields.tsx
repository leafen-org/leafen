import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type {
  AdministratorScheduledBackup as Schedule,
  AdministratorScheduledBackupInput as ScheduleInput,
} from "../../administratorWire.generated";
import { BACKUP_PLACE_FIELDS, BACKUP_PLACE_PATH_STYLE, backupPlacePlaceholder, type BackupPlaceFieldName } from "@/lib/backupPlaceFields";
import { SettingField, SettingSwitch } from "./AdministratorSettingRows";

// The attribute each field is found by (tests and the settings search alike).
const ATTRIBUTE: Record<BackupPlaceFieldName, string> = {
  webdavUrl: "data-scheduled-backup-webdav-url", webdavUsername: "data-scheduled-backup-webdav-username",
  webdavPassword: "data-scheduled-backup-webdav-password", s3Endpoint: "data-scheduled-backup-s3-endpoint",
  s3Region: "data-scheduled-backup-s3-region", s3Bucket: "data-scheduled-backup-s3-bucket", s3Prefix: "data-scheduled-backup-s3-prefix",
  s3AccessKey: "data-scheduled-backup-s3-access-key", s3SecretKey: "data-scheduled-backup-s3-secret",
};

// The refusals each field is the cause of, marked on it with the status line;
// one for what is missing marks only the fields left empty, so the focus a
// refused save hands on (useRefusedFieldFocus) goes to the one to fill.
const REFUSALS: Partial<Record<BackupPlaceFieldName, string[]>> = {
  webdavUrl: ["schedule.webdavInvalid"],
  webdavUsername: ["schedule.webdavUnauthorized"], webdavPassword: ["schedule.webdavUnauthorized"],
  s3Endpoint: ["schedule.s3Invalid"],
  s3AccessKey: ["schedule.s3Unauthorized"], s3SecretKey: ["schedule.s3Unauthorized"],
};
const MISSING: Partial<Record<BackupPlaceFieldName, string>> = {
  webdavUsername: "schedule.webdavCredentialsMissing", webdavPassword: "schedule.webdavCredentialsMissing",
  s3AccessKey: "schedule.s3KeysMissing", s3SecretKey: "schedule.s3KeysMissing",
};

/**
 * A WebDAV server's or S3-compatible storage's fields in Settings › Backups,
 * in the order and words of the one table the first screen's restore reads
 * too (lib/backupPlaceFields.ts): the place's help under the first, the
 * plain-http warning under the address, a kept secret's placeholder saying
 * so, and the path-style switch after the storage's.
 */
export function ScheduledBackupPlaceFields({ target, draft, schedule, disabled, help, insecureNote, refused, onChange }: {
  target: "webdav" | "s3";
  draft: ScheduleInput;
  schedule: Schedule;
  disabled: boolean;
  help: ReactNode;
  insecureNote: ReactNode;
  refused: (...lines: string[]) => Record<string, unknown>;
  onChange: (next: Partial<ScheduleInput>) => void;
}) {
  const { t } = useTranslation("serverConsole");
  const kept = (name: BackupPlaceFieldName) =>
    (name === "webdavPassword" && schedule.webdavPasswordSet) || (name === "s3SecretKey" && schedule.s3SecretSet);
  const causes = (name: BackupPlaceFieldName) => {
    const missing = MISSING[name];
    const empty = !draft[name].trim() && !kept(name);
    return [...(REFUSALS[name] ?? []), ...(missing && empty ? [missing] : [])];
  };
  return (
    <>
      {BACKUP_PLACE_FIELDS[target].map((field, index) => (
        <SettingField key={field.name} title={t(field.label)} description={index === 0 ? help : undefined}>
          <input type={field.type === "text" ? undefined : field.type} value={draft[field.name]} disabled={disabled}
            placeholder={kept(field.name) ? t("schedule.passwordKept") : backupPlacePlaceholder(field, t) ?? ""}
            maxLength={field.maxLength} spellCheck={field.type === "password" ? undefined : false}
            autoComplete={field.type === "password" ? "new-password" : field.type === "url" ? "url" : "off"}
            onChange={(event) => onChange({ [field.name]: event.target.value })}
            {...{ [ATTRIBUTE[field.name]]: "" }} {...refused(...causes(field.name).map((line) => t(line)))} />
          {field.type === "url" ? insecureNote : null}
        </SettingField>
      ))}
      {target === "s3" ? (
        <SettingSwitch title={t(BACKUP_PLACE_PATH_STYLE)} on={draft.s3PathStyle} disabled={disabled}
          onChange={(s3PathStyle) => onChange({ s3PathStyle })} control={{ "data-scheduled-backup-s3-path-style": "" }} />
      ) : null}
    </>
  );
}
