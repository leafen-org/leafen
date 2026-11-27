/**
 * One style object over several CSS modules: a class name takes every
 * module's class for it, so a rule in one module that names classes from
 * another still reaches its elements, as in the stylesheet the modules were
 * split from.
 */
export function mergeStyleModules(
  ...modules: ReadonlyArray<Readonly<Record<string, string>>>
): Record<string, string> {
  const merged: Record<string, string> = {};
  for (const module of modules) {
    for (const [name, value] of Object.entries(module)) {
      merged[name] = merged[name] ? `${merged[name]} ${value}` : value;
    }
  }
  return merged;
}
