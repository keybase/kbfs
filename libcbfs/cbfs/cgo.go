// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build windows

package cbfs

/*
#include "binding.h"
#cgo CFLAGS: -I.
#cgo CXXFLAGS: -I. -std=c++14
#cgo LDFLAGS: -L. -lcbfs -lsetupapi -lversion -lnetapi32 -lole32 -lcomctl32
*/
import "C"

import (
	"fmt"
	"io/ioutil"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/net/context"
)

var initDone bool
var initDoneLock sync.Mutex

type Config struct {
	Path       string
	FileSystem FileSystem
}

type FileSystem interface {
	VolumeLabel(context.Context) string
	VolumeSize(context.Context) (totalBytes, availableBytes int64, err error)
	VolumeId(context.Context) uint32

	CreateFile(ctx context.Context, oc *OpenContext) (File, error)
	GetFileInfo(ctx context.Context, filename string) (*Stat, error)
	Rename(ctx context.Context, srcPath string, newPath string) error
	WithContext(ctx context.Context) (context.Context, context.CancelFunc)
}

type File interface {
	// ReadFile implements read for dokan.
	ReadFile(ctx context.Context, bs []byte, offset int64) (int, error)
	// WriteFile implements write for dokan.
	WriteFile(ctx context.Context, bs []byte, offset int64) (int, error)

	FindFiles(context.Context, string, func(*NamedStat) error) error

	CloseFile(context.Context)

	// SetEndOfFile truncates the file. May be used to extend a file with zeros.
	SetEndOfFile(ctx context.Context, length int64) error
	// SetAllocationSize see FILE_ALLOCATION_INFORMATION on MSDN.
	// For simple semantics if length > filesize then ignore else truncate(length).
	SetAllocationSize(ctx context.Context, length int64) error

	SetFileAttributes(context.Context, *Stat) error

	CanDelete(context.Context) error
	Delete(context.Context) error

	IsDirectoryEmpty(context.Context) (bool, error)
}

type OpenContext struct {
	FileName                                 string
	DesiredAccess, FileAttributes, ShareMode uint32
	CreateFile                               bool
}

func (oc *OpenContext) IsDirectory() bool {
	return (oc.FileAttributes & FileAttributeDirectory) == FileAttributeDirectory
}

type FileInfo struct{}

// Stat is for GetFileInformation and friends.
type Stat struct {
	// Timestamps for the file
	Creation, LastAccess, LastWrite time.Time
	// FileSize is the size of the file in bytes
	FileSize int64
	// FileIndex is a 64 bit (nearly) unique ID of the file
	FileIndex uint64
	// FileAttributes bitmask holds the file attributes.
	FileAttributes FileAttribute
	// VolumeSerialNumber is the serial number of the volume (0 is fine)
	VolumeSerialNumber uint32
	// NumberOfLinks can be omitted, if zero set to 1.
	NumberOfLinks uint32
	// ReparsePointTag is for WIN32_FIND_DATA dwReserved0 for reparse point tags, typically it can be omitted.
	ReparsePointTag uint32
}

// NamedStat is used to for stat responses that require file names.
// If the name is longer than a DOS-name, insert the corresponding
// DOS-name to ShortName.
type NamedStat struct {
	Name      string
	ShortName string
	Stat
}

type FileAttribute uint32

const (
	FileAttributeNormal       = syscall.FILE_ATTRIBUTE_NORMAL
	FileAttributeDirectory    = syscall.FILE_ATTRIBUTE_DIRECTORY
	FileAttributeReparsePoint = syscall.FILE_ATTRIBUTE_REPARSE_POINT
	FileAttributeReadonly     = syscall.FILE_ATTRIBUTE_READONLY
)

var (
	ErrFileNotFound      = CBFSError{int(syscall.ERROR_FILE_NOT_FOUND), ""}
	ErrAccessDenied      = CBFSError{int(syscall.ERROR_ACCESS_DENIED), ""}
	ErrFileAlreadyExists = CBFSError{int(syscall.ERROR_ALREADY_EXISTS), ""}
	ErrNotSupported      = ErrAccessDenied
	ErrDirectoryNotEmpty = ErrAccessDenied
	ErrNotSameDevice     = ErrAccessDenied
)

