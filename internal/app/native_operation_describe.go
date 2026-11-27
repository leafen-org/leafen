package app

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
)

// One operation is described by what a call sends it, as its handler reads
// and checks it (scripts/native-operation-inputs): the body's record and
// the records its fields hold, each field it refuses a call without marked
// required, the values a field takes (a list's every entry), the keys a
// map takes with those it needs (requiredKeys), and the query names it reads
// (2026-11-26, real agents: an export's options and a webhook's eventTypes
// named none, and every try was refused; 2026-11-16, an agent calling every
// operation: asking for one answered every record of its contract, 12–25 KB,
// and none said which fields were needed). Where the body's record is one of
// the contract's, the contract's own words for its fields are given, with
// the values its handler's checks name where the contract names no kind of
// values for one, and so is the record it
// answers with (native_operation_answer.go). Any other record — one a
// field holds, or the contract's where what the handler takes wasn't read —
// gives its first fields and those a call needs, as many as nine in ten
// held records have, with how many it left out in more_fields (2026-11-16:
// a page's metadata change gave a text piece's 33 fields every time, 4 KB
// where the median is 1.6 KB). detail "full" gives every field, and adds
// every record of the contract, its answers' too.

// nativeOperationInputsParsed is what each handler reads, as an operation
// is described and as a refusal names its fields (api_error_details.go):
// no body's own mutationId needed, as the server makes one left out
// (native_api_body_mutation_id.go). Read only.
var nativeOperationInputsParsed = sync.OnceValues(func() (map[string]nativeOperationInput, error) {
	described := map[string]nativeOperationInput{}
	err := json.Unmarshal(nativeOperationInputsJSON, &described)
	nativeOperationInputsMutationIDOptional(described)
	return described, err
})

// describeNativeOperations answers leafen_operations and GET /operations
// alike: one operation with what a call sends it, one group, what English
// words find, every operation (detail list or full), or else the groups
// (native_operation_group_answers.go); with the grant of the connection
// asking, what it can run, and the answer what it holds. tools are the
// connection's (MCP's), for a group, a search and an operation's own.
func describeNativeOperations(input leafenMCPOperationsInput, grant *nativeOperationGrant, tools []nativeOperationTool) (leafenMCPOperationsOutput, error) {
	operations, err := listNativeOperations()
	if err != nil {
		return leafenMCPOperationsOutput{}, err
	}
	output := leafenMCPOperationsOutput{Operations: []nativeOperation{}}
	wanted, query, detail := strings.TrimSpace(input.Operation), strings.TrimSpace(input.Query), strings.TrimSpace(input.Detail)
	// named is the one operation that wanted names, which the answer describes.
	named := false
	switch {
	case wanted != "":
		// A tool's name gives that tool with its input.
		for _, tool := range tools {
			if tool.name == wanted {
				found := tool.listed()
				found.Description, found.InputSchema = tool.description, tool.inputSchema
				output.Tools = []leafenMCPFoundTool{found}
			}
		}
		// An operation by its id, a filled-in path or its name
		// (native_operation_lookup.go); a name several share lists them.
		switch found := nativeOperationsLookedUp(operations, wanted); {
		case len(output.Tools) > 0:
		case len(found) == 1:
			named = true
			operation := found[0]
			operation.Group = nativeOperationGroupEntryOf(operation.ID).Group
			output.Operations = append(output.Operations, operation)
			output.Tools = nativeOperationToolsOfOperation(operation, tools)
		case len(found) > 1:
			for _, operation := range found[:min(len(found), nativeOperationLookupMax)] {
				output.Operations = append(output.Operations, nativeOperationListed(operation, grant, true))
			}
			output.Next = nativeOperationsSameNameLine(len(found), wanted, len(output.Operations))
		default:
			answerNativeOperationLookupNone(&output, operations, tools, grant, wanted)
		}
	case strings.TrimSpace(input.Group) != "" && query == "":
		answerNativeOperationGroup(&output, operations, tools, grant, input.Group, input.All)
	case query != "":
		answerNativeOperationSearch(&output, operations, tools, grant, query, input.All)
	case detail != "":
		// Every operation: its id, name, scope and confirmation; method,
		// path and contract with detail "full".
		output.Operations = operations
		for index := range output.Operations {
			if detail != "full" {
				output.Operations[index].Method, output.Operations[index].Path, output.Operations[index].Contract = "", "", ""
			}
			if grant != nil {
				output.Operations[index].ThisConnection = grant.of(output.Operations[index])
			}
		}
	default:
		// Asked for nothing in particular, the groups (2026-11-16: the
		// whole list was 171 KB; 2026-11-26: the areas of its paths said
		// little of what each holds).
		answerNativeOperationGroups(&output, operations, tools, grant, nativeOperationsMapNext)
	}
	if grant != nil {
		output.Connection = grant.connection()
	}
	if !named {
		return output, nil
	}
	if grant != nil {
		output.Operations[0].ThisConnection = grant.of(output.Operations[0])
	}
	return output, describeNativeOperation(&output, output.Operations[0], detail == "full")
}

