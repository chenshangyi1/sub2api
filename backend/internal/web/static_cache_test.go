//go:build unit

package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/andybalholm/brotli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsFingerprintedEmbeddedAssetPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "fingerprinted_js", path: "assets/index-AbCd1234.js", want: true},
		{name: "fingerprinted_css", path: "assets/app-a1B2c3D4.css", want: true},
		{name: "fingerprinted_url_safe_hash", path: "assets/app-aB1-2_Cd.css", want: true},
		{name: "nested_fingerprinted_asset", path: "assets/vendor/chunk-AbCd1234.js", want: true},
		{name: "leading_slash_fingerprinted_asset", path: "/assets/index-AbCd1234.js", want: true},
		{name: "infinite_canvas_fingerprinted_js", path: "canvas/assets/index-AbCd1234.js", want: true},
		{name: "unhashed_asset", path: "assets/index.js", want: false},
		{name: "short_suffix", path: "assets/index-abc123.js", want: false},
		{name: "logo", path: "logo.png", want: false},
		{name: "favicon", path: "favicon.ico", want: false},
		{name: "fingerprint_outside_assets", path: "downloads/index-AbCd1234.js", want: false},
		{name: "index_html", path: "index.html", want: false},
		{name: "spa_route", path: "dashboard", want: false},
		{name: "assets_prefix_only", path: "assets", want: false},
		{name: "similar_name", path: "assets-backup/x.js", want: false},
		{name: "empty", path: "", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isFingerprintedEmbeddedAssetPath(tc.path))
		})
	}
}

func TestResolveEmbeddedServePath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "canvas/index.html", resolveEmbeddedServePath("canvas"))
	assert.Equal(t, "canvas/index.html", resolveEmbeddedServePath("canvas/"))
	assert.Equal(t, "canvas/index.html", resolveEmbeddedServePath("/canvas/index.html"))
	assert.Equal(t, "canvas/assets/index-AbCd1234.js", resolveEmbeddedServePath("canvas/assets/index-AbCd1234.js"))
	assert.Equal(t, "index.html", resolveEmbeddedServePath(""))
	assert.Equal(t, "index.html", resolveEmbeddedServePath("index.html"))
	assert.Equal(t, "dashboard", resolveEmbeddedServePath("dashboard"))
	assert.True(t, isInfiniteCanvasAppPath("canvas"))
	assert.True(t, isInfiniteCanvasAppPath("/canvas/"))
	assert.True(t, isInfiniteCanvasAppPath("canvas/index.html"))
	assert.False(t, isInfiniteCanvasAppPath("infinite-canvas"))
	assert.False(t, isInfiniteCanvasAppPath("index.html"))
}