const (
	CBFS_MODULE_DRIVER = 2
)

type MountHandle struct {
	fs      *C.CallbackFileSystem
	product string
	ec      chan error
}

func (mh *MountHandle) Close() error {
	return nil
}
func (mh *MountHandle) BlockTillDone() error {
	return <-mh.ec
}

func Mount(cfg *Config) (*MountHandle, error) {
	initDoneLock.Lock()
	defer initDoneLock.Unlock()

	var fs MountHandle
	fs.ec = make(chan error)
	idx := fsTableStore(cfg.FileSystem, fs.ec)
	fs.fs = C.keybase_Cbfs_New_CallbackFileSystem(
		C.uint32_t(idx), &maxFileNameLength)
	fs.product = "kbfscbfs"

	debug(fs.GetModuleStatus(CBFS_MODULE_DRIVER))

	var key string
	if !initDone {
		bs, err := ioutil.ReadFile("cbfs.key.txt")
		if err != nil {
			return nil, err
		}
		key = strings.TrimSpace(string(bs))
	}
	err := fs.initialize(key)
	debug(fs.GetModuleStatus(CBFS_MODULE_DRIVER))
	if err != nil {
		return nil, err
	}
	initDone = true
	err = fs.mountMedia(30 * time.Second)
	if err != nil {
		return nil, err
	}
	debug("Mounting fs to: ", cfg.Path)
	err = fs.addMountingPoint(cfg.Path, CBFS_SYMLINK_MOUNT_MANAGER|CBFS_SYMLINK_LOCAL)
	if err != nil {
		return nil, err
	}

	return &fs, nil
}

func Install(productname, cbfscabpath string) error {
	var w C.keybase_Cbfs_wrap
	prod := string2bytes0(productname)

	C.keybase_cbfs_install(&w,
		C.LPCSTR(unsafe.Pointer(&prod[0])),
		C.LPWSTR(stringToUtf16Ptr(cbfscabpath)),
	)
	return werr(&w)
}

func (fs *MountHandle) initialize(key string) error {
	var w C.keybase_Cbfs_wrap
	w.fs = fs.fs
	appb := string2bytes0(fs.product)
	var codePtr unsafe.Pointer
	var code []byte
	if key != "" {
		code = string2bytes0(key)
		codePtr = unsafe.Pointer(&code[0])
	}
	C.keybase_Cbfs_Initialize(&w,
		(*C.CHAR)(unsafe.Pointer(&appb[0])),
		(*C.CHAR)(codePtr))
	runtime.KeepAlive(appb)
	runtime.KeepAlive(code)
	return werr(&w)
}

func (fs *MountHandle) mountMedia(timeout time.Duration) error {
	var w C.keybase_Cbfs_wrap
	w.fs = fs.fs
	C.keybase_cbfs_mountmedia(&w, C.int(timeout.Nanoseconds()/1000000))
	return werr(&w)
}

func (fs *MountHandle) addMountingPoint(path string, flags MountFlags) error {
	var w C.keybase_Cbfs_wrap
	w.fs = fs.fs
	C.keybase_cbfs_addmountingpoint(&w,
		C.LPWSTR(stringToUtf16Ptr(path)),
		C.DWORD(flags))
	return werr(&w)
}

func (fs *MountHandle) Unmount() error {
	var w C.keybase_Cbfs_wrap
	w.fs = fs.fs
	C.keybase_cbfs_deletestorage(&w, 1)
	select {
	case fs.ec <- nil:
	default:
	}
	C.keybase_Cbfs_Delete_CallbackFileSystem(fs.fs)
	fs.fs = nil
	return werr(&w)
}

type MountFlags uint32

const (
	CBFS_SYMLINK_SIMPLE        = MountFlags(0x00010000)
	CBFS_SYMLINK_MOUNT_MANAGER = MountFlags(0x00020000)
	CBFS_SYMLINK_NETWORK       = MountFlags(0x00040000)
	CBFS_SYMLINK_LOCAL         = MountFlags(0x10000000)
)

