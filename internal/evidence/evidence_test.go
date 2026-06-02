package evidence

import (
	"testing"

	"github.com/hyunseok/smart-k6/internal/openapi"
)

func TestSynthesizeMatchesPathAndBuildsPreciseStep(t *testing.T) {
	plan, err := Synthesize(File{Calls: []Call{
		{
			Method:       "POST",
			Path:         "/orders",
			Body:         map[string]any{"sku": "A-001"},
			ExpectStatus: 201,
			Extract:      map[string]string{"orderId": "data.id"},
		},
		{
			Method:       "GET",
			Path:         "/orders/42",
			ExpectStatus: 200,
			Use:          map[string]string{"id": "orderId"},
		},
	}}, openapi.SpecSummary{Operations: []openapi.OperationSummary{
		{APIID: "createOrder", Method: "POST", Path: "/orders"},
		{APIID: "getOrder", Method: "GET", Path: "/orders/{id}"},
	}})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}

	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %+v", plan.Steps)
	}
	if plan.Steps[0].APIID != "createOrder" || plan.Steps[0].Checks[0].Value != 201 {
		t.Fatalf("first step = %+v", plan.Steps[0])
	}
	if plan.Steps[1].APIID != "getOrder" || plan.Steps[1].Overrides.PathParams["id"] != 42 {
		t.Fatalf("second step = %+v", plan.Steps[1])
	}
}

func TestSynthesizeRejectsUnmatchedCall(t *testing.T) {
	_, err := Synthesize(File{Calls: []Call{{Method: "DELETE", Path: "/orders/42"}}}, openapi.SpecSummary{Operations: []openapi.OperationSummary{
		{APIID: "getOrder", Method: "GET", Path: "/orders/{id}"},
	}})
	if err == nil {
		t.Fatal("Synthesize() expected error")
	}
}
