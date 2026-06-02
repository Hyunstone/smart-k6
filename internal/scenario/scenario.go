package scenario

type Plan struct {
	Steps []Step `json:"steps" required:"true" description:"Ordered API call steps"`
}

type Step struct {
	Step             int               `json:"step" required:"true" description:"1-based execution order"`
	APIID            string            `json:"api_id" required:"true" description:"API identifier from the Swagger summary"`
	ExtractVariables map[string]string `json:"extract_variables" required:"true" description:"Variable name to JSON response path, for example token: data.accessToken"`
	UseVariables     map[string]string `json:"use_variables" required:"true" description:"Request field or parameter name to previously extracted variable name"`
	Overrides        RequestOverride   `json:"overrides,omitempty" description:"Request values inferred from test evidence"`
	Checks           []Check           `json:"checks,omitempty" description:"Response checks inferred from test evidence"`
}

type RequestOverride struct {
	PathParams  map[string]any `json:"path_params,omitempty"`
	QueryParams map[string]any `json:"query_params,omitempty"`
	Headers     map[string]any `json:"headers,omitempty"`
	Body        any            `json:"body,omitempty"`
}

type Check struct {
	Type     string `json:"type" description:"Check type: status, json_path, header, or body_contains"`
	Path     string `json:"path,omitempty" description:"JSON path or header name"`
	Operator string `json:"operator,omitempty" description:"Comparison operator: eq, exists, matches, gte, or lte"`
	Value    any    `json:"value,omitempty" description:"Expected value"`
}

func DefaultPlan(apiIDs []string) Plan {
	steps := make([]Step, 0, len(apiIDs))
	for i, id := range apiIDs {
		steps = append(steps, Step{
			Step:             i + 1,
			APIID:            id,
			ExtractVariables: map[string]string{},
			UseVariables:     map[string]string{},
		})
	}
	return Plan{Steps: steps}
}
