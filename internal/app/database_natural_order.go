package app

import (
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"
)

func foldDatabaseNaturalText(value any) string {
	text := norm.NFC.String(databaseQueryValueText(value))
	return norm.NFC.String(strings.ToLower(text))
}

func asciiDatabaseDigit(value rune) bool {
	return value >= '0' && value <= '9'
}

// databaseNaturalOrderCharacters is how much of a value's folded text sorts
// it, here and in the Web compareNaturalText helper: a key holds 17 bytes a
// character, and a formula's value may be 10,000,000 characters. Values
// alike that far keep their source order.
const databaseNaturalOrderCharacters = 4096

// databaseNaturalOrderKey serializes the exact token stream owned by the Web
// compareNaturalText helper. Each token has a fixed-width sortable encoding;
// the smaller terminal marker preserves prefix order without locale services
// or numeric conversion.
func databaseNaturalOrderKey(value any) string {
	runes := make([]rune, 0, databaseNaturalOrderCharacters)
	for _, item := range foldDatabaseNaturalText(value) {
		if len(runes) == databaseNaturalOrderCharacters {
			break
		}
		runes = append(runes, item)
	}
	var result strings.Builder
	for index := 0; index < len(runes); {
		if asciiDatabaseDigit(runes[index]) {
			end := index
			for end < len(runes) && asciiDatabaseDigit(runes[end]) {
				end++
			}
			significant := index
			for significant < end && runes[significant] == '0' {
				significant++
			}
			writeDatabaseNaturalOrderToken(&result, uint64('0'))
			writeDatabaseNaturalOrderToken(&result, uint64(end-significant))
			for offset := significant; offset < end; offset++ {
				writeDatabaseNaturalOrderToken(&result, uint64(runes[offset]-'0'))
			}
			index = end
			continue
		}
		writeDatabaseNaturalOrderToken(&result, uint64(runes[index]))
		index++
	}
	result.WriteByte('0')
	return result.String()
}

func writeDatabaseNaturalOrderToken(result *strings.Builder, token uint64) {
	result.WriteByte('1')
	fmt.Fprintf(result, "%016x", token)
}

func reverseDatabaseSortKey(value string) string {
	var result strings.Builder
	result.Grow(len(value) * 2)
	for index := 0; index < len(value); index++ {
		fmt.Fprintf(&result, "%02x", 255-value[index])
	}
	return result.String()
}

// databaseStableSourceOrderKey matches the canonical source query's
// position/id order. It is appended only after every requested sort key is
// equal, preserving the incoming order required by the natural-sort contract.
func databaseStableSourceOrderKey(position, rowID string) string {
	return databaseCodePointOrderKey(position) + databaseCodePointOrderKey(rowID)
}

func databaseCodePointOrderKey(value string) string {
	var result strings.Builder
	for _, item := range value {
		writeDatabaseNaturalOrderToken(&result, uint64(item))
	}
	result.WriteByte('0')
	return result.String()
}
