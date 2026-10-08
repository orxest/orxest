package api

import (
	"reflect"
	"time"
)

// normalizeJSON replaces nil slices and nil maps with empty ones, recursively.
//
// Go marshals a nil slice as `null` and a nil map as `null`, which forces every
// API client to defend against nulls for fields that are conceptually always
// collections (`labels`, `changed_files`, a board column's `tasks`, a task's
// `dependencies`, an execution's `result`, …). The public contract of Orxest is
// "collections are always arrays/objects", so the API normalises responses once,
// centrally, instead of relying on every constructor to initialise every slice.
//
// Because reflection can only set addressable values, the caller works on an
// addressable copy (see server.writeJSON).
func normalizeJSON(value reflect.Value) {
	if !value.IsValid() {
		return
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return
		}
		normalizeJSON(value.Elem())

	case reflect.Interface:
		if value.IsNil() {
			return
		}
		// The dynamic value is not addressable, so normalise a copy and store it
		// back when the interface itself can be set.
		elem := value.Elem()
		copyValue := reflect.New(elem.Type()).Elem()
		copyValue.Set(elem)
		normalizeJSON(copyValue)
		if value.CanSet() {
			value.Set(copyValue)
		}

	case reflect.Slice:
		if value.IsNil() {
			if value.CanSet() {
				value.Set(reflect.MakeSlice(value.Type(), 0, 0))
			}
			return
		}
		for i := 0; i < value.Len(); i++ {
			normalizeJSON(value.Index(i))
		}

	case reflect.Map:
		if value.IsNil() {
			if value.CanSet() {
				value.Set(reflect.MakeMap(value.Type()))
			}
			return
		}
		iter := value.MapRange()
		for iter.Next() {
			entry := reflect.New(value.Type().Elem()).Elem()
			entry.Set(iter.Value())
			normalizeJSON(entry)
			if value.CanSet() {
				value.SetMapIndex(iter.Key(), entry)
			}
		}

	case reflect.Struct:
		// time.Time must be left alone: it is opaque and has no JSON-tagged
		// collection fields.
		if value.Type() == reflect.TypeOf(time.Time{}) {
			return
		}
		for i := 0; i < value.NumField(); i++ {
			field := value.Field(i)
			if !field.CanSet() {
				continue
			}
			normalizeJSON(field)
		}
	}
}

// normalizedValue returns an addressable copy of v with nil collections replaced
// by empty ones.
func normalizedValue(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer && rv.Kind() != reflect.Slice && rv.Kind() != reflect.Map && rv.Kind() != reflect.Struct && rv.Kind() != reflect.Interface {
		return v
	}
	copyValue := reflect.New(rv.Type())
	copyValue.Elem().Set(rv)
	normalizeJSON(copyValue.Elem())
	return copyValue.Elem().Interface()
}
