package app

// workspaceExportDocumentCSS styles exported HTML pages as Notion's HTML
// export draws them (rules read from the styles that took effect in its
// export, 2026-11-07): one 900px column with a 2em top, text at 1.5 lines
// keeping its own spaces and line breaks (pre-wrap, 2026-11-14),
// the title 2.5rem bold, each list item its own list 0.6em apart, a done
// to-do struck through and faded, links in the text's color underlined,
// databases as content-wide tables in 0.875rem with gray headers and faint
// lines, simple tables, contents, columns and callouts as Notion's classes
// draw them (an emoji icon from its attribute, a page's icon in its header's
// box over its cover's foot, the cover the column's width with no margin or
// rounding, 2026-11-14), text and block colors in its export's colors (2026-11-14: green
// named teal, a text's background lighter than a block's),
// and they print cleanly: within the page's inch of margin, a
// database fills the width with fixed columns whose words, people and times
// wrap, where on screen a person and a time stay on one line (Notion's print
// rules, seen again 2026-11-12).
const workspaceExportDocumentCSS = `
:root{color-scheme:light;--text:rgb(55,53,47);--ink:rgb(44,44,43);--muted:rgba(55,53,47,.6);--line:rgba(55,53,47,.09);--soft:#f7f6f3}
*{box-sizing:border-box}
body{margin:2em auto;max-width:900px;padding:0;color:var(--text);background:#fff;line-height:1.5;white-space:pre-wrap;
font-family:ui-sans-serif,-apple-system,BlinkMacSystemFont,"Segoe UI Variable Display","Segoe UI","Apple SD Gothic Neo",
"Noto Sans KR",Helvetica,"Apple Color Emoji",Arial,sans-serif,"Segoe UI Emoji","Segoe UI Symbol";word-wrap:break-word}
.page+.page{break-before:page;margin-top:2em}
.page-title,.page-body{color:var(--ink)}
.page-cover-image{display:block;object-fit:cover;width:100%;max-height:30vh}
.page-header-icon{font-size:3rem;margin-bottom:1rem}
.page-header-icon-with-cover{margin-top:-.72em;margin-inline-start:.07em}
.page-header-icon img{border-radius:3px}
h1,h2,h3{letter-spacing:-.01em;line-height:1.2;font-weight:600;margin-bottom:0}
h1{font-size:1.875rem;margin-top:1.875rem}h2{font-size:1.5rem;margin-top:1.5rem}h3{font-size:1.25rem;margin-top:1.25rem}
.page-title{font-size:2.5rem;font-weight:700;margin-top:0;margin-bottom:.75em}
p{margin:.5em 0}
.page-description{margin-bottom:2em}
a{color:inherit;text-decoration:underline}
.indented{padding-left:1.5em}
ol,ul{margin:.6em 0}
ul{padding-inline-start:1.7em}ul>li{list-style:disc;padding-left:.1em}
ol{padding-inline-start:1.6em}ol>li{padding-left:.2em}
ul.to-do-list{padding-inline-start:0}ul.to-do-list>li{list-style:none}
input[type="checkbox"]{margin-right:.4em;vertical-align:middle}
.checkbox{display:inline-flex;vertical-align:text-bottom;width:16px;height:16px;margin-left:2px;margin-right:5px}
.to-do-children-checked{text-decoration:line-through;opacity:.375}
summary{cursor:pointer}
summary>h1,summary>h2,summary>h3,summary>h4{display:inline;margin:0}
blockquote{font-size:1em;margin:1em 0;padding-left:1em;border-left:3px solid var(--text)}
hr{display:block;width:100%;height:1px;border:0;border-bottom:1px solid var(--line);background:transparent}
.code,code{background:rgba(135,131,120,.15);border-radius:3px;padding:.2em .4em;font-size:85%;tab-size:2}
code{color:#eb5757}
.code{padding:1.5em 1em;font-family:SFMono-Regular,Menlo,Consolas,"PT Mono","Liberation Mono",Courier,monospace}
.code-wrap{white-space:pre-wrap;word-break:break-all}
.code>code{background:none;padding:0;font-size:100%;color:inherit}
figure{margin:1.25em 0;break-inside:avoid}
figcaption{opacity:.5;font-size:85%;margin-top:.5em}
figure.image img{max-width:100%;height:auto}
.image{border:none;margin:1.5em 0;padding:0;border-radius:0;text-align:center}
.source{border:1px solid #ddd;border-radius:3px;padding:1.5em;word-break:break-all}
.bookmark{text-decoration:none;max-height:8em;padding:0;display:flex;width:100%;align-items:stretch}
.bookmark-info{flex:4 1 180px;padding:12px 14px 14px;display:flex;flex-direction:column;justify-content:space-between}
.bookmark-text{display:flex;flex-direction:column}
.bookmark-title{font-size:.85em;overflow:hidden;text-overflow:ellipsis;height:1.75em;white-space:nowrap}
.bookmark-description{color:var(--muted);font-size:.75em;overflow:hidden;max-height:4.5em;word-break:break-word}
.bookmark-href{font-size:.75em;margin-top:.25em}
.bookmark-image{width:33%;flex:1 1 180px;display:block;position:relative;object-fit:cover;border-radius:1px}
.callout{border-radius:10px;padding:1rem;-webkit-box-decoration-break:clone;box-decoration-break:clone}
.callout.block-color-default_background{border:1px solid var(--line)}
details,aside{margin:1.25em 0}
.icon[data-emoji]::before{content:attr(data-emoji)}
img.icon{border-radius:3px}
.callout img.notion-static-icon{width:1em;height:1em}
.callout p{margin:0}
.callout h1,.callout h2,.callout h3{margin:0 0 .6rem}
.link-to-page{margin:1em 0;padding:0;border:none;font-weight:500}
.column-list{display:flex;gap:46px}
.column{min-width:0;overflow:hidden}
.column>*:first-child{margin-top:0}
table,th,td{border:1px solid var(--line)}
table{border-collapse:collapse;border-left:none;border-right:none}
th,td{font-weight:normal;padding:.25em .5em;line-height:1.5;min-height:1.5em;text-align:left}
th{color:var(--muted)}
.simple-table{margin-top:1em;font-size:.875rem;empty-cells:show}
.simple-table td,.simple-table th{height:29px;min-width:120px}
.simple-table-header-color{background:rgb(247,246,243);color:black}
.simple-table-header{font-weight:500}
.collection-content{font-size:.875rem}
.collection-content td{white-space:pre-wrap;word-break:break-word}
.collection-content-wrapper{overflow-x:auto}
.collection-title{display:inline-block;margin-right:1em;color:var(--text)}
.icon{display:inline-flex;align-items:center;justify-content:center;max-width:1.2em;max-height:1.2em;text-decoration:none;
vertical-align:text-bottom;margin-right:.5em}
.selected-value{display:inline-block;padding:0 .5em;border-radius:3px;background:rgba(206,205,202,.5);margin:.3em .5em .3em 0;white-space:nowrap}
.select-value-color-default{background-color:rgba(42,28,0,.07)}.select-value-color-gray{background-color:rgba(28,19,1,.11)}
.select-value-color-brown{background-color:rgba(127,51,0,.156)}.select-value-color-orange{background-color:rgba(196,88,0,.203)}
.select-value-color-yellow{background-color:rgba(209,156,0,.282)}.select-value-color-green{background-color:rgba(0,96,38,.156)}
.select-value-color-blue{background-color:rgba(0,118,217,.203)}.select-value-color-purple{background-color:rgba(92,0,163,.141)}
.select-value-color-pink{background-color:rgba(183,0,78,.152)}.select-value-color-red{background-color:rgba(206,24,0,.164)}
time{opacity:.5}
td>.user,td>time{white-space:nowrap}
.user-icon{width:1.5em;height:1.5em;border-radius:100%;margin-inline-end:.5rem}.user-icon-inner{font-size:.8em}
.text-icon{border:1px solid #000;text-align:center}
p>.user{opacity:.5}
.equation,.notion-text-equation-token{font-family:"Times New Roman",serif}
.table_of_contents-item{display:block;font-size:.875rem;line-height:1.3;padding-inline:.125rem;padding-block:.375rem}
.table_of_contents-indent-1{margin-inline-start:1.5rem}
.table_of_contents-indent-2{margin-inline-start:3rem}
.table_of_contents-indent-3{margin-inline-start:4.5rem}
.table_of_contents-link{text-decoration:none;opacity:.7;border-bottom:1px solid rgba(55,53,47,.18)}
.breadcrumb{color:var(--muted)}
.button span{display:inline-block;padding:2px 10px;border:1px solid var(--line);border-radius:4px}
.tab{border-top:1px solid var(--line);padding-top:4px}
.underline{text-decoration:underline}
.comments{margin-top:48px;border-top:1px solid var(--line);padding-top:8px}
.comments h2{font-size:1.1em}
.discussion{border-left:2px solid #ffd966;padding-left:12px;margin:16px 0}
.discussion.resolved{border-left-color:var(--line)}
.comment-meta{color:var(--muted);font-size:.85em}
.comment-status{font-style:italic}
.highlight-gray,.block-color-gray{color:rgb(125,122,117);fill:rgb(125,122,117)}
.highlight-brown,.block-color-brown{color:rgb(159,118,90);fill:rgb(159,118,90)}
.highlight-orange,.block-color-orange{color:rgb(210,123,45);fill:rgb(210,123,45)}
.highlight-yellow,.block-color-yellow{color:rgb(203,148,52);fill:rgb(203,148,52)}
.highlight-teal,.block-color-teal{color:rgb(80,148,110);fill:rgb(80,148,110)}
.highlight-blue,.block-color-blue{color:rgb(56,125,201);fill:rgb(56,125,201)}
.highlight-purple,.block-color-purple{color:rgb(154,107,180);fill:rgb(154,107,180)}
.highlight-pink,.block-color-pink{color:rgb(193,76,138);fill:rgb(193,76,138)}
.highlight-red,.block-color-red{color:rgb(207,81,72);fill:rgb(207,81,72)}
mark{color:inherit;background:none}
[class*="_background"]{border-radius:3px}
.highlight-gray_background{background:rgba(42,28,0,.07)}
.highlight-brown_background{background:rgba(139,46,0,.086)}
.highlight-orange_background{background:rgba(224,101,1,.129)}
.highlight-yellow_background{background:rgba(211,168,0,.137)}
.highlight-teal_background{background:rgba(0,100,45,.09)}
.highlight-blue_background{background:rgba(0,124,215,.094)}
.highlight-purple_background{background:rgba(102,0,178,.078)}
.highlight-pink_background{background:rgba(197,0,93,.086)}
.highlight-red_background{background:rgba(223,22,0,.094)}
.block-color-gray_background{background:rgb(240,239,237)}
.block-color-brown_background{background:rgb(245,237,233)}
.block-color-orange_background{background:rgb(251,235,222)}
.block-color-yellow_background{background:rgb(249,243,220)}
.block-color-teal_background{background:rgb(232,241,236)}
.block-color-blue_background{background:rgb(229,242,252)}
.block-color-purple_background{background:rgb(243,235,249)}
.block-color-pink_background{background:rgb(250,233,241)}
.block-color-red_background{background:rgb(252,233,231)}
@media print{body{max-width:none;margin:0}img{max-height:100vh;object-fit:contain}
table.collection-content{width:100%;table-layout:fixed}table.collection-content th,table.collection-content td{overflow-wrap:anywhere}
table.collection-content td>.user,table.collection-content td>time{white-space:pre-wrap}
.collection-content-wrapper{overflow-x:visible}details>summary{list-style:none}}
@page{margin:1in}
`
