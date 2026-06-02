package k6

import (
	"strings"
	"testing"

	"github.com/hyunseok/smart-k6/internal/openapi"
	"github.com/hyunseok/smart-k6/internal/scenario"
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
	assertContains(t, script, "const headers = cleanObject(applyVariableBindings(normalizeValue(mergeObjects(operation.headers, overrides.headers)), bindings));")
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
	assertContains(t, script, `let url = BASE_URL + buildPath(operation.path, pathParams || {}, bindings);`)
	assertContains(t, script, "return encodeURIComponent(String(normalizeValue(defaultValue)));")
}

func TestRenderUsesScenarioOverridesAndChecks(t *testing.T) {
	script, err := Render(ScriptData{
		TPS:      1,
		Scale:    "1M",
		Duration: "1m",
		Operations: []openapi.OperationSummary{
			{APIID: "getOrder", Method: "GET", Path: "/orders/{id}"},
		},
		Scenario: scenario.Plan{Steps: []scenario.Step{
			{
				Step:  1,
				APIID: "getOrder",
				Overrides: scenario.RequestOverride{
					PathParams:  map[string]any{"id": 42},
					QueryParams: map[string]any{"include": "items"},
				},
				Checks: []scenario.Check{
					{Type: "status", Operator: "eq", Value: 200},
					{Type: "json_path", Path: "data.id", Operator: "exists"},
				},
				ExtractVariables: map[string]string{},
				UseVariables:     map[string]string{},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	assertContains(t, script, `"path_params":{"id":42}`)
	assertContains(t, script, `"query_params":{"include":"items"}`)
	assertContains(t, script, "function evaluateCheck(spec, res, jsonBody)")
	assertContains(t, script, "return compareValues(res.status, operator, spec.value);")
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected output to contain %q\noutput:\n%s", want, got)
	}
}
