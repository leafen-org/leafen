// The words the block menus — the "/" menu and the block "⋮⋮" menu — find
// their rows by: Leafen's own words for each row, Korean and English, and a
// "/" block row's Markdown hint after them. They were written for Leafen and
// tuned by typing a wide set of queries into Notion's "/" menu and Leafen's
// until the same rows came back in the same order (every row name, common
// words, and their 1–3 letter starts, on a Korean and an English screen).
// The menus score one line of a row's words (actionMenuScore): the "/" menu's
// line is these words, its group's words and these words again
// (blockDefinitionSearch), the block menu's the same (blockMenuSearch).

import { i18next } from "@/i18n";

/** The language whose search words the menus read: Korean or English, else none (null). */
export function searchWordsLanguage(): "ko" | "en" | null {
  const language = i18next.resolvedLanguage ?? i18next.language ?? "";
  return language.startsWith("ko") ? "ko" : language.startsWith("en") ? "en" : null;
}

/** A row's search words: Korean and English, the Korean `undefined` where the words are the same in every language. */
export type SearchWords = { ko?: string; en: string; tail?: string };

/** The words in `language`'s line: Korean then English in Korean, English in English, `shown` then English in any other. */
export function searchWordsLine({ ko, en, tail }: SearchWords, language: "ko" | "en" | null, shown: string) {
  const words = ko === undefined || language === "en" ? en : language === "ko" ? `${ko} ${en}` : `${shown} ${en}`;
  // A block row of the "/" menu ends with its Markdown hint, or a space where it has none.
  return tail === undefined ? words : `${words} ${tail}`;
}

// The words of the types a block turns into, by type ("column_list:2" for two
// columns), Korean then English. The heading, toggle and toggle heading rows
// repeat some of their own words: a search of two words typed apart scores by
// what lies between them in a row's line (actionMenuScore), and these lengths
// rank Korean and English mixed ("/toggle 토글", "/heading 제목", "/제목
// heading", "/h1 제목") as Notion's "/" and "⋮⋮" menus do (seen 2026-11-22,
// 55 searches, two tabs); the repeats add no word of their own.
const TURN_WORDS: Readonly<Record<string, readonly [string, string]>> = {
  paragraph: ["텍스트", "Text"],
  heading_1: ["제목 1", "Heading 1 h1"],
  heading_2: ["제목2 소제목2 제목2", "Sub heading 2 2 2 two h2"],
  heading_3: ["제목3 소제목3 소제목3", "Subsub heading 3 h3 h3 three"],
  heading_4: ["하위 하위 4 제목", "Subsubsub heading 4 4 4 four h4"],
  child_page: ["페이지", "Page"],
  bulleted_list_item: ["글머리 기호 목록", "Bulleted list"],
  numbered_list_item: ["번호 매기기 목록", "Numbered list"],
  to_do: ["할일 체크박스 목록 작업 목록", "Todo To-do checkbox list"],
  toggle: ["토글 목록 정보 세부 축소", "Toggle collapse list list expand details"],
  code: ["코드", "Code"],
  quote: ["\" 인용", "Quote \""],
  callout: ["콜아웃", "Callout"],
  equation: ["LaTeX 수학 공식 블록 수식", "LaTeX math block equation formula"],
  synced_block: ["동기화 블록 레퍼런스 포털 임베드 그룹화", "Synced block reference portal block embed group"],
  toggle_heading_1: ["제목 토글 1 토글", "Toggle 1 1 1 heading h1 1 expand 1 1"],
  toggle_heading_2: ["하위 2 제목 토글 2 토글 2", "Toggle 2 2 sub heading h2 2 2 expand 2 two"],
  toggle_heading_3: ["하위 3 소제목 토글 3 토글 3", "Toggle 3 3 subsub heading h3 3 expand h3 three"],
  toggle_heading_4: ["하위 하위 4 제목", "Toggle 4 4 subsubsub heading 4 h4 expand h4 four"],
  "column_list:2": ["열 2개 블록 세로 2열 두개", "Col c2 col2 columns 2col vertical block two"],
  "column_list:3": ["열 3개 블록 세로 3열 세개", "Col c3 col3 columns 3col three"],
  "column_list:4": ["열 4개 블록 세로 4열 네개", "Col c4 col4 columns 4col four"],
  "column_list:5": ["열 5개 블록 세로 5열 다섯개", "Col c5 col5 columns 5col five"],
};

