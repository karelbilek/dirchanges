package dirchanges

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	ErrWatchedFileDeleted = errors.New("error: watched file or folder deleted")
	// ErrSkip is less of an error, but more of a way for path hooks to skip a file or
	// directory.
	ErrSkip = errors.New("error: skipping file")
)

// An Op is a type that is used to describe what type
// of event has occurred during the watching process.
type Op uint32

// Ops
const (
	Create Op = iota
	Write
	Remove
	Rename
	Chmod
	Move
)

var ops = map[Op]string{
	Create: "CREATE",
	Write:  "WRITE",
	Remove: "REMOVE",
	Rename: "RENAME",
	Chmod:  "CHMOD",
	Move:   "MOVE",
}

// String prints the string version of the Op consts
func (e Op) String() string {
	if op, found := ops[e]; found {
		return op
	}
	return "???"
}

// An Event describes an event that is received when files or directory
// changes occur. It includes the os.FileInfo of the changed file or
// directory and the type of event that's occurred and the full path of the file.
type Event struct {
	Op
	Path    string
	OldPath string
	os.FileInfo
}

// String returns a string depending on what type of event occurred and the
// file name associated with the event.
func (e Event) String() string {
	if e.FileInfo == nil {
		return "???"
	}

	pathType := "FILE"
	if e.IsDir() {
		pathType = "DIRECTORY"
	}
	return fmt.Sprintf("%s %q %s [%s]", pathType, e.Name(), e.Op, e.Path)
}

// FilterFileHookFunc is a function that is called to filter files during listings.
// If a file is ok to be listed, nil is returned otherwise ErrSkip is returned.
type FilterFileHookFunc func(info os.FileInfo, fullPath string) error

// RegexFilterHook is a function that accepts or rejects a file
// for listing based on whether it's filename or full path matches
// a regular expression.
func RegexFilterHook(r *regexp.Regexp, useFullPath bool) FilterFileHookFunc {
	return func(info os.FileInfo, fullPath string) error {
		str := info.Name()

		if useFullPath {
			str = fullPath
		}

		// Match
		if r.MatchString(str) {
			return nil
		}

		// No match.
		return ErrSkip
	}
}

// Watcher describes a process that watches files for changes.
type Watcher struct {
	ffh          []FilterFileHookFunc
	names        map[string]bool        // bool for recursive or not.
	files        map[string]os.FileInfo // map of files.
	ignored      map[string]struct{}    // ignored files or directories.
	ops          map[Op]struct{}        // Op filtering.
	ignoreHidden bool                   // ignore hidden files or not.
}

// New creates a new Watcher.
func New() *Watcher {
	return &Watcher{
		files:   make(map[string]os.FileInfo),
		ignored: make(map[string]struct{}),
		names:   make(map[string]bool),
	}
}

// AddFilterHook
func (w *Watcher) AddFilterHook(f FilterFileHookFunc) {
	w.ffh = append(w.ffh, f)
}

// IgnoreHiddenFiles sets the watcher to ignore any file or directory
// that starts with a dot.
func (w *Watcher) IgnoreHiddenFiles(ignore bool) {
	w.ignoreHidden = ignore
}

// FilterOps filters which event op types should be returned
// when an event occurs.
func (w *Watcher) FilterOps(ops ...Op) {
	w.ops = make(map[Op]struct{})
	for _, op := range ops {
		w.ops[op] = struct{}{}
	}
}

func (w *Watcher) list(name string) (map[string]os.FileInfo, error) {
	fileList := make(map[string]os.FileInfo)

	// Make sure name exists.
	stat, err := os.Stat(name)
	if err != nil {
		return nil, err
	}

	fileList[name] = stat

	// If it's not a directory, just return.
	if !stat.IsDir() {
		return fileList, nil
	}

	// It's a directory.
	fInfoList, err := ioutil.ReadDir(name)
	if err != nil {
		return nil, err
	}
	// Add all of the files in the directory to the file list as long
	// as they aren't on the ignored list or are hidden files if ignoreHidden
	// is set to true.
outer:
	for _, fInfo := range fInfoList {
		path := filepath.Join(name, fInfo.Name())
		_, ignored := w.ignored[path]

		isHidden, err := isHiddenFile(path)
		if err != nil {
			return nil, err
		}

		if ignored || (w.ignoreHidden && isHidden) {
			continue
		}

		for _, f := range w.ffh {
			err := f(fInfo, path)
			if err == ErrSkip {
				continue outer
			}
			if err != nil {
				return nil, err
			}
		}

		fileList[path] = fInfo
	}
	return fileList, nil
}

