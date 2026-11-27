// A Korean particle after a value known only when the line is shown — a page
// title, a name, an address — stays the pair the copy writes, as Notion's
// Korean does: its messages put 146 "을(를)" and 78 "(으)로" straight after a
// value ("{actor}을(를) 통해", "색상을 {color}(으)로 설정"; read 2026-11-17)
// and its screen says "색상을 기본값(으)로 설정"; a form is fixed only where
// the words before it are, or where Notion's own line fixes one (its question
// before a language change — the copy check's PARTICLE_AS_NOTION,
// 2026-11-19). Nothing picks a form from the value's last sound.

/** The subject marker after a name, as Notion writes it. */
export const KOREAN_SUBJECT_PAIR = "이(가)";

/** The object marker after a name, as Notion writes it. */
export const KOREAN_OBJECT_PAIR = "을(를)";
