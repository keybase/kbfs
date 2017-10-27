package plumbing

import (
	"io"
	"io/ioutil"
	"os"
)

// FileObject on memory Object implementation
type FileObject struct {
	t    ObjectType
	h    Hash
	path string
	sz   int64
}

func NewFileObject(path string) *FileObject {
	return &FileObject{path: path}
}

// Hash return the object Hash, the hash is calculated on-the-fly the first
// time is called, the subsequent calls the same Hash is returned even if the
// type or the content has changed. The Hash is only generated if the size of
// the content is exactly the Object.Size
func (o *FileObject) Hash() Hash {
	if o.h == ZeroHash {
		buf, err := ioutil.ReadFile(o.path)
		if err != nil {
			return o.h
		}
		o.h = ComputeHash(o.t, buf)
	}

	return o.h
}

// Type return the ObjectType
func (o *FileObject) Type() ObjectType { return o.t }

// SetType sets the ObjectType
func (o *FileObject) SetType(t ObjectType) { o.t = t }

// Size return the size of the object
func (o *FileObject) Size() int64 { return o.sz }

// SetSize set the object size, a content of the given size should be written
// afterwards
func (o *FileObject) SetSize(s int64) { o.sz = s }

// Reader returns a ObjectReader used to read the object's content.
func (o *FileObject) Reader() (io.ReadCloser, error) {
	return os.Open(o.path)
}

// Writer returns a ObjectWriter used to write the object's content.
func (o *FileObject) Writer() (io.WriteCloser, error) {
	return os.Create(o.path)
}