func TestServeResolvedEmbeddedPath_infiniteCanvasApp(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(`<!doctype html><title>雨柠 - AI API Gateway</title>`)},
		"canvas/index.html": &fstest.MapFile{
			Data: []byte(`<!doctype html><title>无限画布</title><script src="/canvas/assets/index-AbCd1234.js"></script>`),
		},
		"canvas/assets/index-AbCd1234.js": &fstest.MapFile{Data: []byte(`console.log("canvas")`)},
	}
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><title>雨柠 - AI API Gateway</title>`)
	})

	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		serveResolvedEmbeddedPath(w, req, fsys, strings.TrimPrefix(path, "/"), spa)
		return w
	}

	canvasIndex := get("/canvas/index.html")
	assert.Equal(t, http.StatusOK, canvasIndex.Code)
	assert.Contains(t, canvasIndex.Body.String(), "无限画布")
	assert.NotContains(t, canvasIndex.Body.String(), "雨柠")

	canvasDir := get("/canvas/")
	assert.Equal(t, http.StatusOK, canvasDir.Code)
	assert.Contains(t, canvasDir.Body.String(), "无限画布")

	canvasBare := get("/canvas")
	assert.Equal(t, http.StatusOK, canvasBare.Code)
	assert.Contains(t, canvasBare.Body.String(), "无限画布")

	canvasAsset := get("/canvas/assets/index-AbCd1234.js")
	assert.Equal(t, http.StatusOK, canvasAsset.Code)
	assert.Contains(t, canvasAsset.Body.String(), `console.log("canvas")`)

	canvasMissing := get("/canvas/missing.js")
	assert.Equal(t, http.StatusNotFound, canvasMissing.Code)
	assert.NotContains(t, canvasMissing.Body.String(), "雨柠")

	spaRoute := get("/dashboard")
	assert.Equal(t, http.StatusOK, spaRoute.Code)
	assert.Contains(t, spaRoute.Body.String(), "雨柠")
}

func TestIsEmbeddedStaticAssetPath(t *testing.T) {
	t.Parallel()

	assert.True(t, isEmbeddedStaticAssetPath("assets/DashboardView-CRpF0aUO.js"))
	assert.True(t, isEmbeddedStaticAssetPath("/assets/index-AbCd1234.js"))
	assert.True(t, isEmbeddedStaticAssetPath("assets/index.js"))
	assert.True(t, isEmbeddedStaticAssetPath("canvas/assets/index-AbCd1234.js"))
	assert.True(t, isEmbeddedStaticAssetPath("/canvas/assets/index.js"))
	assert.False(t, isEmbeddedStaticAssetPath("dashboard"))
	assert.False(t, isEmbeddedStaticAssetPath("index.html"))
	assert.False(t, isEmbeddedStaticAssetPath("canvas/index.html"))
	assert.False(t, isEmbeddedStaticAssetPath("infinite-canvas"))
	assert.False(t, isEmbeddedStaticAssetPath("assets-backup/x.js"))
}

func TestApplyStaticAssetCacheHeaders(t *testing.T) {
	t.Parallel()

	t.Run("sets_immutable_cache_for_fingerprinted_asset", func(t *testing.T) {
		t.Parallel()
		header := make(http.Header)
		applyStaticAssetCacheHeaders(header, "assets/index-AbCd1234.js")
		assert.Equal(t, staticAssetsCacheControl, header.Get("Cache-Control"))
	})

	for _, path := range []string{"assets/index.js", "logo.png", "favicon.ico", "index.html"} {
		path := path
		t.Run("skips_"+path, func(t *testing.T) {
			t.Parallel()
			header := make(http.Header)
			applyStaticAssetCacheHeaders(header, path)
			assert.Empty(t, header.Get("Cache-Control"))
		})
	}

	t.Run("nil_header_is_noop", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			applyStaticAssetCacheHeaders(nil, "assets/index-AbCd1234.js")
		})
	})
}

func TestServeResolvedEmbeddedPath_compressesJSWhenClientAcceptsBrotli(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("console.log('canvas-static');\n", 80)
	fsys := fstest.MapFS{
		"canvas/assets/index-AbCd1234.js": &fstest.MapFile{Data: []byte(payload)},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/canvas/assets/index-AbCd1234.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	serveResolvedEmbeddedPath(w, req, fsys, "canvas/assets/index-AbCd1234.js", nil)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "br", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", w.Header().Get("Vary"))
	assert.Equal(t, staticAssetsCacheControl, w.Header().Get("Cache-Control"))
	assert.Equal(t, "text/javascript; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Less(t, w.Body.Len(), len(payload))
	decoded := decodeBrotli(t, w.Body.Bytes())
	assert.Equal(t, payload, string(decoded))
}

func TestServeResolvedEmbeddedPath_skipsCompressionWithoutAcceptEncoding(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("console.log('plain');\n", 80)
	fsys := fstest.MapFS{
		"assets/index-AbCd1234.js": &fstest.MapFile{Data: []byte(payload)},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/index-AbCd1234.js", nil)
	serveResolvedEmbeddedPath(w, req, fsys, "assets/index-AbCd1234.js", nil)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("Content-Encoding"))
	assert.Equal(t, payload, w.Body.String())
}

func TestServeResolvedEmbeddedPath_gzipWhenBrotliRejected(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("export const x = 1;\n", 80)
	fsys := fstest.MapFS{
		"assets/index-AbCd1234.js": &fstest.MapFile{Data: []byte(payload)},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/index-AbCd1234.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, br;q=0")
	serveResolvedEmbeddedPath(w, req, fsys, "assets/index-AbCd1234.js", nil)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "text/javascript; charset=utf-8", w.Header().Get("Content-Type"))
	decoded := decodeGzip(t, w.Body.Bytes())
	assert.Equal(t, payload, string(decoded))
}

func TestServeResolvedEmbeddedPath_compressedCSSKeepsStylesheetMIME(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("body{color:#111}\n", 80)
	fsys := fstest.MapFS{
		"assets/index-AbCd1234.css": &fstest.MapFile{Data: []byte(payload)},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/index-AbCd1234.css", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	serveResolvedEmbeddedPath(w, req, fsys, "assets/index-AbCd1234.css", nil)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "br", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "text/css; charset=utf-8", w.Header().Get("Content-Type"))
	decoded := decodeBrotli(t, w.Body.Bytes())
	assert.Equal(t, payload, string(decoded))
}

func TestServeResolvedEmbeddedPath_skipsCompressionForRangeRequests(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("0123456789", 40)
	fsys := fstest.MapFS{
		"assets/index-AbCd1234.js": &fstest.MapFile{Data: []byte(payload)},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/index-AbCd1234.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	req.Header.Set("Range", "bytes=0-9")
	serveResolvedEmbeddedPath(w, req, fsys, "assets/index-AbCd1234.js", nil)

	require.Equal(t, http.StatusPartialContent, w.Code)
	assert.Empty(t, w.Header().Get("Content-Encoding"))
	assert.Equal(t, payload[:10], w.Body.String())
}

func decodeBrotli(t *testing.T, raw []byte) []byte {
	t.Helper()
	decoded, err := io.ReadAll(brotli.NewReader(bytes.NewReader(raw)))
	require.NoError(t, err)
	return decoded
}

func decodeGzip(t *testing.T, raw []byte) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	decoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	return decoded
}
