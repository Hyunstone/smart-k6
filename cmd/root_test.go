package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyunseok/smart-k6/internal/openapi"
	"github.com/hyunseok/smart-k6/internal/scenario"
	"github.com/spf13/cobra"
)

func TestResolveInputsUsesPositionalSpecAndScenario(t *testing.T) {
	opts := rootOptions{}
	err := resolveInputs(&opts, []string{"openapi.yaml", "login", "then", "order"}, strings.NewReader(""), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("resolveInputs() error = %v", err)
	}
	if opts.spec != "openapi.yaml" {
		t.Fatalf("spec = %q", opts.spec)
	}
	if opts.prompt != "login then order" {
		t.Fatalf("prompt = %q", opts.prompt)
	}
}

func TestResolveInputsPromptsWhenInteractive(t *testing.T) {
	opts := rootOptions{}
	var out bytes.Buffer
	err := resolveInputs(&opts, nil, strings.NewReader("openapi.yaml\n"), &out)
	if err != nil {
		t.Fatalf("resolveInputs() error = %v", err)
	}
	if opts.spec != "openapi.yaml" || opts.prompt != "" || opts.runK6 {
		t.Fatalf("unexpected opts: %+v", opts)
	}
	if !strings.Contains(out.String(), "Swagger/OpenAPI path or URL") {
		t.Fatalf("prompt output = %q", out.String())
	}
}

func TestResolveInputsRequiresSpecWhenEmpty(t *testing.T) {
	opts := rootOptions{}
	err := resolveInputs(&opts, nil, strings.NewReader(""), &bytes.Buffer{})
	if err == nil {
		t.Fatal("resolveInputs() expected error")
	}
}

func TestShouldPromptForModeStillPromptsWhenRunFlagChanged(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("run", false, "")
	if err := cmd.Flags().Set("run", "true"); err != nil {
		t.Fatalf("set run flag: %v", err)
	}
	if !shouldPromptForMode(cmd, rootOptions{runK6: true}) {
		t.Fatal("shouldPromptForMode() should still prompt when --run is explicit")
	}
}

func TestShouldPromptForModePromptsForBareSpec(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("run", false, "")
	if !shouldPromptForMode(cmd, rootOptions{}) {
		t.Fatal("shouldPromptForMode() should prompt for bare interactive spec")
	}
}

func TestSelectModeChoosesMixedReadCommandScenario(t *testing.T) {
	opts := rootOptions{spec: "openapi.yaml"}
	var out bytes.Buffer
	err := selectMode(openapi.SpecSummary{
		Title:   "Sample API",
		BaseURL: "https://api.example.com",
		Operations: []openapi.OperationSummary{
			{Method: "GET", APIID: "getUser"},
			{Method: "POST", APIID: "createUser"},
		},
	}, &opts, strings.NewReader("3\ny\n"), &out)
	if err != nil {
		t.Fatalf("selectMode() error = %v", err)
	}
	if !opts.runK6 || !opts.includeCommands || opts.prompt != "" || opts.allowUnsafe {
		t.Fatalf("unexpected opts: %+v", opts)
	}
	if !strings.Contains(out.String(), "2 safe public GET/HEAD") && !strings.Contains(out.String(), "1 safe public GET/HEAD") {
		t.Fatalf("selection output missing operation summary: %q", out.String())
	}
}

func TestSelectModeChoosesPreciseScenarioFromTestEvidence(t *testing.T) {
	opts := rootOptions{spec: "openapi.yaml"}
	err := selectMode(openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{Method: "GET", APIID: "getUser"}},
	}, &opts, strings.NewReader("2\n./test-evidence.json\ny\n"), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("selectMode() error = %v", err)
	}
	if opts.fromTests != "./test-evidence.json" || !opts.runK6 || opts.includeCommands {
		t.Fatalf("unexpected opts: %+v", opts)
	}
}

