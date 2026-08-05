package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
)

// fileExists treats non-ENOENT stat errors as present so callers surface
// permission/safety failures by loading instead of silently creating defaults.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	return !errors.Is(err, os.ErrNotExist)
}

func writeJSON(w io.Writer, v any) error {
	v = normalizeJSONValue(v)
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

func normalizeJSONValue(v any) any {
	if v == nil {
		return v
	}
	rv := normalizeJSONReflect(reflect.ValueOf(v))
	if !rv.IsValid() {
		return v
	}
	return rv.Interface()
}

func normalizeJSONReflect(v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	switch v.Kind() {
	case reflect.Interface:
		return normalizeJSONInterface(v)
	case reflect.Pointer:
		return normalizeJSONPointer(v)
	case reflect.Struct:
		return normalizeJSONStruct(v)
	case reflect.Map:
		return normalizeJSONMap(v)
	case reflect.Slice:
		return normalizeJSONSlice(v)
	case reflect.Array:
		return normalizeJSONArray(v)
	default:
		return v
	}
}

func normalizeJSONInterface(v reflect.Value) reflect.Value {
	if v.IsNil() {
		return v
	}
	normalized := normalizeJSONReflect(v.Elem())
	if !normalized.Type().AssignableTo(v.Type()) {
		return v
	}
	out := reflect.New(v.Type()).Elem()
	out.Set(normalized)
	return out
}

func normalizeJSONPointer(v reflect.Value) reflect.Value {
	if v.IsNil() {
		return v
	}
	normalized := normalizeJSONReflect(v.Elem())
	out := reflect.New(v.Type().Elem())
	out.Elem().Set(normalized)
	return out
}

func normalizeJSONStruct(v reflect.Value) reflect.Value {
	for i := range v.NumField() {
		if v.Type().Field(i).PkgPath != "" {
			return v
		}
	}
	out := reflect.New(v.Type()).Elem()
	for i := range v.NumField() {
		out.Field(i).Set(normalizeJSONReflect(v.Field(i)))
	}
	return out
}

func normalizeJSONMap(v reflect.Value) reflect.Value {
	if v.IsNil() {
		return v
	}
	out := reflect.MakeMapWithSize(v.Type(), v.Len())
	for _, key := range v.MapKeys() {
		out.SetMapIndex(key, normalizeJSONReflect(v.MapIndex(key)))
	}
	return out
}

func normalizeJSONSlice(v reflect.Value) reflect.Value {
	if v.IsNil() {
		return reflect.MakeSlice(v.Type(), 0, 0)
	}
	out := reflect.MakeSlice(v.Type(), v.Len(), v.Cap())
	for i := range v.Len() {
		out.Index(i).Set(normalizeJSONReflect(v.Index(i)))
	}
	return out
}

func normalizeJSONArray(v reflect.Value) reflect.Value {
	out := reflect.New(v.Type()).Elem()
	for i := range v.Len() {
		out.Index(i).Set(normalizeJSONReflect(v.Index(i)))
	}
	return out
}
