import { useLayoutEffect, type ReactNode } from "react";
import { coverDocumentChrome } from "@/lib/documentChrome";
import auth from "../AuthGate.module.css";
import { FirstRunLayer } from "../firstSetup/FirstRunLayer";
import css from "./FirstInstall.module.css";
import { pageMarkSource } from "@/lib/pageMark";

/**
 * A first run step drawn as the language and name steps are (the user's word,
 * 2026-10-27): the sign-in screen's panel — the mark and "Leafen" over the
 * step — on a layer of its own over the whole window (FirstRunLayer), wide as
 * the server step where its content is, with no close; the step's own
 * "나중에" passes it. It names the window and its tab after its title.
 */
export function FirstRunScreen({ step, name, view = step, wide = true, children }: {
  /** The step, as data-first-run-screen. */
  step: string;
  /** Its title, which names the window and its tab while it shows. */
  name: string;
  /** What the step shows now, where it has more than one view: a new one takes the focus in. */
  view?: string;
  wide?: boolean;
  children?: ReactNode;
}) {
  useLayoutEffect(() => coverDocumentChrome(name), [name]);
  return (
    <FirstRunLayer view={view} data={{ "data-first-run-screen": step }}>
      <main className={auth.screen}>
        <section className={`${auth.panel} ${wide ? css.wide : ""}`}>
          <div className={auth.brand}>
            <img className={auth.mark} src={pageMarkSource()} alt="" aria-hidden="true" />
            <div><h1>Leafen</h1></div>
          </div>
          {children}
        </section>
      </main>
    </FirstRunLayer>
  );
}
