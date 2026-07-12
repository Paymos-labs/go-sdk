package paymos

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
)

func StringToSign(timestamp, method, path, query string, body []byte) string {
	bodyHash := ""
	if len(body) > 0 {
		sum := sha256.Sum256(body)
		bodyHash = hex.EncodeToString(sum[:])
	}
	return timestamp + "\n" + strings.ToUpper(method) + "\n" + path + "\n" + query + "\n" + bodyHash
}

func Sign(secret, value string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(value))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func AuthorizationHeader(apiKey, apiSecret, timestamp, method, path, query string, body []byte) string {
	return "HMAC-SHA256 " + apiKey + ":" + Sign(apiSecret, StringToSign(timestamp, method, path, query, body))
}

func EncodePathSegment(value string) string { return url.PathEscape(value) }

func BuildQuery(filters any) (string, error) {
	normalized, err := queryFields(filters)
	if err != nil {
		return "", err
	}
	filters = normalized
	filterMap := filters.(map[string]any)
	keys := make([]string, 0, len(filterMap))
	for key := range filterMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0)
	for _, key := range keys {
		if filterMap[key] == nil {
			continue
		}
		values, err := queryValues(filterMap[key])
		if err != nil {
			return "", fmt.Errorf("invalid Paymos list filter %s: %w", key, err)
		}
		if len(values) == 0 {
			return "", fmt.Errorf("Paymos list filter cannot be empty: %s", key)
		}
		sort.Strings(values)
		for _, value := range values {
			if value == "" {
				return "", fmt.Errorf("Paymos list filter cannot be empty: %s", key)
			}
			parts = append(parts, rfc3986(key)+"="+rfc3986(value))
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "?" + strings.Join(parts, "&"), nil
}

func queryFields(filters any) (map[string]any, error) {
	if filters == nil {
		return map[string]any{}, nil
	}
	if values, ok := filters.(map[string]any); ok {
		return values, nil
	}
	value := reflect.ValueOf(filters)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return map[string]any{}, nil
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil, fmt.Errorf("Paymos list filters must be a struct or map, got %T", filters)
	}
	result := make(map[string]any)
	typeInfo := value.Type()
	for index := 0; index < value.NumField(); index++ {
		fieldInfo := typeInfo.Field(index)
		if !fieldInfo.IsExported() {
			continue
		}
		name, options, _ := strings.Cut(fieldInfo.Tag.Get("json"), ",")
		if name == "" {
			name = fieldInfo.Name
		}
		if name == "-" || (strings.Contains(options, "omitempty") && value.Field(index).IsZero()) {
			continue
		}
		result[name] = value.Field(index).Interface()
	}
	return result, nil
}

func queryValues(value any) ([]string, error) {
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Pointer {
		if reflected.IsNil() {
			return nil, nil
		}
		return queryValues(reflected.Elem().Interface())
	}
	switch reflected.Kind() {
	case reflect.String:
		return []string{reflected.String()}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return []string{fmt.Sprint(reflected.Int())}, nil
	case reflect.Slice, reflect.Array:
		values := make([]string, reflected.Len())
		for index := 0; index < reflected.Len(); index++ {
			item, err := queryValues(reflected.Index(index).Interface())
			if err != nil || len(item) != 1 {
				return nil, fmt.Errorf("unsupported type %T", value)
			}
			values[index] = item[0]
		}
		return values, nil
	}
	return nil, fmt.Errorf("unsupported type %T", value)
}

func rfc3986(value string) string { return strings.ReplaceAll(url.QueryEscape(value), "+", "%20") }
