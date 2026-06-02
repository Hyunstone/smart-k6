package k6

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"github.com/hyunseok/smart-k6/internal/openapi"
	"github.com/hyunseok/smart-k6/internal/scenario"
)

// ScriptData is the input model for the static k6 script template.
type ScriptData struct {
	SpecTitle  string
	BaseURL    string
	TPS        int
	Scale      string
	Duration   string
	Operations []openapi.OperationSummary
	Scenario   scenario.Plan
}

type templateOperation struct {
	APIID       string
	Method      string
	Path        string
	Name        string
	PathParams  map[string]any
	QueryParams map[string]any
	Headers     map[string]any
	Body        any
}

type templateData struct {
	SpecTitle  string
	BaseURL    string
	TPS        int
	Scale      string
	ScaleLimit int64
	Duration   string
	Operations []templateOperation
	Scenario   []scenario.Step
}

const scriptTemplate = `import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  scenarios: {
    steady_load: {
      executor: 'constant-arrival-rate',
      rate: {{ .TPS }},
      timeUnit: '1s',
      duration: {{ js .Duration }},
      preAllocatedVUs: Math.max(10, Math.ceil({{ .TPS }} / 2)),
      maxVUs: Math.max(50, {{ .TPS }} * 2),
    },
  },
};

const BASE_URL = __ENV.BASE_URL || {{ js .BaseURL }};
const SCALE_LIMIT = {{ .ScaleLimit }};

const operations = {
{{- range .Operations }}
  {{ js .APIID }}: {
    method: {{ js .Method }},
    path: {{ js .Path }},
    name: {{ js .Name }},
    pathParams: {{ json .PathParams }},
    queryParams: {{ json .QueryParams }},
    headers: {{ json .Headers }},
    body: {{ json .Body }},
  },
{{- end }}
};

const scenario = {{ json .Scenario }};
const vars = {};

function randomId() {
  return Math.floor(Math.random() * SCALE_LIMIT) + 1;
}

function randomUUID() {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (char) => {
    const value = Math.floor(Math.random() * 16);
    return (char === 'x' ? value : (value & 0x3) | 0x8).toString(16);
  });
}

function looksLikeUUIDName(name) {
  return String(name || '').toLowerCase().includes('uuid');
}

function readPath(source, path) {
  if (!path) {
    return undefined;
  }
  return path.split('.').reduce((value, key) => {
    if (value === undefined || value === null) {
      return undefined;
    }
    return value[key];
  }, source);
}

function normalizeValue(value) {
  if (value === '__RANDOM_ID__') {
    return randomId();
  }
  if (value === '__RANDOM_UUID__') {
    return randomUUID();
  }
  if (value === '__AUTH_TOKEN__') {
    return __ENV.AUTH_TOKEN || vars.token || vars.accessToken || undefined;
  }
  if (Array.isArray(value)) {
    return value.map(normalizeValue);
  }
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, normalizeValue(item)]));
  }
  return value;
}

function cleanObject(object) {
  if (!object || typeof object !== 'object') {
    return {};
  }
  return Object.fromEntries(
    Object.entries(object).filter(([, value]) => value !== undefined && value !== null && value !== '')
  );
}

function mergeObjects(base, override) {
  return Object.assign({}, base || {}, override || {});
}

function hasOwn(object, key) {
  return Object.prototype.hasOwnProperty.call(object || {}, key);
}

function applyVariableBindings(target, bindings) {
  if (!target || !bindings) {
    return target;
  }
  if (typeof target !== 'object') {
    return target;
  }
  for (const [field, variable] of Object.entries(bindings)) {
    if (vars[variable] !== undefined) {
      target[field] = vars[variable];
    }
  }
  return target;
}

function buildPath(path, defaults, bindings) {
  return path.replace(/\{([^}]+)\}/g, (_, name) => {
    const variable = bindings && bindings[name];
    if (variable && vars[variable] !== undefined) {
      return encodeURIComponent(vars[variable]);
    }
    const defaultValue = defaults && defaults[name];
    if (defaultValue !== undefined) {
      return encodeURIComponent(String(normalizeValue(defaultValue)));
    }
    if (looksLikeUUIDName(name)) {
      return encodeURIComponent(randomUUID());
    }
    return randomId();
  });
}

function buildURL(operation, pathParams, queryParams, bindings) {
  let url = BASE_URL + buildPath(operation.path, pathParams || {}, bindings);
  queryParams = applyVariableBindings(normalizeValue(queryParams || {}), bindings);
  const encoded = [];
  for (const [key, value] of Object.entries(queryParams)) {
    if (value !== undefined && value !== null && value !== '') {
      encoded.push(encodeURIComponent(key) + '=' + encodeURIComponent(String(value)));
    }
  }
  if (encoded.length === 0) {
    return url;
  }
  return url + (url.includes('?') ? '&' : '?') + encoded.join('&');
}

function compareValues(actual, operator, expected) {
  switch (operator || 'eq') {
    case 'exists':
      return actual !== undefined && actual !== null;
    case 'matches':
      return new RegExp(String(expected)).test(String(actual));
    case 'gte':
      return Number(actual) >= Number(expected);
    case 'lte':
      return Number(actual) <= Number(expected);
    case 'eq':
    default:
      return actual === expected;
  }
}

function evaluateCheck(spec, res, jsonBody) {
  const operator = spec.operator || 'eq';
  switch (spec.type) {
    case 'status':
      return compareValues(res.status, operator, spec.value);
    case 'json_path':
      return compareValues(readPath(jsonBody, spec.path), operator, spec.value);
    case 'header':
      return compareValues(res.headers[spec.path], operator, spec.value);
    case 'body_contains':
      return String(res.body || '').includes(String(spec.value));
    default:
      return false;
  }
}

function checkName(operation, spec) {
  if (spec.type === 'status') {
    return operation.method + ' ' + operation.path + ' status ' + (spec.operator || 'eq') + ' ' + spec.value;
  }
  if (spec.path) {
    return operation.method + ' ' + operation.path + ' ' + spec.type + ' ' + spec.path;
  }
  return operation.method + ' ' + operation.path + ' ' + spec.type;
}

export default function () {
  for (const step of scenario) {
    const operation = operations[step.api_id];
    if (!operation) {
      throw new Error('Unknown API ID in scenario: ' + step.api_id);
    }

    const bindings = step.use_variables || {};
    const overrides = normalizeValue(step.overrides || {});
    const pathParams = mergeObjects(operation.pathParams, overrides.path_params);
    const queryParams = mergeObjects(operation.queryParams, overrides.query_params);
    const headers = cleanObject(applyVariableBindings(normalizeValue(mergeObjects(operation.headers, overrides.headers)), bindings));
    const rawBody = hasOwn(overrides, 'body') ? overrides.body : operation.body;
    const body = applyVariableBindings(normalizeValue(rawBody), bindings);
    const url = buildURL(operation, pathParams, queryParams, bindings);
    const payload = body === null || body === undefined ? null : JSON.stringify(body);
    const res = http.request(operation.method, url, payload, {
      headers,
      tags: { name: operation.name },
    });

    let jsonBody = {};
    try {
      jsonBody = res.json();
    } catch (error) {
      jsonBody = {};
    }

    const checks = step.checks && step.checks.length > 0
      ? Object.fromEntries(step.checks.map((spec) => [checkName(operation, spec), () => evaluateCheck(spec, res, jsonBody)]))
      : {
          [operation.method + ' ' + operation.path + ' status is 2xx-4xx']: (r) => r.status >= 200 && r.status < 500,
        };
    check(res, checks);

    if (step.extract_variables && Object.keys(step.extract_variables).length > 0) {
      for (const [name, path] of Object.entries(step.extract_variables)) {
        const extracted = readPath(jsonBody, path);
        if (extracted !== undefined) {
          vars[name] = extracted;
        }
      }
    }
  }

  sleep(1);
}
`

