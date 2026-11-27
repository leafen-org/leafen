import { triggerBrowserFileDownload } from "@/lib/browserFileDownload";
/**
 * A chart's picture as "다른 이름으로 차트 저장" makes it, as Notion's (seen
 * 2026-10-05): the chart on a card over a background — a gradient, blue,
 * red, yellow, gray or none, each in the theme's own colors — copied or
 * saved as PNG, or saved as SVG.
 * Its size is the picture's own, whatever the chart's height or the
 * window's width, as Notion's (read 2026-11-18 in Chrome, two tabs at 1440
 * and 800, the chart 400 and 600 high, from the preview's canvas and the
 * saved files): a PNG 1168 by 820 drawn at twice that, the card 120 in from
 * its sides and 96 from its top and foot, 12 round, inside a black hairline,
 * and the chart 64 inside the card, so drawn 800 by 500; with no background
 * the card is the whole picture and the chart 1040 by 692. The SVG is the
 * chart alone at that size, on nothing.
 */

export const CHART_BACKGROUNDS = ["gradient", "blue", "red", "yellow", "gray", "none"] as const;
export type ChartBackground = (typeof CHART_BACKGROUNDS)[number];

export type ChartPictureTheme = "light" | "dark";

/*
 * The backgrounds by theme, as Notion's (read 2026-11-18 in Chrome at 1440,
 * dark and light, from the swatches' styles and the saved PNGs' pixels). In
 * light the colors are solid; in dark blue, red and yellow are see-through
 * (0.65, 0.6, 0.6), and the PNG keeps them so. The gradient's swatch is a
 * 123° blend under the menu's hover tint, while the picture's is a glow of
 * its own: cool along its start and top, warming to its far corner — drawn
 * here as a circle about that corner whose six steps keep within 5 of
 * Notion's picture in each channel.
 */
const SOLID: Record<ChartPictureTheme, Record<Exclude<ChartBackground, "gradient" | "none">, string>> = {
  light: { blue: "rgb(231, 243, 248)", red: "rgb(253, 235, 236)", yellow: "rgb(251, 243, 219)", gray: "rgb(248, 248, 247)" },
  dark: { blue: "rgba(51, 126, 169, 0.65)", red: "rgba(222, 85, 82, 0.6)", yellow: "rgba(240, 166, 51, 0.6)", gray: "rgb(90, 90, 90)" },
};
const GRADIENT_SWATCH: Record<ChartPictureTheme, string> = {
  light: "linear-gradient(var(--chart-swatch-tint), var(--chart-swatch-tint)), linear-gradient(123deg, rgb(254, 245, 233) 40.15%, rgb(254, 233, 233) 89.33%)",
  dark: "linear-gradient(var(--chart-swatch-tint), var(--chart-swatch-tint)), linear-gradient(123deg, rgb(60, 63, 65) 40.15%, rgb(74, 47, 39) 89.33%)",
};
const GLOW = { cx: 1125, cy: 725, r: 1075 };
const GLOW_STEPS: Record<ChartPictureTheme, readonly string[]> = {
  light: ["255,243,231", "255,246,231", "254,246,229", "254,247,234", "249,248,250", "246,250,252"],
  dark: ["81,46,11", "80,52,16", "76,56,25", "70,57,44", "62,61,62", "59,64,68"],
};

/** The swatch a background shows: the line across "none" in the menu's own colors. */
export function chartBackgroundSwatch(background: ChartBackground, theme: ChartPictureTheme) {
  if (background === "gradient") return GRADIENT_SWATCH[theme];
  if (background === "none") return "linear-gradient(to right top, var(--bg-elevated) calc(50% - 1px), var(--border-subtle), var(--bg-elevated) calc(50% + 1px))";
  return SOLID[theme][background];
}

// The drawn look each part takes from the page's styles, written onto the
// copy so it reads the same alone.
const STYLE_PROPERTIES = [
  "fill", "fill-opacity", "stroke", "stroke-width", "stroke-dasharray", "stroke-opacity", "stroke-linecap", "stroke-linejoin",
  "opacity", "font-size", "font-family", "font-weight", "letter-spacing", "stop-color", "stop-opacity",
];
const PICTURE = { width: 1168, height: 820 };
const BACKDROP = { x: 120, y: 96 };
const CARD_PAD = 64;

export type ChartPictureSize = { width: number; height: number };

/** The size the chart is drawn at inside its picture, over a background or with none. */
export function chartPictureSize(background: ChartBackground): ChartPictureSize {
  const inset = (backdrop: number) => 2 * ((background === "none" ? 0 : backdrop) + CARD_PAD);
  return { width: PICTURE.width - inset(BACKDROP.x), height: PICTURE.height - inset(BACKDROP.y) };
}

