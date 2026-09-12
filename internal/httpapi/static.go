package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hkjang/jikim/internal/version"
)

// pageDecorator rewrites the SPA shell before it is sent. It is how the
// tracking snippet gets in, and it may set response headers for the page.
type pageDecorator func(w http.ResponseWriter, r *http.Request, page []byte) []byte

type spaHandler struct {
	root       string
	fileServer http.Handler
	decorate   pageDecorator
}

func discoverStaticHandler(logger *slog.Logger, decorate pageDecorator) http.Handler {
	for _, candidate := range []string{"/app/web", "web/dist"} {
		index := filepath.Join(candidate, "index.html")
		if info, err := os.Stat(index); err == nil && !info.IsDir() {
			absolute, err := filepath.Abs(candidate)
			if err != nil {
				continue
			}
			logger.Info("웹 UI 활성화", "directory", absolute)
			return &spaHandler{root: absolute, fileServer: http.FileServer(http.Dir(absolute)), decorate: decorate}
		}
	}
	logger.Warn("웹 UI 파일을 찾지 못했습니다", "searched", []string{"/app/web/index.html", "web/dist/index.html"})
	return nil
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if clean != "." && !strings.HasPrefix(clean, "..") {
		candidate := filepath.Join(h.root, clean)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			h.fileServer.ServeHTTP(w, r)
			return
		}
	}
	h.serveIndex(w, r)
}

// serveIndex sends the SPA shell. The file is small and its rewrite depends on
// the request, so it is read per request rather than cached in a second place.
func (h *spaHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	page, err := os.ReadFile(filepath.Join(h.root, "index.html"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if h.decorate != nil {
		page = h.decorate(w, r, page)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(page))
}

func (s *Server) serveFrontend(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/v1/") || path == "/mcp" ||
		path == "/healthz" || path == "/readyz" {
		writeError(w, r, http.StatusNotFound, "not_found", "요청한 API를 찾을 수 없습니다")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "허용되지 않은 요청 방식입니다")
		return
	}
	if s.static != nil {
		s.static.ServeHTTP(w, r)
		return
	}
	if path == "/" {
		writeData(w, http.StatusOK, map[string]any{"service": version.Current(), "ui": "not_built"})
		return
	}
	writeError(w, r, http.StatusNotFound, "not_found", "화면 파일을 찾을 수 없습니다")
}