type ModuleStatus struct {
	Installed               bool
	VersionHigh, VersionLow int
	ServiceStatus           C.SERVICE_STATUS
}

func (fs *MountHandle) GetModuleStatus(module int) ModuleStatus {
	var installed C.BOOL
	var verh, verl C.INT
	var status C.SERVICE_STATUS
	prod := string2bytes0(fs.product)
	C.keybase_cbfs_getmodulestatus(
		(*C.char)(unsafe.Pointer(&prod[0])),
		C.INT(module),
		&installed,
		&verh,
		&verl,
		&status,
	)
	return ModuleStatus{installed != 0, int(verh), int(verl), status}
}

func string2bytes0(s string) []byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return b
}

//export keybase_cgo_Cbfs_Mount
func keybase_cgo_Cbfs_Mount(w *C.keybase_Cbfs_wrap) {
	debug("OnMount")
}

//export keybase_cgo_Cbfs_Unmount
func keybase_cgo_Cbfs_Unmount(w *C.keybase_Cbfs_wrap) {
	debug("OnUnmount")
}

//export keybase_cgo_Cbfs_GetVolumeSize
func keybase_cgo_Cbfs_GetVolumeSize(w *C.keybase_Cbfs_wrap, TotalNumberOfSectors, NumberOfFreeSectors *int64) {
	debug("GetVolumeSize")
	fs, ctx, cancel := getfs(w)
	defer cancel()
	a, b, err := fs.VolumeSize(ctx)
	*TotalNumberOfSectors = a / 512
	*NumberOfFreeSectors = b / 512
	wpost(w, err)
}

//export keybase_cgo_Cbfs_GetVolumeLabelW
func keybase_cgo_Cbfs_GetVolumeLabelW(w *C.keybase_Cbfs_wrap, ptr *C.WCHAR) {
	debug("GetVolumeLabel")
	fs, ctx, cancel := getfs(w)
	defer cancel()
	s := fs.VolumeLabel(ctx)
	if len(s) > 32 {
		s = s[:32]
	}
	stringToUtf16Buffer(s, ptr, 32)
}

//export keybase_cgo_Cbfs_SetVolumeLabelW
func keybase_cgo_Cbfs_SetVolumeLabelW(w *C.keybase_Cbfs_wrap, ptr *uint16) {
	debug("SetVolumeLabel FIXME")
}

//export keybase_cgo_Cbfs_GetVolumeId
func keybase_cgo_Cbfs_GetVolumeId(w *C.keybase_Cbfs_wrap, ptr *C.DWORD) {
	debug("GetVolumeId")
	fs, ctx, cancel := getfs(w)
	defer cancel()
	*ptr = C.DWORD(fs.VolumeId(ctx))
	wpost(w, nil)
}

//export keybase_cgo_Cbfs_CreateFileW
func keybase_cgo_Cbfs_CreateFileW(w *C.keybase_Cbfs_wrap,
	FileName C.LPCWSTR,
	DesiredAccess C.ACCESS_MASK,
	FileAttributes C.DWORD,
	ShareMode C.DWORD,
	//	FileInfo unsafe.Pointer, //*C.CbFsFileInfo,
	//	HandleInfo unsafe.Pointer) { //*CbFsHandleInfo) {
) {
	fn := lpcwstrToString(FileName)
	debug("CreateFile: ", fn)

	fs, ctx, cancel := getfs(w)
	defer cancel()
	fi, err := fs.CreateFile(ctx, &OpenContext{fn,
		uint32(DesiredAccess),
		uint32(FileAttributes),
		uint32(ShareMode),
		true})
	if err == nil {
		w.file = C.uint32_t(fiTableStoreFile(uint32(w.tag), fi))
	}
	wpost(w, err)
}