func TestSelectModeSafeScriptDisablesCommandMix(t *testing.T) {
	opts := rootOptions{spec: "openapi.yaml", includeCommands: true}
	err := selectMode(openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{Method: "GET", APIID: "getUser"}},
	}, &opts, strings.NewReader("1\nn\n"), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("selectMode() error = %v", err)
	}
	if opts.runK6 || opts.includeCommands {
		t.Fatalf("safe script should disable run and command mix: %+v", opts)
	}
}

func TestSelectModeDefaultsToRunWhenRunFlagWasProvided(t *testing.T) {
	opts := rootOptions{spec: "openapi.yaml", runK6: true}
	var out bytes.Buffer
	err := selectMode(openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{Method: "GET", APIID: "getUser"}},
	}, &opts, strings.NewReader("\n\n"), &out)
	if err != nil {
		t.Fatalf("selectMode() error = %v", err)
	}
	if !opts.runK6 {
		t.Fatalf("runK6 should stay true when --run default is accepted: %+v", opts)
	}
	if !strings.Contains(out.String(), "Selection [1]") {
		t.Fatalf("selection output should show run default: %q", out.String())
	}
}

func TestSelectModeAllowsSettingsBeforeRun(t *testing.T) {
	opts := rootOptions{spec: "openapi.yaml", tps: 1, duration: "1m", scale: "1M"}
	var out bytes.Buffer
	err := selectMode(openapi.SpecSummary{
		BaseURL:    "https://api.example.com",
		Operations: []openapi.OperationSummary{{Method: "GET", APIID: "getUser"}},
	}, &opts, strings.NewReader("6\n1\n25\n2\n30s\n3\n10M\n4\nhttp://localhost:8080\ndone\n3\ny\n"), &out)
	if err != nil {
		t.Fatalf("selectMode() error = %v", err)
	}
	if !opts.runK6 || opts.tps != 25 || opts.duration != "30s" || opts.scale != "10M" || opts.baseURL != "http://localhost:8080" {
		t.Fatalf("unexpected opts: %+v", opts)
	}
	if !strings.Contains(out.String(), "1) TPS: 25") || !strings.Contains(out.String(), "4) Base URL: http://localhost:8080") {
		t.Fatalf("settings output = %q", out.String())
	}
}

func TestSelectModeAllowsAuthSettingsBeforeRun(t *testing.T) {
	opts := rootOptions{spec: "openapi.yaml", tps: 1, duration: "1m", scale: "1M"}
	var out bytes.Buffer
	err := selectMode(openapi.SpecSummary{
		BaseURL: "https://api.example.com",
		Operations: []openapi.OperationSummary{
			{Method: "GET", APIID: "privateMe", RequiresAuth: true},
		},
	}, &opts, strings.NewReader("6\n5\n3\n/tmp/token.txt\ndone\n3\ny\n"), &out)
	if err != nil {
		t.Fatalf("selectMode() error = %v", err)
	}
	if !opts.runK6 || opts.authTokenFile != "/tmp/token.txt" {
		t.Fatalf("unexpected opts: %+v", opts)
	}
	if !strings.Contains(out.String(), "5) Auth: token file: /tmp/token.txt") {
		t.Fatalf("settings output = %q", out.String())
	}
}

func TestSelectModeShowsSettingErrorAndContinues(t *testing.T) {
	opts := rootOptions{spec: "openapi.yaml", tps: 1, duration: "1m", scale: "1M"}
	var out bytes.Buffer
	err := selectMode(openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{Method: "GET", APIID: "getUser"}},
	}, &opts, strings.NewReader("6\n1\n0\n2\n10\ndone\n1\nn\n"), &out)
	if err != nil {
		t.Fatalf("selectMode() error = %v", err)
	}
	if opts.tps != 1 || opts.duration != "1m" {
		t.Fatalf("invalid settings should not be applied: %+v", opts)
	}
	if !strings.Contains(out.String(), "TPS must be a positive integer") {
		t.Fatalf("missing TPS error: %q", out.String())
	}
	if !strings.Contains(out.String(), "duration must be a positive Go duration") {
		t.Fatalf("missing duration error: %q", out.String())
	}
}

