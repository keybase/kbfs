package libfuse

import (
	"syscall"

	"github.com/keybase/kbfs/libkbfs"
	"golang.org/x/net/context"

	"bazil.org/fuse"
)

// XDevRenamer handles rename() across different TLFs.
type renamer interface {
	Rename(ctx context.Context, oldParent *Dir, oldEntryName string,
		newParent *Dir, newEntryName string, req *fuse.RenameRequest) error
}

type noXDevRenamer struct {
	kbfsOps libkbfs.KBFSOps
}

func (r noXDevRenamer) Rename(ctx context.Context, oldParent *Dir, oldEntryName string,
	newParent *Dir, newEntryName string, req *fuse.RenameRequest) error {
	err := r.kbfsOps.Rename(ctx,
		oldParent.node, oldEntryName, newParent.node, newEntryName)
	switch err.(type) {
	case libkbfs.RenameAcrossDirsError:
		return fuse.Errno(syscall.EXDEV)
	default:
		return err
	}
}

/*
type mvForFinderXDevRenamer struct {
	kbfsOps   libkbfs.KBFSOps
	finderPID uint32
	once      sync.Once
}

func (r *mvForFinderXDevRenamer) renameUsingMVCommand(ctx context.Context, oldParent *Dir, oldEntryName string,
	newParent *Dir, newEntryName string) error {
}

// kickOff kicks off a go routine to find Finder's PID. This is a hack until we
// decide to really go with this.
func (r *mvForFinderXDevRenamer) kickOff() {
	r.once.Do(func() {
		re := regexp.MustCompile(`^\s*(\d)+\s+\/System\/Library\/CoreServices\/Finder\.app\/Contents\/MacOS\/Finder\s*$`)
		do := func() {
			o, err := exec.Command("/bin/ps", "-Ao", "pid,command").CombinedOutput()
			if err != nil {
				return
			}
			for _, line := range bytes.Split(o, []byte{'\n'}) {
				if matched := re.FindSubmatch(line); len(matched) == 2 {
					pid, err := strconv.Atoi(matched[1])
					if err != nil {
						return
					}
					atomic.StoreUint32(&r.finderPID, pid)
					return
				}
			}
		}
		do()
		go func() {
			for {
				time.Sleep(time.Second)
				do()
			}
		}()
	})
}

func (r *mvForFinderXDevRenamer) Rename(ctx context.Context, oldParent *Dir, oldEntryName string,
	newParent *Dir, newEntryName string, req *fuse.RenameRequest) error {
	err := r.kbfsOps.Rename(ctx,
		oldParent.node, oldEntryName, newParent.node, newEntryName)
	switch err.(type) {
	case libkbfs.RenameAcrossDirsError:
		if atomic.LoadUint32(&r.finderPID) == req.Pid {
			return r.renameUsingMVCommand(ctx,
				oldParent, oldEntryName, newParent, newEntryName)
		}
		return fuse.Errno(syscall.EXDEV)
	default:
		return err
	}
}
*/
