/**
 * The motion of the picture over "날짜 자동 이동", as Notion's (seen
 * 2026-11-18 in desktop Chrome at 1440, frames sampled for all three choices,
 * and on a Galaxy A90's Chrome at 411 with reduced motion asked for, which it
 * plays all the same): a pointer drags the middle task by springs, one after
 * another, for ever, the other two following as the choice says. Places are
 * from the picture's middle.
 *
 * - "날짜가 겹칠 때만 이동": the middle task to 40 left, 40 right and 80
 *   right of where it starts, pushing a task it meets on either side along;
 *   the tasks fade out in 500ms, start again where they began and fade in.
 * - "항목 간 시간 이동과 유지": 40 right, 40 left, back, all three together.
 * - "자동으로 날짜 이동 금지": 60 right, 60 left, back, alone, an arrow that
 *   runs backwards turning red.
 */

type Point = { x: number; y: number };
type Frame = { x: number; opacity: number };
type Timeline<T> = Generator<T, void, number>;

/**
 * A spring from one place to another, as Notion's: stiffness 35 and damping
 * 10 (an acceleration of 35e-5 per ms² for each pixel left to go, less 0.05
 * of the speed per ms) worked out in steps of 1/60 s by Runge–Kutta, at rest
 * once within 0.001 of its end — over 1.3 s for 40, most of it in the first
 * 300ms.
 */
const STIFFNESS = 35e-5, DAMPING = 0.05, STEP = 1000 / 60, PRECISION = 0.001;

function* springs(stops: readonly number[]): Timeline<number> {
  for (let stop = 0; stop + 1 < stops.length; stop += 1) {
    const end = stops[stop + 1];
    const accel = (place: number, moving: number) => STIFFNESS * (end - place) - DAMPING * moving;
    let value = stops[stop], speed = 0;
    for (;;) {
      let left = Math.max(0, yield value);
      do {
        const step = Math.min(left, STEP);
        left -= step;
        const a1 = accel(value, speed), v2 = speed + 0.5 * a1 * step;
        const a2 = accel(value + 0.5 * speed * step, v2), v3 = speed + 0.5 * a2 * step;
        const a3 = accel(value + 0.5 * v2 * step, v3), v4 = speed + a3 * step;
        const a4 = accel(value + v3 * step, v4);
        value += (step / 6) * (speed + 2 * v2 + 2 * v3 + v4);
        speed += (step / 6) * (a1 + 2 * a2 + 2 * a3 + a4);
      } while (left > 0);
      if (Math.abs(end - value) <= PRECISION) {
        yield end;
        break;
      }
    }
  }
}

/** One value, for `duration` ms or for ever. */
function* hold(value: number, duration = Infinity): Timeline<number> {
  for (let time = 0; time < duration;) time += yield value;
}

/** From one value to another in a straight line over `duration` ms. */
function* fade(from: number, to: number, duration: number): Timeline<number> {
  for (let time = 0; time < duration;) time += yield from + (time / duration) * (to - from);
}

/** The place and the opacity together, done when either is. */
function* together(x: Timeline<number>, opacity: Timeline<number>): Timeline<Frame> {
  let elapsed = 0;
  for (;;) {
    const at = x.next(elapsed), seen = opacity.next(elapsed);
    if (at.done || seen.done) return;
    elapsed = yield { x: at.value, opacity: seen.value };
  }
}

/** The dragged task's place and the tasks' opacity, frame by frame: next(ms since the last). */
export function* dependencyShiftMotion(mode: string): Timeline<Frame> {
  for (;;) {
    if (mode === "overlap") {
      yield* together(springs([0, -40, 40, 80]), hold(1));
      yield* together(hold(80), fade(1, 0, 500));
      yield* together(hold(0), fade(0, 1, 500));
    } else {
      yield* together(springs(mode === "maintain_spacing" ? [0, 40, -40, 0] : [0, 60, -60, 0]), hold(1));
    }
  }
}

const toward = (from: Point, to: Point, by: number): Point => {
  const dx = to.x - from.x, dy = to.y - from.y, share = Math.min(1, by / Math.hypot(dx, dy));
  return { x: from.x + dx * share, y: from.y + dy * share };
};
const half = (a: Point, b: Point): Point => ({ x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 });
const short = (value: number) => Math.round(value * 1000) / 1000;
const at = ({ x, y }: Point) => `${short(x)} ${short(y)}`;
const corner = ({ x, y }: Point) => `${short(x)},${short(y)}`;

/**
 * An arrow from a task's end to the start of the one after it, as Notion's:
 * a line 21 out of the first and 21 short of where it points, its two corners
 * rounded 16 (or what the line between them leaves), ending 1.5 short under a
 * 5×12 head; red when it runs backwards, its task past the next's start.
 */
function link(from: Point, to: Point) {
  const start = { x: from.x + 80, y: from.y + 12 }, end = { x: to.x - 2, y: to.y + 12 };
  const out = { x: start.x + 21, y: start.y }, back = { x: end.x - 21, y: end.y }, tip = { x: end.x - 1.5, y: end.y };
  const leave = toward(out, start, 16), turned = toward(out, back, 16);
  const arrive = toward(back, turned, 16), landed = toward(back, tip, 16);
  return {
    line: `M ${at(start)} L ${at(leave)} C ${at(half(leave, out))} ${at(half(out, turned))} ${at(turned)}`
      + ` L ${at(arrive)} C ${at(half(arrive, back))} ${at(half(back, landed))} ${at(landed)} L ${at(tip)}`,
    head: `${corner({ x: end.x - 5, y: end.y - 6 })} ${corner(end)} ${corner({ x: end.x - 5, y: end.y + 6 })}`,
    backwards: start.x > to.x,
  };
}

/**
 * Where everything is for a frame: three 80×24 tasks in rows 32 apart — the
 * first 160 left of the middle, the dragged one 40 left of the pointer, the
 * one after it 80 right of the middle, moved as the choice says — the pointer
 * 54 down, and the two arrows, the dragged task's first.
 */
export function dependencyShiftScene(mode: string, { x, opacity }: Frame) {
  const first = { x: -160, y: 10 }, dragged = { x: x - 40, y: 42 }, next = { x: 80, y: 74 };
  if (mode === "overlap") {
    first.x = Math.min(first.x, dragged.x - 80);
    next.x = Math.max(next.x, dragged.x + 80);
  } else if (mode === "maintain_spacing") {
    first.x += x;
    next.x += x;
  }
  return { tasks: [first, dragged, next] as const, pointer: { x, y: 54 }, opacity, arrows: [link(dragged, next), link(first, dragged)] as const };
}

export type DependencyShiftScene = ReturnType<typeof dependencyShiftScene>;
