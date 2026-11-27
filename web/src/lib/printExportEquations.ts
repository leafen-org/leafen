/**
 * A printable export's equations, which the server writes as their TeX with
 * KaTeX's style (workspace_export_document_html_equations.go): the frame it
 * prints in runs no script, so the app draws each with its own KaTeX, as the
 * export's script draws them when its file is opened — a block equation in
 * its box, an inline one in place, one KaTeX can't read left as its TeX, and
 * commands that would link or load anything refused (trust off). Printing
 * waits for KaTeX's fonts, which the style holds.
 */
export async function drawPrintedEquations(printed: Document) {
  const equations = Array.from(printed.querySelectorAll("[data-notion-equation], [data-notion-inline-equation]"));
  if (equations.length === 0) return;
  const { default: katex } = await import("katex");
  for (const equation of equations) {
    const display = equation.hasAttribute("data-notion-equation");
    const tex = equation.getAttribute(display ? "data-notion-equation" : "data-notion-inline-equation") ?? "";
    const box = (display && equation.querySelector(".equation-container")) || equation;
    try {
      box.innerHTML = katex.renderToString(tex, { displayMode: display, throwOnError: true, trust: false });
    } catch {
      // Its TeX stays, as the editor shows one it can't read.
    }
  }
  await printed.fonts?.ready;
}
