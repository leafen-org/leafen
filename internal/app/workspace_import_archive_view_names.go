package app

import (
	"slices"
	"strings"
)

// Before migration 817 a server kept a view the app named after its layout,
// in its maker's language, under that name without the namedAsLayout mark,
// and the app read it as never named; since then a view never named is kept
// as "" (viewNeverNamed) and every name is the one given. No data a Leafen
// opens holds a view by the earlier rule (the schema baseline takes 817 in),
// so an older archive is the one input that brings such views, and they
// are read by today's rule as they come in, once.
// An archive holds its views as the server that made it kept them: version 12
// and after by today's rule, and 10 and 11 by the one before, made by a
// server before the migration or by another tool writing to that rule
// (docs/bundle.md). Brought in as they are, such an archive's views still
// called their layout's name came in named so, and their name field and
// every reader's tab showed "표" (2026-11-17). Each view an older archive
// brings, on its own or with a block in the trash, comes in as the
// migration made the ones it found: still called its layout's name without
// the mark, it is never named.

// workspaceImportArchiveLayoutNamedViewsFormatVersion is the last archive
// whose views are named by the rule before migration 817.
const workspaceImportArchiveLayoutNamedViewsFormatVersion = 11

// nameWorkspaceImportArchiveViewsAsNow reads the views an archive entity of
// formatVersion holds by today's rule, in place.
func nameWorkspaceImportArchiveViewsAsNow(formatVersion int, entity workspaceImportArchiveEntity) {
	if formatVersion > workspaceImportArchiveLayoutNamedViewsFormatVersion {
		return
	}
	switch entity.Kind {
	case workspaceExportItemView:
		unnameWorkspaceImportArchiveLayoutNamedView(entity.Value)
	case workspaceExportItemBlockTrash:
		snapshot, _ := entity.Value["snapshot"].(map[string]any)
		views, _ := snapshot["views"].([]any)
		for _, view := range views {
			if value, ok := view.(map[string]any); ok {
				unnameWorkspaceImportArchiveLayoutNamedView(value)
			}
		}
	}
}

func unnameWorkspaceImportArchiveLayoutNamedView(view map[string]any) {
	viewType, _ := view["type"].(string)
	name, _ := view["name"].(string)
	config, _ := view["config"].(map[string]any)
	if nativeViewNamedAsLayout(viewType, name, config) {
		view["name"] = ""
	}
}

// nativeViewNamedAsLayoutKey is the mark the app and the API set, under the
// rule before migration 817, on a view a person named its layout's name.
const nativeViewNamedAsLayoutKey = "namedAsLayout"

