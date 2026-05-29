package openapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseExtractsOperationSummaries(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi.yaml")
	spec := `openapi: 3.0.3
info:
  title: Sample API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /users/{id}:
    get:
      operationId: getUser
      summary: Get user
      description: Returns one user
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
      responses:
        "200":
          description: ok
`
	if err := os.WriteFile(specPath, []byte(spec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	summary, err := Parse(specPath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if summary.Title != "Sample API" {
		t.Fatalf("Title = %q", summary.Title)
	}
	if summary.BaseURL != "https://api.example.com" {
		t.Fatalf("BaseURL = %q", summary.BaseURL)
	}
	if len(summary.Operations) != 1 {
		t.Fatalf("Operations length = %d", len(summary.Operations))
	}

	operation := summary.Operations[0]
	if operation.Method != "GET" || operation.Path != "/users/{id}" || operation.OperationID != "getUser" {
		t.Fatalf("unexpected operation: %+v", operation)
	}
	if operation.APIID != "getUser" {
		t.Fatalf("APIID = %q", operation.APIID)
	}
}

func TestParseExtractsQueryAndRequestBodySamples(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi.yaml")
	spec := `openapi: 3.0.3
info:
  title: Sample API
  version: 1.0.0
paths:
  /orders:
    post:
      operationId: createOrder
      parameters:
        - name: userId
          in: query
          required: true
          schema:
            type: integer
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name]
              properties:
                name:
                  type: string
                quantity:
                  type: integer
      responses:
        "201":
          description: created
`
	if err := os.WriteFile(specPath, []byte(spec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	summary, err := Parse(specPath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	operation := summary.Operations[0]
	if len(operation.Parameters) != 1 {
		t.Fatalf("Parameters length = %d", len(operation.Parameters))
	}
	if operation.Parameters[0].Name != "userId" || operation.Parameters[0].Value != "__RANDOM_ID__" {
		t.Fatalf("unexpected parameter: %+v", operation.Parameters[0])
	}
	body, ok := operation.RequestBody.(map[string]any)
	if !ok {
		t.Fatalf("RequestBody type = %T", operation.RequestBody)
	}
	if body["name"] != "sample" || body["quantity"] != "__RANDOM_ID__" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestParseUsesUUIDSamplesFromSchemaHints(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi.yaml")
	spec := `openapi: 3.0.3
info:
  title: UUID API
  version: 1.0.0
paths:
  /companies/{companyId}/users/{userUuid}:
    post:
      operationId: addCompanyUser
      parameters:
        - name: companyId
          in: path
          required: true
          schema:
            type: string
            format: uuid
        - name: userUuid
          in: path
          required: true
          schema:
            type: string
        - name: requestId
          in: query
          schema:
            type: string
            pattern: "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
      requestBody:
        required: true
        content:
          application/json:
            schema:
              allOf:
                - type: object
                  properties:
                    organizationId:
                      type: string
                      format: uuid
                - type: object
                  properties:
                    role:
                      type: string
                      enum: [ADMIN]
      responses:
        "201":
          description: created
`
	if err := os.WriteFile(specPath, []byte(spec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	summary, err := Parse(specPath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	operation := summary.Operations[0]
	values := map[string]any{}
	for _, param := range operation.Parameters {
		values[param.Name] = param.Value
	}
	if values["companyId"] != "__RANDOM_UUID__" || values["userUuid"] != "__RANDOM_UUID__" || values["requestId"] != "__RANDOM_UUID__" {
		t.Fatalf("unexpected parameters: %+v", values)
	}
	body, ok := operation.RequestBody.(map[string]any)
	if !ok {
		t.Fatalf("RequestBody type = %T", operation.RequestBody)
	}
	if body["organizationId"] != "__RANDOM_UUID__" || body["role"] != "ADMIN" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestParseIgnoresInvalidExamples(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi.yaml")
	spec := `openapi: 3.0.3
info:
  title: Example API
  version: 1.0.0
paths:
  /jobs:
    get:
      operationId: listJobs
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  source:
                    type: string
                    enum: [DIRECT, SCRAPED]
              example:
                source: null
`
	if err := os.WriteFile(specPath, []byte(spec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	summary, err := Parse(specPath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(summary.Operations) != 1 || summary.Operations[0].APIID != "listJobs" {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestParseIgnoresInvalidDefaults(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi.yaml")
	spec := `openapi: 3.0.3
info:
  title: Example API
  version: 1.0.0
paths:
  /interviews:
    post:
      operationId: createInterview
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                count:
                  type: integer
                  default: wrong
      responses:
        "201":
          description: created
`
	if err := os.WriteFile(specPath, []byte(spec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	summary, err := Parse(specPath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(summary.Operations) != 1 || summary.Operations[0].APIID != "createInterview" {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestParseIgnoresNonStandardSecuritySchemeNames(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "openapi.yaml")
	spec := `openapi: 3.0.3
info:
  title: Example API
  version: 1.0.0
components:
  securitySchemes:
    Bearer Authentication:
      type: http
      scheme: bearer
paths:
  /me:
    get:
      operationId: getMe
      security:
        - Bearer Authentication: []
      responses:
        "200":
          description: ok
`
	if err := os.WriteFile(specPath, []byte(spec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	summary, err := Parse(specPath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(summary.Operations) != 1 || !summary.Operations[0].RequiresAuth {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestParseSupportsSwagger2(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "swagger.yaml")
	spec := `swagger: "2.0"
info:
  title: Legacy API
  version: 1.0.0
schemes:
  - https
host: legacy.example.com
basePath: /api
paths:
  /orders:
    post:
      operationId: createOrder
      summary: Create order
      responses:
        "201":
          description: created
`
	if err := os.WriteFile(specPath, []byte(spec), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	summary, err := Parse(specPath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if summary.Title != "Legacy API" {
		t.Fatalf("Title = %q", summary.Title)
	}
	if summary.BaseURL != "https://legacy.example.com/api" {
		t.Fatalf("BaseURL = %q", summary.BaseURL)
	}
	if len(summary.Operations) != 1 {
		t.Fatalf("Operations length = %d", len(summary.Operations))
	}
	operation := summary.Operations[0]
	if operation.Method != "POST" || operation.Path != "/orders" || operation.OperationID != "createOrder" {
		t.Fatalf("unexpected operation: %+v", operation)
	}
}
