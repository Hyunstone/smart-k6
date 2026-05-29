package scenario

type Plan struct {
	Steps []Step `json:"steps" required:"true" description:"Ordered API call steps"`
}

type Step struct {
	Step             int               `json:"step" required:"true" description:"1-based execution order"`
	APIID            string            `json:"api_id" required:"true" description:"API identifier from the Swagger summary"`
	ExtractVariables map[string]string `json:"extract_variables" required:"true" description:"Variable name to JSON response path, for example token: data.accessToken"`
	UseVariables     map[string]string `json:"use_variables" required:"true" description:"Request field or parameter name to previously extracted variable name"`
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
