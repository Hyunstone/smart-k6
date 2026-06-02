package openapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oasdiff/yaml"
)

// OperationSummary is the compact API surface passed to generators and, later,
// to the LLM scenario mapper.
type OperationSummary struct {
	Method           string
	Path             string
	Summary          string
	Description      string
	OperationID      string
	APIID            string
	Parameters       []ParameterSummary
	RequestBody      any
	ResponseStatuses []int
	RequiresAuth     bool
}

type ParameterSummary struct {
	Name     string
	In       string
	Required bool
	Value    any
}

// SpecSummary is a lightweight representation of an OpenAPI document.
type SpecSummary struct {
	Title      string
	BaseURL    string
	Operations []OperationSummary
}

// Parse loads an OpenAPI 3.x document from a URL or local file and extracts the
// fields smart-k6 needs for script generation.
func Parse(spec string) (SpecSummary, error) {
	if strings.TrimSpace(spec) == "" {
		return SpecSummary{}, fmt.Errorf("spec is required")
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	doc, err := loadDocument(loader, spec)
	if err != nil {
		return SpecSummary{}, err
	}
	if doc.OpenAPI == "" {
		return SpecSummary{}, fmt.Errorf("unsupported spec: expected OpenAPI 3.x document")
	}
	summary := SpecSummary{
		BaseURL: firstServerURL(doc),
	}
	if doc.Info != nil {
		summary.Title = doc.Info.Title
	}

	for path, item := range doc.Paths.Map() {
		for method, operation := range item.Operations() {
			if operation == nil {
				continue
			}
			method = strings.ToUpper(method)
			summary.Operations = append(summary.Operations, OperationSummary{
				Method:           method,
				Path:             path,
				Summary:          operation.Summary,
				Description:      operation.Description,
				OperationID:      operation.OperationID,
				APIID:            operationAPIID(method, path, operation.OperationID),
				Parameters:       collectParameters(operation),
				RequestBody:      requestBodySample(operation),
				ResponseStatuses: collectResponseStatuses(operation),
				RequiresAuth:     operationRequiresAuth(doc, operation),
			})
		}
	}

	sort.Slice(summary.Operations, func(i, j int) bool {
		if summary.Operations[i].Path == summary.Operations[j].Path {
			return summary.Operations[i].Method < summary.Operations[j].Method
		}
		return summary.Operations[i].Path < summary.Operations[j].Path
	})

	return summary, nil
}

func operationAPIID(method, path, operationID string) string {
	if operationID != "" {
		return operationID
	}
	replacer := strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_", ".", "_")
	return strings.Trim(replacer.Replace(strings.ToLower(method)+path), "_")
}

func collectParameters(operation *openapi3.Operation) []ParameterSummary {
	params := make([]ParameterSummary, 0, len(operation.Parameters))
	for _, ref := range operation.Parameters {
		if ref == nil || ref.Value == nil {
			continue
		}
		param := ref.Value
		params = append(params, ParameterSummary{
			Name:     param.Name,
			In:       param.In,
			Required: param.Required,
			Value:    parameterValue(param),
		})
	}
	return params
}

func collectResponseStatuses(operation *openapi3.Operation) []int {
	if operation == nil || operation.Responses == nil {
		return nil
	}
	statuses := make([]int, 0, len(operation.Responses.Map()))
	for code := range operation.Responses.Map() {
		status, err := strconv.Atoi(code)
		if err != nil || status < 100 || status > 599 {
			continue
		}
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)
	return statuses
}

func parameterValue(param *openapi3.Parameter) any {
	if param.Example != nil {
		return param.Example
	}
	if param.Schema == nil || param.Schema.Value == nil {
		return "sample"
	}
	return sampleValue(param.Name, param.Schema.Value)
}

func requestBodySample(operation *openapi3.Operation) any {
	if operation.RequestBody == nil || operation.RequestBody.Value == nil {
		return nil
	}
	body := operation.RequestBody.Value
	if len(body.Content) == 0 {
		return map[string]any{}
	}
	if media := body.Content.Get("application/json"); media != nil {
		if media.Example != nil {
			return media.Example
		}
		if media.Schema != nil && media.Schema.Value != nil {
			return sampleValue("body", media.Schema.Value)
		}
	}
	for _, media := range body.Content {
		if media == nil {
			continue
		}
		if media.Example != nil {
			return media.Example
		}
		if media.Schema != nil && media.Schema.Value != nil {
			return sampleValue("body", media.Schema.Value)
		}
	}
	return map[string]any{}
}

func sampleValue(name string, schema *openapi3.Schema) any {
	if schema == nil {
		return "sample"
	}
	if schema.Example != nil {
		return schema.Example
	}
	if schema.Default != nil {
		return schema.Default
	}
	if len(schema.Enum) > 0 {
		return schema.Enum[0]
	}
	if sample, ok := compositeSampleValue(name, schema); ok {
		return sample
	}
	if isUUIDSchema(name, schema) {
		return "__RANDOM_UUID__"
	}
	if schema.Type != nil {
		switch {
		case schema.Type.Is("integer"):
			return "__RANDOM_ID__"
		case schema.Type.Is("number"):
			return 1.0
		case schema.Type.Is("boolean"):
			return true
		case schema.Type.Is("array"):
			if schema.Items != nil && schema.Items.Value != nil {
				return []any{sampleValue(name, schema.Items.Value)}
			}
			return []any{"sample"}
		case schema.Type.Is("object"):
			result := map[string]any{}
			for propName, propRef := range schema.Properties {
				if propRef == nil || propRef.Value == nil || propRef.Value.ReadOnly {
					continue
				}
				result[propName] = sampleValue(propName, propRef.Value)
			}
			if len(result) == 0 {
				return map[string]any{}
			}
			return result
		}
	}
	lowerName := strings.ToLower(name)
	switch {
	case strings.Contains(lowerName, "uuid"):
		return "__RANDOM_UUID__"
	case strings.Contains(lowerName, "email"):
		return "user@example.com"
	case strings.Contains(lowerName, "name"):
		return "sample"
	case strings.Contains(lowerName, "id"):
		return "__RANDOM_ID__"
	default:
		return "sample"
	}
}

func compositeSampleValue(name string, schema *openapi3.Schema) (any, bool) {
	if len(schema.AllOf) > 0 {
		merged := map[string]any{}
		for _, ref := range schema.AllOf {
			if ref == nil || ref.Value == nil {
				continue
			}
			value := sampleValue(name, ref.Value)
			if object, ok := value.(map[string]any); ok {
				for key, item := range object {
					merged[key] = item
				}
				continue
			}
			if len(merged) == 0 {
				return value, true
			}
		}
		if len(merged) > 0 {
			return merged, true
		}
	}
	for _, refs := range []openapi3.SchemaRefs{schema.OneOf, schema.AnyOf} {
		for _, ref := range refs {
			if ref == nil || ref.Value == nil || isNullOnlySchema(ref.Value) {
				continue
			}
			return sampleValue(name, ref.Value), true
		}
	}
	return nil, false
}

func isNullOnlySchema(schema *openapi3.Schema) bool {
	return schema.Type != nil && schema.Type.Is("null")
}

func isUUIDSchema(name string, schema *openapi3.Schema) bool {
	if strings.EqualFold(schema.Format, "uuid") {
		return true
	}
	lowerName := strings.ToLower(name)
	if strings.Contains(lowerName, "uuid") {
		return true
	}
	pattern := strings.ToLower(schema.Pattern)
	return strings.Contains(pattern, "[0-9a-f") &&
		strings.Contains(pattern, "{8}") &&
		strings.Contains(pattern, "{4}") &&
		strings.Contains(pattern, "{12}")
}

func operationRequiresAuth(doc *openapi3.T, operation *openapi3.Operation) bool {
	if operation.Security != nil {
		return len(*operation.Security) > 0
	}
	return len(doc.Security) > 0
}

func loadDocument(loader *openapi3.Loader, spec string) (*openapi3.T, error) {
	parsed, err := url.Parse(spec)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		doc, loadErr := loader.LoadFromURI(parsed)
		if loadErr == nil && doc.OpenAPI != "" {
			return doc, nil
		}

		doc, v2Err := loadSwagger2(spec)
		if v2Err != nil {
			return nil, fmt.Errorf("load OpenAPI URL: %w; load Swagger 2.0 URL: %w", loadErr, v2Err)
		}
		return doc, nil
	}

	doc, loadErr := loader.LoadFromFile(spec)
	if loadErr == nil && doc.OpenAPI != "" {
		return doc, nil
	}

	doc, v2Err := loadSwagger2(spec)
	if v2Err != nil {
		return nil, fmt.Errorf("load OpenAPI file: %w; load Swagger 2.0 file: %w", loadErr, v2Err)
	}
	return doc, nil
}

