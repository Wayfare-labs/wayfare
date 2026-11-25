package runstore

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
)

// A validator for the subset of JSON Schema the published run-store schemas
// use (#698).
//
// The repository allows exactly two modules, and adding a JSON Schema library
// for a test would be a policy change argued in the same PR that introduces a
// supply-chain surface. The subset here is small and closed — type, const,
// enum, required, properties, additionalProperties, items, $ref to #/$defs,
// pattern, minimum, minLength, oneOf — so it is cheaper to own these 200 lines
// than to own a dependency. If someone later writes a schema using a keyword
// this does not implement, compiling it fails loudly rather than validating
// everything by accident.

// SchemaError is one failed constraint, with the path it failed at.
type SchemaError struct {
	Path    string
	Message string
}

func (e SchemaError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return e.Path + ": " + e.Message
}

// Schema is a compiled schema document.
type Schema struct {
	root map[string]any
	// defs holds #/$defs entries, resolved on every $ref.
	defs map[string]map[string]any
}

// keywordSet is every keyword this validator understands. Compiling a schema
// that uses anything else is an error: silently ignoring an unknown keyword
// would turn a stricter published schema into a weaker enforced one.
var keywordSet = map[string]bool{
	"$schema": true, "$id": true, "title": true, "description": true,
	"type": true, "const": true, "enum": true, "required": true,
	"properties": true, "additionalProperties": true, "items": true,
	"$ref": true, "$defs": true, "pattern": true, "minimum": true,
	"minLength": true, "oneOf": true, "format": true,
}

// LoadSchema reads and compiles a schema file.
func LoadSchema(path string) (*Schema, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return CompileSchema(root)
}

// CompileSchema checks that every keyword is one this validator implements.
func CompileSchema(root map[string]any) (*Schema, error) {
	s := &Schema{root: root, defs: map[string]map[string]any{}}
	if rawDefs, ok := root["$defs"].(map[string]any); ok {
		for name, def := range rawDefs {
			obj, ok := def.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("$defs.%s is not an object", name)
			}
			s.defs[name] = obj
		}
	}
	if err := checkKeywords(root, "#"); err != nil {
		return nil, err
	}
	return s, nil
}

func checkKeywords(node map[string]any, path string) error {
	for key, value := range node {
		if !keywordSet[key] {
			return fmt.Errorf("%s: unsupported schema keyword %q", path, key)
		}
		switch key {
		case "properties", "$defs":
			if obj, ok := value.(map[string]any); ok {
				for name, sub := range obj {
					child, ok := sub.(map[string]any)
					if !ok {
						continue
					}
					if err := checkKeywords(child, path+"/"+key+"/"+name); err != nil {
						return err
					}
				}
			}
		case "items", "additionalProperties", "oneOf":
			switch typed := value.(type) {
			case map[string]any:
				if err := checkKeywords(typed, path+"/"+key); err != nil {
					return err
				}
			case []any:
				for i, item := range typed {
					if child, ok := item.(map[string]any); ok {
						if err := checkKeywords(child, fmt.Sprintf("%s/%s/%d", path, key, i)); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

// Validate returns every constraint the value fails, in document order.
func (s *Schema) Validate(value any) []SchemaError {
	return s.validate(s.root, value, "")
}

func (s *Schema) validate(node map[string]any, value any, path string) []SchemaError {
	var errs []SchemaError
	at := func(msg string) { errs = append(errs, SchemaError{Path: path, Message: msg}) }

	if ref, ok := node["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/$defs/")
		def, found := s.defs[name]
		if !found {
			return append(errs, SchemaError{Path: path, Message: "unresolvable $ref " + ref})
		}
		return append(errs, s.validate(def, value, path)...)
	}

	if want, ok := node["type"].(string); ok && !hasType(value, want) {
		at("must be a " + want)
		return errs
	}

	if want, ok := node["const"]; ok && !jsonEqual(want, value) {
		at(fmt.Sprintf("must be %v", want))
	}

	if enum, ok := node["enum"].([]any); ok {
		found := false
		for _, option := range enum {
			if jsonEqual(option, value) {
				found = true
				break
			}
		}
		if !found {
			at(fmt.Sprintf("must be one of %v", enum))
		}
	}

	if options, ok := node["oneOf"].([]any); ok {
		matches := 0
		for _, option := range options {
			sub, ok := option.(map[string]any)
			if !ok {
				continue
			}
			if len(s.validate(sub, value, path)) == 0 {
				matches++
			}
		}
		if matches != 1 {
			at(fmt.Sprintf("must match exactly one of %d alternatives, matched %d", len(options), matches))
		}
	}

	switch typed := value.(type) {
	case string:
		if pattern, ok := node["pattern"].(string); ok {
			re, err := regexp.Compile(pattern)
			if err != nil {
				at("schema pattern does not compile: " + err.Error())
			} else if !re.MatchString(typed) {
				at(fmt.Sprintf("%q does not match %s", typed, pattern))
			}
		}
		if min, ok := node["minLength"].(float64); ok && float64(len(typed)) < min {
			at(fmt.Sprintf("must be at least %d characters", int(min)))
		}
	case float64:
		if min, ok := node["minimum"].(float64); ok && typed < min {
			at(fmt.Sprintf("must be at least %v", min))
		}
	case map[string]any:
		if required, ok := node["required"].([]any); ok {
			for _, name := range required {
				key, _ := name.(string)
				if _, present := typed[key]; !present {
					errs = append(errs, SchemaError{
						Path:    joinPath(path, key),
						Message: "is required",
					})
				}
			}
		}
		props, _ := node["properties"].(map[string]any)
		for key, sub := range typed {
			child, declared := props[key]
			if declared {
				if subSchema, ok := child.(map[string]any); ok {
					errs = append(errs, s.validate(subSchema, sub, joinPath(path, key))...)
				}
				continue
			}
			switch additional := node["additionalProperties"].(type) {
			case bool:
				if !additional {
					errs = append(errs, SchemaError{Path: joinPath(path, key), Message: "is not a field this version has"})
				}
			case map[string]any:
				errs = append(errs, s.validate(additional, sub, joinPath(path, key))...)
			}
		}
	case []any:
		if items, ok := node["items"].(map[string]any); ok {
			for i, item := range typed {
				errs = append(errs, s.validate(items, item, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	}

	return errs
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func hasType(value any, want string) bool {
	switch want {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	case "integer":
		f, ok := value.(float64)
		return ok && f == float64(int64(f))
	case "number":
		_, ok := value.(float64)
		return ok
	}
	return false
}

func jsonEqual(a, b any) bool { return reflect.DeepEqual(a, b) }
