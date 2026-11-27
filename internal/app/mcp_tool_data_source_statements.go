package app

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// The statements notion-update-data-source takes, as Notion's DDL writes them
// (its update-data-source description, read 2026-11-26): ADD COLUMN, DROP
// COLUMN, RENAME COLUMN and ALTER COLUMN … SET, separated by semicolons,
// names in double quotes and a type's options and other text in single
// quotes. One that doesn't read is refused saying which it is and how it is
// written, a column's type as notion-create-database's schema says it
// (mcpDatabaseSchemaModifierRefusal).

// mcpDataSourceStatementForms are the statements as a refusal writes them.
const mcpDataSourceStatementForms = `ADD COLUMN "Name" <type>, DROP COLUMN "Name", RENAME COLUMN "Old" TO "New", ALTER COLUMN "Name" SET <type>, ` +
	`ALTER COLUMN "Name" ADD OPTION 'New':color or ALTER COLUMN "Name" DROP OPTION 'Old', ` +
	`a type as notion-create-database's schema writes it: ADD COLUMN "Priority" SELECT('High':red, 'Medium':yellow, 'Low')`

func parseMCPDataSourceUpdateStatements(raw string) ([]mcpDataSourceUpdateStatement, error) {
	if len(raw) > mcpUpdateDataSourceMaximumBytes {
		return nil, mcpInvalid("statements are at most %d bytes", mcpUpdateDataSourceMaximumBytes)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []mcpDataSourceUpdateStatement{}, nil
	}
	parts, err := splitMCPDataSourceUpdateStatements(raw)
	if err != nil {
		return nil, err
	}
	if len(parts) > mcpUpdateDataSourceMaximumStatements {
		return nil, mcpInvalid("statements: at most %d in one call", mcpUpdateDataSourceMaximumStatements)
	}
	statements := make([]mcpDataSourceUpdateStatement, len(parts))
	for index, part := range parts {
		statement, reason := parseMCPDataSourceUpdateStatement(part)
		if reason != "" {
			return nil, mcpInvalid("statement %d: %s", index+1, reason)
		}
		statements[index] = statement
	}
	return statements, nil
}

// splitMCPDataSourceUpdateStatements splits at the semicolons outside a
// double-quoted name and a single-quoted text, so an option, a formula or a
// comment may hold either quote or a semicolon; a quote doubled stands for
// one, as the parsers read it.
func splitMCPDataSourceUpdateStatements(raw string) ([]string, error) {
	parts := []string{}
	start := 0
	quote := byte(0)
	for index := 0; index < len(raw); index++ {
		switch character := raw[index]; {
		case quote == 0 && (character == '"' || character == '\''):
			quote = character
			continue
		case quote != 0 && character == quote:
			if index+1 < len(raw) && raw[index+1] == quote {
				index++
			} else {
				quote = 0
			}
			continue
		case quote != 0 || character != ';':
			continue
		}
		part := strings.TrimSpace(raw[start:index])
		if part == "" {
			return nil, mcpInvalid("statement %d is empty: statements are separated by one semicolon", len(parts)+1)
		}
		parts = append(parts, part)
		start = index + 1
	}
	if quote == '"' {
		return nil, mcpInvalid("statements: a name's double quote has no end")
	}
	if quote == '\'' {
		return nil, mcpInvalid("statements: a single-quoted text has no closing quote")
	}
	if last := strings.TrimSpace(raw[start:]); last != "" {
		parts = append(parts, last)
	}
	return parts, nil
}

