// Prism highlights every code element on the page as it loads unless told
// it is used by hand; the editor colors its code blocks itself
// (CodeHighlight). Imported before Prism, which reads this as it starts.
// A Prism already loaded is kept (its grammars live on it).
const host = globalThis as { Prism?: { manual?: boolean } };
host.Prism ??= { manual: true };

export {};
