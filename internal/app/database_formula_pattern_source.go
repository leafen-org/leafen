package app

import (
	"fmt"
	"strings"
)

// A JavaScript pattern is rewritten for regexp2's ECMAScript mode where the
// two read patterns differently: "." leaves out every line terminator, \b
// is a boundary of ASCII word characters, a letter escape that JavaScript
// does not know is the letter, only the groups JavaScript knows are groups,
// a modifier group's s and m flags are written out on its "." and anchors,
// and each unit that is half of a surrogate pair is its private-use stand-in.

// databaseFormulaPatternGroup is an open group: whether it is a lookbehind,
// and the s and m flags inside it.
type databaseFormulaPatternGroup struct {
	lookbehind, dotAll, multiline bool
}

// databaseFormulaPatternWriter rewrites a JavaScript pattern, read as its
// UTF-16 units, for regexp2.
type databaseFormulaPatternWriter struct {
	units  []uint16
	at     int
	out    strings.Builder
	groups []databaseFormulaPatternGroup
}

// flags are the s and m flags where the writer is.
func (writer *databaseFormulaPatternWriter) flags() (dotAll, multiline bool) {
	if len(writer.groups) == 0 {
		return false, false
	}
	group := writer.groups[len(writer.groups)-1]
	return group.dotAll, group.multiline
}

const databaseFormulaWordClass = `[A-Za-z0-9_]`

func databaseFormulaPatternSource(pattern string) (string, error) {
	writer := &databaseFormulaPatternWriter{units: databaseFormulaUnits(pattern)}
	for writer.at < len(writer.units) {
		if err := writer.next(); err != nil {
			return "", err
		}
	}
	if len(writer.groups) > 0 {
		return "", errDatabaseFormulaPattern
	}
	return writer.out.String(), nil
}

func (writer *databaseFormulaPatternWriter) peek(offset int) (uint16, bool) {
	if writer.at+offset >= len(writer.units) {
		return 0, false
	}
	return writer.units[writer.at+offset], true
}

// braces is the length of the {n}, {n,} or {n,m} quantifier at offset from
// the current unit, or 0 when a brace there is itself.
func (writer *databaseFormulaPatternWriter) braces(offset int) int {
	if unit, _ := writer.peek(offset); unit != '{' {
		return 0
	}
	length := 1
	digits := func() int {
		count := 0
		for unit, ok := writer.peek(offset + length); ok && unit >= '0' && unit <= '9'; unit, ok = writer.peek(offset + length) {
			length, count = length+1, count+1
		}
		return count
	}
	if digits() == 0 {
		return 0
	}
	if unit, _ := writer.peek(offset + length); unit == ',' {
		length++
		digits()
	}
	if unit, _ := writer.peek(offset + length); unit != '}' {
		return 0
	}
	return length + 1
}

// quantified is whether a quantifier follows, which JavaScript refuses after
// an anchor, a boundary or a lookbehind.
func (writer *databaseFormulaPatternWriter) quantified() bool {
	next, _ := writer.peek(0)
	return next == '*' || next == '+' || next == '?' || writer.braces(0) > 0
}

// databaseFormulaDigitsLess compares two runs of decimal digits by value.
func databaseFormulaDigitsLess(left, right string) bool {
	left, right = strings.TrimLeft(left, "0"), strings.TrimLeft(right, "0")
	if len(left) != len(right) {
		return len(left) < len(right)
	}
	return left < right
}

// literal writes one unit to match itself.
func (writer *databaseFormulaPatternWriter) literal(unit uint16) {
	if value := databaseFormulaPatternRune(unit); value > 0xFFFF {
		writer.out.WriteRune(value)
		return
	}
	fmt.Fprintf(&writer.out, `\u%04X`, unit)
}

