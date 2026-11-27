// What changed from one short text to another, as Notion's updates mark a
// changed equation (seen 2026-11-13: "\int_0^1 x\,dx = \frac{1}{2}" to
// "\int_0^2 x\,dx = 2" strikes the 1 and "\frac{1}{2}" and marks each 2):
// the characters both keep, the longest run of them in order, between what
// went and what came; a kept run no longer than the change on each side of
// it goes and comes back with them, so a word changes whole. Texts too long
// to compare letter by letter keep only their shared ends.

export type TextDiffPart = { kind: "kept" | "deleted" | "added"; text: string };

type Run = { kept: string } | { deleted: string; added: string };

const COMPARED = 250_000;

export function textDiff(was: string, now: string): TextDiffPart[] {
  const from = Array.from(was), to = Array.from(now);
  let start = 0;
  while (start < from.length && start < to.length && from[start] === to[start]) start += 1;
  let end = 0;
  while (end < from.length - start && end < to.length - start && from[from.length - 1 - end] === to[to.length - 1 - end]) end += 1;
  const a = from.slice(start, from.length - end), b = to.slice(start, to.length - end);
  const runs: Run[] = [];
  const push = (kept: boolean, deleted: string, added: string) => {
    const last = runs[runs.length - 1];
    if (kept && last && "kept" in last) last.kept += deleted;
    else if (kept) runs.push({ kept: deleted });
    else if (last && !("kept" in last)) { last.deleted += deleted; last.added += added; }
    else runs.push({ deleted, added });
  };
  if (start) push(true, from.slice(0, start).join(""), "");
  if (a.length * b.length > COMPARED) push(false, a.join(""), b.join(""));
  else {
    // The longest run kept in order, from each pair of ends.
    const width = b.length + 1;
    const longest = new Uint16Array((a.length + 1) * width);
    for (let i = a.length - 1; i >= 0; i -= 1) {
      for (let j = b.length - 1; j >= 0; j -= 1) {
        longest[i * width + j] = a[i] === b[j] ? longest[(i + 1) * width + j + 1] + 1
          : Math.max(longest[(i + 1) * width + j], longest[i * width + j + 1]);
      }
    }
    let i = 0, j = 0;
    while (i < a.length || j < b.length) {
      if (i < a.length && j < b.length && a[i] === b[j]) { push(true, a[i], ""); i += 1; j += 1; }
      else if (j >= b.length || (i < a.length && longest[(i + 1) * width + j] >= longest[i * width + j + 1])) { push(false, a[i], ""); i += 1; }
      else { push(false, "", b[j]); j += 1; }
    }
  }
  if (end) push(true, from.slice(from.length - end).join(""), "");
  // A short kept run between changes goes with them.
  for (let at = 1; at < runs.length - 1; at += 1) {
    const before = runs[at - 1], kept = runs[at], after = runs[at + 1];
    if (!("kept" in kept) || "kept" in before || "kept" in after) continue;
    const length = Array.from(kept.kept).length;
    if (length > Math.max(Array.from(before.deleted).length, Array.from(before.added).length) ||
      length > Math.max(Array.from(after.deleted).length, Array.from(after.added).length)) continue;
    runs.splice(at - 1, 3, { deleted: before.deleted + kept.kept + after.deleted, added: before.added + kept.kept + after.added });
    at = 0;
  }
  return runs.flatMap((run): TextDiffPart[] => "kept" in run ? [{ kind: "kept", text: run.kept }]
    : [{ kind: "deleted" as const, text: run.deleted }, { kind: "added" as const, text: run.added }].filter((part) => part.text));
}
