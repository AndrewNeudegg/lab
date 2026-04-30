package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

func validateJSONAgainstSchema(path string, raw, schema json.RawMessage) []string {
	if len(bytes.TrimSpace(schema)) == 0 {
		return nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return []string{fmt.Sprintf("%s is not valid JSON: %v", path, err)}
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return []string{fmt.Sprintf("%s must contain exactly one JSON value", path)}
	}
	parsed, ok, err := decodeSchemaObject(schema)
	if err != nil {
		return []string{fmt.Sprintf("tool schema is invalid: %v", err)}
	}
	if !ok {
		return nil
	}
	return compactIssueList(validateSchemaValue(path, value, parsed))
}

func validateSchemaValue(path string, value any, schema map[string]json.RawMessage) []string {
	var issues []string
	if enumValues, ok := rawArray(schema["enum"]); ok {
		if !valueMatchesEnum(value, enumValues) {
			issues = append(issues, fmt.Sprintf("%s must be one of %s", path, compactEnumValues(enumValues)))
		}
	}
	types := schemaTypes(schema["type"])
	if len(types) > 0 {
		matched := false
		for _, typ := range types {
			if jsonValueMatchesType(value, typ) {
				matched = true
				break
			}
		}
		if !matched {
			issues = append(issues, fmt.Sprintf("%s must be %s", path, strings.Join(types, " or ")))
			return issues
		}
	}
	if shouldValidateObject(value, schema, types) {
		obj, ok := value.(map[string]any)
		if !ok {
			return issues
		}
		issues = append(issues, validateSchemaObject(path, obj, schema)...)
	}
	if shouldValidateArray(value, schema, types) {
		arr, ok := value.([]any)
		if !ok {
			return issues
		}
		issues = append(issues, validateSchemaArray(path, arr, schema)...)
	}
	if stringValue, ok := value.(string); ok {
		issues = append(issues, validateSchemaString(path, stringValue, schema)...)
	}
	if numberValue, ok := value.(json.Number); ok {
		issues = append(issues, validateSchemaNumber(path, numberValue, schema)...)
	}
	return issues
}

func validateSchemaObject(path string, obj map[string]any, schema map[string]json.RawMessage) []string {
	var issues []string
	props, hasProps, err := schemaProperties(schema["properties"])
	if err != nil {
		return []string{fmt.Sprintf("tool schema properties are invalid: %v", err)}
	}
	required := schemaRequired(schema["required"])
	for _, name := range required {
		if _, ok := obj[name]; !ok {
			issues = append(issues, fmt.Sprintf("%s.%s is required", path, name))
		}
	}
	allowAdditional, additionalSchema, err := schemaAdditionalProperties(schema["additionalProperties"], hasProps)
	if err != nil {
		return []string{fmt.Sprintf("tool schema additionalProperties is invalid: %v", err)}
	}
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		childPath := path + "." + name
		if propSchema, ok := props[name]; ok {
			child, ok, err := decodeSchemaObject(propSchema)
			if err != nil {
				issues = append(issues, fmt.Sprintf("tool schema for %s is invalid: %v", childPath, err))
				continue
			}
			if ok {
				issues = append(issues, validateSchemaValue(childPath, obj[name], child)...)
			}
			continue
		}
		if additionalSchema != nil {
			issues = append(issues, validateSchemaValue(childPath, obj[name], additionalSchema)...)
			continue
		}
		if !allowAdditional {
			issues = append(issues, fmt.Sprintf("%s has unknown key %q", path, name))
		}
	}
	return issues
}

func validateSchemaArray(path string, arr []any, schema map[string]json.RawMessage) []string {
	var issues []string
	if min, ok := schemaInteger(schema["minItems"]); ok && len(arr) < min {
		issues = append(issues, fmt.Sprintf("%s must contain at least %d item(s)", path, min))
	}
	if max, ok := schemaInteger(schema["maxItems"]); ok && len(arr) > max {
		issues = append(issues, fmt.Sprintf("%s must contain at most %d item(s)", path, max))
	}
	itemSchema, ok, err := decodeSchemaObject(schema["items"])
	if err != nil {
		return append(issues, fmt.Sprintf("tool schema items are invalid: %v", err))
	}
	if !ok {
		return issues
	}
	for i, value := range arr {
		issues = append(issues, validateSchemaValue(fmt.Sprintf("%s[%d]", path, i), value, itemSchema)...)
	}
	return issues
}

