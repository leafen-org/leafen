package app

import (
	"slices"
	"strings"
)

// An operation's answer is the record its handler sends back when it does
// what was asked, read from the handler's source as what it takes is
// (scripts/native-operation-inputs, answer.go), so a contract's record of
// another operation isn't taken for it (2026-11-16, an agent calling every
// operation: a property's trash and restore answered a content change's
// receipt, while their contract's DatabasePropertyTrashResponse — the
// permanent delete's — looked like theirs). It is named as its contract
// names it, as the body's record is (scripts/native-operation-inputs,
// wire_names.go). Where a contract the catalog holds has that record, its
// fields come with it in the contract's words, the operation's own contract
// first; an answer no such contract holds is named alone.

// describeNativeOperationAnswer adds an operation's answer to its
// description.
func describeNativeOperationAnswer(output *leafenMCPOperationsOutput, answer, own string, contracts nativeOperationContracts) {
	name := strings.TrimPrefix(answer, "[]")
	switch name {
	case "":
		return
	case "none":
		output.Answer = "none: it answers with no body"
		return
	}
	output.Answer = answer
	names := make([]string, 0, len(contracts.Contracts))
	for contractName := range contracts.Contracts {
		if contractName != own {
			names = append(names, contractName)
		}
	}
	slices.Sort(names)
	for _, contractName := range append([]string{own}, names...) {
		contract, known := contracts.Contracts[contractName]
		if !known {
			continue
		}
		fields, held := contract.Records[name]
		if !held {
			continue
		}
		if _, given := output.Records[name]; given {
			return
		}
		described := make([]leafenMCPOperationRecordField, 0, len(fields))
		for _, field := range fields {
			described = append(described, leafenMCPOperationRecordField{
				Name: field.Name, Type: field.Type, ZeroMeans: field.ZeroMeans, Deprecated: field.Deprecated,
				Values: nativeOperationEnumValues(contract.Enums, field.Type),
			})
		}
		output.Records[name] = described
		return
	}
}
