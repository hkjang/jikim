package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hkjang/jikim/internal/store"
)

func TestRequestedOpenBaoVersion(t *testing.T) {
	for raw, want := range map[string]int{"": 0, "0": 0, "-1": 0, "1": 1, "27": 27} {
		got, err := requestedOpenBaoVersion(raw)
		if err != nil || got != want {
			t.Fatalf("requestedOpenBaoVersion(%q)=(%d,%v), want (%d,nil)", raw, got, err, want)
		}
	}
	for _, raw := range []string{"1.5", "latest"} {
		if _, err := requestedOpenBaoVersion(raw); err == nil {
			t.Fatalf("invalid version %q accepted", raw)
		}
	}
}

func TestOpenBaoListFallback(t *testing.T) {
	for _, raw := range []string{"true", "TRUE", "1", " true "} {
		if !openBaoListFallback(raw) {
			t.Fatalf("list fallback %q rejected", raw)
		}
	}
	for _, raw := range []string{"", "false", "0", "yes"} {
		if openBaoListFallback(raw) {
			t.Fatalf("non-list value %q accepted", raw)
		}
	}
}

func TestOpenBaoMetadataRootListRoute(t *testing.T) {
	mux := http.NewServeMux()
	server := &Server{}
	server.openBaoRoutes(mux)
	for _, target := range []string{
		"/v1/secret/metadata?list=true",
		"/v1/secret/metadata/?list=true",
		"/v1/secret/metadata/team?list=true",
	} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		_, pattern := mux.Handler(request)
		if pattern == "" {
			t.Fatalf("metadata list fallback route missing for %s", target)
		}
	}
}

func TestOpenBaoMetadataWireFields(t *testing.T) {
	created := time.Date(2026, 9, 1, 1, 2, 3, 4, time.UTC)
	updated := created.Add(time.Hour)
	deleted := created.Add(30 * time.Minute)
	metadata := store.OpenBaoKVMetadata{
		CurrentVersion: 2,
		OldestVersion:  0,
		CreatedTime:    created,
		UpdatedTime:    updated,
		CustomMetadata: map[string]any{"environment": "PRD"},
		Versions: []store.OpenBaoKVVersion{
			{Version: 1, CreatedTime: created, DeletionTime: &deleted},
			{Version: 2, CreatedTime: updated, Destroyed: true},
		},
	}
	data := openBaoMetadataData(metadata)
	for _, field := range []string{"cas_required", "created_time", "current_metadata_version", "current_version",
		"custom_metadata", "delete_version_after", "max_versions", "metadata_cas_required", "oldest_version",
		"updated_time", "versions"} {
		if _, ok := data[field]; !ok {
			t.Fatalf("metadata field %q is missing: %#v", field, data)
		}
	}
	versions, ok := data["versions"].(map[string]any)
	if !ok || len(versions) != 2 {
		t.Fatalf("versions=%#v", data["versions"])
	}
	version1 := versions["1"].(map[string]any)
	if version1["deletion_time"] != deleted.Format(time.RFC3339Nano) || version1["destroyed"] != false {
		t.Fatalf("soft-deleted metadata=%#v", version1)
	}
	version2 := versions["2"].(map[string]any)
	if version2["deletion_time"] != "" || version2["destroyed"] != true {
		t.Fatalf("destroyed metadata=%#v", version2)
	}
	if len(version1) != 3 || len(version2) != 3 {
		t.Fatalf("version metadata contains non-OpenBao fields: %#v", versions)
	}
	versionData := openBaoVersionData(metadata, metadata.Versions[0])
	if versionData["version"] != 1 || versionData["custom_metadata"].(map[string]any)["environment"] != "PRD" {
		t.Fatalf("version data=%#v", versionData)
	}
}

func TestDecodeBaoJSONIgnoresUnknownFields(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/secret/delete/example", strings.NewReader(`{"versions":[1],"unknown":true}`))
	response := httptest.NewRecorder()
	var input baoKVVersionsInput
	if !decodeBaoJSON(response, request, &input) {
		t.Fatalf("OpenBao-compatible unknown field was rejected: %s", response.Body.String())
	}
	if len(input.Versions) != 1 || input.Versions[0] != 1 {
		t.Fatalf("decoded versions=%v", input.Versions)
	}
}

func TestDecodeBaoJSONUsesOpenBaoErrorEnvelope(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/secret/delete/example", strings.NewReader(`{"versions":[1]} {}`))
	response := httptest.NewRecorder()
	var input baoKVVersionsInput
	if decodeBaoJSON(response, request, &input) {
		t.Fatal("multiple JSON values were accepted")
	}
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["errors"]; !ok {
		t.Fatalf("OpenBao errors envelope missing: %s", response.Body.String())
	}
	if _, ok := body["error"]; ok {
		t.Fatalf("jikim error envelope leaked into OpenBao response: %s", response.Body.String())
	}
}