//export keybase_cgo_Cbfs_OpenFileW
func keybase_cgo_Cbfs_OpenFileW(w *C.keybase_Cbfs_wrap,
	FileName C.LPCWSTR,
	DesiredAccess C.ACCESS_MASK,
	FileAttributes C.DWORD,
	ShareMode C.DWORD,
	//	FileInfo unsafe.Pointer, //*C.CbFsFileInfo,
	//	HandleInfo unsafe.Pointer) { //*CbFsHandleInfo) {
) {
	fn := lpcwstrToString(FileName)
	debug("OpenFile: ", fn)

	fs, ctx, cancel := getfs(w)
	defer cancel()
	fi, err := fs.CreateFile(ctx, &OpenContext{fn,
		uint32(DesiredAccess),
		uint32(FileAttributes),
		uint32(ShareMode),
		false})
	if err == nil {
		w.file = C.uint32_t(fiTableStoreFile(uint32(w.tag), fi))
	}
	wpost(w, err)
}

//export keybase_cgo_Cbfs_CloseFile
func keybase_cgo_Cbfs_CloseFile(w *C.keybase_Cbfs_wrap,

//	FileInfo unsafe.Pointer, //*C.CbFsFileInfo,
//	HandleInfo unsafe.Pointer) { //*CbFsHandleInfo) {
) {
	debug("CloseFile")
	fi, ctx, cancel := getfi(w)
	defer cancel()
	fi.CloseFile(ctx)
	fiTableFreeFile(uint32(w.tag), uint32(w.file))
	wpost(w, nil)
}

//export keybase_cgo_Cbfs_GetFileInfoW
func keybase_cgo_Cbfs_GetFileInfoW(w *C.keybase_Cbfs_wrap,
	FileName C.LPCWSTR,
	FileExists *C.BOOL,
	CreationTime *C.FILETIME,
	LastAccessTime *C.FILETIME,
	LastWriteTime *C.FILETIME,
	ChangeTime *C.FILETIME,
	EndOfFile *int64,
	AllocationSize *int64,
	FileId *uint64,
	FileAttributes *C.DWORD,
	NumberOfLinks *C.DWORD,
	ShortFileName *C.WCHAR,
	ShortFileNameLength *uint16,
	RealFileName *C.WCHAR,
	RealFileNameLength *uint16,
) {
	fn := lpcwstrToString(FileName)
	debugf("GetFileInfo %q", fn)
	fs, ctx, cancel := getfs(w)
	defer cancel()
	st, err := fs.GetFileInfo(ctx, fn)
	if err == nil {
		*FileExists = 1
		*CreationTime = packTime(st.Creation)
		*LastAccessTime = packTime(st.LastAccess)
		*LastWriteTime = packTime(st.LastWrite)
		*ChangeTime = packTime(st.LastWrite)
		*EndOfFile = st.FileSize
		*AllocationSize = st.FileSize
		*FileId = st.FileIndex
		*FileAttributes = C.DWORD(st.FileAttributes)
		*NumberOfLinks = C.DWORD(st.NumberOfLinks)
	}
	if err == ErrFileNotFound {
		*FileExists = 0
	}
	wpost(w, err)
}
func packTime(t time.Time) C.FILETIME {
	ft := syscall.NsecToFiletime(t.UnixNano())
	return C.FILETIME{dwLowDateTime: C.DWORD(ft.LowDateTime), dwHighDateTime: C.DWORD(ft.HighDateTime)}
}

