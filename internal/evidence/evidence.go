package evidence

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/hyunseok/smart-k6/internal/openapi"
	"github.com/hyunseok/smart-k6/internal/scenario"
)

type File struct {
	Name  string `json:"name,omitempty"`
	Calls []Call `json:"calls"`
}

type Call struct {
	Name         string            `json:"name,omitempty"`
	APIID        string            `json:"api_id,omitempty"`
	Method       string            `json:"method,omitempty"`
	Path         string            `json:"path,omitempty"`
	PathParams   map[string]any    `json:"path_params,omitempty"`
	QueryParams  map[string]any    `json:"query_params,omitempty"`
	Headers      map[string]any    `json:"headers,omitempty"`
	Body         any               `json:"body,omitempty"`
	ExpectStatus any               `json:"expect_status,omitempty"`
	Extract      map[string]string `json:"extract,omitempty"`
	Use          map[string]string `json:"use,omitempty"`
	Checks       []scenario.Check  `json:"checks,omitempty"`
}

func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read test evidence: %w", err)
	}
	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return File{}, fmt.Errorf("parse test evidence JSON: %w", err)
	}
	if len(file.Calls) == 0 {
		return File{}, fmt.Errorf("test evidence must include at least one call")
	}
	return file, nil
}

func Synthesize(file File, summary openapi.SpecSummary) (scenario.Plan, error) {
	steps := make([]scenario.Step, 0, len(file.Calls))
	for i, call := range file.Calls {
		operation, pathParams, err := matchOperation(call, summary.Operations)
		if err != nil {
			return scenario.Plan{}, fmt.Errorf("call %d: %w", i+1, err)
		}
		overrides := scenario.RequestOverride{
			PathParams:  mergeMaps(pathParams, call.PathParams),
			QueryParams: call.QueryParams,
			Headers:     call.Headers,
			Body:        call.Body,
		}
		checks := append([]scenario.Check{}, call.Checks...)
		if status, ok := normalizeStatus(call.ExpectStatus); ok {
			checks = append([]scenario.Check{{Type: "status", Operator: "eq", Value: status}}, checks...)
		}
		steps = append(steps, scenario.Step{
			Step:             i + 1,
			APIID:            operation.APIID,
			ExtractVariables: nonNilStringMap(call.Extract),
			UseVariables:     nonNilStringMap(call.Use),
			Overrides:        overrides,
			Checks:           checks,
		})
	}
	return scenario.Plan{Steps: steps}, nil
}

func matchOperation(call Call, operations []openapi.OperationSummary) (openapi.OperationSummary, map[string]any, error) {
	if strings.TrimSpace(call.APIID) != "" {
		for _, operation := range operations {
			if operation.APIID == call.APIID {
				return operation, map[string]any{}, nil
			}
		}
		return openapi.OperationSummary{}, nil, fmt.Errorf("unknown api_id %q", call.APIID)
	}
	method := strings.ToUpper(strings.TrimSpace(call.Method))
	path := stripQuery(call.Path)
	if method == "" || path == "" {
		return openapi.OperationSummary{}, nil, fmt.Errorf("method and path are required when api_id is not provided")
	}
	for _, operation := range operations {
		if strings.ToUpper(operation.Method) != method {
			continue
		}
		if params, ok := matchPath(operation.Path, path); ok {
			return operation, params, nil
		}
	}
	return openapi.OperationSummary{}, nil, fmt.Errorf("no OpenAPI operation matches %s %s", method, call.Path)
}

func matchPath(templatePath, actualPath string) (map[string]any, bool) {
	templateParts := splitPath(templatePath)
	actualParts := splitPath(actualPath)
	if len(templateParts) != len(actualParts) {
		return nil, false
	}
	params := map[string]any{}
	for i, templatePart := range templateParts {
		actualPart, err := url.PathUnescape(actualParts[i])
		if err != nil {
			actualPart = actualParts[i]
		}
		if name, ok := templateVariable(templatePart); ok {
			params[name] = typedPathValue(actualPart)
			continue
		}
		if templatePart != actualPart {
			return nil, false
		}
	}
	return params, true
}

func stripQuery(path string) string {
	if idx := strings.Index(path, "?"); idx >= 0 {
		return path[:idx]
	}
	return path
}

func splitPath(path string) []string {
	path = strings.Trim(stripQuery(path), "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

var variablePattern = regexp.MustCompile(`^\{([^}]+)\}$`)

func templateVariable(part string) (string, bool) {
	if strings.HasPrefix(part, ":") && len(part) > 1 {
		return strings.TrimPrefix(part, ":"), true
	}
	matches := variablePattern.FindStringSubmatch(part)
	if len(matches) != 2 {
		return "", false
	}
	return matches[1], true
}

func typedPathValue(value string) any {
	if value == "" {
		return value
	}
	if number, err := strconv.Atoi(value); err == nil {
		return number
	}
	return value
}

func normalizeStatus(value any) (int, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, false
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(typed))
		return number, err == nil
	default:
		return 0, false
	}
}

func mergeMaps(base, override map[string]any) map[string]any {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	result := map[string]any{}
	for key, value := range base {
		result[key] = value
	}
	for key, value := range override {
		result[key] = value
	}
	return result
}

func nonNilStringMap(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	return value
}
