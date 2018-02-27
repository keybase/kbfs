Building requires the FUSE dev headers. Those are installed by default
on e.g. Arch Linux, but on e.g. Ubuntu you'll need the `libfuse-dev`
package. Once you've got that installed, build using the standard Rust
toolchain:

```
cargo build --release
```

To mount:

```
sudo ./target/release/redirector /keybase
```
