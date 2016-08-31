// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libdokan

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/kbfs/dokan"
	"github.com/keybase/kbfs/libfs"
	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"
)

// FS implements the newfuse FS interface for KBFS.
type FS struct {
	config libkbfs.Config
	log    logger.Logger

	notifications *libfs.FSNotifications

	root *Root

	// currentUserSID stores the Windows identity of the user running
	// this process.
	currentUserSID *dokan.SID

	// remoteStatus is the current status of remote connections.
	remoteStatus libfs.RemoteStatus
}

// DefaultMountFlags are the default mount flags for libdokan.
const DefaultMountFlags = dokan.CurrentSession

// NewFS creates an FS
func NewFS(ctx context.Context, config libkbfs.Config, log logger.Logger) (*FS, error) {
	sid, err := dokan.CurrentProcessUserSid()
	if err != nil {
		return nil, err
	}
	f := &FS{
		config:         config,
		log:            log,
		notifications:  libfs.NewFSNotifications(log),
		currentUserSID: sid,
	}

	f.root = &Root{
		private: &FolderList{
			fs:         f,
			folders:    make(map[string]fileOpener),
			aliasCache: map[string]string{},
		},
		public: &FolderList{
			fs:         f,
			public:     true,
			folders:    make(map[string]fileOpener),
			aliasCache: map[string]string{},
		}}

	ctx = wrapContext(ctx, f)

	f.remoteStatus.Init(ctx, f.log, f.config)
	f.notifications.LaunchProcessor(ctx)
	go clearFolderListCacheLoop(ctx, f.root)

	return f, nil
}

// Adds log tags etc
func wrapContext(ctx context.Context, f *FS) context.Context {
	ctx = context.WithValue(ctx, CtxAppIDKey, f)
	logTags := make(logger.CtxLogTags)
	logTags[CtxIDKey] = CtxOpID
	ctx = logger.NewContextWithLogTags(ctx, logTags)
	return ctx
}

// WithContext creates context for filesystem operations.
func (f *FS) WithContext(ctx context.Context) (context.Context, context.CancelFunc) {
	id, err := libkbfs.MakeRandomRequestID()
	if err != nil {
		f.log.Errorf("Couldn't make request ID: %v", err)
		return ctx, func() {}
	}

	ctx, cancel := context.WithCancel(ctx)

	// context.WithDeadline uses clock from `time` package, so we are not using
	// f.config.Clock() here
	start := time.Now()
	ctx, err = libkbfs.NewContextWithCancellationDelayer(
		libkbfs.NewContextReplayable(ctx, func(ctx context.Context) context.Context {
			ctx = wrapContext(context.WithValue(ctx, CtxIDKey, id), f)
			ctx, _ = context.WithDeadline(ctx, start.Add(29*time.Second))
			return ctx
		}))
	if err != nil {
		panic(err)
	}
	return ctx, cancel
}

var vinfo = dokan.VolumeInformation{
	VolumeName:             "KBFS",
	MaximumComponentLength: 0xFF, // This can be changed.
	FileSystemFlags: dokan.FileCasePreservedNames | dokan.FileCaseSensitiveSearch |
		dokan.FileUnicodeOnDisk | dokan.FileSupportsReparsePoints |
		dokan.FileSupportsRemoteStorage,
	FileSystemName: "KBFS",
}

// GetVolumeInformation returns information about the whole filesystem for dokan.
func (f *FS) GetVolumeInformation(ctx context.Context) (dokan.VolumeInformation, error) {
	// TODO should this be explicitely refused to other users?
	// As the mount is limited to current session there is little need.
	return vinfo, nil
}

const dummyFreeSpace = 10 * 1024 * 1024 * 1024

// GetDiskFreeSpace returns information about free space on the volume for dokan.
func (f *FS) GetDiskFreeSpace(ctx context.Context) (freeSpace dokan.FreeSpace, err error) {
	// TODO should this be refused to other users?
	// As the mount is limited to current session there is little need.
	f.logEnter(ctx, "FS GetDiskFreeSpace")
	// Refuse private directories while we are in a error state.
	if f.remoteStatus.ExtraFileName() != "" {
		f.log.Warning("Dummy disk free space while errors are present!")
		return dokan.FreeSpace{
			TotalNumberOfBytes:     dummyFreeSpace,
			TotalNumberOfFreeBytes: dummyFreeSpace,
			FreeBytesAvailable:     dummyFreeSpace,
		}, nil
	}
	defer func() { f.reportErr(ctx, libkbfs.ReadMode, err) }()
	uqi, err := f.config.BlockServer().GetUserQuotaInfo(ctx)
	if err != nil {
		return dokan.FreeSpace{}, errToDokan(err)
	}
	free := uint64(uqi.Limit)
	if uqi.Total != nil {
		free -= uint64(uqi.Total.Bytes[libkbfs.UsageWrite])
	}
	return dokan.FreeSpace{
		TotalNumberOfBytes:     uint64(uqi.Limit),
		TotalNumberOfFreeBytes: free,
		FreeBytesAvailable:     free,
	}, nil
}

