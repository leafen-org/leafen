package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Records are named as the wire contracts name them (contracts/*.json, a
// record's tsName for its goName), here, once, for all an operation names:
// its body's record, those its fields hold — in their keys and in the types
// that name them — its actions' and its answer. So leafen_operations and
// GET /operations call a record by one name, the one its contract, the
// TypeScript client and every other operation use (2026-11-17: POST
// /pages/{pageId}/view-links took viewLinkCreateRequest while its contract
// said ViewLinkCreateRequest and its answer was already named ViewLink). A
// contract's records are named so whether or not it lists operations (auth's
// and administrator's records are read by handlers too). A type no contract
// holds keeps its Go name, often lower-case: it has no name outside the
// server, and the description gives its fields.

const contractsRoot = "contracts"

type contractNames struct {
	// wire is a record's wire name by its Go name, where they differ.
	wire map[string]string
	// held is every contract record's Go and wire name, lower-case, by the
	// wire name.
	held map[string]string
}

// readContractNames reads every contract's records' names.
func readContractNames() (contractNames, error) {
	names := contractNames{wire: map[string]string{}, held: map[string]string{}}
	files, err := filepath.Glob(filepath.Join(contractsRoot, "*.json"))
	if err != nil {
		return names, err
	}
	sort.Strings(files)
	type named struct {
		GoName string `json:"goName"`
		TSName string `json:"tsName"`
	}
	for _, file := range files {
		text, err := os.ReadFile(file)
		if err != nil {
			return names, err
		}
		contract := struct {
			Records []named `json:"records"`
			// A union of operations (the content graph's) is a record too,
			// as the operation catalog writes it.
			Operation *struct {
				named
				Variants []json.RawMessage `json:"variants"`
			} `json:"operation"`
		}{}
		if err := json.Unmarshal(text, &contract); err != nil {
			return names, fmt.Errorf("%s: %w", file, err)
		}
		records := contract.Records
		if contract.Operation != nil && len(contract.Operation.Variants) > 0 {
			records = append(records, contract.Operation.named)
		}
		for _, record := range records {
			if record.GoName == "" {
				continue
			}
			wire := record.TSName
			if wire == "" {
				wire = record.GoName
			}
			if wire != record.GoName {
				names.wire[record.GoName] = wire
			}
			names.held[strings.ToLower(record.GoName)] = wire
			names.held[strings.ToLower(wire)] = wire
		}
	}
	return names, nil
}

// recordWord is a name a field's type or an answer is made of
// (graphProperty[], map of graphRowValue, []viewLinkResponse).
var recordWord = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

func (names contractNames) name(record string) string {
	if wire, renamed := names.wire[record]; renamed {
		return wire
	}
	return record
}

func (names contractNames) nameWords(text string) string {
	return recordWord.ReplaceAllStringFunc(text, names.name)
}

// nameRecords names each input's records by their wire names. Inputs may
// share their maps and lists, so each gets new ones.
func (names contractNames) nameRecords(inputs map[string]input) {
	for handler, found := range inputs {
		found.Record = names.name(found.Record)
		found.Answer = names.nameWords(found.Answer)
		if found.Records != nil {
			records := make(map[string][]field, len(found.Records))
			for record, fields := range found.Records {
				named := make([]field, len(fields))
				for index, item := range fields {
					item.Type = names.nameWords(item.Type)
					named[index] = item
				}
				records[names.name(record)] = named
			}
			found.Records = records
		}
		if found.Actions != nil {
			actions := make(map[string]string, len(found.Actions))
			for action, record := range found.Actions {
				actions[action] = names.name(record)
			}
			found.Actions = actions
		}
		inputs[handler] = found
	}
}

// checkRecordNames fails where a record the operation list names starting in
// lower case is a contract's — named by its Go name, or a type of the
// server's own spelled as a contract's record in other cases (page beside
// Page), which an agent would take for it.
func (names contractNames) checkRecordNames(inputs map[string]input) error {
	problems := []string{}
	check := func(handler, record string) {
		record = strings.TrimSuffix(strings.TrimPrefix(record, "[]"), "[]")
		if record == "" || record == answerNone || !unicode.IsLower([]rune(record)[0]) {
			return
		}
		if wire, held := names.held[strings.ToLower(record)]; held && wire != record {
			problems = append(problems, fmt.Sprintf("%s names %s, the contract's %s", handler, record, wire))
		}
	}
	for handler, found := range inputs {
		check(handler, found.Record)
		check(handler, found.Answer)
		for record, fields := range found.Records {
			check(handler, record)
			for _, item := range fields {
				for _, word := range recordWord.FindAllString(item.Type, -1) {
					check(handler, word)
				}
			}
		}
		for _, record := range found.Actions {
			check(handler, record)
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("records named apart from their contracts (contracts/*.json):\n  %s", strings.Join(problems, "\n  "))
}