// nativeViewLayoutNames are each layout's name in every language the app
// shipped when migration 817 was written, the server's "Table" and "표" among
// them: the names an older archive's never-named views carry.
var nativeViewLayoutNames = map[string][]string{
	graphViewTypeTable: {
		"Table", "표", "Tabel", "ሠንጠረዥ", "جدول", "Cədvəl", "Таблица", "টেবিল", "Tabulka", "Tabelle",
		"Πίνακας", "Tabla", "Taulukko", "ટેબલ", "Tebur", "טבלה", "टेबल", "Tablica", "Táblázat",
		"Tabella", "テーブル", "Кесте", "តារាង", "ಕೋಷ್ಟಕ", "പട്ടിക", "तक्ता", "Jadual", "ဇယား", "Tabell",
		"तालिका", "ਟੇਬਲ", "Tabela", "වගුව", "Tabuľka", "Табела", "Jedwali", "அட்டவணை", "టేబుల్",
		"ตาราง", "Tablo", "Таблиця", "ٹیبل", "Jadval", "Bảng tính", "表格",
	},
	graphViewTypeBoard: {
		"Board", "보드", "Bord", "ቦርድ", "لوحة", "Lövhə", "Канбан", "বোর্ড", "Nástěnka", "Tavle",
		"Πίνακας", "Tablero", "تخته", "Taulu", "Kanban", "બોર્ડ", "Allo", "לוח", "बोर्ड", "Ploča",
		"Tábla", "Papan", "Bacheca", "ボード", "Тақта", "ក្តារ", "ಬೋರ್ಡ್", "ബോർഡ്", "ဘုတ်", "ਬੋਰਡ",
		"Tablica", "Quadro", "Tablă", "පුවරුව", "Tabuľa", "Табла", "Tavla", "Bodi", "பலகை", "బోర్డ్",
		"บอร์ด", "Pano", "Дошка", "بورڈ", "Doska", "Bảng", "看板",
	},
	graphViewTypeGallery: {
		"Gallery", "갤러리", "Galery", "ጋለሪ", "معرض الصور", "Qalereya", "Галерия", "গ্যালারি", "Galerie",
		"Galleri", "Συλλογή", "Galería", "گالری", "Galleria", "ગેલેરી", "Galeri", "גלריה", "गैलरी",
		"Galerija", "Galéria", "ギャラリー", "Галерея", "វិចិត្រសាល", "ಗ್ಯಾಲರಿ", "ഗാലറി", "गॅलरी",
		"ပြခန်း", "ग्यालरी", "ਗੈਲਰੀ", "Galeria", "ගැලරිය", "Галерија", "கேலரி", "గ్యాలరీ", "แกลเลอรี",
		"گیلری", "Galereya", "Thư viện", "画廊", "圖庫",
	},
	graphViewTypeList: {
		"List", "리스트", "Lys", "ዝርዝር", "قائمة", "Siyahı", "Списък", "তালিকা", "Seznam", "Liste",
		"Λίστα", "Lista", "فهرست", "Listahan", "લિસ્ટ", "Jeri", "רשימה", "लिस्ट", "Popis", "Daftar",
		"Elenco", "リスト", "Тізім", "បញ្ជី", "ಪಟ್ಟಿ", "ലിസ്റ്റ്", "यादी", "Senarai", "စာရင်း", "सूची",
		"Lijst", "ਸੂਚੀ", "Listă", "Список", "ලැයිස්තුව", "Zoznam", "Листа", "Orodha", "பட்டியல்",
		"జాబితా", "รายการ", "فہرست", "Ro'yxat", "Danh sách", "列表",
	},
	graphViewTypeTimeline: {
		"Timeline", "타임라인", "Tydlyn", "የጊዜ መስመር", "خط زمني", "Zaman xətti", "Хронология", "টাইমলাইন",
		"Časová osa", "Tidslinje", "Zeitleiste", "Χρονολόγιο", "Cronograma", "تایم‌لاین", "Aikajana",
		"Chronologie", "ટાઇમલાઇન", "Layin lokaci", "ציר זמן", "टाइमलाइन", "Vremenska crta",
		"Idővonal", "Lini Masa", "タイムライン", "Уақыт сызығы", "បន្ទាត់ពេលវេលា", "ಟೈಮ್ಲೈನ್", "ടൈംലൈൻ",
		"Garis masa", "အချိန်လိုင်း", "Tijdlijn", "ਟਾਈਮਲਾਈਨ", "Oś czasu", "Linha do tempo",
		"Cronologie", "Таймлайн", "කාලරේඛාව", "Časová os", "Časovnica", "Временска линија",
		"Mstari wa muda", "காலவரிசை", "టైమ్లైన్", "ไทม์ไลน์", "Zaman çizelgesi", "ٹائم لائن",
		"Vaqt chizig'i", "Dòng thời gian", "时间轴", "時程表",
	},
	graphViewTypeCalendar: {
		"Calendar", "캘린더", "Kalender", "ካላንደር", "التقويم", "Təqvim", "Календар", "ক্যালেন্ডার",
		"Kalendář", "Ημερολόγιο", "Calendario", "تقویم", "Kalenteri", "Kalendaryo", "Calendrier",
		"કેલેન્ડર", "Kalanda", "לוח שנה", "कैलेंडर", "Kalendar", "Naptár", "カレンダー", "Күнтізбе",
		"ប្រតិទិន", "ಕ್ಯಾಲೆಂಡರ್", "കലണ്ടർ", "कॅलेंडर", "ပြက္ခဒိန်", "पात्रो", "ਕੈਲੰਡਰ", "Kalendarz",
		"Calendário", "Календарь", "දින දර්ශනය", "Kalendár", "Koledar", "Kalenda", "நாட்காட்டி",
		"క్యాలెండర్", "ปฏิทิน", "Takvim", "کیلنڈر", "Lịch", "日历", "日曆",
	},
	graphViewTypeForm: {
		"Form", "폼", "Vorm", "ቅጽ", "نموذج", "Формуляр", "ফর্ম", "Formulář", "Formular", "Φόρμα",
		"Formulario", "فرم", "Lomake", "Formulaire", "ફોર્મ", "Fom", "טופס", "फ़ॉर्म", "Obrazac",
		"Űrlap", "Formulir", "Modulo", "フォーム", "Пішін", "ទម្រង់", "ಫಾರ್ಮ್", "ഫോം", "फॉर्म", "Borang",
		"ဖောင်", "Skjema", "फारम", "Formulier", "ਫਾਰਮ", "Formularz", "Formulário", "Форма", "පෝරමය",
		"Formulár", "Obrazec", "Образац", "Formulär", "Fomu", "படிவம்", "ఫారమ్", "แบบฟอร์ม", "فارم",
		"Shakl", "Biểu mẫu", "表单", "表單",
	},
	graphViewTypeChart: {
		"Chart", "차트", "Grafiek", "ገበታ", "مخطّط", "Diaqram", "Диаграма", "চার্ট", "Graf", "Diagram",
		"Diagramm", "Γράφημα", "Gráfico", "نمودار", "Kaavio", "Tsart", "Graphique", "ચાર્ટ",
		"Jadawali", "תרשים", "चार्ट", "Grafikon", "Bagan", "Grafico", "チャート", "Диаграмма", "គំនូសតាង",
		"ಚಾರ್ಟ್", "ചാർട്ട്", "Carta", "ဂရပ်", "ਚਾਰਟ", "Wykres", "Grafic", "ප්‍රස්තාරය", "Графикон",
		"Chati", "விளக்கப்படம்", "చార్ట్", "แผนภูมิ", "Grafik", "Діаграма", "چارٹ", "Diagramma",
		"Biểu đồ", "图表", "圖表",
	},
	graphViewTypeDashboard: {
		"Dashboard", "대시보드", "ዳሽቦርድ", "لوحة المعلومات", "Табло", "ড্যাশবোর্ড", "Nástěnka",
		"Πίνακας ελέγχου", "Panel de control", "داشبورد", "Koontinäyttö", "ડેશબોર્ડ", "Dashbod",
		"לוח בקרה", "डैशबोर्ड", "Nadzorna ploča", "Irányítópult", "Dasbor", "ダッシュボード", "ಡ್ಯಾಶ್ಬೋರ್ಡ್",
		"ഡാഷ്ബോർഡ്", "डॅशबोर्ड", "Papan pemuka", "Dashbord", "ड्यासबोर्ड", "ਡੈਸ਼ਬੋਰਡ", "Pulpit",
		"Painel", "Tablou de bord", "Дашборд", "උපකරණ පුවරුව", "Prehľad", "Nadzorna plošča",
		"Контролна табла", "Kontrollpanel", "Dashibodi", "டாஷ்போர்டு", "డాష్బోర్డ్", "แดชบอร์ด",
		"Pano", "ڈیش بورڈ", "Bảng điều khiển", "管理面板", "儀表板",
	},
	graphViewTypeFeed: {
		"Feed", "피드", "Stroom", "ፊድ", "تغذية", "Поток", "ফিড", "Kanál", "Ροή", "فید", "Syöte",
		"Fil d’actualité", "ફીડ", "Ciyarwa", "פיד", "फ़ीड", "Tijek", "Hírfolyam", "フィード", "ಫೀಡ್",
		"ഫീഡ്", "फीड", "Suapan", "ਫੀਡ", "Kanał", "Flux", "Лента", "ෆීඩ්", "Vir", "Ток", "Flöde",
		"Mlisho", "ஊட்டம்", "ఫీడ్", "ฟีด", "Akış", "Стрічка", "فیڈ", "Bảng tin", "动态", "動態",
	},
	graphViewTypeMap: {
		"Map", "지도", "Kaart", "ካርታ", "خريطة", "Карта", "ম্যাপ", "Mapa", "Kort", "Karte", "Χάρτης",
		"نقشه", "Kartta", "Carte", "નકશો", "Taswira", "מפה", "मैप", "Karta", "Térkép", "Peta",
		"Mappa", "地図", "ನಕ್ಷೆ", "മാപ്പ്", "नकाशा", "Kart", "नक्सा", "ਨਕਸ਼ਾ", "Hartă", "සිතියම",
		"Zemljevid", "Мапа", "Ramani", "வரைபடம்", "మ్యాప్", "แผนที่", "Harita", "نقشہ", "Bản đồ",
		"地图", "地圖",
	},
}

// nativeViewNamedAsLayout says whether a view of this layout, name and
// settings was read as never named under the rule before migration 817.
func nativeViewNamedAsLayout(viewType, name string, config map[string]any) bool {
	return config[nativeViewNamedAsLayoutKey] != true && slices.Contains(nativeViewLayoutNames[viewType], strings.TrimSpace(name))
}