// describeNativeOperation adds what a call sends one operation.
func describeNativeOperation(output *leafenMCPOperationsOutput, operation nativeOperation, full bool) error {
	_, contracts, err := nativeOperationRuntime()
	if err != nil {
		return err
	}
	described, err := nativeOperationInputsParsed()
	if err != nil {
		return err
	}
	contract := contracts.Contracts[operation.Contract]
	output.Records = map[string][]leafenMCPOperationRecordField{}
	found, known := described[operation.Handler]
	for _, query := range found.Query {
		output.Query = append(output.Query, leafenMCPOperationRecordField{Name: query.Name, Type: query.Type, Values: query.Values, Required: query.Required})
	}
	if known && found.Record != "" && operation.Method != http.MethodGet {
		output.Input, output.Actions = found.Record, found.Actions
		for _, record := range nativeOperationBodyRecords(output, operation, found, contracts, !full) {
			output.Records[record.name] = record.fields
		}
	} else if operation.Method != http.MethodGet && (known || len(contract.Records) == 0) {
		// A handler read that binds no body takes none: its contract's
		// records aren't what it takes (2026-11-16: an app's call that
		// takes none was described by the app contract's 150 records, 33 KB).
		output.Input = "none its handler reads as JSON; its path says what it acts on"
	}
	// Every record of the contract, its answers' too; where what its handler
	// takes wasn't read, one of them may be its body.
	if full || len(output.Records) == 0 && output.Input == "" && operation.Method != http.MethodGet && len(contract.Records) > 0 {
		for name, fields := range contract.Records {
			if _, done := output.Records[name]; done {
				continue
			}
			for _, field := range fields {
				output.Records[name] = append(output.Records[name], leafenMCPOperationRecordField{
					Name: field.Name, Type: field.Type, ZeroMeans: field.ZeroMeans, Deprecated: field.Deprecated,
					Values: nativeOperationEnumValues(contract.Enums, field.Type),
				})
			}
			if !full {
				output.Records[name] = foldNativeOperationRecord(output, name, output.Records[name])
			}
		}
	}
	describeNativeOperationAnswer(output, found.Answer, operation.Contract, contracts)
	// The kinds the records given name.
	for _, fields := range output.Records {
		for _, field := range fields {
			for name, values := range contract.Enums {
				if strings.Contains(field.Type, name) {
					if output.Kinds == nil {
						output.Kinds = map[string][]string{}
					}
					output.Kinds[name] = values
				}
			}
		}
	}
	if operation.Contract == "content-graph" && (full || output.Input != "") {
		output.BlockContents, output.PropertyConfigs = graphBlockContentShapes, graphPropertyConfigShapes
	}
	// One that takes bytes says what they are and what goes beside them.
	if bytes, takes := nativeOperationByteInputs[operation.ID]; takes {
		output.Input, output.Bytes = "bytes, not JSON: see bytes", &bytes
	}
	// What the records' free-form fields hold.
	for record, fields := range output.Records {
		for _, field := range fields {
			if shape := nativeOperationFieldShape(record, field.Name); shape != "" {
				if output.FieldShapes == nil {
					output.FieldShapes = map[string]string{}
				}
				output.FieldShapes[record+"."+field.Name] = shape
			}
		}
	}
	if len(output.Records) == 0 {
		output.Records = nil
	}
	slices.SortFunc(output.Query, func(a, b leafenMCPOperationRecordField) int { return strings.Compare(a.Name, b.Name) })
	return nil
}

// nativeOperationBodyRecord is a record an operation's body takes, with its
// fields: the body's own record, one its actions read, or one a field of
// another holds (held).
type nativeOperationBodyRecord struct {
	name   string
	held   bool
	fields []leafenMCPOperationRecordField
}