func loadSwagger2(spec string) (*openapi3.T, error) {
	data, err := readSpecBytes(spec)
	if err != nil {
		return nil, err
	}

	var doc2 openapi2.T
	if err := json.Unmarshal(data, &doc2); err != nil {
		if _, yamlErr := yaml.Unmarshal(data, &doc2, yaml.DecodeOpts{DisableTimestamps: true}); yamlErr != nil {
			return nil, fmt.Errorf("parse Swagger 2.0 document: json error: %v, yaml error: %v", err, yamlErr)
		}
	}
	if doc2.Swagger != "2.0" {
		return nil, fmt.Errorf("unsupported spec: expected OpenAPI 3.x or Swagger 2.0 document")
	}

	doc3, err := openapi2conv.ToV3(&doc2)
	if err != nil {
		return nil, fmt.Errorf("convert Swagger 2.0 to OpenAPI 3.x: %w", err)
	}
	return doc3, nil
}

func readSpecBytes(spec string) ([]byte, error) {
	parsed, err := url.Parse(spec)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		resp, getErr := http.Get(spec)
		if getErr != nil {
			return nil, fmt.Errorf("fetch spec URL: %w", getErr)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("fetch spec URL: unexpected status %s", resp.Status)
		}
		return io.ReadAll(resp.Body)
	}

	data, readErr := os.ReadFile(spec)
	if readErr != nil {
		return nil, fmt.Errorf("read spec file: %w", readErr)
	}
	return data, nil
}

func firstServerURL(doc *openapi3.T) string {
	if len(doc.Servers) == 0 || doc.Servers[0] == nil {
		return "http://localhost"
	}
	return strings.TrimRight(doc.Servers[0].URL, "/")
}
