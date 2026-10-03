//go:build embed || unit

package web

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Vite emits content-hashed filenames under assets/, so the backend can apply
// immutable caching without relying on a reverse proxy to classify paths.
const staticAssetsCacheControl = "public, max-age=31536000, immutable"

// isFingerprintedEmbeddedAssetPath reports whether a cleaned URL path refers to
// a Vite asset whose filename contains the default eight-character build hash.
func isFingerprintedEmbeddedAssetPath(cleanPath string) bool {
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	cleanPath = strings.TrimPrefix(cleanPath, infiniteCanvasAppPrefix+"/")
	if !strings.HasPrefix(cleanPath, "assets/") {
		return false
	}

	filename := path.Base(cleanPath)
	extension := path.Ext(filename)
	stem := strings.TrimSuffix(filename, extension)
	const fingerprintLength = 8
	delimiterIndex := len(stem) - fingerprintLength - 1
	if extension == "" || delimiterIndex < 1 || stem[delimiterIndex] != '-' {
		return false
	}

	// Vite hashes use URL-safe characters and are stable for immutable caching.
	fingerprint := stem[delimiterIndex+1:]
	for _, char := range fingerprint {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

// isEmbeddedStaticAssetPath reports whether a cleaned URL path is a bundled
// frontend asset. Missing files under assets/ must 404 rather than fall back
// to index.html, otherwise browsers fail dynamic imports with
// "Failed to fetch dynamically imported module".
func isEmbeddedStaticAssetPath(cleanPath string) bool {
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	return strings.HasPrefix(cleanPath, "assets/") ||
		strings.HasPrefix(cleanPath, infiniteCanvasAppPrefix+"/assets/")
}

// applyStaticAssetCacheHeaders sets Cache-Control for long-cacheable static paths.
// index.html / SPA routes must keep no-cache and are not handled here.
func applyStaticAssetCacheHeaders(header http.Header, cleanPath string) {
	if header == nil || !isFingerprintedEmbeddedAssetPath(cleanPath) {
		return
	}
	header.Set("Cache-Control", staticAssetsCacheControl)
}

const infiniteCanvasAppPrefix = "canvas"

func isPrefixedAppPath(cleanPath, prefix string) bool {
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	cleanPath = strings.TrimSuffix(cleanPath, "/")
	return cleanPath == prefix || strings.HasPrefix(cleanPath, prefix+"/")
}

func isInfiniteCanvasAppPath(cleanPath string) bool {
	return isPrefixedAppPath(cleanPath, infiniteCanvasAppPrefix)
}

func isEmbeddedAppPath(cleanPath string) bool {
	return isInfiniteCanvasAppPath(cleanPath)
}

func resolveEmbeddedServePath(cleanPath string) string {
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	if cleanPath == "" {
		return "index.html"
	}
	trimmed := strings.Trim(cleanPath, "/")
	if trimmed == infiniteCanvasAppPrefix || trimmed == infiniteCanvasAppPrefix+"/index.html" {
		return infiniteCanvasAppPrefix + "/index.html"
	}
	return cleanPath
}

func serveResolvedEmbeddedPath(w http.ResponseWriter, r *http.Request, fsys fs.FS, cleanPath string, spa http.Handler) {
	if w == nil || r == nil || fsys == nil {
		http.NotFound(w, r)
		return
	}
	servePath := resolveEmbeddedServePath(cleanPath)
	if file, err := fsys.Open(servePath); err == nil {
		defer func() { _ = file.Close() }()
		info, statErr := file.Stat()
		if statErr == nil && !info.IsDir() {
			applyStaticAssetCacheHeaders(w.Header(), servePath)
			modTime := info.ModTime()
			name := path.Base(servePath)
			if seeker, ok := file.(io.ReadSeeker); ok {
				if raw, readErr := readAllAndRewind(seeker); readErr == nil &&
					writeMaybeCompressed(w, r, name, raw) {
					return
				}
				http.ServeContent(w, r, name, modTime, seeker)
				return
			}
			body, readErr := io.ReadAll(file)
			if readErr == nil {
				serveMaybeCompressedContent(w, r, name, body, func() {
					http.ServeContent(w, r, name, modTime, bytes.NewReader(body))
				})
				return
			}
		}
	}
	if isEmbeddedAppPath(cleanPath) || isEmbeddedAppPath(servePath) || isEmbeddedStaticAssetPath(servePath) {
		http.NotFound(w, r)
		return
	}
	if spa != nil {
		spa.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

func fileExistsInFS(fsys fs.FS, name string) bool {
	if fsys == nil || name == "" {
		return false
	}
	file, err := fsys.Open(name)
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}
