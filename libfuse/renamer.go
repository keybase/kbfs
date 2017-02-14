package libfuse

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/keybase/client/go/logger"
	"github.com/keybase/kbfs/libkbfs"
	"github.com/keybase/kbfs/sysutils"
	"golang.org/x/net/context"

	"bazil.org/fuse"
)

type renamerStatus int

const (
	_ renamerStatus = iota
	renamerDone
	renamerNext
)

// renamerOp is a step, or an approach, to do the rename. It's part of a
// renamer, which is created with newRenamer(). Implementation of the renamerOp
// should be stateless as the same instance can be used over multiple requests.
type renamerOp interface {
	// do is where a renamerOp implements its processing of the rename() request.
	//
	// If the processing is done and no further renamerOp should be used,
	// renamerDone should be returned. Otherwise, renamerNext should be returned
	// so that renamer knows to try next renamerOp.
	//
	// It is important that, when renamerDone is returned, error is set to either
	// nil, or something FUSE understands, since in this case error is directly
	// returned back to FUSE.  On the other hand, when renamerNext is returned,
	// error should set to the real error so subsequent renamerOps have enough
	// information to deal with it.
	do(ctx context.Context,
		oldParent *Dir, oldEntryName string, newParent *Dir, newEntryName string,
		req *fuse.RenameRequest, prevErr error) (error, renamerStatus)
}

type renamer struct {
	ops []renamerOp
}

func (r renamer) rename(ctx context.Context, oldParent *Dir, oldEntryName string,
	newParent *Dir, newEntryName string, req *fuse.RenameRequest) error {
	var (
		err error
		rs  renamerStatus
	)
	for _, op := range r.ops {
		err, rs = op.do(ctx,
			oldParent, oldEntryName, newParent, newEntryName, req, err)
		switch rs {
		case renamerDone:
			return err
		case renamerNext:
			continue
		default:
			panic("unknown renamerStatus")
		}
	}
	return err
}

// newRenamer creates a new renamer that tries several operations. For example:
//
//	 renamer := newRenamer(
//     newNaiveRenamerOp(folder.fs.config.KBFSOps()),
//     newAmnestyRenamerOp(exePathFinder),
//     newFinderTrickingRenamerOp("/bin/mv", 10*time.Second), TODO: fix this
//     newNotifyingRenamerOp(),
//   )
//
func newRenamer(renamerOps ...renamerOp) renamer {
	return renamer{ops: renamerOps}
}

type naiveRenamerOp struct{}

func newNaiveRenamerOp() renamerOp {
	return naiveRenamerOp{}
}

func (r naiveRenamerOp) do(ctx context.Context,
	oldParent *Dir, oldEntryName string, newParent *Dir, newEntryName string,
	req *fuse.RenameRequest, prevErr error) (err error, status renamerStatus) {
	err = oldParent.folder.fs.config.KBFSOps().Rename(ctx,
		oldParent.node, oldEntryName, newParent.node, newEntryName)
	switch err.(type) {
	case nil:
		return nil, renamerDone
	case libkbfs.RenameAcrossDirsError:
		// Carry on to the next renamer in case we need an effort to resolve EXDEV
		// before returning from the fuse handler.
		return err, renamerNext
	default:
		// Either rename succeeded, or it's an error other than EXDEV. Either way
		// return and error and mark as done.
		//
		// fbo already logs it so there's no need to log the error again.
		return fuse.EIO, renamerDone
	}
}

const (
	exePathFinder = "/System/Library/CoreServices/Finder.app/Contents/MacOS/Finder"
)

// An amnestyRenamer returns (prevErr, renamerNext) for processes in paths, and
// (err, renamerDone) for others. This works as a filter to determine
// whether subsequent renamers should keep working on the same request.
type amnestyRenamerOp struct {
	paths map[string]bool
}

func newAmnestyRenamerOp(amnestyProcesses ...string) renamerOp {
	ret := amnestyRenamerOp{paths: make(map[string]bool)}
	for _, p := range amnestyProcesses {
		ret.paths[p] = true
	}
	return ret
}

