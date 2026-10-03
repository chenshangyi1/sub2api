//go:build embed || unit

package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
)

const minCompressibleBytes = 256

type compressedBody struct {
	once   sync.Once
	gzip   []byte
	brotli []byte
}

var compressedBodies sync.Map // map[sha256hex]*compressedBody

func isCompressibleStaticPath(name string) bool {
	return staticContentType(name) != ""
}

func staticContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".json", ".map":
		return "application/json; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".xml":
		return "application/xml; charset=utf-8"
	case ".wasm":
		return "application/wasm"
	default:
		return ""
	}
}

func negotiateStaticEncoding(acceptEncoding string) string {
	accept := strings.ToLower(acceptEncoding)
	if encodingAccepted(accept, "br") {
		return "br"
	}
	if encodingAccepted(accept, "gzip") || encodingAccepted(accept, "x-gzip") {
		return "gzip"
	}
	return ""
}

func encodingAccepted(accept, token string) bool {
	for _, part := range strings.Split(accept, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, params, _ := strings.Cut(part, ";")
		if strings.TrimSpace(name) != token {
			continue
		}
		quality := 1.0
		for _, param := range strings.Split(params, ";") {
			param = strings.TrimSpace(param)
			key, value, ok := strings.Cut(param, "=")
			if !ok || strings.TrimSpace(key) != "q" {
				continue
			}
			if strings.TrimSpace(value) == "0" || strings.HasPrefix(strings.TrimSpace(value), "0.") {
				quality = 0
			}
		}
		return quality > 0
	}
	return false
}

func compressedVariant(raw []byte, encoding string) []byte {
	if len(raw) < minCompressibleBytes || encoding == "" {
		return nil
	}
	sum := sha256.Sum256(raw)
	key := hex.EncodeToString(sum[:])
	value, _ := compressedBodies.LoadOrStore(key, &compressedBody{})
	cached, _ := value.(*compressedBody)
	if cached == nil {
		return nil
	}
	cached.once.Do(func() {
		cached.brotli = compressBrotli(raw)
		cached.gzip = compressGzip(raw)
	})
	switch encoding {
	case "br":
		return cached.brotli
	case "gzip":
		return cached.gzip
	default:
		return nil
	}
}

func compressBrotli(raw []byte) []byte {
	var buf bytes.Buffer
	w := brotli.NewWriterLevel(&buf, brotli.DefaultCompression)
	if _, err := w.Write(raw); err != nil {
		_ = w.Close()
		return nil
	}
	if err := w.Close(); err != nil {
		return nil
	}
	if buf.Len() >= len(raw) {
		return nil
	}
	return buf.Bytes()
}

func compressGzip(raw []byte) []byte {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil
	}
	if _, err := w.Write(raw); err != nil {
		_ = w.Close()
		return nil
	}
	if err := w.Close(); err != nil {
		return nil
	}
	if buf.Len() >= len(raw) {
		return nil
	}
	return buf.Bytes()
}

func writeMaybeCompressed(w http.ResponseWriter, r *http.Request, name string, raw []byte) bool {
	if w == nil || r == nil || !isCompressibleStaticPath(name) {
		return false
	}
	if strings.TrimSpace(r.Header.Get("Range")) != "" {
		return false
	}
	encoding := negotiateStaticEncoding(r.Header.Get("Accept-Encoding"))
	body := compressedVariant(raw, encoding)
	if len(body) == 0 {
		return false
	}
	header := w.Header()
	header.Del("Content-Length")
	if header.Get("Content-Type") == "" {
		if contentType := staticContentType(name); contentType != "" {
			header.Set("Content-Type", contentType)
		}
	}
	header.Set("Content-Encoding", encoding)
	header.Add("Vary", "Accept-Encoding")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
	return true
}

func serveMaybeCompressedContent(w http.ResponseWriter, r *http.Request, name string, raw []byte, fallback func()) {
	if writeMaybeCompressed(w, r, name, raw) {
		return
	}
	if fallback != nil {
		fallback()
	}
}

func readAllAndRewind(src io.ReadSeeker) ([]byte, error) {
	if src == nil {
		return nil, io.EOF
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(src)
	if err != nil {
		return nil, err
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return body, nil
}
