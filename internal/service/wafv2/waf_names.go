package wafv2

import (
	"encoding/hex"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
)

const wafNameSuffixBytes = 13

func resolveWAFName(name, namePrefix *string, random io.Reader) (string, error) {
	if name != nil {
		return *name, nil
	}
	prefix := "unobin-"
	if namePrefix != nil {
		prefix = *namePrefix
	}
	var suffix [wafNameSuffixBytes]byte
	if _, err := io.ReadFull(random, suffix[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(suffix[:]), nil
}

func wafNamePrefix(name string) *string {
	split := len(name) - hex.EncodedLen(wafNameSuffixBytes)
	if split <= 0 {
		return nil
	}
	if _, err := hex.DecodeString(name[split:]); err != nil {
		return nil
	}
	return aws.String(name[:split])
}
