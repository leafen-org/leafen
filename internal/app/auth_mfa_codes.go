package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/tools/security"
)

// The pure codes of two-step verification: TOTP secrets and codes, recovery
// codes and their hashes, and challenge tickets. No storage or session
// decision lives here; auth_mfa_policy.go owns those.

func randomAuthMFABytes(length int) ([]byte, error) {
	value := make([]byte, length)
	_, err := rand.Read(value)
	return value, err
}

func hashAuthMFAValue(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

// normalizedRecoveryCode reads a recovery code as typed: any case, with or
// without its dashes and spaces, with a Korean keyboard on (typed_code_keys.go).
// The letters of a code made before they were digits read as before.
func normalizedRecoveryCode(value string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(typedCodeKeys(value))))
}

// recoveryCodeLength is a recovery code's digits, in fours: a phone types
// them on its number pad (account_device_codes.go). Twenty are more than the
// 64 random bits of the sixteen hexadecimal letters before (10^20 > 2^64).
const recoveryCodeLength = 20

func recoveryCodeHash(ownerID string, code string) string {
	return hashAuthMFAValue(ownerID + ":" + normalizedRecoveryCode(code))
}

func generateRecoveryCodes(ownerID string) ([]string, []string, error) {
	codes := make([]string, 0, authMFARecoveryCodeLimit)
	hashes := make([]string, 0, authMFARecoveryCodeLimit)
	for len(codes) < authMFARecoveryCodeLimit {
		code := codeInFours(security.RandomStringWithAlphabet(recoveryCodeLength, deviceCodeAlphabet))
		hash := recoveryCodeHash(ownerID, code)
		duplicate := false
		for _, existing := range hashes {
			if existing == hash {
				duplicate = true
				break
			}
		}
		if !duplicate {
			codes = append(codes, code)
			hashes = append(hashes, hash)
		}
	}
	return codes, hashes, nil
}

func generateTOTPSecret() (string, error) {
	random, err := randomAuthMFABytes(20)
	if err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random), nil
}

func generateMFATicket() (string, error) {
	random, err := randomAuthMFABytes(32)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

func matchingTOTPCounter(secret string, code string, now time.Time, minimumExclusive int64) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != authMFATOTPDigits {
		return 0, false
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return 0, false
	}
	current := now.Unix() / authMFATOTPPeriodSeconds
	for _, counter := range []int64{current - 1, current, current + 1} {
		if counter <= minimumExclusive {
			continue
		}
		message := make([]byte, 8)
		binary.BigEndian.PutUint64(message, uint64(counter))
		mac := hmac.New(sha1.New, key)
		_, _ = mac.Write(message)
		digest := mac.Sum(nil)
		offset := digest[len(digest)-1] & 0x0f
		binaryCode := (uint32(digest[offset])&0x7f)<<24 |
			uint32(digest[offset+1])<<16 | uint32(digest[offset+2])<<8 | uint32(digest[offset+3])
		expected := fmt.Sprintf("%0*d", authMFATOTPDigits, binaryCode%1_000_000)
		if hmac.Equal([]byte(expected), []byte(code)) {
			return counter, true
		}
	}
	return 0, false
}
