import { Suspense, type ComponentProps } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import { useDeveloperMode } from "@/lib/developerMode";

// The developer bar (DeveloperBar.tsx) is shown only to a developer who
// turned it on, and its code is asked for only then: asked for on every
// start, its two files went out beside the account's first reads with the
// developer mode off as well (2026-11-13). Without its code it shows nothing.
const DeveloperBar = lazyScreen(() => import("./DeveloperBar").then((module) => ({ default: module.DeveloperBar })), { standIn: () => null, entry: true });

export function DeveloperBarWhenOn(props: ComponentProps<typeof DeveloperBar>) {
  return useDeveloperMode() ? <Suspense fallback={null}><DeveloperBar {...props} /></Suspense> : null;
}
