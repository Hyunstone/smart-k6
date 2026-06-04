package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hyunseok/smart-k6/internal/openapi"
	"github.com/hyunseok/smart-k6/internal/process"
	"github.com/hyunseok/smart-k6/internal/scenario"
)

type codexScenarioPlan struct {
	Steps []codexScenarioStep `json:"steps"`
}

type codexScenarioStep struct {
	Step             int                    `json:"step"`
	APIID            string                 `json:"api_id"`
	ExtractVariables []codexVariableExtract `json:"extract_variables"`
	UseVariables     []codexVariableUse     `json:"use_variables"`
}

type codexVariableExtract struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type codexVariableUse struct {
	Field    string `json:"field"`
	Variable string `json:"variable"`
}

var runCodex = func(ctx context.Context, args []string) ([]byte, error) {
	cmd := process.CommandContext(ctx, "codex", args...)
	return cmd.CombinedOutput()
}

func MapWithCodex(ctx context.Context, summary openapi.SpecSummary, model, prompt string) (scenario.Plan, error) {
	if _, err := exec.LookPath("codex"); err != nil {
		return scenario.Plan{}, fmt.Errorf("codex executable not found: %w", err)
	}

	dir, err := os.MkdirTemp("", "sk6-codex-*")
	if err != nil {
		return scenario.Plan{}, fmt.Errorf("create codex temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	schemaPath := filepath.Join(dir, "scenario.schema.json")
	outputPath := filepath.Join(dir, "scenario.json")
	if err := os.WriteFile(schemaPath, []byte(codexScenarioSchema), 0644); err != nil {
		return scenario.Plan{}, fmt.Errorf("write codex schema: %w", err)
	}

	compact, err := marshalPromptOperations(summary.Operations)
	if err != nil {
		return scenario.Plan{}, fmt.Errorf("marshal Swagger summary: %w", err)
	}

	instructions := fmt.Sprintf(`Map this Swagger/OpenAPI operation summary into a k6 scenario JSON.

Rules:
- Return JSON only.
- Use only api_id values that exist in the summary.
- extract_variables is an array of objects with name and path.
- use_variables is an array of objects with field and variable.
- If no chaining is needed, return the APIs in the best scenario order.

Swagger summary:
%s

User scenario:
%s`, string(compact), prompt)

	args := []string{
		"exec",
		"--ephemeral",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"--output-schema", schemaPath,
		"-o", outputPath,
	}
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", model)
	}
	args = append(args, instructions)

	output, err := runCodex(ctx, args)
	if err != nil {
		return scenario.Plan{}, fmt.Errorf("map scenario with Codex login: %w\n%s", err, strings.TrimSpace(string(output)))
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		return scenario.Plan{}, fmt.Errorf("read Codex scenario output: %w", err)
	}
	var codexPlan codexScenarioPlan
	if err := json.Unmarshal(cleanJSON(data), &codexPlan); err != nil {
		return scenario.Plan{}, fmt.Errorf("parse Codex scenario JSON: %w\n%s", err, strings.TrimSpace(string(data)))
	}
	plan := codexPlan.toScenarioPlan()
	if len(plan.Steps) == 0 {
		return scenario.Plan{}, fmt.Errorf("Codex returned an empty scenario")
	}
	return plan, nil
}

func (plan codexScenarioPlan) toScenarioPlan() scenario.Plan {
	steps := make([]scenario.Step, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		converted := scenario.Step{
			Step:             step.Step,
			APIID:            step.APIID,
			ExtractVariables: map[string]string{},
			UseVariables:     map[string]string{},
		}
		for _, item := range step.ExtractVariables {
			if strings.TrimSpace(item.Name) != "" && strings.TrimSpace(item.Path) != "" {
				converted.ExtractVariables[item.Name] = item.Path
			}
		}
		for _, item := range step.UseVariables {
			if strings.TrimSpace(item.Field) != "" && strings.TrimSpace(item.Variable) != "" {
				converted.UseVariables[item.Field] = item.Variable
			}
		}
		steps = append(steps, converted)
	}
	return scenario.Plan{Steps: steps}
}

func cleanJSON(data []byte) []byte {
	text := strings.TrimSpace(string(data))
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return []byte(strings.TrimSpace(text))
}

const codexScenarioSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["steps"],
  "properties": {
    "steps": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["step", "api_id", "extract_variables", "use_variables"],
        "properties": {
          "step": { "type": "integer" },
          "api_id": { "type": "string" },
          "extract_variables": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["name", "path"],
              "properties": {
                "name": { "type": "string" },
                "path": { "type": "string" }
              }
            }
          },
          "use_variables": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["field", "variable"],
              "properties": {
                "field": { "type": "string" },
                "variable": { "type": "string" }
              }
            }
          }
        }
      }
    }
  }
}`
