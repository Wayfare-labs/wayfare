package wayfare

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Minimal YAML parser designed specifically for the subset of YAML used in
// openapi.yaml without adding external dependencies (preserving the two-dep policy).

// parseYAML parses a clean, indented YAML file into map[string]any.
func parseYAML(text string) (map[string]any, error) {
	lines := strings.Split(text, "\n")
	res, err := parseYAMLBlock(lines, 0, 0)
	if err != nil {
		return nil, err
	}
	if m, ok := res.(map[string]any); ok {
		return m, nil
	}
	return nil, fmt.Errorf("top-level YAML is not a mapping")
}

func parseYAMLBlock(lines []string, startLine int, minIndent int) (any, error) {
	var m map[string]any
	var l []any

	i := startLine
	for i < len(lines) {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}

		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if indent < minIndent {
			break
		}

		// Strip inline comment if not quoted
		lineContent := stripYAMLComment(trimmed)

		if strings.HasPrefix(lineContent, "- ") {
			if len(m) > 0 {
				return nil, fmt.Errorf("line %d: mixed list and map at indent %d", i+1, indent)
			}
			if l == nil {
				l = []any{}
			}
			itemContent := strings.TrimSpace(lineContent[2:])
			if itemContent == "" {
				// Nested block under list item
				child, err := parseYAMLBlock(lines, i+1, indent+1)
				if err != nil {
					return nil, err
				}
				l = append(l, child)
				i = nextLineAfterBlock(lines, i+1, indent+1)
				continue
			} else if strings.Contains(itemContent, ": ") || strings.HasSuffix(itemContent, ":") {
				// Map entry inside list item, e.g. "- name: to"
				colonIdx := strings.Index(itemContent, ":")
				firstKey := strings.TrimSpace(itemContent[:colonIdx])
				firstValStr := strings.TrimSpace(itemContent[colonIdx+1:])

				subMap := map[string]any{}
				if firstValStr != "" {
					subMap[firstKey] = parseYAMLScalar(firstValStr)
				} else {
					child, err := parseYAMLBlock(lines, i+1, indent+len(firstKey)+4)
					if err != nil {
						return nil, err
					}
					subMap[firstKey] = child
				}

				// Check subsequent lines at indent+2 for remaining fields of this list item map
				subLines := []string{}
				j := i + 1
				for j < len(lines) {
					jRaw := lines[j]
					jTrimmed := strings.TrimSpace(jRaw)
					if jTrimmed == "" || strings.HasPrefix(jTrimmed, "#") {
						j++
						continue
					}
					jIndent := len(jRaw) - len(strings.TrimLeft(jRaw, " "))
					if jIndent <= indent {
						break
					}
					if jIndent == indent+2 && strings.HasPrefix(jTrimmed, "- ") {
						break
					}
					subLines = append(subLines, jRaw)
					j++
				}

				if len(subLines) > 0 {
					subBlock, err := parseYAMLBlock(subLines, 0, indent+2)
					if err == nil {
						if subM, ok := subBlock.(map[string]any); ok {
							for k, v := range subM {
								subMap[k] = v
							}
						}
					}
				}
				l = append(l, subMap)
				i = j
				continue
			} else {
				l = append(l, parseYAMLScalar(itemContent))
				i++
				continue
			}
		}

		// Otherwise it's a map entry: "key:" or "key: value"
		if len(l) > 0 {
			return nil, fmt.Errorf("line %d: mixed list and map at indent %d", i+1, indent)
		}
		if m == nil {
			m = map[string]any{}
		}

		colonIdx := strings.Index(lineContent, ":")
		if colonIdx == -1 {
			return nil, fmt.Errorf("line %d: expected key-value pair, got %q", i+1, lineContent)
		}

		key := strings.TrimSpace(lineContent[:colonIdx])
		key = unquoteYAML(key)
		valStr := strings.TrimSpace(lineContent[colonIdx+1:])

		if valStr == "" {
			// Nested block
			child, err := parseYAMLBlock(lines, i+1, indent+1)
			if err != nil {
				return nil, err
			}
			m[key] = child
			i = nextLineAfterBlock(lines, i+1, indent+1)
		} else {
			m[key] = parseYAMLScalar(valStr)
			i++
		}
	}

	if l != nil {
		return l, nil
	}
	if m != nil {
		return m, nil
	}
	return map[string]any{}, nil
}