//export keybase_cgo_Cbfs_EnumerateDirectoryW
func keybase_cgo_Cbfs_EnumerateDirectoryW(w *C.keybase_Cbfs_wrap,
	//	DirectoryInfo *C.CbFsFileInfo,
	//	HandleInfo *C.CbFsHandleInfo,
	//	EnumerationInfo *C.CbFsDirectoryEnumerationInfo,
	Mask C.LPCWSTR,
	Index C.INT,
	//	Restart C.BOOL,
	FileFound C.LPBOOL,
	FileName C.LPWSTR,
	FileNameLength C.PDWORD,
	ShortFileNameOptional C.LPWSTR,
	ShortFileNameLengthOptional C.PUCHAR,
	CreationTime C.PFILETIME,
	LastAccessTime C.PFILETIME,
	LastWriteTime C.PFILETIME,
	ChangeTime C.PFILETIME,
	EndOfFile *int64,
	AllocationSize *int64,
	FileId *uint64,
	FileAttributes *C.DWORD,
) {
	var err error
	var mask string
	var re *regexp.Regexp
	if !(Mask == nil || *Mask == 0 ||
		(*Mask == '*' && *nextWchar(Mask) == 0)) {
		mask = lpcwstrToString(Mask)
		if strings.ContainsAny(mask, "?*") {
			m := regexp.QuoteMeta(mask)
			m = strings.Replace(m, `\?`, `.`, -1)
			m = strings.Replace(m, `\*`, `.*`, -1)
			re, err = regexp.Compile(m)
			if err != nil {
				debug("Regexp compile failed:", mask, "=>", m)
			}
		}
	}
	debugf("EnumerateDirectory: %q %d", mask, Index)
	fi, ctx, cancel := getfi(w)
	defer cancel()
	var fs namedStats
	err = fi.FindFiles(ctx, mask, func(st *NamedStat) error {
		if mask == "" || ((re != nil && re.MatchString(st.Name)) || st.Name == mask) {
			fs = append(fs, *st)
		}
		return nil
	})
	if err != nil {
		wpost(w, err)
		return
	}
	if int(Index) >= len(fs) {
		*FileFound = 0
		wpost(w, nil)
		return
	}
	sort.Sort(fs)
	st := fs[int(Index)]
	*FileFound = 1
	stringToUtf16Buffer(st.Name, FileName, C.DWORD(maxFileNameLength))
	*FileNameLength = C.DWORD(len(utf16.Encode([]rune(st.Name))))
	*CreationTime = packTime(st.Creation)
	*LastAccessTime = packTime(st.LastAccess)
	*LastWriteTime = packTime(st.LastWrite)
	*ChangeTime = packTime(st.LastWrite)
	*EndOfFile = st.FileSize
	*AllocationSize = st.FileSize
	*FileId = st.FileIndex
	*FileAttributes = C.DWORD(st.FileAttributes)
	wpost(w, nil)
}

var maxFileNameLength C.uint32_t

type namedStats []NamedStat

func (ns namedStats) Len() int           { return len(ns) }
func (ns namedStats) Less(i, j int) bool { return ns[i].Name < ns[j].Name }
func (ns namedStats) Swap(i, j int)      { ns[i], ns[j] = ns[j], ns[i] }

func nextWchar(ptr *C.WCHAR) *C.WCHAR {
	return (*C.WCHAR)(unsafe.Pointer(uintptr(unsafe.Pointer(ptr)) + 2))
}

/* Handled in C++ at the moment:
//export keybase_cgo_Cbfs_CloseDirectoryEnumeration
func keybase_cgo_Cbfs_CloseDirectoryEnumeration(w *C.keybase_Cbfs_wrap,
	FileInfo *C.CbFsFileInfo,
	EnumerationInfo *C.CbFsDirectoryEnumerationInfo,
) {
	debug("CloseDirectoryEnumeration")
}*/

//export keybase_cgo_Cbfs_SetAllocationSize
func keybase_cgo_Cbfs_SetAllocationSize(w *C.keybase_Cbfs_wrap,
	//	FileInfo *C.CbFsFileInfo,
	AllocationSize int64,
) {
	debug("SetAllocationSize")
	fi, ctx, cancel := getfi(w)
	defer cancel()
	err := fi.SetAllocationSize(ctx, AllocationSize)
	wpost(w, err)
}

//export keybase_cgo_Cbfs_SetEndOfFile
func keybase_cgo_Cbfs_SetEndOfFile(w *C.keybase_Cbfs_wrap,
	//	FileInfo *C.CbFsFileInfo,
	Size int64,
) {
	debug("SetEndOfFile")
	fi, ctx, cancel := getfi(w)
	defer cancel()
	err := fi.SetEndOfFile(ctx, Size)
	wpost(w, err)
}