// openContext is for opening files.
type openContext struct {
	fi *dokan.FileInfo
	*dokan.CreateData
	redirectionsLeft int
	// isUppercasePath marks a path containing only upper case letters,
	// associated with e.g. resolving some reparse points. This has
	// special case insensitive path resolving functionality.
	isUppercasePath bool
}

// reduceRedictionsLeft reduces redirections and returns whether there are
// redirections left (true), or whether processing should be stopped (false).
func (oc *openContext) reduceRedirectionsLeft() bool {
	oc.redirectionsLeft--
	return oc.redirectionsLeft > 0
}

// isCreation checks the flags whether a file creation is wanted.
func (oc *openContext) isCreateDirectory() bool {
	return oc.isCreation() && oc.CreateOptions&fileDirectoryFile != 0
}

const fileDirectoryFile = 1

// isCreation checks the flags whether a file creation is wanted.
func (oc *openContext) isCreation() bool {
	switch oc.CreateDisposition {
	case dokan.FileSupersede, dokan.FileCreate, dokan.FileOpenIf, dokan.FileOverwriteIf:
		return true
	}
	return false
}
func (oc *openContext) isExistingError() bool {
	switch oc.CreateDisposition {
	case dokan.FileCreate:
		return true
	}
	return false
}

// isTruncate checks the flags whether a file truncation is wanted.
func (oc *openContext) isTruncate() bool {
	switch oc.CreateDisposition {
	case dokan.FileSupersede, dokan.FileOverwrite, dokan.FileOverwriteIf:
		return true
	}
	return false
}

// isOpenReparsePoint checks the flags whether a reparse point open is wanted.
func (oc *openContext) isOpenReparsePoint() bool {
	return oc.CreateOptions&dokan.FileOpenReparsePoint != 0
}

// returnDirNoCleanup returns a dir or nothing depending on the open
// flags and does not call .Cleanup on error.
func (oc *openContext) returnDirNoCleanup(f dokan.File) (dokan.File, bool, error) {
	if oc.mayNotBeDirectory() {
		return nil, false, dokan.ErrFileIsADirectory
	}
	return f, true, nil
}

func (oc *openContext) mayNotBeDirectory() bool {
	return oc.CreateOptions&dokan.FileNonDirectoryFile != 0
}

func newSyntheticOpenContext() *openContext {
	var oc openContext
	oc.CreateData = &dokan.CreateData{}
	oc.CreateDisposition = dokan.FileOpen
	oc.redirectionsLeft = 30
	return &oc
}

// CreateFile called from dokan, may be a file or directory.
func (f *FS) CreateFile(ctx context.Context, fi *dokan.FileInfo, cd *dokan.CreateData) (dokan.File, bool, error) {
	// Only allow the current user access
	if !fi.IsRequestorUserSidEqualTo(f.currentUserSID) {
		return nil, false, dokan.ErrAccessDenied
	}
	f.logEnter(ctx, "FS CreateFile")
	return f.openRaw(ctx, fi, cd)
}

// openRaw is a wrapper between CreateFile/CreateDirectory/OpenDirectory and open
func (f *FS) openRaw(ctx context.Context, fi *dokan.FileInfo, caf *dokan.CreateData) (dokan.File, bool, error) {
	ps, err := windowsPathSplit(fi.Path())
	if err != nil {
		return nil, false, err
	}
	oc := openContext{fi: fi, CreateData: caf, redirectionsLeft: 30}
	file, isd, err := f.open(ctx, &oc, ps)
	if err != nil {
		err = errToDokan(err)
	}
	return file, isd, err
}

