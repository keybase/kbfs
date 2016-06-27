// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"bytes"
	"fmt"
	"reflect"

	"github.com/keybase/go-codec/codec"
)

// ext is a no-op extension that's useful for tagging interfaces with
// a type.  Note that it cannot be used for anything that has nested
// extensions.
type ext struct {
	// codec should NOT encode extension types
	codec Codec
}

// ConvertExt implements the codec.Ext interface for ext.
func (e ext) ConvertExt(v interface{}) interface{} {
	panic("ConvertExt not supported")
}

// UpdateExt implements the codec.Ext interface for ext.
func (e ext) UpdateExt(dest interface{}, v interface{}) {
	panic("UpdateExt not supported")
}

// WriteExt implements the codec.Ext interface for ext.
func (e ext) WriteExt(v interface{}) (buf []byte) {
	buf, err := e.codec.Encode(v)
	if err != nil {
		panic(fmt.Sprintf("Couldn't encode data in %v", v))
	}
	return buf
}

// ReadExt implements the codec.Ext interface for ext.
func (e ext) ReadExt(v interface{}, buf []byte) {
	err := e.codec.Decode(buf, v)
	if err != nil {
		panic(fmt.Sprintf("Couldn't decode data into %v", v))
	}
}

// extSlice is an extension that's useful for slices that contain
// extension types as elements.  The contained extension types cannot
// themselves contain nested extension types.
type extSlice struct {
	// codec SHOULD encode extension types
	codec Codec
	typer func(interface{}) reflect.Value
}

// ConvertExt implements the codec.Ext interface for extSlice.
func (es extSlice) ConvertExt(v interface{}) interface{} {
	panic("ConvertExt not supported")
}

// UpdateExt implements the codec.Ext interface for extSlice.
func (es extSlice) UpdateExt(dest interface{}, v interface{}) {
	panic("UpdateExt not supported")
}

// WriteExt implements the codec.Ext interface for extSlice.
func (es extSlice) WriteExt(v interface{}) (buf []byte) {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Slice {
		panic(fmt.Sprintf("Non-slice passed to extSlice.WriteExt %v",
			val.Kind()))
	}

	ifaceArray := make([]interface{}, val.Len())
	for i := 0; i < val.Len(); i++ {
		ifaceArray[i] = val.Index(i).Interface()
	}

	buf, err := es.codec.Encode(ifaceArray)
	if err != nil {
		panic(fmt.Sprintf("Couldn't encode data in %v", v))
	}
	return buf
}

// ReadExt implements the codec.Ext interface for extSlice.
func (es extSlice) ReadExt(v interface{}, buf []byte) {
	// ReadExt actually receives a pointer to the list
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr {
		panic(fmt.Sprintf("Non-pointer passed to extSlice.ReadExt: %v",
			val.Kind()))
	}

	val = val.Elem()
	if val.Kind() != reflect.Slice {
		panic(fmt.Sprintf("Non-slice passed to extSlice.ReadExt %v",
			val.Kind()))
	}

	var ifaceArray []interface{}
	err := es.codec.Decode(buf, &ifaceArray)
	if err != nil {
		panic(fmt.Sprintf("Couldn't decode data into %v", v))
	}

	if len(ifaceArray) > 0 {
		val.Set(reflect.MakeSlice(val.Type(), len(ifaceArray),
			len(ifaceArray)))
	}

	for i, v := range ifaceArray {
		if es.typer != nil {
			val.Index(i).Set(es.typer(v))
		} else {
			val.Index(i).Set(reflect.ValueOf(v))
		}
	}
}

// CodecMsgpack implements the Codec interface using msgpack
// marshaling and unmarshaling.
type CodecMsgpack struct {
	h        codec.Handle
	extCodec *CodecMsgpack
}

// NewCodecMsgpack constructs a new CodecMsgpack.
func NewCodecMsgpack() *CodecMsgpack {
	return newCodecMsgpackHelper(true)
}

// newCodecMsgpackHelper constructs a new CodecMsgpack that may or may
// not handle unknown fields.
func newCodecMsgpackHelper(handleUnknownFields bool) *CodecMsgpack {
	handle := codec.MsgpackHandle{}
	handle.Canonical = true
	handle.WriteExt = true
	handle.DecodeUnknownFields = handleUnknownFields
	handle.EncodeUnknownFields = handleUnknownFields

	// save a codec that doesn't write extensions, so that we can just
	// call Encode/Decode when we want to (de)serialize extension
	// types.
	handleNoExt := handle
	handleNoExt.WriteExt = false
	extCodec := &CodecMsgpack{&handleNoExt, nil}
	return &CodecMsgpack{&handle, extCodec}
}

// Decode implements the Codec interface for CodecMsgpack
func (c *CodecMsgpack) Decode(buf []byte, obj interface{}) (err error) {
	err = codec.NewDecoderBytes(buf, c.h).Decode(obj)
	return
}

// Encode implements the Codec interface for CodecMsgpack
func (c *CodecMsgpack) Encode(obj interface{}) (buf []byte, err error) {
	err = codec.NewEncoderBytes(&buf, c.h).Encode(obj)
	return
}

// RegisterType implements the Codec interface for CodecMsgpack
func (c *CodecMsgpack) RegisterType(rt reflect.Type, code extCode) {
	c.h.(*codec.MsgpackHandle).SetExt(rt, uint64(code), ext{c.extCodec})
}

// RegisterIfaceSliceType implements the Codec interface for CodecMsgpack
func (c *CodecMsgpack) RegisterIfaceSliceType(rt reflect.Type, code extCode,
	typer func(interface{}) reflect.Value) {
	c.h.(*codec.MsgpackHandle).SetExt(rt, uint64(code), extSlice{c, typer})
}

// CodecEqual returns whether or not the given objects serialize to
// the same byte string. x or y (or both) can be nil.
func CodecEqual(c Codec, x, y interface{}) (bool, error) {
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

// CodecUpdate encodes src into a byte string, and then decode it into
// dst.
func CodecUpdate(c Codec, dst interface{}, src interface{}) error {
	buf, err := c.Encode(src)
	if err != nil {
		return err
	}
	err = c.Decode(buf, dst)
	if err != nil {
		return err
	}
	return nil
}
