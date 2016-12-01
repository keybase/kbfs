// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package kbfscodec

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
)

// ExtCode is used to register codec extensions
type ExtCode uint64

// these track the start of a range of unique ExtCodes for various
// types of extensions.
const (
	ExtCodeOpsRangeStart  = 1
	ExtCodeListRangeStart = 101
)

// Codec encodes and decodes arbitrary data
type Codec interface {
	// Decode unmarshals the given buffer into the given object, if possible.
	Decode(buf []byte, obj interface{}) error
	// Encode marshals the given object (which must not be typed
	// or untyped nil) into a returned buffer.
	Encode(obj interface{}) ([]byte, error)
	// RegisterType should be called for all types that are stored
	// under ambiguous types (like interface{} or nil interface) in a
	// struct that will be encoded/decoded by the codec.  Each must
	// have a unique ExtCode.  Types that include other extension
	// types are not supported.
	RegisterType(rt reflect.Type, code ExtCode)
	// RegisterIfaceSliceType should be called for all encoded slices
	// that contain ambiguous interface types.  Each must have a
	// unique ExtCode.  Slice element types that include other
	// extension types are not supported.
	//
	// If non-nil, typer is used to do a type assertion during
	// decoding, to convert the encoded value into the value expected
	// by the rest of the code.  This is needed, for example, when the
	// codec cannot decode interface types to their desired pointer
	// form.
	RegisterIfaceSliceType(rt reflect.Type, code ExtCode,
		typer func(interface{}) reflect.Value)
}

func isTypedNil(obj interface{}) bool {
	v := reflect.ValueOf(obj)
	// This switch is needed because IsNil() panics for other
	// kinds.
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
	}
	return false
}

func isNil(obj interface{}) bool {
	return obj == nil || isTypedNil(obj)
}

// Equal returns whether or not the given objects, if both non-nil,
// serialize to the same byte string. If either x or y is (typed or
// untyped) nil, then Equal returns true if the other one is (typed or
// untyped) nil, too.
func Equal(c Codec, x, y interface{}) (bool, error) {
	if isNil(x) {
		return isNil(y), nil
	}
	if isNil(y) {
		return false, nil
	}

	xBuf, err := c.Encode(x)
	if err != nil {
		return false, err
	}
	yBuf, err := c.Encode(y)
	if err != nil {
		return false, err
	}
	return bytes.Equal(xBuf, yBuf), nil
}

// Update encodes src into a byte string, and then decode it into the
// object pointed to by dstPtr.
func Update(c Codec, dstPtr interface{}, src interface{}) error {
	buf, err := c.Encode(src)
	if err != nil {
		return err
	}
	err = c.Decode(buf, dstPtr)
	if err != nil {
		return err
	}
	return nil
}

// SerializeToFile serializes the given object and writes it to the
// given file, making its parent directory first if necessary.
func SerializeToFile(c Codec, obj interface{}, path string) error {
	err := os.MkdirAll(filepath.Dir(path), 0700)
	if err != nil {
		return err
	}

	buf, err := c.Encode(obj)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(path, buf, 0600)
	if err != nil {
		return err
	}

	return nil
}

// DeserializeFromFile deserializes the given file into the object
// pointed to by objPtr.
func DeserializeFromFile(c Codec, path string, objPtr interface{}) error {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	err = c.Decode(data, objPtr)
	if err != nil {
		return err
	}

	return nil
}