// open tries to open a file deferring to more specific implementations.
func (f *FS) open(ctx context.Context, oc *openContext, ps []string) (dokan.File, bool, error) {
	f.log.CDebugf(ctx, "open: %#v", ps)
	psl := len(ps)
	switch {
	case psl < 1:
		return nil, false, dokan.ErrObjectNameNotFound
	case psl == 1 && ps[0] == ``:
		return oc.returnDirNoCleanup(f.root)
	case libkbfs.ErrorFile == ps[psl-1]:
		return NewErrorFile(f), false, nil
	case libfs.MetricsFileName == ps[psl-1]:
		return NewMetricsFile(f), false, nil
	case libfs.StatusFileName == ps[0]:
		return NewStatusFile(f.root.private.fs, nil), false, nil
	case libfs.ResetCachesFileName == ps[0]:
		return &ResetCachesFile{fs: f.root.private.fs}, false, nil
	// TODO
	// Unfortunately sometimes we end up in this case while using
	// reparse points.
	case `PUBLIC` == ps[0]:
		oc.isUppercasePath = true
		fallthrough
	case PublicName == ps[0]:
		// Refuse private directories while we are in a a generic error state.
		if f.remoteStatus.ExtraFileName() == libfs.HumanErrorFileName {
			f.log.CWarningf(ctx, "Refusing access to public directory while errors are present!")
			return nil, false, dokan.ErrAccessDenied
		}
		return f.root.public.open(ctx, oc, ps[1:])
	case `PRIVATE` == ps[0]:
		oc.isUppercasePath = true
		fallthrough
	case PrivateName == ps[0]:
		// Refuse private directories while we are in a error state.
		if f.remoteStatus.ExtraFileName() != "" {
			f.log.CWarningf(ctx, "Refusing access to private directory while errors are present!")
			return nil, false, dokan.ErrAccessDenied
		}
		return f.root.private.open(ctx, oc, ps[1:])
	case libfs.ProfileListDirName == ps[0]:
		return (ProfileList{fs: f}).open(ctx, oc, ps[1:])
	case libfs.HumanErrorFileName == ps[0], libfs.HumanNoLoginFileName == ps[0]:
		return &SpecialReadFile{
			read: f.remoteStatus.NewSpecialReadFunc,
			fs:   f}, false, nil
	case ".kbfs_unmount" == ps[0]:
		os.Exit(0)
	}
	return nil, false, dokan.ErrObjectNameNotFound
}

