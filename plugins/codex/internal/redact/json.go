package redact

import (
	"encoding/json"
	"strings"
)

func (r *Redactor) RedactJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var v any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&v); err == nil {
		data, _ := json.Marshal(r.redactJSONValue("", v))
		return data
	}
	data, _ := json.Marshal(r.Redact(string(raw)))
	return data
}

func (r *Redactor) RedactJSONForText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return r.Redact(s)
	}
	return string(r.RedactJSON(raw))
}

func (r *Redactor) redactJSONValue(key string, v any) any {
	if isSensitiveJSONKey(key) && !isEmptyJSONValue(v) {
		return "[REDACTED:json-secret-field]"
	}
	switch x := v.(type) {
	case string:
		return r.Redact(x)
	case []any:
		for i := range x {
			x[i] = r.redactJSONValue("", x[i])
		}
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[r.Redact(k)] = r.redactJSONValue(k, v)
		}
		return out
	default:
		return x
	}
}

func isSensitiveJSONKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	k = strings.ReplaceAll(k, "-", "_")
	switch k {
	case "authorization", "proxy_authorization", "cookie", "set_cookie":
		return true
	}
	parts := strings.FieldsFunc(k, func(r rune) bool {
		return r == '_' || r == '.' || r == ' '
	})
	for _, p := range parts {
		switch p {
		case "password", "passwd", "pwd", "secret", "token", "credential":
			return true
		}
	}
	compact := strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(k)
	for _, needle := range []string{
		"password",
		"passwd",
		"secret",
		"token",
		"credential",
		"apikey",
		"privatekey",
		"accesskey",
		"clientsecret",
		"authtoken",
		"secretkey",
	} {
		if strings.Contains(compact, needle) {
			return true
		}
	}
	return false
}

func isEmptyJSONValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	default:
		return false
	}
}
