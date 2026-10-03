package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const sha256HexLength = sha256.Size * 2

func Fingerprint(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal migration fingerprint: %w", err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
