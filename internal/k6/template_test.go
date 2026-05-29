package k6

import (
	"strings"
	"testing"

	"github.com/hyunseok/smart-k6/internal/openapi"
)

func TestRenderUsesConstantArrivalRateAndScale(t *testing.T) {
	script, err := Render(ScriptData{
		BaseURL:  "https://api.example.com/o'clock",
		TPS:      250,
		Scale:    "10M",
		Duration: "30s",
		Operations: []openapi.OperationSummary{
			{
				Method:      "GET",
				Path:        "/users/{id}",
				OperationID: "getUser",
			},
		},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	assertContains(t, script, "executor: 'constant-arrival-rate'")
	assertContains(t, script, "rate: 250")
	assertContains(t, script, `duration: "30s"`)
	assertContains(t, script, `const BASE_URL = __ENV.BASE_URL || "https://api.example.com/o'clock";`)
	assertContains(t, script, "const SCALE_LIMIT = 10000000;")
	assertContains(t, script, `path: "/users/{id}"`)
}

func TestRenderRejectsInvalidScale(t *testing.T) {
	_, err := Render(ScriptData{
		TPS:      1,
		Scale:    "many",
		Duration: "1m",
		Operations: []openapi.OperationSummary{
			{Method: "GET", Path: "/health"},
		},
	})
	if err == nil {
		t.Fatal("Render() expected invalid scale error")
	}
}

func TestRenderOmitsAuthorizationHeaderWhenTokenIsMissing(t *testing.T) {
	script, err := Render(ScriptData{
		TPS:      1,
		Scale:    "1M",
		Duration: "1m",
		Operations: []openapi.OperationSummary{
			{Method: "GET", Path: "/me", RequiresAuth: true},
		},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	assertContains(t, script, "return __ENV.AUTH_TOKEN || vars.token || vars.accessToken || undefined;")
	assertContains(t, script, "function cleanObject(object)")
	assertContains(t, script, "const headers = cleanObject(applyVariableBindings(normalizeValue(operation.headers || {}), bindings));")
}

func TestRenderUsesPathParameterSamples(t *testing.T) {
	script, err := Render(ScriptData{
		TPS:      1,
		Scale:    "1M",
		Duration: "1m",
		Operations: []openapi.OperationSummary{
			{
				Method: "GET",
				Path:   "/companies/{companyId}/users/{userUuid}",
				Parameters: []openapi.ParameterSummary{
					{Name: "companyId", In: "path", Required: true, Value: "__RANDOM_UUID__"},
					{Name: "userUuid", In: "path", Required: true, Value: "__RANDOM_UUID__"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	assertContains(t, script, "function randomUUID()")
	assertContains(t, script, `"companyId":"__RANDOM_UUID__"`)
	assertContains(t, script, `let url = BASE_URL + buildPath(operation.path, operation.pathParams || {}, bindings);`)
	assertContains(t, script, "return encodeURIComponent(String(normalizeValue(defaultValue)));")
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected output to contain %q\noutput:\n%s", want, got)
	}
}
