export const LEAFEN_ARCHIVE_EXT = ".leafen.zip";

export function safeLeafenFileStem(name: string) {
  return (
    name
      .replace(/[\\/:*?"<>|#\u0000-\u001f]/g, "")
      .replace(/\s+/g, " ")
      .trim()
      .slice(0, 80) || "leafen"
  );
}

function localDateStamp(now: Date) {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

export function leafenArchiveFileName(stem: string, now = new Date()) {
  return `${safeLeafenFileStem(stem)}-${localDateStamp(now)}${LEAFEN_ARCHIVE_EXT}`;
}
