/*
 *    Copyright 2026 Han Li and contributors
 *
 *    Licensed under the Apache License, Version 2.0 (the "License");
 *    you may not use this file except in compliance with the License.
 *    You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *    Unless required by applicable law or agreed to in writing, software
 *    distributed under the License is distributed on an "AS IS" BASIS,
 *    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *    See the License for the specific language governing permissions and
 *    limitations under the License.
 */

package codec

import (
	"errors"
	"reflect"
	"strconv"

	lua "github.com/yuin/gopher-lua"
)

// modified from https://cs.opensource.google/go/go/+/master:src/encoding/json/decode.go
// indirect walks down v allocating pointers as needed,
// until it gets to a non-pointer.
func indirect(v reflect.Value) reflect.Value {
	v0 := v
	haveAddr := false

	if v.Kind() != reflect.Pointer && v.Type().Name() != "" && v.CanAddr() {
		haveAddr = true
		v = v.Addr()
	}
	for {
		if v.Kind() == reflect.Interface && !v.IsNil() {
			e := v.Elem()
			if e.Kind() == reflect.Pointer && !e.IsNil() && (e.Elem().Kind() == reflect.Pointer) {
				haveAddr = false
				v = e
				continue
			}
		}

		if v.Kind() != reflect.Pointer {
			break
		}

		if v.Elem().Kind() == reflect.Interface && v.Elem().Elem() == v {
			v = v.Elem()
			break
		}
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}

		if haveAddr {
			v = v0
			haveAddr = false
		} else {
			v = v.Elem()
		}
	}
	return v
}

