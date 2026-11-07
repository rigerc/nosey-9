// Package reflectutil isolates the reflection used by go-clack's generic
// workflow and required-value APIs. It is the single package allowed to
// import reflect in this repository's anti-slop configuration.
package reflectutil

import (
	"errors"
	"fmt"
	"reflect"
)

// RequiredMissing reports whether a required value is empty. Booleans are
// always present, matching go-clack's original behavior.
// CONTRACT: WrapValidate calls this with its generic TValue.
func RequiredMissing(value any) bool {
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.Bool:
		return false
	case reflect.Slice, reflect.Array, reflect.Map:
		return v.Len() == 0 || v.IsZero()
	default:
		return v.IsZero()
	}
}

// AssignField stores value in the struct field of result named by fieldName.
// result must be a non-nil pointer to a struct, and the field must be settable
// and assignable from value.
// CONTRACT: WorkflowBuilder accepts heterogeneous Step results.
func AssignField(result any, fieldName string, value any) error {
	v := reflect.ValueOf(result)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return errors.New("workflow error: result must be a pointer to a struct")
	}

	field := v.Elem().FieldByName(fieldName)
	if !field.IsValid() {
		return fmt.Errorf("workflow error: invalid field `%s`", fieldName)
	}
	if !field.CanSet() {
		return fmt.Errorf("workflow error: cannot set field `%s`", fieldName)
	}

	if value == nil {
		switch field.Kind() {
		case reflect.Interface, reflect.Slice, reflect.Pointer, reflect.Map, reflect.Func:
			field.SetZero()
			return nil
		default:
			return fmt.Errorf("workflow error: nil cannot be assigned to field `%s`", fieldName)
		}
	}

	resolved := reflect.ValueOf(value)
	if !resolved.Type().AssignableTo(field.Type()) {
		return fmt.Errorf("workflow error: type `%s` is not assignable to field `%s`", resolved.Type(), fieldName)
	}
	field.Set(resolved)
	return nil
}
