// Small line icons for the other apps' screens the connector guide draws:
// shapes like theirs, not their artwork. 10px, in the text's color.

export type ScreenIconName =
  | "plus" | "search" | "stack" | "shapes" | "case" | "spark" | "chevron" | "globe"
  | "close" | "check" | "compose" | "image" | "at" | "gear" | "arrowUp";

const PATHS: Record<ScreenIconName, string> = {
  plus: "M8 3v10M3 8h10",
  search: "M7 12A5 5 0 1 0 7 2a5 5 0 0 0 0 10ZM10.6 10.6 14 14",
  stack: "M2.5 5.5 8 3l5.5 2.5L8 8 2.5 5.5ZM2.5 8.5 8 11l5.5-2.5M2.5 11.5 8 14l5.5-2.5",
  shapes: "M3 3h4v4H3zM9.5 9.5h4v4h-4zM11.5 2.5l2 3.5h-4zM5 9.5a2 2 0 1 1 0 4 2 2 0 0 1 0-4Z",
  case: "M2.5 5.5h11v7.5h-11zM6 5.5V3.5h4v2M2.5 8.5h11",
  spark: "M8 1.5v13M1.5 8h13M3.4 3.4l9.2 9.2M12.6 3.4l-9.2 9.2",
  chevron: "M4.5 6.5 8 10l3.5-3.5",
  globe: "M8 14.5A6.5 6.5 0 1 0 8 1.5a6.5 6.5 0 0 0 0 13ZM1.5 8h13M8 1.5c2 2 2.6 4.2 2.6 6.5S10 12.5 8 14.5C6 12.5 5.4 10.3 5.4 8S6 3.5 8 1.5Z",
  close: "M4 4l8 8M12 4l-8 8",
  check: "M3 8.5 6.5 12 13 4.5",
  compose: "M9.5 3H3v10h10V6.5M7 9l1.2-.3 5.3-5.3-1-1-5.3 5.3L7 9Z",
  image: "M2.5 3.5h11v9h-11zM2.5 10.5l3-3 3 3 2-2 3 3M10.5 6a.8.8 0 1 0 0 .1",
  at: "M10.5 8a2.5 2.5 0 1 1-5 0 2.5 2.5 0 0 1 5 0Zm0 0v1.2a1.8 1.8 0 0 0 3.5.3A6.5 6.5 0 1 0 11 13.6",
  gear: "M8 10.2a2.2 2.2 0 1 0 0-4.4 2.2 2.2 0 0 0 0 4.4ZM8 1.5v2M8 12.5v2M1.5 8h2M12.5 8h2M3.4 3.4l1.4 1.4M11.2 11.2l1.4 1.4M3.4 12.6l1.4-1.4M11.2 4.8l1.4-1.4",
  arrowUp: "M8 13V3M4 7l4-4 4 4",
};

export function ScreenIcon({ name }: { name: ScreenIconName }) {
  return (
    <svg viewBox="0 0 16 16" width="10" height="10" aria-hidden="true" focusable="false"
      fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round"
      style={{ flex: "0 0 auto" }}>
      <path d={PATHS[name]} />
    </svg>
  );
}
