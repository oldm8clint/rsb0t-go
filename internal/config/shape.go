package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Why: json.Decoder.DisallowUnknownFields names an unknown key but not where it sits, matches keys
// case-insensitively, and lets null silently keep a default. This walk runs first so every
// problem carries its dotted path; the strict decoder stays behind it as the backstop.

var unmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()

// checkShape reports unknown keys, nulls and wrongly typed values in raw against t.
func checkShape(raw json.RawMessage, t reflect.Type, path string, r reporter) bool {
	ok := true
	walkShape(raw, t, path, r, &ok)
	return ok
}

func walkShape(raw json.RawMessage, t reflect.Type, path string, r reporter, ok *bool) {
	trimmed := bytes.TrimSpace(raw)
	if string(trimmed) == "null" {
		r.add(path, "null is not allowed; leave the key out to use the default")
		*ok = false
		return
	}
	if t.Kind() == reflect.Struct && !reflect.PtrTo(t).Implements(unmarshalerType) {
		obj, isObject := asObject(trimmed)
		if !isObject {
			r.add(path, "want an object, got %s", jsonKind(trimmed))
			*ok = false
			return
		}
		fields := jsonFields(t)
		for _, k := range sortedKeys(obj) {
			f, known := fields[k]
			if !known {
				r.add(joinPath(path, k), "%s", unknownField(k, fields))
				*ok = false
				continue
			}
			walkShape(obj[k], f.Type, joinPath(path, k), r, ok)
		}
		return
	}
	if t.Kind() == reflect.Map && t.Key().Kind() == reflect.String && t.Elem().Kind() == reflect.Struct && !reflect.PtrTo(t.Elem()).Implements(unmarshalerType) {
		obj, isObject := asObject(trimmed)
		if !isObject {
			r.add(path, "want an object, got %s", jsonKind(trimmed))
			*ok = false
			return
		}
		for _, k := range sortedKeys(obj) {
			walkShape(obj[k], t.Elem(), joinPath(path, k), r, ok)
		}
		return
	}
	target := reflect.New(t)
	if err := json.Unmarshal(trimmed, target.Interface()); err != nil {
		r.add(path, "%s", leafError(err, t))
		*ok = false
	}
}

// decodeSection shape-checks raw against v's type, then decodes it over v's current (default)
// values with unknown fields disallowed.
func decodeSection(raw json.RawMessage, v interface{}, r reporter) bool {
	if !checkShape(raw, reflect.TypeOf(v).Elem(), "", r) {
		return false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		r.add("", "%s", err.Error())
		return false
	}
	return true
}

func jsonFields(t reflect.Type) map[string]reflect.StructField {
	out := make(map[string]reflect.StructField, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		name := f.Name
		if tag, ok := f.Tag.Lookup("json"); ok {
			tagName, _, _ := strings.Cut(tag, ",")
			if tagName == "-" {
				continue
			}
			if tagName != "" {
				name = tagName
			}
		}
		out[name] = f
	}
	return out
}

func unknownField(key string, fields map[string]reflect.StructField) string {
	if strings.EqualFold(key, "password") || strings.EqualFold(key, "sharedPassword") {
		return "passwords never go in the settings file; use passwordEnv or login.credentialsFile"
	}
	for name := range fields {
		if strings.EqualFold(name, key) {
			return fmt.Sprintf("unknown field %q; did you mean %q?", key, name)
		}
	}
	return fmt.Sprintf("unknown field %q", key)
}

func leafError(err error, t reflect.Type) string {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		return fmt.Sprintf("want %s, got %s", describeType(t), te.Value)
	}
	return err.Error()
}

func describeType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "true or false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "a whole number"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "a whole number of at least 0"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.String:
		return "a string"
	case reflect.Slice:
		switch t.Elem().Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return "an array of whole numbers"
		}
		return "an array"
	case reflect.Map:
		return "an object"
	}
	return t.String()
}

func jsonKind(raw []byte) string {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return "nothing"
	}
	switch t[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	}
	return "number"
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func sortedKeys(obj map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// asObject decodes a JSON object one level deep. Empty input is an empty object, so an absent
// section behaves like {}.
func asObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return map[string]json.RawMessage{}, true
	}
	if t[0] != '{' {
		return nil, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(t, &m); err != nil {
		return nil, false
	}
	return m, true
}

func hasKey(raw json.RawMessage, key string) bool {
	obj, ok := asObject(raw)
	if !ok {
		return false
	}
	_, has := obj[key]
	return has
}

// mergeRaw overlays over onto base: objects merge key by key at every depth, anything else in over
// replaces base.
func mergeRaw(base, over json.RawMessage) json.RawMessage {
	tb, to := bytes.TrimSpace(base), bytes.TrimSpace(over)
	if len(to) == 0 {
		return base
	}
	if len(tb) == 0 {
		return over
	}
	if tb[0] != '{' || to[0] != '{' {
		return over
	}
	bo, baseIsObject := asObject(tb)
	oo, overIsObject := asObject(to)
	if !baseIsObject || !overIsObject {
		return over
	}
	for k, v := range oo {
		if bv, ok := bo[k]; ok {
			bo[k] = mergeRaw(bv, v)
		} else {
			bo[k] = v
		}
	}
	out, err := json.Marshal(bo)
	if err != nil {
		return over
	}
	return out
}

// mergeAccount overlays one account onto the defaults section.
// Why: settings keys belong to one script, so an account naming a different script drops them.
func mergeAccount(defaults, account json.RawMessage) json.RawMessage {
	d, _ := asObject(defaults)
	a, _ := asObject(account)
	if as := stringValue(a["script"]); as != "" && as != stringValue(d["script"]) {
		delete(d, "settings")
	}
	base, err := json.Marshal(d)
	if err != nil {
		base = defaults
	}
	return mergeRaw(base, account)
}

func stringValue(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func peekUsername(raw json.RawMessage) string {
	var a struct {
		Username string `json:"username"`
	}
	_ = json.Unmarshal(raw, &a)
	return a.Username
}