func (writer *databaseFormulaPatternWriter) next() error {
	unit := writer.units[writer.at]
	writer.at++
	switch unit {
	case '\\':
		return writer.escape()
	case '[':
		return writer.class()
	case '.':
		if dotAll, _ := writer.flags(); dotAll {
			writer.out.WriteString(`[\s\S]`)
		} else {
			writer.out.WriteString(`[^\n\r\u2028\u2029]`)
		}
	case '(':
		return writer.open()
	case ')':
		if len(writer.groups) == 0 {
			return errDatabaseFormulaPattern
		}
		lookbehind := writer.groups[len(writer.groups)-1].lookbehind
		writer.groups = writer.groups[:len(writer.groups)-1]
		if lookbehind && writer.quantified() {
			return errDatabaseFormulaPattern
		}
		writer.out.WriteByte(')')
	case '^', '$':
		if writer.quantified() {
			return errDatabaseFormulaPattern
		}
		_, multiline := writer.flags()
		switch {
		case !multiline:
			writer.out.WriteByte(byte(unit))
		case unit == '^':
			writer.out.WriteString(`(?:^|(?<=[\n\r\u2028\u2029]))`)
		default:
			writer.out.WriteString(`(?:$|(?=[\n\r\u2028\u2029]))`)
		}
	case '{':
		length := writer.braces(-1)
		if length == 0 {
			writer.literal(unit)
			return nil
		}
		quantifier := databaseFormulaFromUnits(writer.units[writer.at-1 : writer.at-1+length])
		if low, high, ranged := strings.Cut(strings.Trim(quantifier, "{}"), ","); ranged && high != "" &&
			databaseFormulaDigitsLess(high, low) {
			return errDatabaseFormulaPattern
		}
		writer.out.WriteString(quantifier)
		writer.at += length - 1
	case '*', '+', '?', '|':
		writer.out.WriteByte(byte(unit))
	default:
		writer.literal(unit)
	}
	return nil
}

// modifiers writes a (?ims-ims: group, whose i flag regexp2 reads itself;
// JavaScript refuses a repeated flag, a flag both added and removed, and
// (?-:.
func (writer *databaseFormulaPatternWriter) modifiers(group *databaseFormulaPatternGroup) error {
	seen := map[uint16]bool{}
	added, removed, sign := "", "", false
	for end := writer.at + 1; end < len(writer.units); end++ {
		switch unit := writer.units[end]; {
		case unit == ':':
			if sign && removed == "" {
				return errDatabaseFormulaPattern
			}
			for _, flag := range added {
				group.dotAll = group.dotAll || flag == 's'
				group.multiline = group.multiline || flag == 'm'
			}
			for _, flag := range removed {
				group.dotAll = group.dotAll && flag != 's'
				group.multiline = group.multiline && flag != 'm'
			}
			writer.out.WriteString("(?")
			if strings.ContainsRune(added, 'i') {
				writer.out.WriteByte('i')
			}
			if strings.ContainsRune(removed, 'i') {
				writer.out.WriteString("-i")
			}
			writer.out.WriteByte(':')
			writer.at = end + 1
			return nil
		case unit == '-' && !sign:
			sign = true
		case (unit == 'i' || unit == 'm' || unit == 's') && !seen[unit]:
			seen[unit] = true
			if sign {
				removed += string(rune(unit))
			} else {
				added += string(rune(unit))
			}
		default:
			return errDatabaseFormulaPattern
		}
	}
	return errDatabaseFormulaPattern
}

// open writes a group; JavaScript knows (?:, (?=, (?!, (?<=, (?<!,
// (?<name> and modifier groups.
func (writer *databaseFormulaPatternWriter) open() error {
	dotAll, multiline := writer.flags()
	group := databaseFormulaPatternGroup{dotAll: dotAll, multiline: multiline}
	if next, _ := writer.peek(0); next == '?' {
		kind, _ := writer.peek(1)
		switch kind {
		case ':', '=', '!':
			writer.out.WriteString("(?" + string(rune(kind)))
			writer.at += 2
		case '<':
			switch after, _ := writer.peek(2); after {
			case '=', '!':
				group.lookbehind = true
				writer.out.WriteString("(?<" + string(rune(after)))
				writer.at += 3
			default:
				end := writer.at + 2
				for end < len(writer.units) && writer.units[end] != '>' {
					end++
				}
				if end == writer.at+2 || end == len(writer.units) {
					return errDatabaseFormulaPattern
				}
				writer.out.WriteString("(?<" + databaseFormulaFromUnits(writer.units[writer.at+2:end]) + ">")
				writer.at = end + 1
			}
		case 'i', 'm', 's', '-':
			if err := writer.modifiers(&group); err != nil {
				return err
			}
		default:
			return errDatabaseFormulaPattern
		}
	} else {
		writer.out.WriteByte('(')
	}
	writer.groups = append(writer.groups, group)
	return nil
}

