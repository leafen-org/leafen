package app

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// formatDate() reads the format tokens Notion's formulas read (moment.js
// style, the names and layouts of the reader's language,
// database_formula_locale.go); text in [brackets] or after a backslash is
// kept. A layout token (LT, LL, ...) stands for the language's layout.

var databaseFormulaDateTokens = []string{
	"LTS", "LT", "LLLL", "LLL", "LL", "L", "llll", "lll", "ll", "l",
	"YYYY", "YY", "Y", "Q", "MMMM", "MMM", "MM", "M", "Do", "DDDD", "DDD", "DD", "D",
	"dddd", "ddd", "dd", "d", "WW", "W", "HH", "H", "hh", "h", "kk", "k",
	"mm", "m", "ss", "s", "SSS", "A", "a", "ZZ", "Z", "X", "x",
}

func databaseFormulaPad(value, width int) string {
	text := strconv.Itoa(databaseFormulaAbsInt(value))
	if len(text) < width {
		text = strings.Repeat("0", width-len(text)) + text
	}
	if value < 0 {
		return "-" + text
	}
	return text
}

func databaseFormulaOrdinal(value int) string {
	suffix := "th"
	if value%100 < 11 || value%100 > 13 {
		switch value % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(value) + suffix
}

func databaseFormulaDateToken(token string, at time.Time, locale *databaseFormulaLocale) string {
	hour12 := at.Hour() % 12
	if hour12 == 0 {
		hour12 = 12
	}
	_, week := at.ISOWeek()
	_, offset := at.Zone()
	zone := func(separator string) string {
		sign := "+"
		if offset < 0 {
			sign = "-"
		}
		minutes := databaseFormulaAbsInt(offset) / 60
		return sign + databaseFormulaPad(minutes/60, 2) + separator + databaseFormulaPad(minutes%60, 2)
	}
	switch token {
	case "YYYY":
		return databaseFormulaPad(at.Year(), 4)
	case "YY":
		return databaseFormulaPad(at.Year()%100, 2)
	case "Y":
		return strconv.Itoa(at.Year())
	case "Q":
		return strconv.Itoa((int(at.Month())-1)/3 + 1)
	case "MMMM":
		return locale.months[at.Month()-1]
	case "MMM":
		return locale.monthsShort[at.Month()-1]
	case "MM":
		return databaseFormulaPad(int(at.Month()), 2)
	case "M":
		return strconv.Itoa(int(at.Month()))
	case "Do":
		return locale.ordinal(at.Day())
	case "DDDD":
		return databaseFormulaPad(at.YearDay(), 3)
	case "DDD":
		return strconv.Itoa(at.YearDay())
	case "DD":
		return databaseFormulaPad(at.Day(), 2)
	case "D":
		return strconv.Itoa(at.Day())
	case "dddd":
		return locale.weekdays[at.Weekday()]
	case "ddd":
		return locale.weekdaysShort[at.Weekday()]
	case "dd":
		return locale.weekdaysMin[at.Weekday()]
	case "d":
		return strconv.Itoa(int(at.Weekday()))
	case "WW":
		return databaseFormulaPad(week, 2)
	case "W":
		return strconv.Itoa(week)
	case "HH":
		return databaseFormulaPad(at.Hour(), 2)
	case "H":
		return strconv.Itoa(at.Hour())
	case "hh":
		return databaseFormulaPad(hour12, 2)
	case "h":
		return strconv.Itoa(hour12)
	case "kk":
		return databaseFormulaPad(at.Hour()+1, 2)
	case "k":
		return strconv.Itoa(at.Hour() + 1)
	case "mm":
		return databaseFormulaPad(at.Minute(), 2)
	case "m":
		return strconv.Itoa(at.Minute())
	case "ss":
		return databaseFormulaPad(at.Second(), 2)
	case "s":
		return strconv.Itoa(at.Second())
	case "SSS":
		return databaseFormulaPad(at.Nanosecond()/int(time.Millisecond), 3)
	case "A", "a":
		return locale.meridiem(at.Hour(), token == "a")
	case "ZZ":
		return zone("")
	case "Z":
		return zone(":")
	case "X":
		return strconv.FormatInt(int64(math.Floor(float64(at.UnixMilli())/1000)), 10)
	}
	return strconv.FormatInt(at.UnixMilli(), 10)
}

func databaseFormulaFormatDate(moment databaseFormulaMoment, layout string, locale *databaseFormulaLocale) string {
	characters := []rune(layout)
	var result strings.Builder
	for index := 0; index < len(characters); {
		if characters[index] == '[' {
			end := index + 1
			for end < len(characters) && characters[end] != ']' && characters[end] != '[' {
				end++
			}
			if end < len(characters) && characters[end] == ']' {
				result.WriteString(string(characters[index+1 : end]))
				index = end + 1
				continue
			}
		}
		if characters[index] == '\\' && index+1 < len(characters) {
			result.WriteRune(characters[index+1])
			index += 2
			continue
		}
		matched := ""
		for _, token := range databaseFormulaDateTokens {
			if strings.HasPrefix(string(characters[index:]), token) {
				matched = token
				break
			}
		}
		if matched == "" {
			result.WriteRune(characters[index])
			index++
			continue
		}
		if layout, isLayout := locale.layouts[matched]; isLayout {
			result.WriteString(databaseFormulaFormatDate(moment, layout, locale))
		} else {
			result.WriteString(databaseFormulaDateToken(matched, moment.at, locale))
		}
		index += len([]rune(matched))
	}
	return result.String()
}

// databaseFormulaFixed writes a number with a fixed count of decimals as
// JavaScript's toFixed() does: from its exact value, halves away from zero.
func databaseFormulaFixed(value float64, digits int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) >= 1e21 {
		return databaseFormulaNumberText(value)
	}
	sign := ""
	if value < 0 {
		sign, value = "-", -value
	}
	whole, fraction, _ := strings.Cut(new(big.Float).SetFloat64(value).Text('f', 1100), ".")
	return sign + databaseFormulaRoundDigits(whole, fraction, digits)
}

