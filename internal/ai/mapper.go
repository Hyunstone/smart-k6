package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"

	"github.com/hyunseok/smart-k6/internal/openapi"
	"github.com/hyunseok/smart-k6/internal/scenario"
)

type Mapper struct {
	client *openai.Client
	model  string
}

func NewMapper(apiKey, model string) *Mapper {
	if strings.TrimSpace(model) == "" {
		model = openai.GPT4oMini
	}
	return &Mapper{
		client: openai.NewClient(apiKey),
		model:  model,
	}
}

func (m *Mapper) Map(ctx context.Context, summary openapi.SpecSummary, prompt string) (scenario.Plan, error) {
	var result scenario.Plan
	schema, err := jsonschema.GenerateSchemaForType(result)
	if err != nil {
		return scenario.Plan{}, fmt.Errorf("generate scenario schema: %w", err)
	}

	compact, err := marshalPromptOperations(summary.Operations)
	if err != nil {
		return scenario.Plan{}, fmt.Errorf("marshal Swagger summary: %w", err)
	}

	resp, err := m.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: m.model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role: openai.ChatMessageRoleSystem,
				Content: "You map Swagger/OpenAPI operations into a k6 API call sequence. " +
					"Return only API IDs that exist in the provided summary. " +
					"Use extract_variables to capture response JSON paths needed by later steps. " +
					"Use use_variables to bind a request parameter/body field/header name to an extracted variable name.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: fmt.Sprintf("Swagger summary JSON:\n%s\n\nUser scenario:\n%s", string(compact), prompt),
			},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name:   "smart_k6_scenario",
				Schema: schema,
				Strict: true,
			},
		},
	})
	if err != nil {
		return scenario.Plan{}, fmt.Errorf("map scenario with OpenAI: %w", err)
	}
	if len(resp.Choices) == 0 {
		return scenario.Plan{}, fmt.Errorf("map scenario with OpenAI: empty response")
	}

	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &result); err != nil {
		return scenario.Plan{}, fmt.Errorf("parse OpenAI scenario JSON: %w", err)
	}
	if len(result.Steps) == 0 {
		return scenario.Plan{}, fmt.Errorf("OpenAI returned an empty scenario")
	}
	return result, nil
}