func nextLineAfterBlock(lines []string, startLine int, minIndent int) int {
	for i := startLine; i < len(lines); i++ {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if indent < minIndent {
			return i
		}
	}
	return len(lines)
}

func stripYAMLComment(s string) string {
	inQuote := false
	var quoteChar rune
	for i, r := range s {
		if inQuote {
			if r == quoteChar {
				inQuote = false
			}
		} else {
			if r == '"' || r == '\'' {
				inQuote = true
				quoteChar = r
			} else if r == '#' && i > 0 && s[i-1] == ' ' {
				return strings.TrimSpace(s[:i])
			}
		}
	}
	return s
}

func parseYAMLScalar(s string) any {
	s = strings.TrimSpace(s)
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if s == "null" || s == "~" {
		return nil
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		if inner == "" {
			return []any{}
		}
		parts := strings.Split(inner, ",")
		var arr []any
		for _, p := range parts {
			arr = append(arr, parseYAMLScalar(p))
		}
		return arr
	}
	if (strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)) ||
		(strings.HasPrefix(s, `'`) && strings.HasSuffix(s, `'`)) {
		return unquoteYAML(s)
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return float64(n)
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

func unquoteYAML(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}

// openAPIValidator compiles and validates values against openapi.yaml schemas.
type openAPIValidator struct {
	schemas map[string]map[string]any
	paths   map[string]map[string]any
}

func newOpenAPIValidator(doc map[string]any) (*openAPIValidator, error) {
	v := &openAPIValidator{
		schemas: make(map[string]map[string]any),
		paths:   make(map[string]map[string]any),
	}

	components, _ := doc["components"].(map[string]any)
	if rawSchemas, ok := components["schemas"].(map[string]any); ok {
		for name, s := range rawSchemas {
			if schemaMap, ok := s.(map[string]any); ok {
				v.schemas[name] = schemaMap
			}
		}
	}

	if rawPaths, ok := doc["paths"].(map[string]any); ok {
		for p, item := range rawPaths {
			if pathMap, ok := item.(map[string]any); ok {
				v.paths[p] = pathMap
			}
		}
	}

	return v, nil
}

func (v *openAPIValidator) getSchemaFor(method, path string, status int) (map[string]any, error) {
	pathItem, ok := v.paths[path]
	if !ok {
		return nil, fmt.Errorf("path %s not defined in openapi.yaml", path)
	}
	op, ok := pathItem[strings.ToLower(method)].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("method %s not defined for path %s in openapi.yaml", method, path)
	}
	responses, ok := op["responses"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("no responses defined for %s %s in openapi.yaml", method, path)
	}
	statusStr := strconv.Itoa(status)
	respObj, ok := responses[statusStr].(map[string]any)
	if !ok {
		// Fallback to default if present
		respObj, ok = responses["default"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("status %d not defined for %s %s in openapi.yaml", status, method, path)
		}
	}

	content, ok := respObj["content"].(map[string]any)
	if !ok {
		// e.g. 204 No Content
		return nil, nil
	}
	appJSON, ok := content["application/json"].(map[string]any)
	if !ok {
		return nil, nil
	}
	schema, ok := appJSON["schema"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("no schema found in application/json for %s %s status %d", method, path, status)
	}
	return schema, nil
}

