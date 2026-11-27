import { useEffect, useId, useRef } from "react";
import { dependencyShiftMotion, dependencyShiftScene, type DependencyShiftScene } from "./dependencyShiftMotion";
import styles from "./DependencyShiftPicture.module.css";

// Notion's picture (seen 2026-11-18 in desktop Chrome at 1440 and on a Galaxy
// A90's Chrome at 411), laid from the picture's middle so it holds its place at
// any width: a dotted day line every 40px from 160 left of the middle, and the
// tasks, arrows and pointer the motion moves (dependencyShiftMotion.ts).
const DAY = 40;
const DAY_LINES = Array.from({ length: 24 }, (_, day) => -160 + DAY * day);
const TASK_WIDTH = 80, TASK_HEIGHT = 24;
const START = dependencyShiftScene("none", { x: 0, opacity: 1 });
const POINTER_OUTLINE = "M14 12.5h.25v-1.413l-.073-.073-7.69-7.69-.12-.12-.154.065-1.059.44L5 3.775v11.142l.073.073.75.75.177.177"
  + ".177-.177 2.092-2.091 2.05 4.948.096.23.23-.095 2.31-.956.231-.096-.096-.231-2.05-4.948H14z";

/** Puts a frame's places, arrows and opacity on the drawn picture. */
function draw(picture: SVGSVGElement, scene: DependencyShiftScene) {
  const opacity = String(Math.round(scene.opacity * 1000) / 1000);
  picture.querySelectorAll<SVGRectElement>("rect").forEach((task, index) => {
    task.setAttribute("x", String(scene.tasks[index].x));
    task.setAttribute("opacity", opacity);
  });
  picture.querySelectorAll<SVGGElement>("[data-part=\"arrow\"]").forEach((arrow, index) => {
    const { line, head, backwards } = scene.arrows[index];
    arrow.toggleAttribute("data-backwards", backwards);
    arrow.querySelector("path")?.setAttribute("d", line);
    arrow.querySelector("polyline")?.setAttribute("points", head);
    for (const part of arrow.children) part.setAttribute("opacity", opacity);
  });
  const pointer = picture.querySelector("[data-part=\"pointer\"]");
  pointer?.setAttribute("transform", `translate(${scene.pointer.x} ${scene.pointer.y})`);
  pointer?.setAttribute("opacity", opacity);
}

/**
 * A task's ring and shadow, as Notion's: a 1px ring (rgba(18,18,18,.1), the
 * dragged one's rgba(51,126,169,.2)) and a shadow 2 down blurred 2
 * (rgba(18,18,18,.1); the pointer's .2, no ring).
 */
function Lift({ id, ring, ringOpacity, shadowOpacity, region }: {
  id: string; ring?: string; ringOpacity?: number; shadowOpacity: number;
  /** The shadow's box in the drawing's own units, as Notion's pointer cuts it (0.75, 0.91, 17.75 × 24.244). */
  region?: { x: number; y: number; width: number; height: number };
}) {
  return (
    <filter id={id} colorInterpolationFilters="sRGB" {...(region ? { ...region, filterUnits: "userSpaceOnUse" as const }
      : { x: "-20%", y: "-25%", width: "140%", height: "190%" })}>
      <feOffset in="SourceAlpha" dy="2" result="drop" />
      <feGaussianBlur in="drop" stdDeviation="2" result="blur" />
      <feFlood floodColor="rgb(18, 18, 18)" floodOpacity={shadowOpacity} result="shadowColor" />
      <feComposite in="shadowColor" in2="blur" operator="in" result="shadow" />
      {ring ? <>
        <feMorphology in="SourceAlpha" operator="dilate" radius="1" result="edge" />
        <feFlood floodColor={ring} floodOpacity={ringOpacity} result="ringColor" />
        <feComposite in="ringColor" in2="edge" operator="in" result="ring" />
      </> : null}
      <feMerge>
        <feMergeNode in="shadow" />
        {ring ? <feMergeNode in="ring" /> : null}
        <feMergeNode in="SourceGraphic" />
      </feMerge>
    </filter>
  );
}

/**
 * Notion's picture of a task moving the one after it along, over
 * "날짜 자동 이동": a box 112 high with a 1px line, round 6, on its palette's
 * light gray (DependencyShiftPicture.module.css), playing the drag of the
 * chosen way (`mode`, its choices' values), from the start again when the
 * choice changes. Its place and width are the screen's (`className`).
 */
export function DependencyShiftPicture({ className, mode }: { className?: string; mode: string }) {
  const id = useId().replace(/[^\w-]/g, "");
  const picture = useRef<SVGSVGElement>(null);
  // Frame by frame, as Notion's, by the time since the last; drawn on the
  // picture itself, nothing else drawn again.
  useEffect(() => {
    const shown = picture.current;
    if (!shown) return undefined;
    const motion = dependencyShiftMotion(mode);
    const play = (elapsed: number) => {
      const frame = motion.next(elapsed);
      if (!frame.done) draw(shown, dependencyShiftScene(mode, frame.value));
    };
    play(0);
    let last = performance.now(), request = 0;
    const tick = () => {
      request = requestAnimationFrame(tick);
      const now = performance.now();
      play(now - last);
      last = now;
    };
    request = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(request);
  }, [mode]);
  return (
    <svg ref={picture} className={`${styles.picture} ${className ?? ""}`} aria-hidden="true">
      <defs>
        <Lift id={`${id}-task`} ring="rgb(18, 18, 18)" ringOpacity={0.1} shadowOpacity={0.1} />
        <Lift id={`${id}-moved`} ring="rgb(51, 126, 169)" ringOpacity={0.2} shadowOpacity={0.1} />
        <Lift id={`${id}-pointer`} shadowOpacity={0.2} region={{ x: 0.75, y: 0.91, width: 17.75, height: 24.244 }} />
      </defs>
      <svg x="50%" overflow="visible">
        {DAY_LINES.map((x) => <line key={x} x1={x} y1="0" x2={x} y2="100%" />)}
        {START.arrows.map(({ line, head }, index) => <g key={index} data-part="arrow"><path d={line} /><polyline points={head} /></g>)}
        {START.tasks.map((task, index) => (
          <rect key={index} x={task.x} y={task.y} width={TASK_WIDTH} height={TASK_HEIGHT} rx="3"
            data-part={index === 1 ? "moved" : "task"} filter={`url(#${id}-${index === 1 ? "moved" : "task"})`} />
        ))}
        {/* Notion's two-layer pointer, its paths as they are (seen 2026-11-18 in its picture): the arrow, and over it the
            same arrow a quarter pixel larger, lined white 0.5. */}
        <g data-part="pointer" filter={`url(#${id}-pointer)`} transform={`translate(${START.pointer.x} ${START.pointer.y})`}>
          <path d="M14 12.25h-3.334l2.193 5.294-2.309.956-2.194-5.294L6 15.563l-.75-.75V3.94l1.06-.44L14 11.19v1.06z" />
          <path data-outline="" d={POINTER_OUTLINE} />
        </g>
      </svg>
    </svg>
  );
}