// Render returns a static k6 JavaScript script from parsed OpenAPI operations.
func Render(data ScriptData) (string, error) {
	if data.TPS <= 0 {
		return "", fmt.Errorf("tps must be greater than 0")
	}
	if strings.TrimSpace(data.Duration) == "" {
		return "", fmt.Errorf("duration is required")
	}
	if len(data.Operations) == 0 {
		return "", fmt.Errorf("at least one operation is required")
	}

	scaleLimit, err := parseScale(data.Scale)
	if err != nil {
		return "", err
	}

	tplData := templateData{
		SpecTitle:  data.SpecTitle,
		BaseURL:    strings.TrimRight(defaultString(data.BaseURL, "http://localhost"), "/"),
		TPS:        data.TPS,
		Scale:      data.Scale,
		ScaleLimit: scaleLimit,
		Duration:   data.Duration,
		Operations: make([]templateOperation, 0, len(data.Operations)),
		Scenario:   data.Scenario.Steps,
	}
	if len(tplData.Scenario) == 0 {
		ids := make([]string, 0, len(data.Operations))
		for _, operation := range data.Operations {
			ids = append(ids, operationAPIID(operation))
		}
		tplData.Scenario = scenario.DefaultPlan(ids).Steps
	}

	for _, operation := range data.Operations {
		tplData.Operations = append(tplData.Operations, templateOperation{
			APIID:       operationAPIID(operation),
			Method:      operation.Method,
			Path:        operation.Path,
			Name:        operationName(operation),
			PathParams:  operationParams(operation, "path"),
			QueryParams: operationParams(operation, "query"),
			Headers:     operationHeaders(operation),
			Body:        operation.RequestBody,
		})
	}

	tpl, err := template.New("k6").Funcs(template.FuncMap{
		"js":   strconv.Quote,
		"json": mustJSON,
	}).Parse(scriptTemplate)
	if err != nil {
		return "", fmt.Errorf("parse k6 template: %w", err)
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, tplData); err != nil {
		return "", fmt.Errorf("render k6 template: %w", err)
	}
	return buf.String(), nil
}

