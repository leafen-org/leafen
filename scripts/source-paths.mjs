// How the build's checks name a file: by its path from a folder, with '/'
// between folders on every system, as their rules and the files they make
// write it. Windows joins folders with '\', and checks that matched such a
// path against a rule written with '/' judged the file otherwise there
// (2026-11-10). Line endings are .gitattributes': text is checked out as LF.
import { relative, sep } from 'node:path'

/** The path with '/' between its folders. */
export function slashPath(path) {
  return sep === '/' ? path : path.split(sep).join('/')
}

/** The path of `to` from the folder `from`, with '/' between its folders. */
export function relativeSlashPath(from, to) {
  return slashPath(relative(from, to))
}