// databaseFormulaRoundDigits writes whole.fraction with digits decimals,
// rounding halves away from zero.
func databaseFormulaRoundDigits(whole, fraction string, digits int) string {
	fraction += strings.Repeat("0", digits+1)
	kept := []byte(whole + fraction[:digits])
	if fraction[digits] >= '5' {
		index := len(kept) - 1
		for ; index >= 0; index-- {
			if kept[index] < '9' {
				kept[index]++
				break
			}
			kept[index] = '0'
		}
		if index < 0 {
			kept = append([]byte{'1'}, kept...)
		}
	}
	text := string(kept)
	if digits > 0 {
		text = text[:len(text)-digits] + "." + text[len(text)-digits:]
	}
	return text
}

// databaseFormulaShortestDigits is the size of a finite number as its
// shortest decimal form writes it, moved shift places left, in whole and
// fraction digits.
func databaseFormulaShortestDigits(value float64, shift int) (string, string) {
	mantissa, exponentText, _ := strings.Cut(strconv.FormatFloat(math.Abs(value), 'e', -1, 64), "e")
	exponent, _ := strconv.Atoi(exponentText)
	digits := strings.Replace(mantissa, ".", "", 1)
	point := 1 + exponent + shift
	if point < 1 {
		digits = strings.Repeat("0", 1-point) + digits
		point = 1
	}
	if point > len(digits) {
		digits += strings.Repeat("0", point-len(digits))
	}
	return strings.TrimLeft(digits[:point-1], "0") + digits[point-1:point], digits[point:]
}

// databaseFormulaCurrency is a currency formatNumber() writes in: its code,
// its sign as Notion writes it, and its usual decimals. Yen and won without
// given decimals round halves up, as Notion rounds them (dong too, seen
// 2026-11-08).
type databaseFormulaCurrency struct {
	code, prefix string
	decimals     int
	roundsUp     bool
}

