package ai

import (
	"encoding/json"
	"strings"

	"github.com/hyunseok/smart-k6/internal/openapi"
)

type promptOperation struct {
	APIID       string            `json:"api_id"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Summary     string            `json:"summary,omitempty"`
	Params      []promptParameter `json:"params,omitempty"`
	RequestBody any               `json:"request_body,omitempty"`
	Responses   []int             `json:"responses,omitempty"`
	Auth        bool              `json:"auth,omitempty"`
}

type promptParameter struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required,omitempty"`
}

func marshalPromptOperations(operations []openapi.OperationSummary) ([]byte, error) {
	promptOperations := make([]promptOperation, 0, len(operations))
	for _, operation := range operations {
		promptOperations = append(promptOperations, promptOperation{
			APIID:       operation.APIID,
			Method:      operation.Method,
			Path:        operation.Path,
			Summary:     truncateText(displayOperationSummary(operation), 160),
			Params:      promptParameters(operation.Parameters),
			RequestBody: compactPromptValue(operation.RequestBody, 0),
			Responses:   operation.ResponseStatuses,
			Auth:        operation.RequiresAuth,
		})
	}
	return json.Marshal(promptOperations)
}

func displayOperationSummary(operation openapi.OperationSummary) string {
	if strings.TrimSpace(operation.Summary) != "" {
		return operation.Summary
	}
	return operation.Description
}

func promptParameters(parameters []openapi.ParameterSummary) []promptParameter {
	if len(parameters) == 0 {
		return nil
	}
	result := make([]promptParameter, 0, len(parameters))
	for _, parameter := range parameters {
		result = append(result, promptParameter{
			Name:     parameter.Name,
			In:       parameter.In,
			Required: parameter.Required,
		})
	}
	return result
}

func compactPromptValue(value any, depth int) any {
	if value == nil || depth > 3 {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 {
			return nil
		}
		result := map[string]any{}
		count := 0
		for key, item := range typed {
			if count >= 12 {
				result["_truncated"] = true
				break
			}
			result[key] = compactPromptValue(item, depth+1)
			count++
		}
		return result
	case []any:
		if len(typed) == 0 {
			return nil
		}
		return []any{compactPromptValue(typed[0], depth+1)}
	case string:
		return truncateText(typed, 80)
	default:
		return typed
	}
}

func truncateText(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
