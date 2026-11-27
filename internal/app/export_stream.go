package app

import (
	"crypto/sha256"
	"encoding"
	"encoding/base64"
	"hash"
)

func restoreExportContentHasher(state string, invalid error) (hash.Hash, error) {
	hasher := sha256.New()
	if state == "" {
		return hasher, nil
	}
	encoded, err := base64.RawStdEncoding.DecodeString(state)
	if err != nil {
		return nil, invalid
	}
	unmarshaler, ok := hasher.(encoding.BinaryUnmarshaler)
	if !ok || unmarshaler.UnmarshalBinary(encoded) != nil {
		return nil, invalid
	}
	return hasher, nil
}

func encodeExportContentHasher(hasher hash.Hash, invalid error) (string, error) {
	marshaler, ok := hasher.(encoding.BinaryMarshaler)
	if !ok {
		return "", invalid
	}
	state, err := marshaler.MarshalBinary()
	if err != nil {
		return "", invalid
	}
	return base64.RawStdEncoding.EncodeToString(state), nil
}

func jsonArrayExportWireFragment(fragment string, hasPrevious bool) string {
	if fragment == "" {
		return ""
	}
	if hasPrevious {
		return "," + fragment
	}
	return "[" + fragment
}
