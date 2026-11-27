package app

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// How an organization's DLP policy is stored and normalized: the
// organization module keeps the policy in this form, and the core's schema
// check validates every stored policy against it.

var (
	errDLPPolicyInvalid = errors.New(errorCodeDLPPolicyInvalid)
	dlpFold             = cases.Fold()
)

type storedOrganizationDLPPolicy struct {
	OrganizationID       string `db:"organization_id"`
	Enabled              int    `db:"enabled"`
	ContentScanMode      string `db:"content_scan_mode"`
	SensitiveTermsJSON   string `db:"sensitive_terms_json"`
	BlockPublicSharing   int    `db:"block_public_sharing"`
	BlockExternalSharing int    `db:"block_external_sharing"`
	BlockFileDownloads   int    `db:"block_file_downloads"`
	BlockExports         int    `db:"block_exports"`
	Revision             int    `db:"revision"`
	UpdatedAt            int64  `db:"updated_at"`
}

func normalizeDLPPolicyInput(input dlpPolicyInput) (dlpPolicyInput, error) {
	if input.ContentScanMode == "" {
		input.ContentScanMode = dlpContentScanBlock
	}
	if input.ContentScanMode != dlpContentScanOff && input.ContentScanMode != dlpContentScanBlock {
		return dlpPolicyInput{}, errDLPPolicyInvalid
	}
	if len(input.SensitiveTerms) > dlpMaxSensitiveTerms {
		return dlpPolicyInput{}, errDLPPolicyInvalid
	}
	normalizedTerms := make([]string, 0, len(input.SensitiveTerms))
	seen := make(map[string]struct{}, len(input.SensitiveTerms))
	for _, rawTerm := range input.SensitiveTerms {
		term := norm.NFKC.String(strings.TrimSpace(rawTerm))
		if term == "" || utf8.RuneCountInString(term) > dlpMaxSensitiveTermRunes {
			return dlpPolicyInput{}, errDLPPolicyInvalid
		}
		key := dlpFold.String(term)
		if _, duplicate := seen[key]; duplicate {
			return dlpPolicyInput{}, errDLPPolicyInvalid
		}
		seen[key] = struct{}{}
		normalizedTerms = append(normalizedTerms, term)
	}
	input.SensitiveTerms = normalizedTerms
	return input, nil
}

func (stored storedOrganizationDLPPolicy) response() (dlpPolicyResponse, error) {
	terms := []string{}
	if err := json.Unmarshal([]byte(stored.SensitiveTermsJSON), &terms); err != nil {
		return dlpPolicyResponse{}, err
	}
	return dlpPolicyResponse{
		Enabled: stored.Enabled == 1, ContentScanMode: stored.ContentScanMode,
		SensitiveTerms: terms, BlockPublicSharing: stored.BlockPublicSharing == 1,
		BlockExternalSharing: stored.BlockExternalSharing == 1,
		BlockFileDownloads:   stored.BlockFileDownloads == 1,
		BlockExports:         stored.BlockExports == 1, Revision: stored.Revision,
	}, nil
}

func dlpPolicyMatchesInput(policy dlpPolicyResponse, input dlpPolicyInput) bool {
	return policy.Enabled == input.Enabled && policy.ContentScanMode == input.ContentScanMode &&
		slices.Equal(policy.SensitiveTerms, input.SensitiveTerms) &&
		policy.BlockPublicSharing == input.BlockPublicSharing &&
		policy.BlockExternalSharing == input.BlockExternalSharing &&
		policy.BlockFileDownloads == input.BlockFileDownloads && policy.BlockExports == input.BlockExports
}