func validateSchemaString(path, value string, schema map[string]json.RawMessage) []string {
	var issues []string
	if min, ok := schemaInteger(schema["minLength"]); ok && len([]rune(value)) < min {
		issues = append(issues, fmt.Sprintf("%s must contain at least %d character(s)", path, min))
	}
	if max, ok := schemaInteger(schema["maxLength"]); ok && len([]rune(value)) > max {
		issues = append(issues, fmt.Sprintf("%s must contain at most %d character(s)", path, max))
	}
	return issues
}

func validateSchemaNumber(path string, value json.Number, schema map[string]json.RawMessage) []string {
	var issues []string
	number, err := value.Float64()
	if err != nil {
		return append(issues, fmt.Sprintf("%s must be a valid number", path))
	}
	if min, ok := schemaNumber(schema["minimum"]); ok && number < min {
		issues = append(issues, fmt.Sprintf("%s must be at least %s", path, trimFloat(min)))
	}
	if max, ok := schemaNumber(schema["maximum"]); ok && number > max {
		issues = append(issues, fmt.Sprintf("%s must be at most %s", path, trimFloat(max)))
	}
	return issues
}

func decodeSchemaObject(raw json.RawMessage) (map[string]json.RawMessage, bool, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false, nil
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, false, err
	}
	return parsed, true, nil
}

func schemaTypes(raw json.RawMessage) []string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		return many
	}
	return nil
}

func jsonValueMatchesType(value any, typ string) bool {
	switch typ {
	case "null":
		return value == nil
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
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "integer":
		number, ok := value.(json.Number)
		return ok && isJSONInteger(number)
	default:
		return true
	}
}

func shouldValidateObject(value any, schema map[string]json.RawMessage, types []string) bool {
	if _, ok := value.(map[string]any); !ok {
		return false
	}
	if schemaStringSliceContains(types, "object") {
		return true
	}
	_, hasProps := schema["properties"]
	_, hasRequired := schema["required"]
	_, hasAdditional := schema["additionalProperties"]
	return len(types) == 0 && (hasProps || hasRequired || hasAdditional)
}

func shouldValidateArray(value any, schema map[string]json.RawMessage, types []string) bool {
	if _, ok := value.([]any); !ok {
		return false
	}
	if schemaStringSliceContains(types, "array") {
		return true
	}
	_, hasItems := schema["items"]
	_, hasMin := schema["minItems"]
	_, hasMax := schema["maxItems"]
	return len(types) == 0 && (hasItems || hasMin || hasMax)
}

func schemaProperties(raw json.RawMessage) (map[string]json.RawMessage, bool, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false, nil
	}
	var props map[string]json.RawMessage
	if err := json.Unmarshal(raw, &props); err != nil {
		return nil, true, err
	}
	return props, true, nil
}

func schemaRequired(raw json.RawMessage) []string {
	var required []string
	_ = json.Unmarshal(raw, &required)
	return required
}

func schemaAdditionalProperties(raw json.RawMessage, hasProperties bool) (bool, map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return !hasProperties, nil, nil
	}
	var allow bool
	if err := json.Unmarshal(raw, &allow); err == nil {
		return allow, nil, nil
	}
	var schema map[string]json.RawMessage
	if err := json.Unmarshal(raw, &schema); err != nil {
		return false, nil, err
	}
	return true, schema, nil
}

func rawArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, false
	}
	return values, true
}

func valueMatchesEnum(value any, enumValues []json.RawMessage) bool {
	rawValue, err := json.Marshal(value)
	if err != nil {
		return false
	}
	rawValue = bytes.TrimSpace(rawValue)
	for _, enumValue := range enumValues {
		if bytes.Equal(rawValue, bytes.TrimSpace(enumValue)) {
			return true
		}
	}
	return false
}

func compactEnumValues(values []json.RawMessage) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(bytes.TrimSpace(value)))
	}
	return "[" + strings.Join(out, ", ") + "]"
}

func schemaInteger(raw json.RawMessage) (int, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return 0, false
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return 0, false
	}
	value, err := strconv.Atoi(number.String())
	return value, err == nil
}

func schemaNumber(raw json.RawMessage) (float64, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return 0, false
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return 0, false
	}
	value, err := number.Float64()
	return value, err == nil
}

func isJSONInteger(number json.Number) bool {
	text := number.String()
	if strings.ContainsAny(text, ".eE") {
		f, err := number.Float64()
		if err != nil {
			return false
		}
		return f == float64(int64(f))
	}
	_, err := strconv.ParseInt(text, 10, 64)
	return err == nil
}

func schemaStringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func trimFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
