package httpapi

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/hkjang/jikim/internal/version"
)

type spaHandler struct {
	root       string
	fileServer http.Handler
}

func discoverStaticHandler(logger *slog.Logger) http.Handler {
	for _, candidate := range []string{"/app/web", "web/dist"} {
		index := filepath.Join(candidate, "index.html")
		if info, err := os.Stat(index); err == nil && !info.IsDir() {
			absolute, err := filepath.Abs(candidate)
			if err != nil {
				continue
			}
			logger.Info("웹 UI 활성화", "directory", absolute)
			return &spaHandler{root: absolute, fileServer: http.FileServer(http.Dir(absolute))}
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
	http.ServeFile(w, r, filepath.Join(h.root, "index.html"))
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