func (w *Watcher) AddRecursive(name string) (err error) {
	name, err = filepath.Abs(name)
	if err != nil {
		return err
	}

	fileList, err := w.listRecursive(name)
	if err != nil {
		return err
	}
	for k, v := range fileList {
		w.files[k] = v
	}

	// Add the name to the names list.
	w.names[name] = true

	return nil
}

func (w *Watcher) listRecursive(name string) (map[string]os.FileInfo, error) {
	fileList := make(map[string]os.FileInfo)

	return fileList, filepath.Walk(name, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		for _, f := range w.ffh {
			err := f(info, path)
			if err == ErrSkip {
				return nil
			}
			if err != nil {
				return err
			}
		}

		// If path is ignored and it's a directory, skip the directory. If it's
		// ignored and it's a single file, skip the file.
		_, ignored := w.ignored[path]

		isHidden, err := isHiddenFile(path)
		if err != nil {
			return err
		}

		if ignored || (w.ignoreHidden && isHidden) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Add the path and it's info to the file list.
		fileList[path] = info
		return nil
	})
}

// Remove removes either a single file or directory from the file's list.
func (w *Watcher) Remove(name string) (err error) {

	name, err = filepath.Abs(name)
	if err != nil {
		return err
	}

	// Remove the name from w's names list.
	delete(w.names, name)

	// If name is a single file, remove it and return.
	info, found := w.files[name]
	if !found {
		return nil // Doesn't exist, just return.
	}
	if !info.IsDir() {
		delete(w.files, name)
		return nil
	}

	// Delete the actual directory from w.files
	delete(w.files, name)

	// If it's a directory, delete all of it's contents from w.files.
	for path := range w.files {
		if filepath.Dir(path) == name {
			delete(w.files, path)
		}
	}
	return nil
}

// RemoveRecursive removes either a single file or a directory recursively from
// the file's list.
func (w *Watcher) RemoveRecursive(name string) (err error) {

	name, err = filepath.Abs(name)
	if err != nil {
		return err
	}

	// Remove the name from w's names list.
	delete(w.names, name)

	// If name is a single file, remove it and return.
	info, found := w.files[name]
	if !found {
		return nil // Doesn't exist, just return.
	}
	if !info.IsDir() {
		delete(w.files, name)
		return nil
	}

	// If it's a directory, delete all of it's contents recursively
	// from w.files.
	for path := range w.files {
		if strings.HasPrefix(path, name) {
			delete(w.files, path)
		}
	}
	return nil
}

// Ignore adds paths that should be ignored.
//
// For files that are already added, Ignore removes them.
func (w *Watcher) Ignore(paths ...string) (err error) {
	for _, path := range paths {
		path, err = filepath.Abs(path)
		if err != nil {
			return err
		}
		// Remove any of the paths that were already added.
		if err := w.RemoveRecursive(path); err != nil {
			return err
		}
		w.ignored[path] = struct{}{}
	}
	return nil
}

// WatchedFiles returns a map of files added to a Watcher.
func (w *Watcher) WatchedFiles() map[string]os.FileInfo {

	files := make(map[string]os.FileInfo)
	for k, v := range w.files {
		files[k] = v
	}

	return files
}

// fileInfo is an implementation of os.FileInfo that can be used
// as a mocked os.FileInfo when triggering an event when the specified
// os.FileInfo is nil.
type fileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	sys     interface{}
	dir     bool
}

