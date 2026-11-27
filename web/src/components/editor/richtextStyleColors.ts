// Pasted HTML names a Notion color by its CSS value; these read the color
// token back from it. Notion's copy of words names it twice, by its
// data-notion-highlight ("teal" for green) and by one value in either theme
// (seen 2026-11-14, all nine colors and backgrounds, dark and light: the
// first rows below); the rows after them are its older copies' values, in
// the light and the dark theme.

const NOTION_COLORS = new Set(["gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"]);

const STYLE_TEXT_COLOR_BY_RGB = new Map([
  ["125,122,117", "gray"], ["159,118,90", "brown"], ["210,123,45", "orange"],
  ["203,148,52", "yellow"], ["80,148,110", "green"], ["56,125,201", "blue"],
  ["154,107,180", "purple"], ["193,76,138", "pink"], ["207,81,72", "red"],
  ["120,119,116", "gray"], ["159,107,83", "brown"], ["217,115,13", "orange"],
  ["203,145,47", "yellow"], ["68,131,97", "green"], ["51,126,169", "blue"],
  ["144,101,176", "purple"], ["193,76,138", "pink"], ["212,76,71", "red"],
  ["155,155,155", "gray"], ["186,133,111", "brown"], ["199,125,72", "orange"],
  ["202,152,73", "yellow"], ["82,158,114", "green"], ["94,135,201", "blue"],
  ["157,104,211", "purple"], ["209,87,150", "pink"], ["223,84,82", "red"],
]);

const STYLE_BACKGROUND_COLOR_BY_RGB = new Map([
  ["42,28,0", "gray_background"], ["139,46,0", "brown_background"],
  ["224,101,1", "orange_background"], ["211,168,0", "yellow_background"],
  ["0,100,45", "green_background"], ["0,124,215", "blue_background"],
  ["102,0,178", "purple_background"], ["197,0,93", "pink_background"],
  ["223,22,0", "red_background"],
  ["241,241,239", "gray_background"], ["244,238,238", "brown_background"],
  ["251,236,221", "orange_background"], ["251,243,219", "yellow_background"],
  ["237,243,236", "green_background"], ["231,243,248", "blue_background"],
  ["244,240,247", "purple_background"], ["249,238,243", "pink_background"],
  ["253,235,236", "red_background"], ["47,47,47", "gray_background"],
  ["74,50,40", "brown_background"], ["92,59,35", "orange_background"],
  ["86,67,40", "yellow_background"], ["36,61,48", "green_background"],
  ["20,58,78", "blue_background"], ["60,45,73", "purple_background"],
  ["78,44,60", "pink_background"], ["82,46,42", "red_background"],
]);

/** The text color or background a Notion copy's data-notion-highlight names. */
function notionHighlightToken(element: HTMLElement): { color?: string; background?: string } {
  const value = element.getAttribute("data-notion-highlight")?.trim().toLowerCase().replace(/^teal(?=$|_)/u, "green") ?? "";
  const background = value.endsWith("_background");
  const name = background ? value.slice(0, -"_background".length) : value;
  if (!NOTION_COLORS.has(name)) return {};
  return background ? { background: value } : { color: value };
}

/** Whether this element's words carry a Notion copy's color or background, which its plain text loses. */
export function holdsNotionHighlight(element: Element) {
  return element instanceof HTMLElement && Object.keys(notionHighlightToken(element)).length > 0;
}

function rgbKeyFromCssColor(value: string) {
  const trimmed = value.trim().toLowerCase();
  if (!trimmed || trimmed === "transparent") return undefined;
  const hex = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(trimmed);
  if (hex) {
    const raw = hex[1];
    const full = raw.length === 3 ? raw.split("").map((char) => char + char).join("") : raw;
    const [red, green, blue] = [0, 2, 4].map((offset) => parseInt(full.slice(offset, offset + 2), 16));
    if ([red, green, blue].every((part) => Number.isFinite(part))) return `${red},${green},${blue}`;
  }
  const rgb =
    /^rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})(?:\s*,\s*([0-9.]+)\s*)?\)$/.exec(trimmed)
    ?? /^rgba?\(\s*(\d{1,3})\s+(\d{1,3})\s+(\d{1,3})(?:\s*\/\s*([0-9.]+))?\s*\)$/.exec(trimmed);
  if (!rgb) return undefined;
  const [red, green, blue] = rgb.slice(1, 4).map(Number);
  const alpha = rgb[4] === undefined ? 1 : Number(rgb[4]);
  if ([red, green, blue, alpha].some((part) => !Number.isFinite(part)) || alpha <= 0) return undefined;
  if ([red, green, blue].some((part) => part < 0 || part > 255)) return undefined;
  return `${red},${green},${blue}`;
}

/** The text color and the background an element's style names, which Notion's copied text may carry together. */
export function styleColorTokens(element: HTMLElement): { color?: string; background?: string } {
  const named = notionHighlightToken(element);
  if (named.color || named.background) return named;
  const backgroundKey = rgbKeyFromCssColor(element.style.backgroundColor);
  const background = backgroundKey ? STYLE_BACKGROUND_COLOR_BY_RGB.get(backgroundKey) : undefined;
  const colorKey = rgbKeyFromCssColor(element.style.color);
  const color = colorKey ? STYLE_TEXT_COLOR_BY_RGB.get(colorKey) : undefined;
  return { ...(color ? { color } : {}), ...(background ? { background } : {}) };
}
