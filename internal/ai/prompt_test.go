package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyunseok/smart-k6/internal/openapi"
)

func TestMarshalPromptOperationsDropsVerboseFields(t *testing.T) {
	data, err := marshalPromptOperations([]openapi.OperationSummary{
		{
			APIID:       "createOrder",
			Method:      "POST",
			Path:        "/orders",
			Description: strings.Repeat("long description ", 30),
			Parameters: []openapi.ParameterSummary{
				{Name: "userId", In: "query", Required: true, Value: "__RANDOM_ID__"},
			},
			RequestBody: map[string]any{
				"name":        strings.Repeat("a", 120),
				"description": "sample",
			},
			RequiresAuth: true,
		},
	})
	if err != nil {
		t.Fatalf("marshalPromptOperations() error = %v", err)
	}

	text := string(data)
	if strings.Contains(text, "OperationID") || strings.Contains(text, "__RANDOM_ID__") || strings.Contains(text, strings.Repeat("long description ", 10)) {
		t.Fatalf("prompt payload kept verbose fields: %s", text)
	}

	var decoded []map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal prompt payload: %v", err)
	}
	if decoded[0]["api_id"] != "createOrder" || decoded[0]["auth"] != true {
		t.Fatalf("decoded payload = %+v", decoded[0])
	}
}

func TestCompactPromptValueLimitsLargeObjects(t *testing.T) {
	value := map[string]any{}
	for i := 0; i < 20; i++ {
		value[string(rune('a'+i))] = "sample"
	}

	compact, ok := compactPromptValue(value, 0).(map[string]any)
	if !ok {
		t.Fatalf("compact value type = %T", compact)
	}
	if len(compact) > 13 || compact["_truncated"] != true {
		t.Fatalf("compact value = %+v", compact)
	}
}