func (fs *fileInfo) IsDir() bool {
	return fs.dir
}
func (fs *fileInfo) ModTime() time.Time {
	return fs.modTime
}
func (fs *fileInfo) Mode() os.FileMode {
	return fs.mode
}
func (fs *fileInfo) Name() string {
	return fs.name
}
func (fs *fileInfo) Size() int64 {
	return fs.size
}
func (fs *fileInfo) Sys() interface{} {
	return fs.sys
}

// Add adds either a single file or directory to the file list.
func (w *Watcher) Add(name string) (err error) {

	name, err = filepath.Abs(name)
	if err != nil {
		return err
	}

	// If name is on the ignored list or if hidden files are
	// ignored and name is a hidden file or directory, simply return.
	_, ignored := w.ignored[name]

	isHidden, err := isHiddenFile(name)
	if err != nil {
		return err
	}

	if ignored || (w.ignoreHidden && isHidden) {
		return nil
	}

	// Add the directory's contents to the files list.
	fileList, err := w.list(name)
	if err != nil {
		return err
	}
	for k, v := range fileList {
		w.files[k] = v
	}

	// Add the name to the names list.
	w.names[name] = false

	return nil
}

func (w *Watcher) retrieveFileList() (map[string]os.FileInfo, error) {

	fileList := make(map[string]os.FileInfo)

	var list map[string]os.FileInfo
	var err error

	for name, recursive := range w.names {
		if recursive {
			list, err = w.listRecursive(name)
			if err != nil {
				if os.IsNotExist(err) {
					if name == err.(*os.PathError).Path {
						return nil, ErrWatchedFileDeleted
						w.RemoveRecursive(name)
					}
				} else {
					return nil, err
				}
			}
		} else {
			list, err = w.list(name)
			if err != nil {
				if os.IsNotExist(err) {
					if name == err.(*os.PathError).Path {
						return nil, ErrWatchedFileDeleted
						w.Remove(name)
					}
				} else {
					return nil, err
				}
			}
		}
		// Add the file's to the file list.
		for k, v := range list {
			fileList[k] = v
		}
	}

	return fileList, nil
}

func (w *Watcher) Diff() ([]Event, error) {

	fileList, err := w.retrieveFileList()
	if err != nil {
		return nil, err
	}
	diff := w.getDiff(fileList)
	return diff, nil
}

func (w *Watcher) getDiff(files map[string]os.FileInfo) []Event {

	var res []Event

	// Store create and remove events for use to check for rename events.
	creates := make(map[string]os.FileInfo)
	removes := make(map[string]os.FileInfo)

	// Check for removed files.
	for path, info := range w.files {
		if _, found := files[path]; !found {
			removes[path] = info
		}
	}

	// Check for created files, writes and chmods.
	for path, info := range files {
		oldInfo, found := w.files[path]
		if !found {
			// A file was created.
			creates[path] = info
			continue
		}
		if oldInfo.ModTime() != info.ModTime() {
			res = append(res, Event{Write, path, path, info})

		}
		if oldInfo.Mode() != info.Mode() {
			res = append(res, Event{Chmod, path, path, info})
		}
	}

	// Check for renames and moves.
	for path1, info1 := range removes {
		for path2, info2 := range creates {
			if sameFile(info1, info2) {
				e := Event{
					Op:       Move,
					Path:     path2,
					OldPath:  path1,
					FileInfo: info1,
				}
				// If they are from the same directory, it's a rename
				// instead of a move event.
				if filepath.Dir(path1) == filepath.Dir(path2) {
					e.Op = Rename
				}

				delete(removes, path1)
				delete(creates, path2)

				res = append(res, e)

			}
		}
	}

	// Send all the remaining create and remove events.
	for path, info := range creates {
		res = append(res, Event{Create, path, "", info})
	}
	for path, info := range removes {
		res = append(res, Event{Remove, path, path, info})
	}

	var filteredRes = res
	if len(w.ops) > 0 { // Filter Ops.
		filteredRes = nil
		for _, event := range res {
			_, found := w.ops[event.Op]
			if found {
				filteredRes = append(filteredRes, event)
			}
		}
	}
	return filteredRes
}
