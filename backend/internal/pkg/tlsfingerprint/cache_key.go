package tlsfingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// CacheKey identifies the complete configured template, including slice order.
func (p *Profile) CacheKey() string {
	if p == nil {
		return "none"
	}
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