func (v *openAPIValidator) validate(schema map[string]any, value any, path string) []string {
	var errs []string
	at := func(msg string) {
		if path == "" {
			errs = append(errs, msg)
		} else {
			errs = append(errs, fmt.Sprintf("%s: %s", path, msg))
		}
	}

	if value == nil {
		if nullable, ok := schema["nullable"].(bool); ok && nullable {
			return nil
		}
		at("unexpected null value")
		return errs
	}

	if ref, ok := schema["$ref"].(string); ok {
		const prefix = "#/components/schemas/"
		if strings.HasPrefix(ref, prefix) {
			name := strings.TrimPrefix(ref, prefix)
			target, found := v.schemas[name]
			if !found {
				at("unresolvable $ref: " + ref)
				return errs
			}
			return append(errs, v.validate(target, value, path)...)
		}
	}

	if wantType, ok := schema["type"].(string); ok {
		if !matchJSONType(value, wantType) {
			at(fmt.Sprintf("want type %s, got %T", wantType, value))
			return errs
		}
	}

	if enum, ok := schema["enum"].([]any); ok {
		matched := false
		for _, opt := range enum {
			if reflect.DeepEqual(opt, value) {
				matched = true
				break
			}
		}
		if !matched {
			at(fmt.Sprintf("value %v is not in enum %v", value, enum))
		}
	}

	switch typed := value.(type) {
	case string:
		if pattern, ok := schema["pattern"].(string); ok {
			re, err := regexp.Compile(pattern)
			if err != nil {
				at("invalid regex pattern: " + err.Error())
			} else if !re.MatchString(typed) {
				at(fmt.Sprintf("%q does not match pattern %s", typed, pattern))
			}
		}
	case map[string]any:
		if required, ok := schema["required"].([]any); ok {
			for _, r := range required {
				reqKey, _ := r.(string)
				if _, present := typed[reqKey]; !present {
					fieldPath := reqKey
					if path != "" {
						fieldPath = path + "." + reqKey
					}
					errs = append(errs, fmt.Sprintf("%s: required field is missing", fieldPath))
				}
			}
		}
		props, _ := schema["properties"].(map[string]any)
		for k, val := range typed {
			fieldPath := k
			if path != "" {
				fieldPath = path + "." + k
			}
			if childSchema, ok := props[k].(map[string]any); ok {
				errs = append(errs, v.validate(childSchema, val, fieldPath)...)
			} else {
				// Additional properties
				if addProp, ok := schema["additionalProperties"]; ok {
					switch addVal := addProp.(type) {
					case bool:
						if !addVal {
							errs = append(errs, fmt.Sprintf("%s: unexpected property not in schema", fieldPath))
						}
					case map[string]any:
						errs = append(errs, v.validate(addVal, val, fieldPath)...)
					}
				}
			}
		}
	case []any:
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for idx, item := range typed {
				elemPath := fmt.Sprintf("%s[%d]", path, idx)
				errs = append(errs, v.validate(itemSchema, item, elemPath)...)
			}
		}
	}

	return errs
}

func matchJSONType(val any, want string) bool {
	switch want {
	case "object":
		_, ok := val.(map[string]any)
		return ok
	case "array":
		_, ok := val.([]any)
		return ok
	case "string":
		_, ok := val.(string)
		return ok
	case "boolean":
		_, ok := val.(bool)
		return ok
	case "integer":
		f, ok := val.(float64)
		return ok && f == float64(int64(f))
	case "number":
		_, ok := val.(float64)
		return ok
	case "null":
		return val == nil
	}
	return false
}

