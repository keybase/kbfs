// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libfuse

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"time"
)

// finderInfoExtendedAttribute is for extended attributes for the Finder
// on macOS/darwin. It is here as gzipped bytes so we can embed in the
// go binary. For more details on extended attributes for the Finder,
// see http://gotofritz.net/blog/geekery/os-x-extended-attibutes/.
var finderInfoExtendedAttribute = []byte("\x1f\x8b\x08\x00\x00\x09\x6e\x88\x00\xff\x62\x60\x15\x63\x67\x60\x62\x60\xf0\x4d\x4c\x56\xf0\x0f\x56\x88\x50\x80\x02\x90\x18\x03\x27\x10\x1b\x31\x30\xf0\x6d\x00\xd2\x40\x3e\xdf\x23\x06\x06\x46\x39\x06\x28\x60\x61\xc0\x05\x1c\x43\x42\x82\x5e\xef\x7d\xa9\x0f\xd1\xc1\x50\x81\x53\xe1\x28\x18\x05\xa3\x60\x14\x8c\x82\x51\x30\x0a\x46\xc1\x28\x18\x05\xa3\x60\x14\x8c\x82\x51\x40\x45\xc0\x08\xc5\x60\x20\x17\x92\x91\x59\xac\x50\x94\x5a\x9c\x5f\x5a\x94\x9c\xaa\x90\x96\x5f\x94\xad\x90\x99\x57\x92\x9a\x57\x92\x99\x9f\x97\x98\x93\x53\xa9\x90\x93\x9a\x56\xa2\x90\x94\x93\x98\x97\x0d\xea\x07\x0f\x03\x80\xea\x7f\xb8\xb0\x0c\x83\xdc\xff\xff\x80\x00\x00\x00\xff\xff\x37\x65\x97\xf3\x00\x10\x00\x00")

var finderInfoExtendedAttributeRaw []byte

var finderInfoFileDate = time.Now()

func finderInfoExtendedAttributeBytes() ([]byte, error) {
	if finderInfoExtendedAttributeRaw == nil {
		var err error
		finderInfoExtendedAttributeRaw, err = readBinData(finderInfoExtendedAttribute)
		if err != nil {
			return nil, err
		}
	}
	return finderInfoExtendedAttributeRaw, nil
}

func newFinderInfoExtendedAttributeFile() (*SpecialReadFile, error) {
	data, err := finderInfoExtendedAttributeBytes()
	if err != nil {
		return nil, err
	}
	return newByteFile(data, finderInfoFileDate), nil
}

func readBinData(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("Read %v", err)
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, gz)
	clErr := gz.Close()

	if err != nil {
		return nil, fmt.Errorf("Read %v", err)
	}
	if clErr != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