func databaseFormulaHexValue(units []uint16) (uint16, bool) {
	value := uint16(0)
	for _, unit := range units {
		switch {
		case unit >= '0' && unit <= '9':
			value = value*16 + unit - '0'
		case unit >= 'a' && unit <= 'f':
			value = value*16 + unit - 'a' + 10
		case unit >= 'A' && unit <= 'F':
			value = value*16 + unit - 'A' + 10
		default:
			return 0, false
		}
	}
	return value, true
}

// hex reads the width hex digits after \x or \u, when they are there.
func (writer *databaseFormulaPatternWriter) hex(width int) (uint16, bool) {
	if writer.at+width > len(writer.units) {
		return 0, false
	}
	value, ok := databaseFormulaHexValue(writer.units[writer.at : writer.at+width])
	if ok {
		writer.at += width
	}
	return value, ok
}

func databaseFormulaASCIILetter(unit uint16) bool {
	return unit >= 'a' && unit <= 'z' || unit >= 'A' && unit <= 'Z'
}

func (writer *databaseFormulaPatternWriter) escape() error {
	unit, ok := writer.peek(0)
	if !ok {
		return errDatabaseFormulaPattern
	}
	writer.at++
	switch {
	case unit == 'b' || unit == 'B':
		if writer.quantified() {
			return errDatabaseFormulaPattern
		}
		if unit == 'b' {
			writer.out.WriteString(`(?:(?<=` + databaseFormulaWordClass + `)(?!` + databaseFormulaWordClass + `)|(?<!` +
				databaseFormulaWordClass + `)(?=` + databaseFormulaWordClass + `))`)
		} else {
			writer.out.WriteString(`(?:(?<=` + databaseFormulaWordClass + `)(?=` + databaseFormulaWordClass + `)|(?<!` +
				databaseFormulaWordClass + `)(?!` + databaseFormulaWordClass + `))`)
		}
	case unit >= '0' && unit <= '9':
		// A backreference or an octal escape, read whole as JavaScript does.
		writer.out.WriteString(`\` + string(rune(unit)))
		for digit, ok := writer.peek(0); ok && digit >= '0' && digit <= '9'; digit, ok = writer.peek(0) {
			writer.out.WriteByte(byte(digit))
			writer.at++
		}
	case unit == 'k':
		writer.out.WriteString(`\k`)
		if next, _ := writer.peek(0); next == '<' {
			for end := writer.at + 1; end < len(writer.units); end++ {
				if writer.units[end] == '>' {
					writer.out.WriteString(databaseFormulaFromUnits(writer.units[writer.at : end+1]))
					writer.at = end + 1
					break
				}
			}
		}
	case strings.ContainsRune("dDsSwWfnrtv", rune(unit)):
		writer.out.WriteString(`\` + string(rune(unit)))
	case unit == 'c':
		// \c and a letter is a control character; otherwise the backslash
		// is itself.
		if letter, ok := writer.peek(0); ok && databaseFormulaASCIILetter(letter) {
			writer.at++
			writer.literal(letter % 32)
		} else {
			writer.literal('\\')
			writer.at--
		}
	case unit == 'x' || unit == 'u':
		width := 2
		if unit == 'u' {
			width = 4
		}
		if value, ok := writer.hex(width); ok {
			writer.literal(value)
		} else {
			writer.literal(unit)
		}
	default:
		writer.literal(unit)
	}
	return nil
}

// classAtom is one member of a character set: a unit, or a class escape such
// as \d, which cannot end a range.
type databaseFormulaClassAtom struct {
	unit  uint16
	class string
}

func (writer *databaseFormulaPatternWriter) classAtom() (databaseFormulaClassAtom, error) {
	unit := writer.units[writer.at]
	writer.at++
	if unit != '\\' {
		return databaseFormulaClassAtom{unit: unit}, nil
	}
	unit, ok := writer.peek(0)
	if !ok {
		return databaseFormulaClassAtom{}, errDatabaseFormulaPattern
	}
	writer.at++
	switch {
	case unit == 'b':
		return databaseFormulaClassAtom{unit: '\b'}, nil
	case strings.ContainsRune("dDsSwW", rune(unit)):
		return databaseFormulaClassAtom{class: `\` + string(rune(unit))}, nil
	case unit == 'f' || unit == 'n' || unit == 'r' || unit == 't' || unit == 'v':
		return databaseFormulaClassAtom{unit: map[uint16]uint16{'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v'}[unit]}, nil
	case unit == 'c':
		// Inside a set, \c also takes a digit or an underscore.
		if next, ok := writer.peek(0); ok && (databaseFormulaASCIILetter(next) || next >= '0' && next <= '9' || next == '_') {
			writer.at++
			return databaseFormulaClassAtom{unit: next % 32}, nil
		}
		writer.at--
		return databaseFormulaClassAtom{unit: '\\'}, nil
	case unit == 'x' || unit == 'u':
		width := 2
		if unit == 'u' {
			width = 4
		}
		if value, ok := writer.hex(width); ok {
			return databaseFormulaClassAtom{unit: value}, nil
		}
		return databaseFormulaClassAtom{unit: unit}, nil
	case unit >= '0' && unit <= '7':
		// A legacy octal escape: up to three digits, at most 0o377.
		value := unit - '0'
		for digits := 1; digits < 3; digits++ {
			next, ok := writer.peek(0)
			if !ok || next < '0' || next > '7' || value*8+next-'0' > 0o377 {
				break
			}
			value = value*8 + next - '0'
			writer.at++
		}
		return databaseFormulaClassAtom{unit: value}, nil
	}
	return databaseFormulaClassAtom{unit: unit}, nil
}

// classRange writes the units from low to high; the surrogates among them
// stand in as their private-use characters.
func (writer *databaseFormulaPatternWriter) classRange(low, high uint16) {
	part := func(from, to uint16) {
		writer.literal(from)
		if to != from {
			writer.out.WriteByte('-')
			writer.literal(to)
		}
	}
	if low <= 0xD7FF {
		part(low, min(high, 0xD7FF))
	}
	if low <= 0xDFFF && high >= 0xD800 {
		part(max(low, 0xD800), min(high, 0xDFFF))
	}
	if high >= 0xE000 {
		part(max(low, 0xE000), high)
	}
}

// class writes a character set; as in JavaScript, [] matches nothing and [^]
// anything, a set's first ] closes it, and a dash next to a class escape is
// itself.
func (writer *databaseFormulaPatternWriter) class() error {
	writer.out.WriteByte('[')
	if next, _ := writer.peek(0); next == '^' {
		writer.out.WriteByte('^')
		writer.at++
	}
	for {
		unit, ok := writer.peek(0)
		if !ok {
			return errDatabaseFormulaPattern
		}
		if unit == ']' {
			writer.at++
			writer.out.WriteByte(']')
			return nil
		}
		low, err := writer.classAtom()
		if err != nil {
			return err
		}
		dash, _ := writer.peek(0)
		after, hasAfter := writer.peek(1)
		if dash != '-' || !hasAfter || after == ']' {
			writer.classAtomWrite(low)
			continue
		}
		writer.at++
		high, err := writer.classAtom()
		if err != nil {
			return err
		}
		if low.class != "" || high.class != "" {
			writer.classAtomWrite(low)
			writer.literal('-')
			writer.classAtomWrite(high)
			continue
		}
		if low.unit > high.unit {
			return errDatabaseFormulaPattern
		}
		writer.classRange(low.unit, high.unit)
	}
}

func (writer *databaseFormulaPatternWriter) classAtomWrite(atom databaseFormulaClassAtom) {
	if atom.class != "" {
		writer.out.WriteString(atom.class)
		return
	}
	writer.classRange(atom.unit, atom.unit)
}