var databaseFormulaCurrencyFormats = map[string]databaseFormulaCurrency{
	"dollar": {"usd", "$", 2, false}, "australian_dollar": {"aud", "A$", 2, false},
	"canadian_dollar": {"cad", "CA$", 2, false}, "singapore_dollar": {"sgd", "SGD\u00a0", 2, false},
	"euro": {"eur", "€", 2, false}, "pound": {"gbp", "£", 2, false}, "yen": {"jpy", "¥", 0, true},
	"ruble": {"rub", "RUB\u00a0", 2, false}, "rupee": {"inr", "₹", 2, false}, "won": {"krw", "₩", 0, true},
	"yuan": {"cny", "CN¥", 2, false}, "real": {"brl", "R$", 2, false}, "lira": {"try", "TRY\u00a0", 2, false},
	"rupiah": {"idr", "IDR\u00a0", 0, false}, "franc": {"chf", "CHF\u00a0", 2, false},
	"hong_kong_dollar": {"hkd", "HK$", 2, false}, "new_zealand_dollar": {"nzd", "NZ$", 2, false},
	"krona": {"sek", "SEK\u00a0", 2, false}, "norwegian_krone": {"nok", "NOK\u00a0", 2, false},
	"mexican_peso": {"mxn", "MX$", 2, false}, "rand": {"zar", "ZAR\u00a0", 2, false},
	"new_taiwan_dollar": {"twd", "NT$", 2, false}, "danish_krone": {"dkk", "DKK\u00a0", 2, false},
	"zloty": {"pln", "PLN\u00a0", 2, false}, "baht": {"thb", "THB\u00a0", 2, false},
	"forint": {"huf", "HUF\u00a0", 0, false}, "koruna": {"czk", "CZK\u00a0", 2, false},
	"shekel": {"ils", "₪", 2, false}, "chilean_peso": {"clp", "CLP\u00a0", 0, false},
	"philippine_peso": {"php", "₱", 2, false}, "dirham": {"aed", "AED\u00a0", 2, false},
	"colombian_peso": {"cop", "COP\u00a0", 0, false}, "riyal": {"sar", "SAR\u00a0", 2, false},
	"ringgit": {"myr", "MYR\u00a0", 2, false}, "leu": {"ron", "RON\u00a0", 2, false},
	"argentine_peso": {"ars", "ARS\u00a0", 2, false}, "uruguayan_peso": {"uyu", "UYU\u00a0", 2, false},
	"peruvian_sol": {"pen", "PEN\u00a0", 2, false}, "vietnamese_dong": {"vnd", "₫", 0, true},
	"pakistani_rupee": {"pkr", "PKR\u00a0", 0, false}, "nigerian_naira": {"ngn", "NGN\u00a0", 2, false},
}

var databaseFormulaCurrencies = func() map[string]databaseFormulaCurrency {
	currencies := map[string]databaseFormulaCurrency{}
	for name, currency := range databaseFormulaCurrencyFormats {
		currencies[name], currencies[currency.code] = currency, currency
	}
	return currencies
}()

func databaseFormulaGroupThousands(text string) string {
	whole, fraction, hasFraction := strings.Cut(text, ".")
	if len(whole) > 3 && !strings.ContainsAny(whole, "e") {
		groups := []string{}
		for len(whole) > 3 {
			groups = append([]string{whole[len(whole)-3:]}, groups...)
			whole = whole[:len(whole)-3]
		}
		whole = whole + "," + strings.Join(groups, ",")
	}
	if hasFraction {
		return whole + "." + fraction
	}
	return whole
}

