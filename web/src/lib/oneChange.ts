// What one saved change holds: the server's bound on one update, 64 MiB
// (2026-10-13). Text the editor pastes, converts or sends as one operation
// is held to as many characters, and nothing smaller.
export const ONE_CHANGE_CHARACTERS = 64 * 1024 * 1024;
