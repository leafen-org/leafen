// Named color → chip background/text, using the global color tokens.

export const COLOR_NAMES = [
  "default",
  "gray",
  "brown",
  "orange",
  "yellow",
  "green",
  "blue",
  "purple",
  "pink",
  "red",
] as const;

export type ColorName = (typeof COLOR_NAMES)[number];

/** An option's pill: its color's see-through wash and text (app/optionColors.css). */
/**
 * The color ticked among an option's colors: none for a status option whose
 * color is its group's, as Notion's new "시작 전" (seen 2026-11-02: a grey
 * pill, no color ticked; "기본" picked makes it the lighter pill).
 */
export function chosenOptionColor(option: { color?: string; colorFromGroup?: boolean }): string | undefined {
  return option.colorFromGroup ? "" : option.color;
}

/** An option given a color: its own from then on. */
export function recoloredOption<T extends { color: string; colorFromGroup?: boolean }>(option: T, color: string): T {
  return { ...option, color, colorFromGroup: undefined };
}

export function chipStyle(color?: string): React.CSSProperties {
  const name = color && (COLOR_NAMES as readonly string[]).includes(color) ? color : "default";
  return {
    background: `var(--c-chip-bg-${name})`,
    color: `var(--c-chip-text-${name})`,
    // A status's dot is in its color's accent, gray for its default.
    "--chip-accent": `var(--c-accent-${name === "default" ? "gray" : name})`,
  } as React.CSSProperties;
}

/**
 * A new option's color: any of them, "default" too, by chance, as Notion's
 * (seen 2026-11-02: two empty selects took blue, green, red, brown and blue,
 * gray, brown; another's first was default and its second orange).
 */
export function newOptionColor(): ColorName {
  return COLOR_NAMES[Math.floor(Math.random() * COLOR_NAMES.length)];
}