// nativeOperationBodyRecords are the records an operation's body takes, the
// body's first: those its actions read, and every record a field's type
// names, the handler's or its contract's. A record of the contract is given
// in the contract's words, marked required where the handler refuses a call
// without a field. With fold, a held record keeps its first fields and those
// a call needs, as a description gives it, and only those lead further.
func nativeOperationBodyRecords(output *leafenMCPOperationsOutput, operation nativeOperation, found nativeOperationInput, contracts nativeOperationContracts, fold bool) []nativeOperationBodyRecord {
	contract := contracts.Contracts[operation.Contract]
	// What the handler reads is named as the contracts name it
	// (scripts/native-operation-inputs, wire_names.go), so one of its records
	// is the contract's by that name alone.
	inContract := func(name string) bool {
		_, held := contract.Records[name]
		return held
	}
	records := []nativeOperationBodyRecord{}
	pending := []nativeOperationBodyRecord{{name: found.Record}}
	for _, record := range found.Actions {
		pending = append(pending, nativeOperationBodyRecord{name: record})
	}
	given := map[string]bool{}
	for len(pending) > 0 {
		next := pending[0]
		name := next.name
		pending = pending[1:]
		if given[strings.ToLower(name)] {
			continue
		}
		given[strings.ToLower(name)] = true
		fields := found.Records[name]
		read := map[string]nativeOperationInputField{}
		for _, field := range fields {
			read[field.Name] = field
		}
		if inContract(name) {
			// In the contract's words, with what the handler's checks say a
			// kind the contract names and doesn't list takes (EventWebhookType[]),
			// or a plain text whose checks refuse any other value (closed); a
			// plain text's other checks may name a few values it treats apart.
			for _, field := range contract.Records[name] {
				handled := read[field.Name]
				values := nativeOperationEnumValues(contract.Enums, field.Type)
				if len(values) == 0 && (nativeOperationNamedKind(field.Type) || handled.Closed) {
					values = handled.Values
				}
				next.fields = append(next.fields, leafenMCPOperationRecordField{
					Name: field.Name, Type: field.Type, ZeroMeans: field.ZeroMeans, Required: handled.Required,
					Deprecated: field.Deprecated, Values: values, Keys: handled.Keys, RequiredKeys: handled.RequiredKeys,
				})
			}
		} else {
			next.fields = []leafenMCPOperationRecordField{}
			for _, field := range fields {
				next.fields = append(next.fields, leafenMCPOperationRecordField{
					Name: field.Name, Type: field.Type, Values: field.Values, Required: field.Required,
					Keys: field.Keys, RequiredKeys: field.RequiredKeys,
				})
			}
		}
		if next.held && fold {
			next.fields = foldNativeOperationRecord(output, name, next.fields)
		}
		records = append(records, next)
		for _, field := range next.fields {
			for _, word := range strings.FieldsFunc(field.Type, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') }) {
				// A record the input's fields hold is the input's too.
				if _, held := found.Records[word]; held {
					pending = append(pending, nativeOperationBodyRecord{name: word, held: true})
				}
				if inContract(word) {
					pending = append(pending, nativeOperationBodyRecord{name: word, held: true})
				}
			}
		}
	}
	return records
}

// nativeOperationNamedKind says a contract's type is a kind it names, or a
// list of one, rather than a plain value or values written out.
func nativeOperationNamedKind(fieldType string) bool {
	kind := strings.TrimSuffix(strings.TrimSpace(fieldType), "[]")
	return kind != "" && kind[0] >= 'A' && kind[0] <= 'Z' && !strings.ContainsAny(kind, " |\"<")
}

// nativeOperationHeldFieldsShown is how many fields a record a field holds
// gives without detail "full": nine in ten such records have 11 or fewer,
// and the rest 13 to 34 (2026-11-16, 302 records).
const nativeOperationHeldFieldsShown = 12

// foldNativeOperationRecord keeps a record's first fields and those a call
// needs, and notes how many it left out.
func foldNativeOperationRecord(output *leafenMCPOperationsOutput, name string, fields []leafenMCPOperationRecordField) []leafenMCPOperationRecordField {
	if len(fields) <= nativeOperationHeldFieldsShown {
		return fields
	}
	shown := slices.Clone(fields[:nativeOperationHeldFieldsShown])
	for _, field := range fields[nativeOperationHeldFieldsShown:] {
		if field.Required {
			shown = append(shown, field)
		}
	}
	if left := len(fields) - len(shown); left > 0 {
		if output.MoreFields == nil {
			output.MoreFields = map[string]int{}
		}
		output.MoreFields[name] = left
	}
	return shown
}
