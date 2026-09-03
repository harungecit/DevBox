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
	"fmt"
	"reflect"

	lua "github.com/yuin/gopher-lua"
)

// Marshal converts a Go value into a Lua value. Struct fields are named by
// their `json` tag (falling back to the Go name); slices become 1-indexed
// tables; Go functions become callable Lua functions.
func Marshal(state *lua.LState, v any) (lua.LValue, error) {
	reflected := reflect.ValueOf(v)
	if reflected.Kind() == reflect.Ptr {
		reflected = reflected.Elem()
	}

	if !reflected.IsValid() {
		return lua.LNil, nil
	}

	switch reflected.Kind() {
	case reflect.Struct:
		table := state.NewTable()
		for i := 0; i < reflected.NumField(); i++ {
			field := reflected.Field(i)
			if field.Kind() == reflect.Ptr {
				field = field.Elem()
			}

			fieldType := reflected.Type().Field(i)
			tag := fieldType.Tag.Get("json")
			if tag == "" {
				tag = fieldType.Name
			}

			if !field.IsValid() {
				continue
			}

			sub, err := Marshal(state, field.Interface())
			if err != nil {
				return nil, err
			}
			if lf, ok := sub.(*lua.LFunction); ok {
				state.SetField(table, tag, lf)
			} else {
				table.RawSetString(tag, sub)
			}

		}
		return table, nil
	case reflect.String:
		return lua.LString(reflected.String()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return lua.LNumber(reflected.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return lua.LNumber(reflected.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return lua.LNumber(reflected.Float()), nil
	case reflect.Bool:
		return lua.LBool(reflected.Bool()), nil
	case reflect.Array, reflect.Slice:
		table := state.NewTable()
		for i := 0; i < reflected.Len(); i++ {
			field := reflected.Index(i)
			if !field.IsValid() {
				continue
			}

			value, err := Marshal(state, field.Interface())
			if err != nil {
				return nil, err
			}
			table.RawSetInt(i+1, value)
		}
		return table, nil
	case reflect.Map:
		table := state.NewTable()
		for _, key := range reflected.MapKeys() {
			field := reflected.MapIndex(key)
			if !field.IsValid() {
				continue
			}

			value, err := Marshal(state, field.Interface())
			if err != nil {
				return nil, err
			}

			switch key.Kind() {
			case reflect.String:
				table.RawSetString(key.String(), value)
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				table.RawSetInt(int(key.Int()), value)
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				table.RawSetInt(int(key.Uint()), value)
			default:
				return nil, errors.New("marshal: unsupported type " + key.Kind().String() + " for key")
			}

		}
		return table, nil
	case reflect.Func:
		goFuncType := reflected.Type()
		// If it's already an LGFunction, use it directly
		if goFuncType.ConvertibleTo(reflect.TypeOf(lua.LGFunction(nil))) {
			lf := reflected.Convert(reflect.TypeOf(lua.LGFunction(nil))).Interface().(lua.LGFunction)
			return state.NewFunction(lf), nil
		}

		// Generic Go function wrapper. Plugins call context functions with
		// colon syntax (ctx:getInstalledVersions()), which pushes the table
		// itself as a leading argument — surplus leading arguments are
		// dropped so both call styles work.
		luaFunc := func(L *lua.LState) int {
			numIn := goFuncType.NumIn()
			actualNumArgs := L.GetTop()
			isVariadic := goFuncType.IsVariadic()

			expectedMinArgs := numIn
			if isVariadic {
				expectedMinArgs = numIn - 1
			}

			offset := 0
			if !isVariadic && actualNumArgs > numIn {
				offset = actualNumArgs - numIn
			}
			actualNumArgs -= offset

			if actualNumArgs < expectedMinArgs {
				L.RaiseError("expected at least %d arguments for %s, got %d", expectedMinArgs, goFuncType.String(), actualNumArgs)
				return 0
			}

			goArgs := make([]reflect.Value, numIn)
			for i := 0; i < numIn; i++ {
				goArgType := goFuncType.In(i)

				if isVariadic && i == numIn-1 {
					sliceElementType := goArgType.Elem()
					variadicLen := actualNumArgs - (numIn - 1)
					if variadicLen < 0 {
						variadicLen = 0
					}
					variadicSlice := reflect.MakeSlice(goArgType, variadicLen, variadicLen)
					for j := 0; j < variadicLen; j++ {
						luaVariadicArg := L.CheckAny(offset + i + 1 + j)
						elemPtr := reflect.New(sliceElementType)
						err := Unmarshal(luaVariadicArg, elemPtr.Interface())
						if err != nil {
							L.Push(lua.LNil)
							L.Push(lua.LString(fmt.Sprintf("error unmarshaling variadic argument %d (item %d): %s", i+1, j+1, err.Error())))
							return 2
						}
						variadicSlice.Index(j).Set(elemPtr.Elem())
					}
					goArgs[i] = variadicSlice
					break
				} else {
					luaArg := L.CheckAny(offset + i + 1)
					goArgPtr := reflect.New(goArgType)
					err := Unmarshal(luaArg, goArgPtr.Interface())
					if err != nil {
						L.Push(lua.LNil)
						L.Push(lua.LString(fmt.Sprintf("error unmarshaling argument %d: %s", i+1, err.Error())))
						return 2
					}
					goArgs[i] = goArgPtr.Elem()
				}
			}

			var results []reflect.Value
			if isVariadic {
				results = reflected.CallSlice(goArgs)
			} else {
				results = reflected.Call(goArgs)
			}

			if len(results) == 0 {
				return 0
			}

			for _, result := range results {
				luaResult, err := Marshal(L, result.Interface())
				if err != nil {
					L.Push(lua.LNil)
					L.Push(lua.LString(fmt.Sprintf("error marshaling result: %s", err.Error())))
					return 2
				}
				L.Push(luaResult)
			}
			return len(results)
		}
		return state.NewFunction(luaFunc), nil
	default:
		return nil, errors.New("marshal: unsupported type " + reflected.Kind().String())
	}
}

// MustMarshal is Marshal that panics on error (for values known to be encodable).
func MustMarshal(state *lua.LState, v interface{}) lua.LValue {
	value, err := Marshal(state, v)
	if err != nil {
		panic(err)
	}
	return value
}
