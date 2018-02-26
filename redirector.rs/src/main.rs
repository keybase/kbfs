extern crate env_logger;
extern crate fuse;
extern crate libc;
extern crate log;
extern crate time;

use std::env;
use std::ffi::OsStr;
use std::fs::File;
use std::io::prelude::*;
use std::io::BufReader;
use std::os::unix::prelude::*;
use std::path::PathBuf;
use libc::ENOENT;
use time::Timespec;
use fuse::{FileAttr, FileType, Filesystem, ReplyAttr, ReplyData, ReplyDirectory, ReplyEntry,
           Request};

// A TTL of zero for all our attribute replies.
// TODO: Should this be nonzero for performance reasons?
const TTL: Timespec = Timespec { sec: 0, nsec: 0 };

// FD 1 is a magical value that we can't change, but the others are arbitrary.
const ROOT_DIR_INODE: u64 = 1;
const PRIVATE_LINK_INODE: u64 = 2;
const PUBLIC_LINK_INODE: u64 = 3;
const TEAM_LINK_INODE: u64 = 4;

struct Redirector {
    // We use the process start time for all of our stat timestamps.
    start_time: Timespec,
}

impl Redirector {
    fn root_dir_attr(&self) -> FileAttr {
        FileAttr {
            ino: ROOT_DIR_INODE,
            size: 0,
            blocks: 0,
            atime: self.start_time,
            mtime: self.start_time,
            ctime: self.start_time,
            crtime: self.start_time,
            kind: FileType::Directory,
            perm: 0o755,
            nlink: 0,
            uid: 0,
            gid: 0,
            rdev: 0,
            flags: 0,
        }
    }

    fn link_attr(&self, ino: u64) -> FileAttr {
        FileAttr {
            ino,
            size: 0,
            blocks: 0,
            atime: self.start_time,
            mtime: self.start_time,
            ctime: self.start_time,
            crtime: self.start_time,
            kind: FileType::Symlink,
            perm: 0o644,
            nlink: 0,
            uid: 0,
            gid: 0,
            rdev: 0,
            flags: 0,
        }
    }
}

impl Filesystem for Redirector {
    fn lookup(&mut self, _req: &Request, _parent: u64, name: &OsStr, reply: ReplyEntry) {
        match name.to_str() {
            Some("private") => reply.entry(&TTL, &self.link_attr(PRIVATE_LINK_INODE), 0),
            Some("public") => reply.entry(&TTL, &self.link_attr(PUBLIC_LINK_INODE), 0),
            Some("team") => reply.entry(&TTL, &self.link_attr(TEAM_LINK_INODE), 0),
            _ => reply.error(ENOENT),
        }
    }

    fn getattr(&mut self, _req: &Request, ino: u64, reply: ReplyAttr) {
        match ino {
            ROOT_DIR_INODE => reply.attr(&TTL, &self.root_dir_attr()),
            PRIVATE_LINK_INODE | PUBLIC_LINK_INODE | TEAM_LINK_INODE => {
                reply.attr(&TTL, &self.link_attr(ino))
            }
            _ => reply.error(ENOENT),
        }
    }

    fn readlink(&mut self, req: &Request, ino: u64, reply: ReplyData) {
        let subdir = match ino {
            PRIVATE_LINK_INODE => "private",
            PUBLIC_LINK_INODE => "public",
            TEAM_LINK_INODE => "team",
            _ => return reply.error(ENOENT),
        };
        // TODO: Should we cache the mountpoint somehow?
        if let Some(mountpoint) = get_keybase_fuse_mount(req.uid()) {
            let target = mountpoint.join(subdir);
            reply.data(target.as_os_str().as_bytes());
        } else {
            reply.error(ENOENT);
        }
    }

    fn readdir(
        &mut self,
        _req: &Request,
        ino: u64,
        _fh: u64,
        offset: i64,
        mut reply: ReplyDirectory,
    ) {
        if ino == ROOT_DIR_INODE {
            if offset == 0 {
                reply.add(ROOT_DIR_INODE, 0, FileType::Directory, ".");
                reply.add(ROOT_DIR_INODE, 1, FileType::Directory, "..");
                reply.add(PRIVATE_LINK_INODE, 2, FileType::Symlink, "private");
                reply.add(PUBLIC_LINK_INODE, 3, FileType::Symlink, "public");
                reply.add(TEAM_LINK_INODE, 4, FileType::Symlink, "team");
            }
            reply.ok();
        } else {
            reply.error(ENOENT);
        }
    }
}

// This is explicitly only implemented for Linux, so that building on other
// platforms is a compiler error.
#[cfg(target_os = "linux")]
fn get_keybase_fuse_mount(uid: u32) -> Option<PathBuf> {
    let uid_str = format!("user_id={}", uid);
    let file = File::open("/proc/mounts").expect("failed to open /proc/mounts");
    let reader = BufReader::new(file);
    // TODO: Avoid assuming UTF8?
    // TODO: Sort the paths first?
    for line in reader.lines() {
        let line = line.expect("failed to read from /proc/mounts");
        let parts: Vec<&str> = line.split_whitespace().collect();
        // Check the mount type.
        if parts[2] != "fuse" {
            continue;
        }
        // Look for the uid in the mount options.
        if parts[3].split(',').find(|s| s == &uid_str).is_none() {
            continue;
        }
        // Make sure "keybase" is in the path somewhere. We do this loosely,
        // rather than splitting exactly on slashes, to handle "keybase.devel"
        // etc.
        if parts[1].to_lowercase().find("keybase").is_none() {
            continue;
        }
        let path: PathBuf = parts[1].into();
        // TODO: We want to check that .kbfs_error exists, but the kbfsfuse
        // mount doesn't currently set "allow_root".
        return Some(path);
    }
    None
}

// Toy implementation for testing on Mac.
#[cfg(target_os = "darwin")]
fn get_keybase_fuse_mount(uid: u32) -> Option<PathBuf> {
    Some("/foo/bar".into())
}

fn main() {
    // The rust-fuse library writes logs using the standard log crate
    // interface. To init a log backend that will actually print those,
    // uncomment this line:
    // env_logger::Builder::new().filter(None, log::LevelFilter::Debug).init();

    let redirector = Redirector {
        start_time: time::get_time(),
    };
    let mountpoint = env::args_os().nth(1).expect("must have a path argument");
    let options = &["-o", "allow_other", "-o", "fsname=keybase-redirector"];
    let options_osstr: Vec<&OsStr> = options.iter().map(|o| o.as_ref()).collect();
    fuse::mount(redirector, &mountpoint, &options_osstr).expect("mount returned an error");
}