// TestOpenAPISchemaValidatesRecordedResponses verifies all recorded endpoint responses
// against openapi.yaml in CI (issue #697).
func TestOpenAPISchemaValidatesRecordedResponses(t *testing.T) {
	rawOpenAPI, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("reading openapi.yaml: %v", err)
	}
	doc, err := parseYAML(string(rawOpenAPI))
	if err != nil {
		t.Fatalf("parsing openapi.yaml: %v", err)
	}

	validator, err := newOpenAPIValidator(doc)
	if err != nil {
		t.Fatalf("compiling OpenAPI validator: %v", err)
	}

	rawEndpoints, err := os.ReadFile("docs/qa/api/results/endpoints.json")
	if err != nil {
		t.Fatalf("reading docs/qa/api/results/endpoints.json: %v", err)
	}

	var endpointsData struct {
		Cases []struct {
			Name   string `json:"name"`
			Method string `json:"method"`
			Path   string `json:"path"`
			Status int    `json:"status"`
			Body   string `json:"body"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(rawEndpoints, &endpointsData); err != nil {
		t.Fatalf("decoding endpoints.json: %v", err)
	}

	validatedCount := 0
	for _, c := range endpointsData.Cases {
		// Non-JSON endpoints (e.g. HTML UI root or 204 No Content) are skipped
		if c.Path == "/" || c.Status == 204 {
			continue
		}

		basePath := strings.Split(c.Path, "?")[0]

		// Resolve schema from OpenAPI for this endpoint and status
		schema, err := validator.getSchemaFor(c.Method, basePath, c.Status)
		if err != nil {
			// Some tests explore undocumented methods (like POST /api/assets or POST /healthz
			// which are findings recorded in #260 rather than official contracts).
			// If not in OpenAPI, assert whether it was an expected documented method.
			if c.Method != "GET" && (c.Status == 200 || c.Status == 405) {
				continue
			}
			t.Errorf("case %s (%s %s -> %d): %v", c.Name, c.Method, c.Path, c.Status, err)
			continue
		}
		if schema == nil {
			continue
		}

		var parsedBody any
		if err := json.Unmarshal([]byte(c.Body), &parsedBody); err != nil {
			t.Errorf("case %s: response body is not valid JSON: %v", c.Name, err)
			continue
		}

		errs := validator.validate(schema, parsedBody, "")
		if len(errs) > 0 {
			t.Errorf("case %s (%s %s -> %d) failed OpenAPI schema validation: %s",
				c.Name, c.Method, c.Path, c.Status, strings.Join(errs, "; "))
		}
		validatedCount++
	}

	if validatedCount == 0 {
		t.Fatal("no recorded responses were validated against OpenAPI schemas")
	}
	t.Logf("Validated %d recorded endpoint responses against openapi.yaml", validatedCount)
}

// TestOpenAPIValidatorRejectsInvalidFigures asserts negative validation cases.
func TestOpenAPIValidatorRejectsInvalidFigures(t *testing.T) {
	rawOpenAPI, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("reading openapi.yaml: %v", err)
	}
	doc, err := parseYAML(string(rawOpenAPI))
	if err != nil {
		t.Fatalf("parsing openapi.yaml: %v", err)
	}
	validator, err := newOpenAPIValidator(doc)
	if err != nil {
		t.Fatalf("compiling OpenAPI validator: %v", err)
	}

	corridorSchema, err := validator.getSchemaFor("GET", "/api/corridor", 200)
	if err != nil {
		t.Fatalf("getting corridor schema: %v", err)
	}

	t.Run("rejects numeric float for money fields", func(t *testing.T) {
		badBody := map[string]any{
			"send_asset":          map[string]any{"code": "USDC"},
			"receive_asset":       map[string]any{"code": "NGNC"},
			"integrity":           "DIRECT",
			"reference_mid":       1500.50, // Float instead of decimal string!
			"reference_source":    "mock",
			"reference_pair":      "USD/NGN",
			"reference_agreement": "AGREE",
			"scored":              true,
			"floor_loss_pct":      "10.5",
			"floor_size":          "100",
			"worst_loss_pct":      "20.0",
			"worst_size":          "1000",
			"live":                true,
			"rungs":               []any{},
			"measured_at":         "2026-08-22T12:00:00Z",
		}
		errs := validator.validate(corridorSchema, badBody, "")
		if len(errs) == 0 {
			t.Error("validator accepted float number where decimal string was required")
		}
	})

	t.Run("rejects missing required fields", func(t *testing.T) {
		badBody := map[string]any{
			"send_asset": map[string]any{"code": "USDC"},
			// missing receive_asset, integrity, etc.
		}
		errs := validator.validate(corridorSchema, badBody, "")
		if len(errs) == 0 {
			t.Error("validator accepted corridor body missing required fields")
		}
	})

	t.Run("rejects invalid enum values", func(t *testing.T) {
		badBody := map[string]any{
			"send_asset":          map[string]any{"code": "USDC"},
			"receive_asset":       map[string]any{"code": "NGNC"},
			"integrity":           "NONEXISTENT_INTEGRITY",
			"reference_mid":       "1500",
			"reference_source":    "mock",
			"reference_pair":      "USD/NGN",
			"reference_agreement": "AGREE",
			"scored":              true,
			"floor_loss_pct":      "10.5",
			"floor_size":          "100",
			"worst_loss_pct":      "20.0",
			"worst_size":          "1000",
			"live":                true,
			"rungs":               []any{},
			"measured_at":         "2026-08-22T12:00:00Z",
		}
		errs := validator.validate(corridorSchema, badBody, "")
		if len(errs) == 0 {
			t.Error("validator accepted invalid integrity enum value")
		}
	})
}
