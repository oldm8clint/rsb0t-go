package settings

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// This file is the lenient half of Bag's decoding, for a file a PROGRAM writes and a different
// build of the same program may read back: a script's own persisted progress (ScarabSwarm's stage
// checkpoint is the case it was written for), never a settings object an operator types.
//
// WHY IT EXISTS (C1 of private evidence notes 2026-09-23): Decode is strict, which
// is right for settings a person writes (a typo'd key must be an error, not a silent default), and
// wrong for a checkpoint. ScarabSwarm decoded its checkpoint with Decode and treated ANY error as
// "no checkpoint", so a file written by a newer build -- one that had added a single field, as the
// scarabprod branch does with `"prod"` -- failed to decode, the account started again at Stage 1,
// and the next save (within 15 s) overwrote the newer build's file with a Stage 1 one. A rollback,
// or a mixed-build deploy, would have reset every Stage 6 account for good.
//
// DecodeCompat decodes what the type knows and reports what it does not; EncodeCompat writes the
// type back and puts the unknown fields back where they were, so a file survives a round trip
// through an older build with nothing lost. A value of the WRONG TYPE is still an error: a field
// this build knows but cannot represent is not something it may silently zero and write back.

// Unknown is the part of a stored JSON object that a Go type does not model, remembered so that
// EncodeCompat can write it back unchanged. The zero value is "nothing unknown".
type Unknown struct {
	paths   []string
	dropped []string
	root    *unknownNode
}

// Paths is every unknown field's dotted path ("prod", "stage6.kills2", "list[3].x"), sorted.
func (u Unknown) Paths() []string { return append([]string(nil), u.paths...) }

// Dropped is the subset of Paths that EncodeCompat can NOT write back: an unknown field inside a
// list element, which has no identity that survives the list being rebuilt. Reported so a caller's
// log can say so rather than claim everything is kept.
func (u Unknown) Dropped() []string { return append([]string(nil), u.dropped...) }

// Empty is true when the stored object held nothing the type does not know.
func (u Unknown) Empty() bool { return len(u.paths) == 0 }

// unknownNode mirrors one JSON object level: values are the keys at this level the type does not
// know (kept as raw JSON, so nothing later can mutate them), kids are keys the type DOES know
// whose object value has unknown keys further down.
type unknownNode struct {
	values map[string]json.RawMessage
	kids   map[string]*unknownNode
}

func (n *unknownNode) ensure() *unknownNode {
	if n == nil {
		n = &unknownNode{}
	}
	if n.values == nil {
		n.values = map[string]json.RawMessage{}
	}
	if n.kids == nil {
		n.kids = map[string]*unknownNode{}
	}
	return n
}

// DecodeCompat decodes the Bag into v (a pointer), ignoring fields v's type does not have instead
// of failing on them, and returns those fields so EncodeCompat can keep them. Use it ONLY for a
// file a program writes (a checkpoint); operator-written settings keep Decode's strictness.
//
// It still fails on a value of the wrong type for a field the type knows (encoding/json's
// UnmarshalTypeError), and on anything else encoding/json refuses: such a file is not one this
// build can faithfully resume from, and the caller must treat it as unreadable, not as empty.
func (b *Bag) DecodeCompat(v interface{}) (Unknown, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return Unknown{}, fmt.Errorf("settings: decode: DecodeCompat needs a non-nil pointer, got %T", v)
	}
	b.mu.Lock()
	data, err := json.Marshal(b.values)
	b.mu.Unlock()
	if err != nil {
		return Unknown{}, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return Unknown{}, fmt.Errorf("settings: decode: %w", err)
	}
	// Walk a private copy of the values (re-decoded from the bytes just marshaled), so the
	// unknown values kept below can never alias a map a later Set/Encode changes.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Unknown{}, fmt.Errorf("settings: decode: %w", err)
	}
	var u Unknown
	u.root = findUnknown(raw, rv.Type().Elem(), "", &u)
	sort.Strings(u.paths)
	sort.Strings(u.dropped)
	return u, nil
}

// EncodeCompat is Encode, then every field keep holds is written back at its own path (unless v's
// own encoding already put something there). keep is what DecodeCompat returned for the same
// file; the zero Unknown makes this exactly Encode.
func (b *Bag) EncodeCompat(v interface{}, keep Unknown) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var values map[string]interface{}
	if err := json.Unmarshal(data, &values); err != nil {
		return fmt.Errorf("settings: encode: %w", err)
	}
	if values == nil {
		values = map[string]interface{}{}
	}
	keep.root.mergeInto(values)
	b.mu.Lock()
	b.values = values
	b.mu.Unlock()
	return nil
}