/** A type a block turns into's words ("column_list:2" for two columns), and "페이지 위치:"'s. */
export function turnWords(key: string, placeIn = false): SearchWords | undefined {
  if (placeIn) return { ko: "페이지 위치:", en: "Page in" };
  const words = TURN_WORDS[key];
  return words ? { ko: words[0], en: words[1] } : undefined;
}

// A text or background color's words, Korean then English. A background's
// English words are its name, "background" and its name joined with
// "background", so what lies between that "background" and its group's
// "color" grows with the name: "background color" lists 빨간색 배경 first and
// 초록색 배경 last, and "background b" 갈색 배경 and 파란색 배경 first (Notion's
// "/" and "⋮⋮" menus, seen 2026-11-21); 기본 배경's first "background" is
// inside a word, so "background" finds it after the others.
const COLOR_WORDS: Readonly<Record<string, readonly [string, string]>> = {
  default: ["기본 검정 흰색", "Default black white"],
  gray: ["회색", "Grey gray"],
  brown: ["갈색", "Brown"],
  orange: ["주황색", "Orange"],
  yellow: ["노란색", "Yellow"],
  green: ["초록색", "Teal green"],
  blue: ["파란색", "Blue"],
  purple: ["보라색", "Purple"],
  pink: ["분홍색", "Pink"],
  red: ["빨간색", "Red"],
  default_background: ["기본 검정 흰색 기본배경", "Default black whitebackground defaultbackground"],
  gray_background: ["회색 배경", "Grey gray background grey graybackground"],
  brown_background: ["갈색 배경", "Brown background brownbackground"],
  orange_background: ["주황색 배경", "Orange background orangebackground"],
  yellow_background: ["노란색 배경", "Yellow background yellowbackground"],
  green_background: ["초록색 배경", "Teal green background teal greenbackground"],
  blue_background: ["파란색 배경", "Blue background bluebackground"],
  purple_background: ["보라색 배경", "Purple background purplebackground"],
  pink_background: ["분홍색 배경", "Pink background pinkbackground"],
  red_background: ["빨간색 배경", "Red background redbackground"],
};

/** A color's words ("red", "red_background", "default_background"). */
export function colorWords(color: string): SearchWords | undefined {
  const words = COLOR_WORDS[color];
  return words ? { ko: words[0], en: words[1] } : undefined;
}

// The backgrounds' group words, in the "/" menu and the "⋮⋮" menu alike: no
// "background color" stands in a row's line, so that query ranks the rows
// (COLOR_WORDS) where "color background" and "color b" find them all alike,
// before the text colors; "배경색" finds them by its letters alone, and with
// another word ("배경색 회색", "배경색 color") nothing (Notion's, seen
// 2026-11-21 in both menus on a Korean screen); "배경 색상", their shown head,
// finds none.
const BACKGROUND_GROUP: SearchWords = { ko: "배경", en: "Color background" };

// The block menu's last used color row and its list, found by words of their
// own (seen 2026-11-21 in Notion's "⋮⋮" menu on a Korean screen): the row by
// "하이라이트" and "highlight", under "마지막으로 사용한 색" and "color last
// used" — "마지막", "사용한", "last" and "used" find it, its shown head "최근
// 사용" and "recent" don't. A screen in another language finds the lists by
// their shown heads and the English words.
const RECENT_COLOR: SearchWords = { ko: "하이라이트", en: "Highlight" };
const RECENT_COLOR_LIST: SearchWords = { ko: "마지막으로 사용한 색", en: "Color last used" };