// parseMCPDataSourceUpdateStatement reads one statement, or says how it is
// written.
func parseMCPDataSourceUpdateStatement(raw string) (mcpDataSourceUpdateStatement, string) {
	const addPrefix = "ADD COLUMN"
	const renamePrefix = "RENAME COLUMN"
	const alterPrefix = "ALTER COLUMN"
	const dropPrefix = "DROP COLUMN"
	switch {
	case hasMCPDatabaseStatementPrefix(raw, dropPrefix):
		body := strings.TrimSpace(raw[len(dropPrefix):])
		name, next, reason := readMCPDataSourceStatementName(body)
		if reason != "" {
			return mcpDataSourceUpdateStatement{}, reason
		}
		if strings.TrimSpace(body[next:]) != "" {
			return mcpDataSourceUpdateStatement{}, `written DROP COLUMN "Name", with nothing after the name`
		}
		return mcpDataSourceUpdateStatement{Kind: mcpDataSourceStatementDrop, Name: name}, ""
	case hasMCPDatabaseStatementPrefix(raw, alterPrefix):
		body := strings.TrimSpace(raw[len(alterPrefix):])
		name, next, reason := readMCPDataSourceStatementName(body)
		if reason != "" {
			return mcpDataSourceUpdateStatement{}, reason
		}
		body = strings.TrimSpace(body[next:])
		// One option added or dropped alone (mcp_data_source_option_statements.go).
		if statement, reason, option := parseMCPDataSourceOptionStatement(name, body); option {
			return statement, reason
		}
		if !hasMCPDatabaseStatementPrefix(body, "SET") {
			return mcpDataSourceUpdateStatement{}, `written ALTER COLUMN "Name" SET <type>, as ALTER COLUMN "Priority" SET SELECT('High':red, 'Low')`
		}
		// A column as CREATE TABLE writes one: a select's or status's options
		// are all it keeps, and a formula takes its expression.
		column, reason := parseMCPDataSourceStatementColumn(mcpDataSourceStatementAlter, name, body[len("SET"):])
		if reason != "" {
			return mcpDataSourceUpdateStatement{}, reason
		}
		return mcpDataSourceUpdateStatementOf(mcpDataSourceStatementAlter, column), ""
	case hasMCPDatabaseStatementPrefix(raw, addPrefix):
		body := strings.TrimSpace(raw[len(addPrefix):])
		name, next, reason := readMCPDataSourceStatementName(body)
		if reason != "" {
			return mcpDataSourceUpdateStatement{}, reason
		}
		// A column as CREATE TABLE writes one: its type, then any options,
		// number format and description.
		column, reason := parseMCPDataSourceStatementColumn(mcpDataSourceStatementAdd, name, body[next:])
		if reason != "" {
			return mcpDataSourceUpdateStatement{}, reason
		}
		return mcpDataSourceUpdateStatementOf(mcpDataSourceStatementAdd, column), ""
	case hasMCPDatabaseStatementPrefix(raw, renamePrefix):
		body := strings.TrimSpace(raw[len(renamePrefix):])
		name, next, reason := readMCPDataSourceStatementName(body)
		if reason != "" {
			return mcpDataSourceUpdateStatement{}, reason
		}
		body = strings.TrimSpace(body[next:])
		if !hasMCPDatabaseStatementPrefix(body, "TO") {
			return mcpDataSourceUpdateStatement{}, `written RENAME COLUMN "Old" TO "New"`
		}
		body = strings.TrimSpace(body[len("TO"):])
		newName, next, reason := readMCPDataSourceStatementName(body)
		switch {
		case reason != "":
			return mcpDataSourceUpdateStatement{}, reason
		case strings.TrimSpace(body[next:]) != "":
			return mcpDataSourceUpdateStatement{}, `written RENAME COLUMN "Old" TO "New", with nothing after the new name`
		case name == newName:
			return mcpDataSourceUpdateStatement{}, "RENAME COLUMN gives the column a name it doesn't have"
		}
		return mcpDataSourceUpdateStatement{
			Kind: mcpDataSourceStatementRename, Name: name, NewName: newName,
		}, ""
	default:
		return mcpDataSourceUpdateStatement{}, "a statement is " + mcpDataSourceStatementForms
	}
}

// readMCPDataSourceStatementName reads a column's name, in the words
// notion-create-database's schema refuses one with.
func readMCPDataSourceStatementName(body string) (string, int, string) {
	name, next, ok := readMCPDatabaseQuotedIdentifier(body, 0)
	if !ok || !validMCPDatabasePropertyName(name) {
		return "", 0, fmt.Sprintf(`a column's name is in double quotes, without spaces around it, at most %d characters, as "Name"`,
			maxGraphIdentifierLength)
	}
	return name, next, ""
}

// parseMCPDataSourceStatementColumn reads a column's type and what follows
// it to the statement's end, as CREATE TABLE writes a column: a type ADD
// COLUMN or ALTER COLUMN … SET can give it.
func parseMCPDataSourceStatementColumn(kind, name, body string) (mcpDatabaseSchemaProperty, string) {
	offset := skipMCPDatabaseSchemaSpace(body, 0)
	typeStart := offset
	for offset < len(body) && isMCPDatabaseSchemaTypeByte(body[offset]) {
		offset++
	}
	propertyType, ok := mcpDatabaseSchemaPropertyTypes[strings.ToUpper(body[typeStart:offset])]
	switch {
	case typeStart == offset || !ok:
		return mcpDatabaseSchemaProperty{}, fmt.Sprintf("column %q: its type is one of %s", name, mcpDatabaseSchemaTypeNames())
	case propertyType == graphValueTypeTitle && kind == mcpDataSourceStatementAdd:
		return mcpDatabaseSchemaProperty{}, "a data source has its one TITLE column, which ADD COLUMN can't make"
	case kind == mcpDataSourceStatementAlter && (propertyType == graphValueTypeTitle ||
		propertyType == graphValueTypeRelation || propertyType == graphValueTypeRollup):
		return mcpDatabaseSchemaProperty{}, "ALTER COLUMN … SET can't make a column TITLE, RELATION or ROLLUP"
	}
	column := mcpDatabaseSchemaProperty{Name: name, Type: propertyType}
	if offset, ok = parseMCPDatabaseSchemaModifiers(body, offset, &column); !ok {
		return column, mcpDatabaseSchemaModifierRefusal(column, body, offset)
	}
	// What reads stops at a comma, where CREATE TABLE's next column starts.
	if strings.TrimSpace(body[offset:]) != "" {
		return column, `a statement changes one column; statements are separated by semicolons, as ADD COLUMN "A" TEXT; ADD COLUMN "B" DATE`
	}
	return column, ""
}

// mcpDataSourceUpdateStatementOf is an added or changed column's statement,
// with the settings it names.
func mcpDataSourceUpdateStatementOf(kind string, column mcpDatabaseSchemaProperty) mcpDataSourceUpdateStatement {
	statement := mcpDataSourceUpdateStatement{Kind: kind, Name: column.Name, Type: column.Type}
	if mcpDatabaseSchemaHasSettings(column) {
		statement.Column = &column
	}
	return statement
}

func hasMCPDatabaseStatementPrefix(value, prefix string) bool {
	if len(value) < len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		return false
	}
	return len(value) == len(prefix) || value[len(prefix)] == ' ' || value[len(prefix)] == '\t' ||
		value[len(prefix)] == '\r' || value[len(prefix)] == '\n'
}

func validMCPDatabasePropertyName(name string) bool {
	return name != "" && strings.TrimSpace(name) == name &&
		utf8.RuneCountInString(name) <= maxGraphIdentifierLength
}
