package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCapabilitiesAdvertiseEveryMCPTool(t *testing.T) {
	response := httptest.NewRecorder()
	(&Server{}).capabilities(response, httptest.NewRequest("GET", "/api/v1/capabilities", nil))
	var envelope struct {
		Data struct {
			MCP struct {
				Tools []string `json:"tools"`
			} `json:"mcp"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, tool := range mcpTools() {
		want[tool["name"].(string)] = true
	}
	for _, name := range envelope.Data.MCP.Tools {
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("capability profile omitted MCP tools: %v", want)
	}
}

func TestOpenAPICoversIntegrationAndOpenBaoLifecycle(t *testing.T) {
	response := httptest.NewRecorder()
	(&Server{}).openAPI(response, httptest.NewRequest("GET", "/api/openapi.json", nil))
	var document struct {
		OpenAPI string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.OpenAPI != "3.1.0" {
		t.Fatalf("openapi=%q", document.OpenAPI)
	}
	for path, method := range map[string]string{
		"/api/openapi.json":                                  "get",
		"/api/v1/secrets/{id}":                               "patch",
		"/api/v1/integrations/ai/test":                       "post",
		"/api/v1/integrations/webhook/deliveries/{id}/retry": "post",
		"/api/v1/policies/simulate":                          "post",
		"/v1/secret/delete/{path}":                           "post",
		"/v1/secret/undelete/{path}":                         "post",
		"/v1/secret/destroy/{path}":                          "put",
		"/v1/secret/metadata/{path}":                         "delete",
	} {
		if _, ok := document.Paths[path][method]; !ok {
			t.Errorf("OpenAPI operation missing: %s %s", method, path)
		}
	}
	for path, item := range document.Paths {
		start := strings.IndexByte(path, '{')
		if start < 0 {
			continue
		}
		end := strings.IndexByte(path[start:], '}')
		if end < 0 {
			t.Fatalf("invalid OpenAPI path template: %s", path)
		}
		parameterName := path[start+1 : start+end]
		for method, raw := range item {
			var operation struct {
				Parameters []struct {
					Name     string `json:"name"`
					In       string `json:"in"`
					Required bool   `json:"required"`
				} `json:"parameters"`
			}
			if err := json.Unmarshal(raw, &operation); err != nil {
				t.Fatalf("decode %s %s: %v", method, path, err)
			}
			found := false
			for _, parameter := range operation.Parameters {
				if parameter.Name == parameterName && parameter.In == "path" && parameter.Required {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("required path parameter missing: %s %s {%s}", method, path, parameterName)
			}
		}
	}
}