// windowsPathSplit handles paths we get from Dokan.
// As a special case `` means `\`, it gets generated
// on special occasions.
func windowsPathSplit(raw string) ([]string, error) {
	if raw == `` {
		raw = `\`
	}
	if raw[0] != '\\' || raw[len(raw)-1] == '*' {
		return nil, dokan.ErrObjectNameNotFound
	}
	return strings.Split(raw[1:], `\`), nil
}

// MoveFile tries to move a file.
func (f *FS) MoveFile(ctx context.Context, source *dokan.FileInfo, targetPath string, replaceExisting bool) (err error) {
	// User checking is handled by the opening of the source file
	f.logEnter(ctx, "FS MoveFile")
	defer func() { f.reportErr(ctx, libkbfs.WriteMode, err) }()

	oc := newSyntheticOpenContext()
	src, _, err := f.openRaw(ctx, source, oc.CreateData)
	f.log.CDebugf(ctx, "FS Rename source open -> %v,%v srcType %T", src, err, src)
	if err != nil {
		return err
	}
	defer src.Cleanup(ctx, nil)

	// Source directory
	srcDirPath, err := windowsPathSplit(source.Path())
	if err != nil {
		return err
	}
	if len(srcDirPath) < 1 {
		return errors.New("Invalid source for move")
	}
	srcName := srcDirPath[len(srcDirPath)-1]
	srcDirPath = srcDirPath[0 : len(srcDirPath)-1]
	srcDir, _, err := f.open(ctx, oc, srcDirPath)
	if err != nil {
		return err
	}
	defer srcDir.Cleanup(ctx, nil)

	// Destination directory, not the destination file
	dstPath, err := windowsPathSplit(targetPath)
	if err != nil {
		return err
	}
	if len(dstPath) < 1 {
		return errors.New("Invalid destination for move")
	}
	dstDirPath := dstPath[0 : len(dstPath)-1]

	dstDir, dstIsDir, err := f.open(ctx, oc, dstDirPath)
	f.log.CDebugf(ctx, "FS Rename dest open %v -> %v,%v,%v dstType %T", dstDirPath, dstDir, dstIsDir, err, dstDir)
	if err != nil {
		return err
	}
	defer dstDir.Cleanup(ctx, nil)
	if !dstIsDir {
		return errors.New("Tried to move to a non-directory path")
	}

	fl1, ok := srcDir.(*FolderList)
	fl2, ok2 := dstDir.(*FolderList)
	if ok && ok2 && fl1 == fl2 {
		return f.folderListRename(ctx, fl1, oc, src, srcName, dstPath, replaceExisting)
	}

	srcDirD := asDir(ctx, srcDir)
	if srcDirD == nil {
		return errors.New("Parent of src not a Dir")
	}
	srcFolder := srcDirD.folder
	srcParent := srcDirD.node

	ddst := asDir(ctx, dstDir)
	if ddst == nil {
		return errors.New("Destination directory is not of type Dir")
	}

	switch src.(type) {
	case *Dir:
	case *File:
	case *TLF:
	default:
		return dokan.ErrAccessDenied
	}

	// here we race...
	if !replaceExisting {
		x, _, err := f.open(ctx, oc, dstPath)
		if err == nil {
			defer x.Cleanup(ctx, nil)
		}
		if !isNoSuchNameError(err) {
			f.log.CDebugf(ctx, "FS Rename target open error %T %v", err, err)
			return errors.New("Refusing to replace existing target!")
		}

	}

	if srcFolder != ddst.folder {
		return dokan.ErrNotSameDevice
	}

	// overwritten node, if any, will be removed from Folder.nodes, if
	// it is there in the first place, by its Forget

	f.log.CDebugf(ctx, "FS Rename KBFSOps().Rename(ctx,%v,%v,%v,%v)", srcParent, srcName, ddst.node, dstPath[len(dstPath)-1])
	if err := srcFolder.fs.config.KBFSOps().Rename(
		ctx, srcParent, srcName, ddst.node, dstPath[len(dstPath)-1]); err != nil {
		f.log.CDebugf(ctx, "FS Rename KBFSOps().Rename FAILED %v", err)
		return err
	}

	switch x := src.(type) {
	case *Dir:
		x.parent = ddst.node
	case *File:
		x.parent = ddst.node
	}

	f.log.CDebugf(ctx, "FS Rename SUCCESS")
	return nil
}

func (f *FS) folderListRename(ctx context.Context, fl *FolderList, oc *openContext, src dokan.File, srcName string, dstPath []string, replaceExisting bool) error {
	ef, ok := src.(*EmptyFolder)
	f.log.CDebugf(ctx, "FS Rename folderlist %v", ef)
	if !ok || !isNewFolderName(srcName) {
		return dokan.ErrAccessDenied
	}
	dstName := dstPath[len(dstPath)-1]
	// Yes, this is slow, but that is ok here.
	if _, err := libkbfs.ParseTlfHandle(
		ctx, f.config.KBPKI(), dstName, fl.public); err != nil {
		return dokan.ErrObjectNameNotFound
	}
	fl.mu.Lock()
	_, ok = fl.folders[dstName]
	fl.mu.Unlock()
	if !replaceExisting && ok {
		f.log.CDebugf(ctx, "FS Rename folderlist refusing to replace target")
		return dokan.ErrAccessDenied
	}
	// Perhaps create destination by opening it.
	x, _, err := f.open(ctx, oc, dstPath)
	if err == nil {
		x.Cleanup(ctx, nil)
	}
	fl.mu.Lock()
	defer fl.mu.Unlock()
	_, ok = fl.folders[dstName]
	delete(fl.folders, srcName)
	if !ok {
		f.log.CDebugf(ctx, "FS Rename folderlist adding target")
		fl.folders[dstName] = ef
	}
	f.log.CDebugf(ctx, "FS Rename folderlist success")
	return nil
}

func (f *FS) queueNotification(fn func()) {
	f.notifications.QueueNotification(fn)
}

func (f *FS) reportErr(ctx context.Context, mode libkbfs.ErrorModeType, err error) {
	if err == nil {
		f.log.CDebugf(ctx, "Request complete")
		return
	}

	f.config.Reporter().ReportErr(ctx, "", false, mode, err)
	// We just log the error as debug, rather than error, because it
	// might just indicate an expected error such as an ENOENT.
	//
	// TODO: Classify errors and escalate the logging level of the
	// important ones.
	f.log.CDebugf(ctx, err.Error())
}

// NotificationGroupWait waits till the local notification group is done.
func (f *FS) NotificationGroupWait() {
	f.notifications.Wait()
}

func (f *FS) logEnter(ctx context.Context, s string) {
	f.log.CDebugf(ctx, "=> %s", s)
}

func (f *FS) logEnterf(ctx context.Context, fmt string, args ...interface{}) {
	f.log.CDebugf(ctx, "=> "+fmt, args...)
}

// Root represents the root of the KBFS file system.
type Root struct {
	emptyFile
	private *FolderList
	public  *FolderList
}

// GetFileInformation for dokan stats.
func (r *Root) GetFileInformation(ctx context.Context, fi *dokan.FileInfo) (*dokan.Stat, error) {
	return defaultDirectoryInformation()
}

// FindFiles for dokan readdir.
func (r *Root) FindFiles(ctx context.Context, fi *dokan.FileInfo, ignored string, callback func(*dokan.NamedStat) error) error {
	var ns dokan.NamedStat
	var err error
	ns.FileAttributes = dokan.FileAttributeDirectory
	ename, esize := r.private.fs.remoteStatus.ExtraFileNameAndSize()
	switch ename {
	case "":
		ns.Name = PrivateName
		err = callback(&ns)
		if err != nil {
			return err
		}
		fallthrough
	case libfs.HumanNoLoginFileName:
		ns.Name = PublicName
		err = callback(&ns)
		if err != nil {
			return err
		}
	}
	if ename != "" {
		ns.Name = ename
		ns.FileAttributes = dokan.FileAttributeNormal
		ns.FileSize = esize
		err = callback(&ns)
		if err != nil {
			return err
		}
	}
	return nil
}
