package app

import (
	"encoding/binary"
	"encoding/hex"
	"io"
	"math"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// A PDF's words, page by page, with each font's codes read as a reader
// should read them (PDF 32000-1, 9.10): the font's own ToUnicode map first;
// for a composite font without one, a predefined CMap whose codes are
// Unicode (the UCS-2 and UTF-16 ones Korean, Japanese and Chinese documents
// name, such as UniKS-UCS2-H) or a legacy one of those languages (EUC-KR and
// UHC, Shift JIS, GBK, Big5); a simple font through the reader's own
// encodings. A composite font whose codes say nothing of their characters
// (Identity-H without ToUnicode) gives no words rather than wrong ones. The
// PDF library alone takes the font's encoding name before its ToUnicode,
// passes unknown CMaps' bytes through as text, and keeps one font per name
// across pages.

type pdfTextDecoder func(raw string) string

func pdfPlainText(reader *pdf.Reader, limit int) string {
	var text strings.Builder
	runes := 0
	for number := 1; number <= reader.NumPage() && runes < limit; number++ {
		page := reader.Page(number)
		if page.V.IsNull() || page.V.Key("Contents").Kind() == pdf.Null {
			continue
		}
		decoders := map[string]pdfTextDecoder{}
		decode := pdfTextDecoder(func(string) string { return "" })
		lines := pdfTextLines{a: 1, d: 1}
		started := false
		// A TJ's strings are one run: they join as they stand but at a gap.
		show := func(raw string, continued bool) {
			words := decode(raw)
			if words == "" {
				return
			}
			switch {
			case !started:
				if text.Len() > 0 {
					text.WriteByte('\n')
				}
			case lines.moved():
				text.WriteByte('\n')
			case lines.gap || !continued && pdfRunsNeedSpace(text.String(), words):
				if !pdfEndsInSpace(text.String()) && !strings.HasPrefix(words, " ") {
					text.WriteByte(' ')
				}
			}
			started, lines.gap = true, false
			lines.shown()
			runes += utf8.RuneCountInString(words)
			text.WriteString(words)
		}
		pdf.Interpret(page.V.Key("Contents"), func(stack *pdf.Stack, op string) {
			args := make([]pdf.Value, stack.Len())
			for index := len(args) - 1; index >= 0; index-- {
				args[index] = stack.Pop()
			}
			switch op {
			case "BT":
				lines.begin()
			case "Td", "TD":
				if len(args) == 2 {
					lines.move(args[0].Float64(), args[1].Float64(), op == "TD")
				}
			case "Tm":
				if len(args) == 6 {
					lines.set(args[0].Float64(), args[1].Float64(), args[2].Float64(), args[3].Float64(), args[4].Float64(), args[5].Float64())
				}
			case "TL":
				if len(args) == 1 {
					lines.leading = args[0].Float64()
				}
			case "T*":
				lines.next()
			case "Tf":
				if len(args) != 2 {
					return
				}
				name := args[0].Name()
				if _, read := decoders[name]; !read {
					decoders[name] = pdfFontDecoder(page.Font(name))
				}
				decode = decoders[name]
			case "Tj":
				if len(args) == 1 {
					show(args[0].RawString(), false)
				}
			case "'":
				lines.next()
				if len(args) == 1 {
					show(args[0].RawString(), false)
				}
			case "\"":
				lines.next()
				if len(args) == 3 {
					show(args[2].RawString(), false)
				}
			case "TJ":
				if len(args) == 1 {
					for index := 0; index < args[0].Len(); index++ {
						part := args[0].Index(index)
						switch part.Kind() {
						case pdf.String:
							show(part.RawString(), index > 0)
						case pdf.Integer, pdf.Real:
							// A move back of more than a quarter of the font's
							// size is a gap between words.
							if part.Float64() < -250 {
								lines.gap = true
							}
						}
					}
				}
			}
		})
	}
	return text.String()
}

// A PDF writes a line's words as runs, each at its own place — LibreOffice a
// run for every font, so a period in another font is a run of its own — and
// spaces as glyphs within the runs. A new line starts where the baseline
// moves, and a line's runs join as they stand; a space goes between runs only
// where two words would otherwise touch (2026-10-16: a text object a line
// put each run on a line of its own, read as "있습니다 . 둘째 문단 :").
type pdfTextLines struct {
	a, b, c, d, e, f float64
	leading          float64
	lastY            float64
	gap              bool
}

func (lines *pdfTextLines) begin() {
	lines.a, lines.b, lines.c, lines.d, lines.e, lines.f = 1, 0, 0, 1, 0, 0
}

func (lines *pdfTextLines) set(a, b, c, d, e, f float64) {
	lines.a, lines.b, lines.c, lines.d, lines.e, lines.f = a, b, c, d, e, f
}

func (lines *pdfTextLines) move(tx, ty float64, setLeading bool) {
	if setLeading {
		lines.leading = -ty
	}
	lines.e, lines.f = tx*lines.a+ty*lines.c+lines.e, tx*lines.b+ty*lines.d+lines.f
}

func (lines *pdfTextLines) next() { lines.move(0, -lines.leading, false) }

// moved says the baseline is not where the last run stood.
func (lines *pdfTextLines) moved() bool { return math.Abs(lines.f-lines.lastY) > 2 }

func (lines *pdfTextLines) shown() { lines.lastY = lines.f }

func pdfEndsInSpace(text string) bool {
	last, _ := utf8.DecodeLastRuneInString(text)
	return unicode.IsSpace(last)
}

// pdfRunsNeedSpace says two runs on a line would join two words: a run
// starting with closing punctuation, or after opening punctuation, joins.
func pdfRunsNeedSpace(before, run string) bool {
	last, _ := utf8.DecodeLastRuneInString(before)
	first, _ := utf8.DecodeRuneInString(run)
	if unicode.IsSpace(last) || unicode.IsSpace(first) {
		return false
	}
	if strings.ContainsRune(".,:;!?)]}%»”’」』】〉》、。，．：；！？）…", first) || strings.ContainsRune("([{«“‘「『【〈《", last) {
		return false
	}
	return true
}

// pdfFontDecoder reads a font's codes as text.
func pdfFontDecoder(font pdf.Font) pdfTextDecoder {
	if toUnicode := font.V.Key("ToUnicode"); toUnicode.Kind() == pdf.Stream {
		if cmap := readPDFToUnicode(toUnicode); cmap != nil {
			return cmap.decode
		}
	}
	if font.V.Key("Subtype").Name() != "Type0" {
		encoder := font.Encoder()
		return encoder.Decode
	}
	name := font.V.Key("Encoding").Name()
	switch {
	case strings.HasPrefix(name, "Uni") && (strings.Contains(name, "UCS2") || strings.Contains(name, "UTF16")):
		return pdfUTF16Text
	case strings.HasPrefix(name, "Uni") && strings.Contains(name, "UTF32"):
		return pdfUTF32Text
	case strings.HasPrefix(name, "Uni") && strings.Contains(name, "UTF8"):
		return func(raw string) string { return strings.ToValidUTF8(raw, "") }
	}
	for prefix, legacy := range map[string]encoding.Encoding{
		"KSC": korean.EUCKR, "90ms-RKSJ": japanese.ShiftJIS, "90pv-RKSJ": japanese.ShiftJIS, "EUC-": japanese.EUCJP,
		"GBK": simplifiedchinese.GBK, "GBpc-EUC": simplifiedchinese.GBK, "GB-EUC": simplifiedchinese.GBK,
		"B5": traditionalchinese.Big5, "ETen-B5": traditionalchinese.Big5, "HKscs-B5": traditionalchinese.Big5,
	} {
		if strings.HasPrefix(name, prefix) {
			decoder := legacy.NewDecoder()
			return func(raw string) string {
				text, err := decoder.String(raw)
				if err != nil {
					return ""
				}
				return text
			}
		}
	}
	// Identity-H and the rest name glyphs, not characters.
	return func(string) string { return "" }
}

func pdfUTF16Text(raw string) string {
	units := make([]uint16, 0, len(raw)/2)
	for index := 0; index+1 < len(raw); index += 2 {
		units = append(units, binary.BigEndian.Uint16([]byte(raw[index:index+2])))
	}
	return string(utf16.Decode(units))
}

func pdfUTF32Text(raw string) string {
	var text strings.Builder
	for index := 0; index+3 < len(raw); index += 4 {
		if r := rune(binary.BigEndian.Uint32([]byte(raw[index : index+4]))); utf8.ValidRune(r) {
			text.WriteRune(r)
		}
	}
	return text.String()
}

// pdfToUnicode is a ToUnicode CMap: the code lengths its code space ranges
// allow, and each code's or range of codes' text.
type pdfToUnicode struct {
	spaces [4][][2]string
	chars  map[string]string
	ranges []pdfToUnicodeRange
}

type pdfToUnicodeRange struct {
	low, high string
	// The first code's text, counting up through the range, or each code's.
	start string
	each  []string
}

// readPDFToUnicode reads a ToUnicode CMap's code space ranges and its
// codes' text from the stream's own words, as a CMap writes them: hex
// strings, arrays of them, and the begin…end sections that hold them.
func readPDFToUnicode(stream pdf.Value) (cmap *pdfToUnicode) {
	defer func() {
		if recover() != nil {
			cmap = nil
		}
	}()
	data, err := io.ReadAll(io.LimitReader(stream.Reader(), pdfToUnicodeMaxBytes))
	if err != nil {
		return nil
	}
	cmap = &pdfToUnicode{chars: map[string]string{}}
	section := ""
	// The section's entries: a code, or an array of them.
	var entries []pdfCMapEntry
	var array []string
	inArray := false
	for _, token := range pdfCMapTokens(string(data)) {
		switch {
		case token.hex:
			if inArray {
				array = append(array, token.text)
			} else {
				entries = append(entries, pdfCMapEntry{code: token.text})
			}
		case token.text == "[":
			inArray, array = true, nil
		case token.text == "]":
			inArray = false
			entries = append(entries, pdfCMapEntry{array: array, isArray: true})
		case token.text == "begincodespacerange" || token.text == "beginbfchar" || token.text == "beginbfrange":
			section, entries = token.text, nil
		case token.text == "endcodespacerange" && section == "begincodespacerange":
			for index := 0; index+1 < len(entries); index += 2 {
				low, high := entries[index].code, entries[index+1].code
				if len(low) >= 1 && len(low) <= 4 && len(low) == len(high) {
					cmap.spaces[len(low)-1] = append(cmap.spaces[len(low)-1], [2]string{low, high})
				}
			}
			section = ""
		case token.text == "endbfchar" && section == "beginbfchar":
			for index := 0; index+1 < len(entries); index += 2 {
				cmap.chars[entries[index].code] = pdfUTF16Text(entries[index+1].code)
			}
			section = ""
		case token.text == "endbfrange" && section == "beginbfrange":
			for index := 0; index+2 < len(entries); index += 3 {
				span := pdfToUnicodeRange{low: entries[index].code, high: entries[index+1].code}
				if target := entries[index+2]; target.isArray {
					for _, each := range target.array {
						span.each = append(span.each, pdfUTF16Text(each))
					}
				} else {
					span.start = target.code
				}
				cmap.ranges = append(cmap.ranges, span)
			}
			section = ""
		}
	}
	if len(cmap.chars) == 0 && len(cmap.ranges) == 0 {
		return nil
	}
	return cmap
}

const pdfToUnicodeMaxBytes = 4 << 20

type pdfCMapEntry struct {
	code    string
	array   []string
	isArray bool
}

type pdfCMapToken struct {
	text string
	// A hex string, its bytes in text.
	hex bool
}

// pdfCMapTokens splits a CMap into its hex strings, array brackets and
// words, leaving out comments, names' slashes and dictionaries' brackets.
func pdfCMapTokens(data string) []pdfCMapToken {
	tokens := []pdfCMapToken{}
	for index := 0; index < len(data); {
		switch ch := data[index]; {
		case ch == '%':
			for index < len(data) && data[index] != '\n' && data[index] != '\r' {
				index++
			}
		case ch == '<' && index+1 < len(data) && data[index+1] == '<':
			index += 2
		case ch == '>':
			index++
		case ch == '<':
			end := strings.IndexByte(data[index:], '>')
			if end < 0 {
				return tokens
			}
			digits := strings.Join(strings.Fields(data[index+1:index+end]), "")
			if len(digits)%2 == 1 {
				digits += "0"
			}
			decoded, err := hex.DecodeString(digits)
			if err == nil {
				tokens = append(tokens, pdfCMapToken{text: string(decoded), hex: true})
			}
			index += end + 1
		case ch == '[' || ch == ']':
			tokens = append(tokens, pdfCMapToken{text: string(ch)})
			index++
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '\f' || ch == 0 || ch == '/' || ch == '(' || ch == ')' || ch == '{' || ch == '}':
			index++
		default:
			start := index
			for index < len(data) && !strings.ContainsRune(" \t\n\r\f\x00/<>[](){}%", rune(data[index])) {
				index++
			}
			tokens = append(tokens, pdfCMapToken{text: data[start:index]})
		}
	}
	return tokens
}

func (cmap *pdfToUnicode) decode(raw string) string {
	var text strings.Builder
	for len(raw) > 0 {
		length := cmap.codeLength(raw)
		text.WriteString(cmap.textOf(raw[:length]))
		raw = raw[length:]
	}
	return text.String()
}

// codeLength is how many bytes the next code takes: the shortest a code
// space range holds, else two bytes when ranges say nothing, as most
// composite fonts' codes are.
func (cmap *pdfToUnicode) codeLength(raw string) int {
	for length := 1; length <= 4 && length <= len(raw); length++ {
		for _, space := range cmap.spaces[length-1] {
			if space[0] <= raw[:length] && raw[:length] <= space[1] {
				return length
			}
		}
	}
	for length := 1; length <= 4; length++ {
		if len(cmap.spaces[length-1]) > 0 {
			return min(length, len(raw))
		}
	}
	return min(2, len(raw))
}

func (cmap *pdfToUnicode) textOf(code string) string {
	if text, found := cmap.chars[code]; found {
		return text
	}
	for _, span := range cmap.ranges {
		if len(span.low) != len(code) || code < span.low || code > span.high {
			continue
		}
		offset := 0
		for index := 0; index < len(code); index++ {
			offset = offset<<8 + int(code[index]) - int(span.low[index])
		}
		if span.each != nil {
			if offset < len(span.each) {
				return span.each[offset]
			}
			return ""
		}
		// The last unit of the start's text counts up through the range.
		units := []byte(span.start)
		if len(units) < 2 {
			return ""
		}
		last := int(binary.BigEndian.Uint16(units[len(units)-2:])) + offset
		binary.BigEndian.PutUint16(units[len(units)-2:], uint16(last))
		return pdfUTF16Text(string(units))
	}
	return ""
}