func TestAuthDisplayDoesNotLeakBearerToken(t *testing.T) {
	display := authDisplay(rootOptions{authToken: "secret-token"})
	if strings.Contains(display, "secret-token") {
		t.Fatalf("auth display leaked token: %q", display)
	}
	if display != "bearer token provided" {
		t.Fatalf("auth display = %q", display)
	}
}

func TestSelectModeChoosesAIScenarioPrompt(t *testing.T) {
	opts := rootOptions{spec: "openapi.yaml"}
	err := selectMode(openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{Method: "GET", APIID: "getUser"}},
	}, &opts, strings.NewReader("4\nlogin then get profile\ny\n"), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("selectMode() error = %v", err)
	}
	if opts.prompt != "login then get profile" || !opts.runK6 {
		t.Fatalf("unexpected opts: %+v", opts)
	}
}

func TestSelectModeRequiresUnsafeConfirmation(t *testing.T) {
	opts := rootOptions{spec: "openapi.yaml"}
	err := selectMode(openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{Method: "POST", APIID: "createUser"}},
	}, &opts, strings.NewReader("5\nno\n"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("selectMode() expected unsafe cancellation error")
	}
}

func TestConfirmAIUseRequiresExplicitConsent(t *testing.T) {
	opts := rootOptions{spec: "openapi.yaml", prompt: "login then get profile", aiProvider: "codex"}
	err := confirmAIUse(openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{Method: "GET", APIID: "getUser"}},
	}, &opts, bufio.NewReader(strings.NewReader("n\n")), &bytes.Buffer{})
	if err == nil {
		t.Fatal("confirmAIUse() expected cancellation error")
	}
	if opts.aiConfirmed {
		t.Fatal("aiConfirmed should remain false")
	}
}

func TestConfirmAIUseMarksConsent(t *testing.T) {
	opts := rootOptions{spec: "openapi.yaml", prompt: "login then get profile", aiProvider: "codex"}
	var out bytes.Buffer
	err := confirmAIUse(openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{Method: "GET", APIID: "getUser"}},
	}, &opts, bufio.NewReader(strings.NewReader("y\n")), &out)
	if err != nil {
		t.Fatalf("confirmAIUse() error = %v", err)
	}
	if !opts.aiConfirmed {
		t.Fatal("aiConfirmed should be true")
	}
	if !strings.Contains(out.String(), "Operation summary count: 1") {
		t.Fatalf("confirmation output = %q", out.String())
	}
}

func TestConfigureAuthChoosesTokenFile(t *testing.T) {
	opts := rootOptions{}
	err := configureAuth(openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{Method: "GET", APIID: "privateMe", RequiresAuth: true}},
	}, &opts, bufio.NewReader(strings.NewReader("3\n/tmp/token.txt\n")), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("configureAuth() error = %v", err)
	}
	if opts.authTokenFile != "/tmp/token.txt" {
		t.Fatalf("authTokenFile = %q", opts.authTokenFile)
	}
}

func TestConfigureAuthClearsPreviousAuthMode(t *testing.T) {
	opts := rootOptions{
		authTokenFile: "/tmp/old-token.txt",
	}
	err := configureAuth(openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{Method: "GET", APIID: "privateMe", RequiresAuth: true}},
	}, &opts, bufio.NewReader(strings.NewReader("4\n/api/login\nuser@example.com\nsecret\ndata.accessToken\n")), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("configureAuth() error = %v", err)
	}
	if opts.authTokenFile != "" || opts.authLoginPath != "/api/login" || opts.authUsername != "user@example.com" || opts.authPassword != "secret" {
		t.Fatalf("unexpected auth opts: %+v", opts)
	}
}

func TestConfigureAuthContinueClearsLocalAuth(t *testing.T) {
	opts := rootOptions{
		authToken:     "old-token",
		authTokenFile: "/tmp/old-token.txt",
		authLoginPath: "/api/login",
		authUsername:  "user@example.com",
		authPassword:  "secret",
	}
	err := configureAuth(openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{Method: "GET", APIID: "privateMe", RequiresAuth: true}},
	}, &opts, bufio.NewReader(strings.NewReader("1\n")), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("configureAuth() error = %v", err)
	}
	if hasAuthInput(opts) {
		t.Fatalf("auth opts should be cleared: %+v", opts)
	}
}

func TestResolveAuthTokenReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.txt")
	if err := os.WriteFile(path, []byte("abc123\n"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	token, err := resolveAuthToken(context.Background(), rootOptions{authTokenFile: path}, "")
	if err != nil {
		t.Fatalf("resolveAuthToken() error = %v", err)
	}
	if token != "abc123" {
		t.Fatalf("token = %q", token)
	}
}

func TestResolveAuthTokenLogsInWithTestAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/login" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["email"] != "user@example.com" || body["password"] != "secret" {
			t.Fatalf("unexpected body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"accessToken":"login-token"}}`))
	}))
	defer server.Close()

	token, err := resolveAuthToken(context.Background(), rootOptions{
		authLoginPath:     "/api/login",
		authUsername:      "user@example.com",
		authPassword:      "secret",
		authUsernameField: "email",
		authPasswordField: "password",
		authTokenJSONPath: "data.accessToken",
	}, server.URL)
	if err != nil {
		t.Fatalf("resolveAuthToken() error = %v", err)
	}
	if token != "login-token" {
		t.Fatalf("token = %q", token)
	}
}

func TestAnnotatePromptForConfiguredAuth(t *testing.T) {
	opts := rootOptions{prompt: "실제 서버처럼 불규칙한 트래픽", authTokenFile: ".token"}
	annotatePromptForConfiguredAuth(&opts)
	if !strings.Contains(opts.prompt, "Authentication is already configured") {
		t.Fatalf("prompt = %q", opts.prompt)
	}
	if strings.Contains(opts.prompt, ".token") {
		t.Fatalf("prompt leaked auth source: %q", opts.prompt)
	}
}

func TestValidateScenarioRejectsUnknownAPIID(t *testing.T) {
	err := validateScenario(
		scenario.Plan{Steps: []scenario.Step{{Step: 1, APIID: "missing", ExtractVariables: map[string]string{}, UseVariables: map[string]string{}}}},
		[]openapi.OperationSummary{{APIID: "getUser"}},
	)
	if err == nil {
		t.Fatal("validateScenario() expected error")
	}
}

func TestValidateScenarioRejectsNonContiguousSteps(t *testing.T) {
	err := validateScenario(
		scenario.Plan{Steps: []scenario.Step{
			{Step: 1, APIID: "getUser", ExtractVariables: map[string]string{}, UseVariables: map[string]string{}},
			{Step: 3, APIID: "getUser", ExtractVariables: map[string]string{}, UseVariables: map[string]string{}},
		}},
		[]openapi.OperationSummary{{APIID: "getUser"}},
	)
	if err == nil {
		t.Fatal("validateScenario() expected error")
	}
}

func TestValidateScenarioAcceptsValidPlan(t *testing.T) {
	err := validateScenario(
		scenario.Plan{Steps: []scenario.Step{{Step: 1, APIID: "getUser", ExtractVariables: map[string]string{}, UseVariables: map[string]string{}}}},
		[]openapi.OperationSummary{{APIID: "getUser"}},
	)
	if err != nil {
		t.Fatalf("validateScenario() error = %v", err)
	}
}

func TestBuildScenarioUsesTestEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(path, []byte(`{"calls":[{"method":"POST","path":"/orders","expect_status":201,"extract":{"orderId":"data.id"}},{"method":"GET","path":"/orders/42","use":{"id":"orderId"},"expect_status":200}]}`), 0644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}

	plan, err := buildScenario(context.Background(), openapi.SpecSummary{Operations: []openapi.OperationSummary{
		{APIID: "createOrder", Method: "POST", Path: "/orders"},
		{APIID: "getOrder", Method: "GET", Path: "/orders/{id}"},
	}}, rootOptions{fromTests: path})
	if err != nil {
		t.Fatalf("buildScenario() error = %v", err)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].Checks[0].Value != 201 || plan.Steps[1].Overrides.PathParams["id"] != 42 {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestBuildStaticScenarioDefaultsToPublicReadOnlyOperations(t *testing.T) {
	plan, err := buildStaticScenario(openapi.SpecSummary{Operations: []openapi.OperationSummary{
		{Method: "GET", APIID: "publicList", ResponseStatuses: []int{200, 404}},
		{Method: "HEAD", APIID: "publicHead"},
		{Method: "POST", APIID: "createUser"},
		{Method: "GET", APIID: "privateMe", RequiresAuth: true},
	}}, rootOptions{})
	if err != nil {
		t.Fatalf("buildStaticScenario() error = %v", err)
	}

	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %+v", plan.Steps)
	}
	if plan.Steps[0].APIID != "publicList" || plan.Steps[1].APIID != "publicHead" {
		t.Fatalf("steps = %+v", plan.Steps)
	}
	if len(plan.Steps[0].Checks) != 1 || plan.Steps[0].Checks[0].Value != 200 {
		t.Fatalf("expected OpenAPI status check on safe read step: %+v", plan.Steps[0])
	}
}

func TestBuildStaticScenarioCommandMixIncludesNonDeleteCommands(t *testing.T) {
	plan, err := buildStaticScenario(openapi.SpecSummary{Operations: []openapi.OperationSummary{
		{Method: "GET", APIID: "publicList"},
		{Method: "POST", APIID: "createUser"},
		{Method: "PATCH", APIID: "updateUser"},
		{Method: "DELETE", APIID: "deleteUser"},
		{Method: "GET", APIID: "privateMe", RequiresAuth: true},
	}}, rootOptions{includeCommands: true})
	if err != nil {
		t.Fatalf("buildStaticScenario() error = %v", err)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("steps = %+v", plan.Steps)
	}
	got := []string{plan.Steps[0].APIID, plan.Steps[1].APIID, plan.Steps[2].APIID}
	want := []string{"publicList", "createUser", "updateUser"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("steps = %+v", plan.Steps)
		}
	}
}

func TestBuildStaticScenarioCommandMixIncludesAuthWhenConfigured(t *testing.T) {
	plan, err := buildStaticScenario(openapi.SpecSummary{Operations: []openapi.OperationSummary{
		{Method: "GET", APIID: "publicList"},
		{Method: "GET", APIID: "privateMe", RequiresAuth: true},
		{Method: "POST", APIID: "privateCreate", RequiresAuth: true},
		{Method: "DELETE", APIID: "privateDelete", RequiresAuth: true},
	}}, rootOptions{includeCommands: true, authTokenFile: "/tmp/token.txt"})
	if err != nil {
		t.Fatalf("buildStaticScenario() error = %v", err)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("steps = %+v", plan.Steps)
	}
	if plan.Steps[1].APIID != "privateMe" || plan.Steps[2].APIID != "privateCreate" {
		t.Fatalf("steps = %+v", plan.Steps)
	}
}

func TestBuildStaticScenarioAllowUnsafeIncludesEveryOperation(t *testing.T) {
	plan, err := buildStaticScenario(openapi.SpecSummary{Operations: []openapi.OperationSummary{
		{Method: "POST", APIID: "createUser"},
		{Method: "GET", APIID: "privateMe", RequiresAuth: true},
	}}, rootOptions{allowUnsafe: true})
	if err != nil {
		t.Fatalf("buildStaticScenario() error = %v", err)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %+v", plan.Steps)
	}
}

func TestBuildStaticScenarioRejectsWhenNoSafeOperations(t *testing.T) {
	_, err := buildStaticScenario(openapi.SpecSummary{Operations: []openapi.OperationSummary{
		{Method: "POST", APIID: "createUser"},
		{Method: "GET", APIID: "privateMe", RequiresAuth: true},
	}}, rootOptions{})
	if err == nil {
		t.Fatal("buildStaticScenario() expected error")
	}
}

func TestCleanupFilesRemovesExistingFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.js")
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cleanupFiles(path, path, "")

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, stat err = %v", err)
	}
}
