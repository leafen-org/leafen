// The limit as the field shows it, in MiB, with as many places as bring the
// same bytes back when saved again: two places turned 0.005 into "0" (no
// limit, if saved) and 0.00576 into "0.01" (seen 2026-11-06).
export function formatStorageLimitDraft(bytes: number | null | undefined) {
  if (!bytes || !Number.isFinite(bytes)) return "";
  const mb = bytes / (1024 * 1024);
  if (Number.isInteger(mb)) return String(mb);
  for (let places = 2; places < 7; places += 1) {
    const draft = mb.toFixed(places).replace(/\.?0+$/u, "");
    if (Math.floor(Number(draft) * 1024 * 1024) === bytes) return draft;
  }
  return String(mb);
}

export function parseStorageLimitDraft(value: string, storageLimitInvalid: string) {
  const trimmed = value.trim();
  if (!trimmed) return null;
  const mb = Number(trimmed);
  if (!Number.isFinite(mb) || mb < 0) throw new Error(storageLimitInvalid);
  const bytes = Math.floor(mb * 1024 * 1024);
  return bytes > 0 ? bytes : null;
}
