/**
 * A suspended person's picture, as Notion's help says (read 2026-10-02): "a
 * gray crossed out silhouette". It fills the avatar it sits in.
 */
export function SuspendedPersonGlyph() {
  return (
    <svg width="100%" height="100%" viewBox="0 0 20 20" aria-hidden="true" data-suspended-person>
      <circle cx="10" cy="10" r="10" fill="var(--bg-hover)" />
      <circle cx="10" cy="7.6" r="3.1" fill="var(--text-tertiary)" />
      <path d="M4.4 16.4c1.1-2.7 3.1-4 5.6-4s4.5 1.3 5.6 4z" fill="var(--text-tertiary)" />
      <path d="M4.2 15.8 15.8 4.2" stroke="var(--bg-hover)" strokeWidth="2.6" strokeLinecap="round" />
      <path d="M4.2 15.8 15.8 4.2" stroke="var(--text-tertiary)" strokeWidth="1.2" strokeLinecap="round" />
    </svg>
  );
}