function escapeXml(text: string) {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

/** The chart alone, every part carrying the look it has on the page. */
function standaloneChart(svg: SVGSVGElement) {
  const copy = svg.cloneNode(true) as SVGSVGElement;
  const originals = [svg, ...svg.querySelectorAll("*")];
  const copies = [copy, ...copy.querySelectorAll("*")];
  originals.forEach((original, index) => {
    const target = copies[index] as SVGElement;
    const computed = getComputedStyle(original);
    target.removeAttribute("class");
    target.setAttribute("style", STYLE_PROPERTIES.map((name) => `${name}:${computed.getPropertyValue(name)}`).join(";"));
  });
  copy.querySelectorAll("title").forEach((title) => title.remove());
  const [, , width, height] = (svg.getAttribute("viewBox") ?? "0 0 760 320").split(/\s+/).map(Number);
  return { inner: copy.innerHTML, width, height };
}

/** The picture's SVG text: the chart, drawn at its picture's size, on its card over the theme's background. */
export function chartPictureSvg(svg: SVGSVGElement, background: ChartBackground, theme: ChartPictureTheme) {
  const { inner } = standaloneChart(svg);
  const { width, height } = PICTURE;
  const backdrop = background !== "none";
  const cardColor = escapeXml(getComputedStyle(svg).getPropertyValue("--bg-app").trim() || "#fff");
  const fill = background === "gradient" ? "url(#chart-background)" : background === "none" ? cardColor : SOLID[theme][background];
  const card = { x: BACKDROP.x, y: BACKDROP.y, width: width - BACKDROP.x * 2, height: height - BACKDROP.y * 2 };
  const offset = { x: (backdrop ? card.x : 0) + CARD_PAD, y: (backdrop ? card.y : 0) + CARD_PAD };
  const steps = GLOW_STEPS[theme];
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">`
    + (background === "gradient" ? `<defs><radialGradient id="chart-background" gradientUnits="userSpaceOnUse" cx="${GLOW.cx}" cy="${GLOW.cy}" r="${GLOW.r}">`
      + steps.map((color, index) => `<stop offset="${index / (steps.length - 1)}" stop-color="rgb(${color})"/>`).join("")
      + `</radialGradient></defs>` : "")
    + `<rect width="${width}" height="${height}" fill="${fill}"/>`
    + (backdrop ? `<rect x="${card.x - 0.25}" y="${card.y - 0.25}" width="${card.width + 0.5}" height="${card.height + 0.5}" rx="12.25" fill="none" stroke="#000" stroke-width="0.5"/>`
      + `<rect x="${card.x}" y="${card.y}" width="${card.width}" height="${card.height}" rx="12" fill="${cardColor}"/>` : "")
    + `<g transform="translate(${offset.x} ${offset.y})">${inner}</g></svg>`;
}

/** The chart alone as "SVG 다운로드" saves it: at its picture's size, on nothing. */
export function chartAloneSvg(svg: SVGSVGElement) {
  const { inner, width, height } = standaloneChart(svg);
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">${inner}</svg>`;
}

/** The picture as PNG, at twice its size. */
export async function chartPicturePng(svg: SVGSVGElement, background: ChartBackground, theme: ChartPictureTheme): Promise<Blob> {
  const markup = chartPictureSvg(svg, background, theme);
  const image = new Image();
  const url = URL.createObjectURL(new Blob([markup], { type: "image/svg+xml" }));
  try {
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve();
      image.onerror = () => reject(new Error("chart picture"));
      image.src = url;
    });
    const canvas = document.createElement("canvas");
    canvas.width = image.width * 2;
    canvas.height = image.height * 2;
    const context = canvas.getContext("2d");
    if (!context) throw new Error("chart picture");
    context.scale(2, 2);
    context.drawImage(image, 0, 0);
    return await new Promise<Blob>((resolve, reject) => canvas.toBlob((blob) => blob ? resolve(blob) : reject(new Error("chart picture")), "image/png"));
  } finally {
    URL.revokeObjectURL(url);
  }
}

/** Saves a picture under a name, as a download. */
export function saveChartPicture(blob: Blob, name: string) {
  return triggerBrowserFileDownload(blob, name, { revokeAfterMs: 1000 });
}

/** A picture's file name: Notion's "차트.png" or "차트.svg" whatever the view is called (seen 2026-11-18, two tabs). */
export function chartFileName(name: string, extension: "png" | "svg") {
  const base = name.trim().replace(/[\\/:*?"<>|]+/g, " ").trim() || "chart";
  return `${base}.${extension}`;
}