func storeLiteral(value reflect.Value, lvalue lua.LValue) {
	value = indirect(value)

	switch value.Kind() {
	case reflect.String:
		value.SetString(lvalue.String())
	case reflect.Bool:
		if b, ok := lvalue.(lua.LBool); ok {
			value.SetBool(bool(b))
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n, ok := lvalue.(lua.LNumber); ok {
			value.SetInt(int64(n))
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if n, ok := lvalue.(lua.LNumber); ok {
			value.SetUint(uint64(n))
		}
	case reflect.Float32, reflect.Float64:
		if n, ok := lvalue.(lua.LNumber); ok {
			value.SetFloat(float64(n))
		}
	}
}

func objectInterface(lvalue *lua.LTable) any {
	var v = make(map[string]any)
	lvalue.ForEach(func(key, value lua.LValue) {
		v[key.String()] = valueInterface(value)
	})
	return v
}

// valueInterface converts a Lua value into the generic Go shape:
// bool, float64, string, []any (array tables), map[string]any, or nil.
func valueInterface(lvalue lua.LValue) any {
	switch lvalue.Type() {
	case lua.LTTable:
		isArray := lvalue.(*lua.LTable).RawGetInt(1) != lua.LNil
		if isArray {
			return arrayInterface(lvalue.(*lua.LTable))
		}
		return objectInterface(lvalue.(*lua.LTable))
	case lua.LTString:
		return lvalue.String()
	case lua.LTNumber:
		return float64(lvalue.(lua.LNumber))
	case lua.LTBool:
		return bool(lvalue.(lua.LBool))
	}
	return nil
}

func arrayInterface(lvalue *lua.LTable) any {
	var v = make([]any, 0)
	lvalue.ForEach(func(key, value lua.LValue) {
		v = append(v, valueInterface(value))
	})

	return v
}

func unmarshalWorker(value lua.LValue, reflected reflect.Value) error {
	reflected = indirect(reflected)

	switch value.Type() {
	case lua.LTTable:

		switch reflected.Kind() {
		case reflect.Interface:
			if reflected.NumMethod() == 0 {
				result := valueInterface(value)
				reflected.Set(reflect.ValueOf(result))
			}
		case reflect.Map:
			t := reflected.Type()
			keyType := t.Key()
			switch keyType.Kind() {
			case reflect.String,
				reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			default:
				return errors.New("unmarshal: unsupported map key type " + keyType.String())
			}

			if reflected.IsNil() {
				reflected.Set(reflect.MakeMap(t))
			}

			var mapElem reflect.Value

			value.(*lua.LTable).ForEach(func(key, value lua.LValue) {
				var subv reflect.Value

				elemType := t.Elem()
				if !mapElem.IsValid() {
					mapElem = reflect.New(elemType).Elem()
				} else {
					mapElem.SetZero()
				}

				subv = mapElem

				unmarshalWorker(value, subv)

				var kv reflect.Value
				switch keyType.Kind() {
				case reflect.String:
					kv = reflect.New(keyType).Elem()
					kv.SetString(key.String())
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
					s := key.String()
					n, err := strconv.ParseInt(s, 10, 64)
					if err != nil {
						break
					}
					kv = reflect.New(keyType).Elem()
					kv.SetInt(n)
				case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
					s := key.String()
					n, err := strconv.ParseUint(s, 10, 64)
					if err != nil {
						break
					}
					kv = reflect.New(keyType).Elem()
					kv.SetUint(n)
				default:
					panic("unmarshal: Unexpected key type")
				}
				if kv.IsValid() {
					reflected.SetMapIndex(kv, subv)
				}

			})
		case reflect.Slice:
			i := 0

			value.(*lua.LTable).ForEach(func(key, value lua.LValue) {
				if i >= reflected.Cap() {
					reflected.Grow(1)
				}
				if i >= reflected.Len() {
					reflected.SetLen(i + 1)
				}
				if i < reflected.Len() {
					unmarshalWorker(value, reflected.Index(i))
				} else {
					unmarshalWorker(value, reflect.Value{})
				}
				i++
			})

			if i < reflected.Len() {
				reflected.SetLen(i)
			}

			if i == 0 {
				reflected.Set(reflect.MakeSlice(reflected.Type(), 0, 0))
			}
		case reflect.Struct:
			for i := 0; i < reflected.NumField(); i++ {
				f := reflected.Type().Field(i)
				if f.Anonymous && reflected.Field(i).Kind() == reflect.Ptr && reflected.Field(i).IsNil() {
					reflected.Field(i).Set(reflect.New(reflected.Field(i).Type().Elem()))
				}
			}

			(value.(*lua.LTable)).ForEach(func(key, value lua.LValue) {
				fieldName := key.String()

				field := findField(reflected, fieldName)

				if !field.IsValid() {
					return
				}

				unmarshalWorker(value, field)
			})
		}
	default:
		switch reflected.Kind() {
		case reflect.Interface:
			if reflected.NumMethod() == 0 {
				result := valueInterface(value)
				reflected.Set(reflect.ValueOf(result))
			}
		default:
			storeLiteral(reflected, value)
		}
	}
	return nil
}

// findField finds a field in the struct, including embedded fields recursively
func findField(reflected reflect.Value, fieldName string) reflect.Value {
	field := reflected.FieldByName(fieldName)
	if field.IsValid() {
		return field
	}

	t := reflected.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == fieldName {
			return reflected.Field(i)
		}
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous {
			embeddedValue := reflected.Field(i)
			if embeddedValue.Kind() == reflect.Ptr {
				if embeddedValue.IsNil() {
					embeddedValue.Set(reflect.New(embeddedValue.Type().Elem()))
				}
				embeddedValue = embeddedValue.Elem()
			} else if embeddedValue.Kind() == reflect.Struct {
				// ok
			} else {
				continue
			}
			subField := findField(embeddedValue, fieldName)
			if subField.IsValid() {
				return subField
			}
		}
	}
	return reflect.Value{}
}

// Unmarshal decodes a Lua value into v, which must be a non-nil pointer.
func Unmarshal(value lua.LValue, v any) error {
	reflected := reflect.ValueOf(v)

	if reflected.Kind() != reflect.Pointer || reflected.IsNil() {
		return errors.New("unmarshal: value must be a pointer")
	}

	return unmarshalWorker(value, reflected)
}
