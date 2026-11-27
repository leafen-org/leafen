// x2t reads neighbouring cells that keep their text in the cell (t="inlineStr",
// as many export libraries write them) as all holding the first one's text, so
// the editor would show and then save the wrong values. A workbook with such
// cells goes to it with that text in its shared strings, where x2t reads it
// right; any other workbook goes unchanged.

const inlineCell = /<c\b([^>]*?)\s+t="inlineStr"([^>]*?)(?<!\/)>(.*?)<\/c>/gsu;
const sstContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml";
const sstRelation = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings";

export async function sharedInlineStrings(bytes, loadScript) {
  await loadScript("jszip-3.10.1/jszip.min.js");
  const zip = await globalThis.JSZip.loadAsync(bytes);
  const sheets = zip.file(/^xl\/worksheets\/[^/]+\.xml$/u);
  const texts = await Promise.all(sheets.map((sheet) => sheet.async("string")));
  if (!texts.some((text) => text.includes('t="inlineStr"'))) return bytes;

  const relationsPath = "xl/_rels/workbook.xml.rels";
  const relations = await zip.file(relationsPath)?.async("string");
  if (relations == null) return bytes;
  const sstTag = relations.match(/<Relationship\b[^>]*>/gu)?.find((tag) => tag.includes(sstRelation));
  const sstTarget = sstTag && /Target="([^"]+)"/u.exec(sstTag)?.[1];
  const sstPath = sstTarget ? (sstTarget.startsWith("/") ? sstTarget.slice(1) : `xl/${sstTarget}`) : "xl/sharedStrings.xml";
  const sst = await zip.file(sstPath)?.async("string");
  const held = sst == null ? 0 : (sst.match(/<si[\s>]/gu)?.length ?? 0);

  // The next index counts the strings the file already holds.
  const added = [];
  sheets.forEach((sheet, index) => {
    if (!texts[index].includes('t="inlineStr"')) return;
    zip.file(sheet.name, texts[index].replace(inlineCell, (cell, before, after, inner) => {
      const run = /<is(?:\s[^>]*)?>(.*?)<\/is>/su.exec(inner);
      if (!run) return cell;
      added.push(`<si>${run[1]}</si>`);
      const rest = inner.replace(run[0], "").replace(/<v(?:\s[^>]*)?>.*?<\/v>/su, "");
      return `<c${before} t="s"${after}>${rest}<v>${held + added.length - 1}</v></c>`;
    }));
  });

  if (sst == null) {
    zip.file(sstPath, '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
      + `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">${added.join("")}</sst>`);
    zip.file(relationsPath, relations.replace("</Relationships>",
      `<Relationship Id="leafenSharedStrings" Type="${sstRelation}" Target="sharedStrings.xml"/></Relationships>`));
    const types = await zip.file("[Content_Types].xml").async("string");
    zip.file("[Content_Types].xml", types.replace("</Types>",
      `<Override PartName="/${sstPath}" ContentType="${sstContentType}"/></Types>`));
  } else {
    // Its counts, when it states them, are left to be recounted: both are optional.
    zip.file(sstPath, sst.replace(/(<sst\b[^>]*?)\s+count="\d+"/u, "$1").replace(/(<sst\b[^>]*?)\s+uniqueCount="\d+"/u, "$1")
      .replace(/<sst\b([^>]*?)\/>/u, "<sst$1></sst>").replace("</sst>", `${added.join("")}</sst>`));
  }
  return zip.generateAsync({ type: "uint8array", compression: "DEFLATE" });
}