// formatNumber(number, format, decimals) writes a number as Notion does:
// plainly ("number"), with thousands separators ("number_with_commas",
// "commas", or no format), as a percent, or in a currency named by its
// Notion format name or its code, in any case, or as a byte size; a format
// it doesn't know writes nothing. Decimals are a whole number from 0, at most
// 12 (more writes 12, as Notion's, seen 2026-11-08), else unused, and a
// currency without them has its usual decimals. A number rounds halves away
// from zero as its shortest decimal form writes it.
func databaseFormulaFormatNumber(values []any) (string, error) {
	value := databaseFormulaFiniteNumber(databaseFormulaArgument(values, 0))
	format := "number_with_commas"
	if len(values) > 1 {
		format = strings.ToLower(databaseFormulaText(values[1]))
	}
	digits := -1
	if len(values) > 2 {
		if given := databaseFormulaFiniteNumber(values[2]); given == math.Trunc(given) && given >= 0 {
			digits = int(math.Min(given, 12))
		}
	}
	if format == "humanize" {
		return databaseFormulaCompactNumber(value), nil
	}
	if format == "bytes_binary" || format == "bytes_decimal" {
		return databaseFormulaByteSize(value, format == "bytes_binary", digits), nil
	}
	currency, isCurrency := databaseFormulaCurrencies[format]
	switch {
	case format == "commas":
		format = "number_with_commas"
	case isCurrency:
		format = "currency"
	case format != "number" && format != "number_with_commas" && format != "percent":
		// A format it doesn't know fails the formula, as Notion's (seen
		// 2026-11-14: formatNumber(1, "foo") and formatNumber(1, "") are no
		// value), as the editor's.
		return "", errDatabaseFormulaInvalid
	}
	sign := ""
	if value < 0 {
		sign = "-"
	}
	shift := 0
	if format == "percent" {
		shift = 2
	}
	var text string
	switch {
	case math.IsNaN(value) || math.IsInf(value, 0):
		text = databaseFormulaNumberText(math.Abs(value))
	case digits >= 0:
		whole, fraction := databaseFormulaShortestDigits(value, shift)
		text = databaseFormulaRoundDigits(whole, fraction, digits)
	case isCurrency && currency.roundsUp:
		rounded := math.Floor(value)
		if value-rounded >= 0.5 {
			rounded++
		}
		text = strconv.FormatFloat(math.Abs(rounded), 'f', 0, 64)
	case isCurrency:
		whole, fraction := databaseFormulaShortestDigits(value, 0)
		text = databaseFormulaRoundDigits(whole, fraction, currency.decimals)
	case shift > 0:
		whole, fraction := databaseFormulaShortestDigits(value, shift)
		shifted, _ := strconv.ParseFloat(whole+"."+fraction+"0", 64)
		text = databaseFormulaNumberText(shifted)
	default:
		text = databaseFormulaNumberText(math.Abs(value))
	}
	switch format {
	case "number":
		return sign + text, nil
	case "percent":
		return sign + text + "%", nil
	case "currency":
		return sign + currency.prefix + databaseFormulaGroupThousands(text), nil
	}
	return sign + databaseFormulaGroupThousands(text), nil
}

// databaseFormulaByteSize writes a number of bytes as Notion's formatNumber
// does (seen 2026-11-08): in the largest unit its size reaches, by steps of
// 1024 (KiB … YiB) or 1000 (KB … YB), with at most two decimals or the given
// ones and no trailing zeros or thousands separators: 1536 is "1.5 KiB" or
// "1.54 KB", 1023 is "1023 B", -2048 is "-2 KiB".
func databaseFormulaByteSize(value float64, binary bool, digits int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return ""
	}
	base, units := 1000.0, []string{"B", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}
	if binary {
		base, units = 1024, []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB", "ZiB", "YiB"}
	}
	sign, size := "", math.Abs(value)
	if value < 0 {
		sign = "-"
	}
	unit := 0
	for unit < len(units)-1 && size >= base {
		size /= base
		unit++
	}
	if digits < 0 {
		digits = 2
	}
	whole, fraction := databaseFormulaShortestDigits(size, 0)
	text := databaseFormulaRoundDigits(whole, fraction, digits)
	if strings.Contains(text, ".") {
		text = strings.TrimSuffix(strings.TrimRight(text, "0"), ".")
	}
	return sign + text + " " + units[unit]
}

// databaseFormulaCompactNumber writes a number short, in English as the public
// API reads it (the screen writes it in the viewer's language), as Notion's
// (seen 2026-11-04): two significant digits below 100, whole above: 1.2B, 15K,
// and 1000T past the last unit.
func databaseFormulaCompactNumber(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return ""
	}
	sign, size := "", math.Abs(value)
	if value < 0 {
		sign = "-"
	}
	round := func(number float64) float64 {
		if number >= 100 || number == 0 {
			return math.Round(number)
		}
		scale := math.Pow(10, 1-math.Floor(math.Log10(number)))
		return math.Round(number*scale) / scale
	}
	units := []string{"", "K", "M", "B", "T"}
	unit := 0
	for unit < len(units)-1 && size >= 1000 {
		size /= 1000
		unit++
	}
	shown := round(size)
	if shown >= 1000 && unit < len(units)-1 {
		shown, unit = round(shown/1000), unit+1
	}
	return sign + strconv.FormatFloat(shown, 'f', -1, 64) + units[unit]
}
