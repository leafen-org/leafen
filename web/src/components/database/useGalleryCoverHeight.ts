import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from "react";

/** A size's narrowest card, its grid's minimum column (.gallery). */
const NARROWEST: Record<string, number> = { small: 180, medium: 260, large: 320 };

/**
 * How tall a gallery's previews are, as Notion's (its gallery code, read
 * 2026-11-11): 16:9 of its first card's width, taken when the card size is
 * changed and again as the window is resized (a tenth of a second after);
 * first drawn, before it has taken a card's width, 16:9 of its size's
 * narrowest card — 101.25, 146.25 or 180 however wide the cards are
 * (seen 2026-11-07 and 2026-11-11: 338 wide cards opened 146.25 and 180
 * tall, then 190.125 once the size was changed or the window resized). The
 * view narrowing by itself, as a sidebar opening, leaves it (seen 2026-11-07).
 */
export function useGalleryCoverHeight(size: string) {
  const ref = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState<number | null>(null);
  const drawnSize = useRef(size);
  const measure = useRef(() => {});
  measure.current = () => {
    const card = ref.current?.querySelector<HTMLElement>("[data-row-card]");
    setHeight(card ? card.offsetWidth * 9 / 16 : (NARROWEST[size] ?? NARROWEST.medium) * 9 / 16);
  };
  useLayoutEffect(() => {
    if (drawnSize.current === size) return;
    drawnSize.current = size;
    measure.current();
  }, [size]);
  useEffect(() => {
    let timer = 0;
    const resized = () => { window.clearTimeout(timer); timer = window.setTimeout(() => measure.current(), 100); };
    window.addEventListener("resize", resized);
    return () => { window.clearTimeout(timer); window.removeEventListener("resize", resized); };
  }, []);
  const style = height === null ? undefined : { "--gallery-cover-height": `${height}px` } as CSSProperties;
  return { ref, style };
}
