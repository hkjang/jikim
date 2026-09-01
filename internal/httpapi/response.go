package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const maxJSONBody = 2 << 20

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeData(w http.ResponseWriter, status int, value any) {
	writeJSON(w, status, map[string]any{"data": value})
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"code": code, "message": message, "request_id": requestIDFrom(r),
	}})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "JSON 요청 본문이 올바르지 않습니다")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "JSON 본문은 하나만 허용됩니다")
		return false
	}
	return true
}

func parsePage(r *http.Request) (int, int) {
	limit, offset := 50, 0
	if value := r.URL.Query().Get("limit"); value != "" {
		_, _ = fmtSscanf(value, &limit)
	}
	if value := r.URL.Query().Get("offset"); value != "" {
		_, _ = fmtSscanf(value, &offset)
	}
	return limit, offset
}

func fmtSscanf(value string, dst *int) (int, error) {
	var n int
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, errors.New("number")
		}
		n = n*10 + int(c-'0')
	}
	*dst = n
	return 1, nil
}