//export keybase_cgo_Cbfs_SetFileAttributesW
func keybase_cgo_Cbfs_SetFileAttributesW(w *C.keybase_Cbfs_wrap,
	//	FileInfo *C.CbFsFileInfo,
	//	HandleInfo *C.CbFsHandleInfo,
	CreationTime *C.FILETIME,
	LastAccessTime *C.FILETIME,
	LastWriteTime *C.FILETIME,
	ChangeTime *C.FILETIME,
	FileAttributes C.DWORD,
) {
	debug("SetFileAttributes FIXME")
	fi, ctx, cancel := getfi(w)
	defer cancel()
	var st Stat
	st.FileAttributes = FileAttribute(FileAttributes)
	if CreationTime != nil {
		st.Creation = unpackTime(*CreationTime)
	}
	if LastAccessTime != nil {
		st.LastAccess = unpackTime(*LastAccessTime)
	}
	if LastWriteTime != nil {
		st.LastWrite = unpackTime(*LastWriteTime)
	}
	//ChangeTime FIXME
	err := fi.SetFileAttributes(ctx, &st)
	wpost(w, err)
}
func unpackTime(c C.FILETIME) time.Time {
	ft := syscall.Filetime{LowDateTime: uint32(c.dwLowDateTime), HighDateTime: uint32(c.dwHighDateTime)}
	// This is valid, see docs and code for package time.
	return time.Unix(0, ft.Nanoseconds())
}

//export keybase_cgo_Cbfs_CanFileBeDeleted
func keybase_cgo_Cbfs_CanFileBeDeleted(w *C.keybase_Cbfs_wrap,
	//	FileInfo *C.CbFsFileInfo,
	//	HandleInfo *C.CbFsHandleInfo,
	CanBeDeleted *C.BOOL,
) {
	debug("CanFileBeDeleted")
	fi, ctx, cancel := getfi(w)
	defer cancel()
	err := fi.CanDelete(ctx)
	if err != nil {
		*CanBeDeleted = 0
	} else {
		*CanBeDeleted = 1
	}
	// By purpose nil here.
	wpost(w, nil)
}

//export keybase_cgo_Cbfs_DeleteFileW
func keybase_cgo_Cbfs_DeleteFileW(w *C.keybase_Cbfs_wrap,
	//	FileInfo *C.CbFsFileInfo,
	FileName C.LPCWSTR,
) {
	debug("DeleteFile")
	fs, ctx, cancel := getfs(w)
	defer cancel()
	fi, err := fs.CreateFile(ctx, &OpenContext{FileName: lpcwstrToString(FileName)})
	if err == nil {
		err = fi.Delete(ctx)
		fi.CloseFile(ctx)
	}
	wpost(w, err)
}

//export keybase_cgo_Cbfs_RenameOrMoveFileW
func keybase_cgo_Cbfs_RenameOrMoveFileW(w *C.keybase_Cbfs_wrap,
	//	FileInfo *C.CbFsFileInfo,
	OldFileName C.LPCWSTR,
	NewFileName C.LPCWSTR,
) {
	debug("RenameOrMoveFile")
	fi, ctx, cancel := getfs(w)
	defer cancel()
	err := fi.Rename(ctx, lpcwstrToString(OldFileName), lpcwstrToString(NewFileName))
	wpost(w, err)
}

//export keybase_cgo_Cbfs_ReadFile
func keybase_cgo_Cbfs_ReadFile(w *C.keybase_Cbfs_wrap,
	//	FileInfo *C.CbFsFileInfo,
	Position int64,
	Buffer unsafe.Pointer,
	Size C.DWORD,
	NDone *C.DWORD,
) {
	debug("ReadFile")
	fi, ctx, cancel := getfi(w)
	defer cancel()
	n, err := fi.ReadFile(ctx,
		bufToSlice(Buffer, uint32(Size)), int64(Position))
	*NDone = C.DWORD(n)
	wpost(w, err)
}

//export keybase_cgo_Cbfs_WriteFile
func keybase_cgo_Cbfs_WriteFile(w *C.keybase_Cbfs_wrap,
	//	FileInfo *C.CbFsFileInfo,
	Position int64,
	Buffer unsafe.Pointer,
	Size C.DWORD,
	NDone *C.DWORD,
) {
	debug("WriteFile")
	fi, ctx, cancel := getfi(w)
	defer cancel()
	n, err := fi.WriteFile(ctx,
		bufToSlice(Buffer, uint32(Size)), int64(Position))
	*NDone = C.DWORD(n)
	wpost(w, err)
}