func (r amnestyRenamerOp) do(ctx context.Context,
	oldParent *Dir, oldEntryName string, newParent *Dir, newEntryName string,
	req *fuse.RenameRequest, prevErr error) (err error, status renamerStatus) {
	if _, ok := prevErr.(libkbfs.RenameAcrossDirsError); !ok {
		// We only deal with RenameAcrossDirsError here. Otherwise log the error
		// and mark as done.
		oldParent.folder.fs.log.CDebugf(ctx,
			"amnestyRenamerOp: unsupported error=%v", err)
		return fuse.Errno(syscall.EIO), renamerDone
	}
	var exe string
	if exe, err = sysutils.GetExecPathFromPID(int(req.Pid)); err != nil {
		oldParent.folder.fs.log.CDebugf(ctx,
			"amnestyRenamerOp: GetExecPathFromPid: error=%v", err)
		// Perhaps GetExecPathFromPID is not implemented on the platform. Perhaps
		// there's a problem calling the syscall. Perhaps the process is gone.
		// Either way, just return the error and mark it as done.
		return fuse.Errno(syscall.EXDEV), renamerDone
	}

	if r.paths[exe] {
		// It's one of the "amnesty" processes. Additional stuff should be done
		// before returning from the fuse handler.
		return prevErr, renamerNext
	}

	// No an "amnesty" process. Done!
	return fuse.Errno(syscall.EXDEV), renamerDone
}

// finderTrickingRenameOp creates a secondary mount, and uses Mac Automation to
// get Finder to move the thing into the secondary mount ...
// TODO: finish this.
type finderTrickingRenamerOp struct {
}

func newFinderTrickingRenamerOp() renamerOp {
	return &finderTrickingRenamerOp{}
}

const osaMoveJS = `Application('Finder').move(Path(%q), {'to': Path(%q)})`

func (r *finderTrickingRenamerOp) rewriteNewParentPath(p string) string {
	return strings.Replace(p, "/keybase", "/tmp/k", 1)
}

func (r *finderTrickingRenamerOp) spawnOSAMoveWithWatch(
	ctx context.Context, logger logger.Logger,
	oldEntryPath, rewrittenNewParentPath string, watchDuration time.Duration) (
	watchErr error) {
	cmd := exec.Command("/usr/bin/osascript", "-l", "JavaScript")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	fmt.Fprintf(stdin, osaMoveJS, oldEntryPath, rewrittenNewParentPath)
	if err = stdin.Close(); err != nil {
		return err
	}

	waitCh := make(chan error, 1)
	go func() {
		er := cmd.Wait()
		if er != nil {
			o, _ := cmd.CombinedOutput()
			er = errors.New(er.Error() + ": " + string(o))
		}
		waitCh <- er
		close(waitCh)
	}()

	select {
	case <-time.After(watchDuration):
		// No error after watchDuration, so we assume it's working on the move.
		// Note that OSA doesn't return an error if the operation is canceled by
		// the user, so we don't have to / cannot worry about that case.
		//
		// Spawn a new routine here to wait for and log the error when the cmd
		// finishes.
		go func() {
			logger.CDebugf(ctx, "finderTrickingRenamerOp: cmd.Wait: error=%v", <-waitCh)
		}()
		return nil
	case err = <-waitCh:
		return err
	}
}

func (r *finderTrickingRenamerOp) do(ctx context.Context,
	oldParent *Dir, oldEntryName string, newParent *Dir, newEntryName string,
	req *fuse.RenameRequest, prevErr error) (error, renamerStatus) {
	op, err := oldParent.folder.fs.config.KBFSOps().GetPathStringByNode(
		ctx, oldParent.node)
	if err != nil {
		oldParent.folder.fs.log.CDebugf(ctx,
			"finderTrickingRenamerOp: oldParent,GetPathStringByNode: error=%v", err)
		return err, renamerNext
	}
	np, err := newParent.folder.fs.config.KBFSOps().GetPathStringByNode(
		ctx, newParent.node)
	if err != nil {
		newParent.folder.fs.log.CDebugf(ctx,
			"finderTrickingRenamerOp: newParent,GetPathStringByNode: error=%v", err)
		return err, renamerNext
	}
	oldPath := filepath.Join(op, oldEntryName)

	if err = r.spawnOSAMoveWithWatch(ctx, oldParent.folder.fs.log,
		oldPath, r.rewriteNewParentPath(np), 4*time.Second); err != nil {
		newParent.folder.fs.log.CDebugf(ctx,
			"finderTrickingRenamerOp: spawnOSAMoveWithWatch: error=%v", err)
		return err, renamerNext
	}

	// Return a nil error so Finder doesn't complain to user.
	return nil, renamerDone
}

type notifyingRenamerOp struct{}

func newNotifyingRenamerOp() renamerOp {
	return notifyingRenamerOp{}
}

func (r notifyingRenamerOp) do(ctx context.Context,
	oldParent *Dir, oldEntryName string, newParent *Dir, newEntryName string,
	req *fuse.RenameRequest, prevErr error) (error, renamerStatus) {
	oldParent.folder.fs.log.CDebugf(ctx,
		"notifyingRenamerOp: called but not implemented. prevErr=%v", prevErr)
	// TODO: send notification
	return fuse.Errno(syscall.EXDEV), renamerDone
}
