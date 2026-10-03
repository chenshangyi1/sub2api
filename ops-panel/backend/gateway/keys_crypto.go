// 网关 API Key 哈希、前缀脱敏与自动生成。
package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// HashAPIKey 对明文 API Key 做 SHA-256 hex，用于入库与鉴权比对。
func HashAPIKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// KeyPrefix 返回密钥脱敏前缀（展示用），过短则原样返回。
func KeyPrefix(plain string) string {
	plain = strings.TrimSpace(plain)
	if len(plain) <= 10 {
		return plain
	}
	return plain[:7] + "..." + plain[len(plain)-4:]
}

// allowedAPIKeyLens 自动生成时 sk- 之后的字符长度（总长 = 3 + n）。
var allowedAPIKeyLens = map[int]struct{}{
	16: {}, 24: {}, 32: {}, 48: {}, 64: {},
}

// defaultAPIKeyLen 自动生成密钥时 sk- 后默认十六进制长度（总长 51）。
const defaultAPIKeyLen = 48

// GenerateAPIKey 生成 sk- + n 位十六进制密钥。n 仅允许 16/24/32/48/64；非法时回退 48。
// 例如 n=48 → "sk-" + 48 hex，总长 51。
func GenerateAPIKey(keyLen int) (string, error) {
	if _, ok := allowedAPIKeyLens[keyLen]; !ok {
		keyLen = defaultAPIKeyLen
	}
	// 合法取值均为偶数，用 n/2 字节 hex 编码得到恰好 n 个字符
	buf := make([]byte, keyLen/2)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "sk-" + hex.EncodeToString(buf), nil
}
