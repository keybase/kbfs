// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import "github.com/keybase/go-codec/codec"

// IndirectDirPtr pairs an indirect dir block with the start of that
// block's range of directory entries (inclusive)
type IFCERFTIndirectDirPtr struct {
	// TODO: Make sure that the block is not dirty when the EncodedSize
	// field is non-zero.
	IFCERFTBlockInfo
	Off string `codec:"o"`

	codec.UnknownFieldSetHandler
}

// IndirectFilePtr pairs an indirect file block with the start of that
// block's range of bytes (inclusive)
type IFCERFTIndirectFilePtr struct {
	// When the EncodedSize field is non-zero, the block must not
	// be dirty.
	IFCERFTBlockInfo
	Off int64 `codec:"o"`
	// Marker for files with holes
	Holes bool `codec:"h,omitempty"`

	codec.UnknownFieldSetHandler
}

// CommonBlock holds block data that is common for both subdirectories
// and files.
type IFCERFTCommonBlock struct {
	// IsInd indicates where this block is so big it requires indirect pointers
	IsInd bool `codec:"s"`

	codec.UnknownFieldSetHandler

	// cachedEncodedSize is the locally-cached (non-serialized)
	// encoded size for this block.
	cachedEncodedSize uint32
}

// GetEncodedSize implements the Block interface for CommonBlock
func (cb IFCERFTCommonBlock) GetEncodedSize() uint32 {
	return cb.cachedEncodedSize
}

// SetEncodedSize implements the Block interface for CommonBlock
func (cb *IFCERFTCommonBlock) SetEncodedSize(size uint32) {
	cb.cachedEncodedSize = size
}

// DataVersion returns data version for this block.
func (cb *IFCERFTCommonBlock) DataVersion() IFCERFTDataVer {
	return IFCERFTFirstValidDataVer
}

// NewCommonBlock returns a generic block, unsuitable for caching.
func IFCERFTNewCommonBlock() IFCERFTBlock {
	return &IFCERFTCommonBlock{}
}

// DirBlock is the contents of a directory
type IFCERFTDirBlock struct {
	IFCERFTCommonBlock
	// if not indirect, a map of path name to directory entry
	Children map[string]IFCERFTDirEntry `codec:"c,omitempty"`
	// if indirect, contains the indirect pointers to the next level of blocks
	IPtrs []IFCERFTIndirectDirPtr `codec:"i,omitempty"`
}

// NewDirBlock creates a new, empty DirBlock.
func IFCERFTNewDirBlock() IFCERFTBlock {
	return &IFCERFTDirBlock{
		Children: make(map[string]IFCERFTDirEntry),
	}
}

// DeepCopy makes a complete copy of a DirBlock
func (db IFCERFTDirBlock) DeepCopy(codec IFCERFTCodec) (*IFCERFTDirBlock, error) {
	var dirBlockCopy IFCERFTDirBlock
	err := CodecUpdate(codec, &dirBlockCopy, db)
	if err != nil {
		return nil, err
	}
	if dirBlockCopy.Children == nil {
		dirBlockCopy.Children = make(map[string]IFCERFTDirEntry)
	}
	return &dirBlockCopy, nil
}

// FileBlock is the contents of a file
type IFCERFTFileBlock struct {
	IFCERFTCommonBlock
	// if not indirect, the full contents of this block
	Contents []byte `codec:"c,omitempty"`
	// if indirect, contains the indirect pointers to the next level of blocks
	IPtrs []IFCERFTIndirectFilePtr `codec:"i,omitempty"`

	// this is used for caching plaintext (block.Contents) hash. It is used by
	// only direct blocks.
	hash *IFCERFTRawDefaultHash
}

// NewFileBlock creates a new, empty FileBlock.
func IFCERFTNewFileBlock() IFCERFTBlock {
	return &IFCERFTFileBlock{
		Contents: make([]byte, 0, 0),
	}
}

// DataVersion returns data version for this block.
func (fb *IFCERFTFileBlock) DataVersion() IFCERFTDataVer {
	for i := range fb.IPtrs {
		if fb.IPtrs[i].Holes {
			return IFCERFTFilesWithHolesDataVer
		}
	}
	return IFCERFTFirstValidDataVer
}

// DeepCopy makes a complete copy of a FileBlock
func (fb IFCERFTFileBlock) DeepCopy(codec IFCERFTCodec) (*IFCERFTFileBlock, error) {
	var fileBlockCopy IFCERFTFileBlock
	err := CodecUpdate(codec, &fileBlockCopy, fb)
	if err != nil {
		return nil, err
	}
	return &fileBlockCopy, nil
}

// DefaultNewBlockDataVersion returns the default data version for new blocks.
func IFCERFTDefaultNewBlockDataVersion(c IFCERFTConfig, holes bool) IFCERFTDataVer {
	if holes {
		return IFCERFTFilesWithHolesDataVer
	}
	return IFCERFTFirstValidDataVer
}