func (n *unknownNode) mergeInto(obj map[string]interface{}) {
	if n == nil || obj == nil {
		return
	}
	for k, raw := range n.values {
		if _, taken := obj[k]; taken {
			continue
		}
		var val interface{}
		if json.Unmarshal(raw, &val) == nil {
			obj[k] = val
		}
	}
	for k, kid := range n.kids {
		// A known object that this build's encoding left out (an omitempty map it emptied, a
		// nil pointer) takes its unknown children with it: re-creating the object just to hold
		// them would resurrect state this build deliberately cleared.
		if sub, ok := obj[k].(map[string]interface{}); ok {
			kid.mergeInto(sub)
		}
	}
}

var (
	jsonUnmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
)

// opaque is true for a type that decodes itself: its JSON shape is its own business, so nothing
// inside it can be "unknown" in the struct-field sense.
func opaque(t reflect.Type) bool {
	pt := reflect.PointerTo(t)
	return t.Implements(jsonUnmarshalerType) || pt.Implements(jsonUnmarshalerType) ||
		t.Implements(textUnmarshalerType) || pt.Implements(textUnmarshalerType)
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// findUnknown walks raw (a decoded JSON value) against t and records every object key t does not
// model. It returns the node to keep for this level, or nil when there is nothing to keep.
func findUnknown(raw interface{}, t reflect.Type, path string, u *Unknown) *unknownNode {
	return walkUnknown(raw, t, path, u, false)
}

// walkUnknown is findUnknown's recursion. inList is true under a list element, where an unknown
// key is reported (Paths, Dropped) but cannot be kept.
func walkUnknown(raw interface{}, t reflect.Type, path string, u *Unknown, inList bool) *unknownNode {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if opaque(t) {
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := raw.(map[string]interface{})
		if !ok {
			return nil
		}
		fields := jsonFields(t)
		var n *unknownNode
		for _, k := range sortedKeys(obj) {
			val := obj[k]
			p := joinPath(path, k)
			ft, known := lookupField(fields, k)
			if !known {
				u.paths = append(u.paths, p)
				if inList {
					u.dropped = append(u.dropped, p)
					continue
				}
				data, err := json.Marshal(val)
				if err != nil {
					u.dropped = append(u.dropped, p)
					continue
				}
				n = n.ensure()
				n.values[k] = data
				continue
			}
			if kid := walkUnknown(val, ft, p, u, inList); kid != nil {
				n = n.ensure()
				n.kids[k] = kid
			}
		}
		return n
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil
		}
		obj, ok := raw.(map[string]interface{})
		if !ok {
			return nil
		}
		var n *unknownNode
		for _, k := range sortedKeys(obj) {
			if kid := walkUnknown(obj[k], t.Elem(), joinPath(path, k), u, inList); kid != nil {
				n = n.ensure()
				n.kids[k] = kid
			}
		}
		return n
	case reflect.Slice, reflect.Array:
		arr, ok := raw.([]interface{})
		if !ok {
			return nil
		}
		for i, val := range arr {
			walkUnknown(val, t.Elem(), fmt.Sprintf("%s[%d]", path, i), u, true)
		}
		return nil
	}
	return nil
}

func sortedKeys(obj map[string]interface{}) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// jsonFields is the set of JSON object keys encoding/json maps onto t's fields, with each field's
// type: the json tag's name (or the Go field name), "-" skipped, unexported fields skipped, and an
// untagged embedded struct's fields promoted, as encoding/json does. An outer field shadows a
// promoted one of the same name.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	var promoted []map[string]reflect.Type
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			ft := f.Type
			for ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				promoted = append(promoted, jsonFields(ft))
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = f.Type
	}
	for _, p := range promoted {
		for k, v := range p {
			if _, ok := out[k]; !ok {
				out[k] = v
			}
		}
	}
	return out
}

// lookupField matches key the way encoding/json does: an exact name first, then a
// case-insensitive one.
func lookupField(fields map[string]reflect.Type, key string) (reflect.Type, bool) {
	if t, ok := fields[key]; ok {
		return t, true
	}
	for name, t := range fields {
		if strings.EqualFold(name, key) {
			return t, true
		}
	}
	return nil, false
}
