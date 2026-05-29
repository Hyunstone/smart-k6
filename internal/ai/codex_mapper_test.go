package ai

import (
	"context"
	"os"
	"testing"

	"github.com/hyunseok/smart-k6/internal/openapi"
)

func TestMapWithCodexParsesStructuredOutput(t *testing.T) {
	original := runCodex
	t.Cleanup(func() { runCodex = original })
	runCodex = func(ctx context.Context, args []string) ([]byte, error) {
		outputPath := argAfter(args, "-o")
		if outputPath == "" {
			t.Fatal("missing -o output path")
		}
		if argAfter(args, "--output-schema") == "" {
			t.Fatalf("missing output schema: %v", args)
		}
		data := `{"steps":[{"step":1,"api_id":"login","extract_variables":[{"name":"token","path":"data.accessToken"}],"use_variables":[]},{"step":2,"api_id":"getUser","extract_variables":[],"use_variables":[{"field":"Authorization","variable":"token"}]}]}`
		return nil, os.WriteFile(outputPath, []byte(data), 0644)
	}

	plan, err := MapWithCodex(context.Background(), openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{APIID: "login", Method: "POST", Path: "/login"}, {APIID: "getUser", Method: "GET", Path: "/users/{id}"}},
	}, "", "fetch user")
	if err != nil {
		t.Fatalf("MapWithCodex() error = %v", err)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].ExtractVariables["token"] != "data.accessToken" || plan.Steps[1].UseVariables["Authorization"] != "token" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestMapWithCodexOmitsModelWhenUnset(t *testing.T) {
	original := runCodex
	t.Cleanup(func() { runCodex = original })
	runCodex = func(ctx context.Context, args []string) ([]byte, error) {
		if argAfter(args, "--model") != "" {
			t.Fatalf("unexpected model override in args: %v", args)
		}
		return nil, os.WriteFile(argAfter(args, "-o"), []byte(`{"steps":[{"step":1,"api_id":"getUser","extract_variables":[],"use_variables":[]}]}`), 0644)
	}

	_, err := MapWithCodex(context.Background(), openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{APIID: "getUser", Method: "GET", Path: "/users/{id}"}},
	}, "", "fetch user")
	if err != nil {
		t.Fatalf("MapWithCodex() error = %v", err)
	}
}

func TestMapWithCodexUsesExplicitModelOverride(t *testing.T) {
	original := runCodex
	t.Cleanup(func() { runCodex = original })
	runCodex = func(ctx context.Context, args []string) ([]byte, error) {
		if got := argAfter(args, "--model"); got != "gpt-5" {
			t.Fatalf("model override = %q, args = %v", got, args)
		}
		return nil, os.WriteFile(argAfter(args, "-o"), []byte(`{"steps":[{"step":1,"api_id":"getUser","extract_variables":[],"use_variables":[]}]}`), 0644)
	}

	_, err := MapWithCodex(context.Background(), openapi.SpecSummary{
		Operations: []openapi.OperationSummary{{APIID: "getUser", Method: "GET", Path: "/users/{id}"}},
	}, "gpt-5", "fetch user")
	if err != nil {
		t.Fatalf("MapWithCodex() error = %v", err)
	}
}

func TestMapWithCodexRejectsInvalidJSON(t *testing.T) {
	original := runCodex
	t.Cleanup(func() { runCodex = original })
	runCodex = func(ctx context.Context, args []string) ([]byte, error) {
		return nil, os.WriteFile(argAfter(args, "-o"), []byte(`not-json`), 0644)
	}

	_, err := MapWithCodex(context.Background(), openapi.SpecSummary{}, "gpt-4o-mini", "bad")
	if err == nil {
		t.Fatal("MapWithCodex() expected error")
	}
}

func argAfter(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
