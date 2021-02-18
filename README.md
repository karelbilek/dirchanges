# dirchanges

Simple package to tell difference in directory (directories) before and after some operation. It allows to
add a folder recursively.

It detects changes *just by modification date*, not by actual content of the file.
As such is not 100% reliable. Use it only if you can rely on file's modification dates in FS and you don't care
about identical files.

Built by forking github.com/radovskyb/watcher and removing the channels/parallelism/... and keeping just
the diff detection.

As such, this is NOT concurrency safe. It is NOT intended for ongoing notifications about file changes.

See https://github.com/radovskyb/watcher if you want that.

The source code is pretty much unchanged from watcher, just functionality deleted;
so you can still see `watcher` etc there.