//export keybase_cgo_Cbfs_IsDirectoryEmpty
func keybase_cgo_Cbfs_IsDirectoryEmpty(w *C.keybase_Cbfs_wrap,
	//	FileInfo *C.CbFsFileInfo,
	//	FileName C.LPCWSTR,
	IsEmpty *C.BOOL,
) {
	debug("IsDirectoryEmpty")
	fi, ctx, cancel := getfi(w)
	defer cancel()
	res, err := fi.IsDirectoryEmpty(ctx)
	if res {
		*IsEmpty = 1
	} else {
		*IsEmpty = 0
	}
	wpost(w, err)
}

func werr(wctx *C.keybase_Cbfs_wrap) error {
	if wctx.ecode == 0 {
		return nil
	}
	var msg string
	if wctx.emsg != nil {
		msg = lpcwstrToString(wctx.emsg)
		C.free(unsafe.Pointer(wctx.emsg))
		wctx.emsg = nil
	}
	return CBFSError{int(wctx.ecode), msg}
}

type CBFSError struct {
	Code    int
	Message string
}

func (ce CBFSError) Error() string {
	return fmt.Sprintf("%d: %q", ce.Code, ce.Message)
}

func wpost(w *C.keybase_Cbfs_wrap, err error) {
	if err == nil {
		return
	}
	debug("Error: ", err)
	if e, ok := err.(CBFSError); ok {
		w.ecode = C.DWORD(e.Code)
	} else {
		w.ecode = 5 // ERROR_ACCESS_DENIED
	}
}

// stringToUtf16Ptr return a pointer to the string as utf16 with zero
// termination.
func stringToUtf16Ptr(s string) unsafe.Pointer {
	tmp := utf16.Encode([]rune(s + "\000"))
	return unsafe.Pointer(&tmp[0])
}

// lpcwstrToString converts a nul-terminated Windows wide string to a Go string,
func lpcwstrToString(ptr C.LPCWSTR) string {
	if ptr == nil {
		return ""
	}
	var len = 0
	for tmp := ptr; *tmp != 0; tmp = (C.LPCWSTR)(unsafe.Pointer((uintptr(unsafe.Pointer(tmp)) + 2))) {
		len++
	}
	raw := ptrUcs2Slice(ptr, len)
	return string(utf16.Decode(raw))
}

// ptrUcs2Slice takes a C Windows wide string and length in UCS2
// and returns it aliased as a uint16 slice.
func ptrUcs2Slice(ptr C.LPCWSTR, lenUcs2 int) []uint16 {
	return *(*[]uint16)(unsafe.Pointer(&reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(ptr)),
		Len:  lenUcs2,
		Cap:  lenUcs2}))
}

// stringToUtf16Buffer pokes a string into a Windows wide string buffer.
// On overflow does not poke anything and returns false.
func stringToUtf16Buffer(s string, ptr C.LPWSTR, blenUcs2 C.DWORD) bool {
	if ptr == nil || blenUcs2 == 0 {
		return false
	}
	src := utf16.Encode([]rune(s))
	tgt := ptrUcs2Slice(C.LPCWSTR(unsafe.Pointer(ptr)), int(blenUcs2))
	if len(src)+1 >= len(tgt) {
		tgt[0] = 0
		return false
	}
	copy(tgt, src)
	tgt[len(src)] = 0
	return true
}

// bufToSlice returns a byte slice aliasing the pointer and length given as arguments.
func bufToSlice(ptr unsafe.Pointer, nbytes uint32) []byte {
	if ptr == nil || nbytes == 0 {
		return nil
	}
	return *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{
		Data: uintptr(ptr),
		Len:  int(nbytes),
		Cap:  int(nbytes)}))
}

func getfs(w *C.keybase_Cbfs_wrap) (FileSystem, context.Context, context.CancelFunc) {
	fs := fsTableGet(uint32(w.tag))
	ctx, cancel := fs.WithContext(context.Background())
	return fs, ctx, cancel
}

func getfi(w *C.keybase_Cbfs_wrap) (File, context.Context, context.CancelFunc) {
	_, ctx, cancel := getfs(w)
	return fiTableGetFile(uint32(w.file)), ctx, cancel
}