/** The last used color row's own word: Korean in Korean, else English. */
export function recentColorWord() {
  return searchWordsLanguage() === "ko" && RECENT_COLOR.ko ? RECENT_COLOR.ko : RECENT_COLOR.en;
}

function listWords({ ko, en }: SearchWords, shown: string) {
  const language = searchWordsLanguage();
  return language === "ko" && ko ? [ko, en] : language === "en" ? [en] : [shown, en];
}

/** The words of the list the last used color stands in, its head `shown`. */
export const recentColorListWords = (shown: string) => listWords(RECENT_COLOR_LIST, shown);

/** The words of the backgrounds' list, its head `shown`. */
export const backgroundListWords = (shown: string) => listWords(BACKGROUND_GROUP, shown);

// The "/" rows that make a block or act on the line, by Leafen's item key. A
// block's row ends with its Markdown hint, or a space (`tail`); an embed
// service's words are its name in every language, "link preview" before
// those that draw a preview.
const block = (ko: string | undefined, en: string, tail = ""): SearchWords => ({ ko, en, tail });
const service = (name: string, preview = false) => block(undefined, preview ? `link preview ${name}` : name);
const turned = (key: string, tail?: string) => block(TURN_WORDS[key]![0], TURN_WORDS[key]![1], tail);
const SLASH_WORDS: Readonly<Record<string, SearchWords>> = {
  paragraph: turned("paragraph"),
  heading_1: turned("heading_1", "#"),
  heading_2: turned("heading_2", "##"),
  heading_3: turned("heading_3", "###"),
  heading_4: turned("heading_4", "####"),
  bulleted_list_item: turned("bulleted_list_item", "-"),
  numbered_list_item: turned("numbered_list_item", "1."),
  to_do: turned("to_do", "[]"),
  toggle: turned("toggle", ">"),
  child_page: turned("child_page"),
  callout: turned("callout"),
  quote: turned("quote", "\""),
  simple_table: block("표", "Table"),
  divider: block("수평선 구분선 섹션 구분", "Horizontal rule divider line section separator hr", "---"),
  link_to_page: block("페이지 링크 ltp", "Link to page"),
  image: block("이미지 사진 GIF JPG", "Image picture gif Giphy photo img"),
  video: block("동영상", "Video"),
  audio: block("오디오 사운드 음악", "Audio sound music"),
  code: turned("code", "```"),
  file: block("파일 업로드 첨부", "File upload attach"),
  bookmark: block("웹 링크 북마크", "Web link bookmark"),
  inline_database_table: block("표 보기", "Table view"),
  inline_database_board: block("보드 보기 칸반", "Board view kanban"),
  inline_database_gallery: block("갤러리 보기", "Gallery view"),
  inline_database_list: block("리스트 보기", "List view"),
  inline_database_feed: block("피드 보기", "Feed view"),
  inline_database_dashboard: block("대시보드 보기", "Create a dashboard view"),
  inline_database_calendar: block("캘린더 보기", "Calendar view"),
  inline_database_timeline: block("타임라인 보기", "Timeline view"),
  inline_database_map: block("지도 보기", "Map view"),
  chart_bar: block("세로 막대 차트", "Vertical bar chart"),
  chart_horizontal_bar: block("가로 막대 차트", "Horizontal bar chart"),
  chart_line: block("꺾은선 차트", "Line chart"),
  chart_donut: block("도넛 차트", "Donut chart"),
  chart_number: block("숫자 차트", "Number chart"),
  inline_database_form: block("폼", "Form"),
  inline_database: block("데이터베이스 인라인 db", "Database inline db"),
  child_database: block("데이터베이스 전체 페이지 db", "Database full page db"),
  inline_database_linked: block("데이터 소스 보기 연결 db lv", "Create linked view of data source db"),
  table_of_contents: block("TOC 목차", "TOC table of contents"),
  equation: turned("equation"),
  button: block("버튼 자동화 템플릿 복제", "Button automation template duplicate"),
  breadcrumb: block("이동 경로", "Breadcrumb"),
  tab: block("탭 스위치 토글", "Tabs tab switch toggle"),
  synced_block: turned("synced_block"),
  toggle_heading_1: turned("toggle_heading_1", "# >"),
  toggle_heading_2: turned("toggle_heading_2", "## >"),
  toggle_heading_3: turned("toggle_heading_3", "### >"),
  column_list_2: turned("column_list:2"),
  column_list_3: turned("column_list:3"),
  column_list_4: turned("column_list:4"),
  column_list_5: turned("column_list:5"),
  code_mermaid: block("머메이드 다이어그램 순서도 플로차트 그래프 graph 코드", "Mermaid diagram flowchart graphviz code"),
  inline_person: { ko: "사용자 멘션하기", en: "Mention a person" },
  inline_page: { ko: "멘션하는 페이지 데이터 소스 입력", en: "Mention a page or data source" },
  inline_date: { ko: "날짜 또는 리마인더", en: "Date or reminder" },
  inline_emoji: { ko: "이모지", en: "Emoji" },
  inline_equation: { ko: "레이텍스 인라인 수학 공식", en: "LaTeX math inline equation" },
  embed: block("iFrame 임베드", "Embed iFrame"),
  embed_html: block("HTML 아티팩트 임베드 웹사이트 페이지", "html artifact embed website page"),
  embed_google_drive: service("Google Drive", true),
  embed_google_maps: service("Google Maps"),
  embed_figma: service("Figma", true),
  embed_miro: service("Miro"),
  embed_excalidraw: service("Excalidraw"),
  pdf: service("PDF"),
  embed_loom: service("Loom"),
  embed_typeform: service("Typeform"),
  embed_codepen: service("CodePen"),
  embed_replit: service("Replit"),
  import_csv: { ko: "CSV 가져오기", en: "CSV import" },
  import_markdown: { ko: "텍스트 Markdown 가져오기", en: "Text Markdown import" },
  import_word: { ko: "Word 가져오기", en: "Word import" },
  import_zip: { ko: "일반 범용 ZIP 가져오기", en: "Generic universal ZIP import" },
  import_pdf: { ko: "PDF 가져오기", en: "PDF import" },
  action_copy_link: { ko: "블록 링크 복사", en: "Copy link to block" },
  action_duplicate: { ko: "복제", en: "Duplicate" },
  action_move_to: { ko: "옮기기", en: "Move to" },
  action_delete: { ko: "삭제", en: "Delete" },
  action_present: { ko: "여기에서 발표", en: "Present from here" },
};

/** A "/" row's words by Leafen's item key (blockDefKey), if it has its own. */
export function slashWords(key: string): SearchWords | undefined {
  return SLASH_WORDS[key];
}

// The words of the "/" menu's groups, Korean and English.
const SLASH_GROUP_WORDS: Readonly<Record<string, SearchWords>> = {
  Basic: { ko: "기본 블록", en: "Basic blocks" },
  Media: { ko: "미디어", en: "Media" },
  Database: { ko: "데이터베이스", en: "Database" },
  Advanced: { ko: "고급 블록", en: "Advanced blocks" },
  Inline: { ko: "인라인", en: "Inline" },
  Embed: { ko: "임베드", en: "Embeds" },
  Import: { ko: "가져오기", en: "Import" },
  TurnInto: { ko: "전환", en: "Turn into" },
  Actions: { ko: "작업", en: "Actions" },
  TextColor: { ko: "텍스트 색상", en: "Text color" },
  BackgroundColor: BACKGROUND_GROUP,
};

/** A "/" group's words, if it has its own. */
export function slashGroupWords(group: string): SearchWords | undefined {
  return SLASH_GROUP_WORDS[group];
}