func operationAPIID(operation openapi.OperationSummary) string {
	if operation.APIID != "" {
		return operation.APIID
	}
	return strings.ToLower(operation.Method + "_" + strings.Trim(strings.ReplaceAll(operation.Path, "/", "_"), "_"))
}

func operationParams(operation openapi.OperationSummary, in string) map[string]any {
	values := map[string]any{}
	for _, param := range operation.Parameters {
		if param.In == in {
			values[param.Name] = param.Value
		}
	}
	return values
}

func operationHeaders(operation openapi.OperationSummary) map[string]any {
	headers := map[string]any{}
	for _, param := range operation.Parameters {
		if param.In == "header" {
			headers[param.Name] = param.Value
		}
	}
	if operation.RequestBody != nil {
		headers["Content-Type"] = "application/json"
	}
	if operation.RequiresAuth {
		headers["Authorization"] = "Bearer __AUTH_TOKEN__"
	}
	return headers
}

func mustJSON(value any) (string, error) {
	if value == nil {
		return "null", nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func operationName(operation openapi.OperationSummary) string {
	if operation.OperationID != "" {
		return operation.OperationID
	}
	if operation.Summary != "" {
		return operation.Summary
	}
	return operation.Method + " " + operation.Path
}

func parseScale(scale string) (int64, error) {
	value := strings.TrimSpace(strings.ToUpper(scale))
	if value == "" {
		return 1_000_000, nil
	}

	multiplier := int64(1)
	switch {
	case strings.HasSuffix(value, "K"):
		multiplier = 1_000
		value = strings.TrimSuffix(value, "K")
	case strings.HasSuffix(value, "M"):
		multiplier = 1_000_000
		value = strings.TrimSuffix(value, "M")
	case strings.HasSuffix(value, "B"):
		multiplier = 1_000_000_000
		value = strings.TrimSuffix(value, "B")
	}

	number, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("invalid scale %q: use values like 1000, 1M, 10M, or 1B", scale)
	}
	return number * multiplier, nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
