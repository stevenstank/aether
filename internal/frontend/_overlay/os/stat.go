package os

// File information and directories (v2 evaluation): gc's Stat, Lstat,
// File.Stat, ReadDir, File.ReadDir, File.Readdir, File.Readdirnames,
// SameFile, CreateTemp, MkdirTemp, and RemoveAll, on Linux x86-64, over
// newfstatat(2) and getdents64(2). A FileInfo's Sys is nil: Aether's
// syscall has no Stat_t.

import (
	"errors"
	"io"
	"io/fs"
	"slices"
	"syscall"
	"time"
)

// A FileInfo describes a file and is returned by Stat and Lstat.
type FileInfo = fs.FileInfo

// A DirEntry is an entry read from a directory.
type DirEntry = fs.DirEntry

// fileInfo is a FileInfo from the kernel's struct stat, as gc's fileStat.
type fileInfo struct {
	name    string
	size    int64
	mode    FileMode
	modTime time.Time
	dev     uint64
	ino     uint64
}

func (fs *fileInfo) Name() string       { return fs.name }
func (fs *fileInfo) IsDir() bool        { return fs.Mode().IsDir() }
func (fs *fileInfo) Size() int64        { return fs.size }
func (fs *fileInfo) Mode() FileMode     { return fs.mode }
func (fs *fileInfo) ModTime() time.Time { return fs.modTime }
func (fs *fileInfo) Sys() any           { return nil }

// The kernel's file types and mode bits (S_IF*, S_IS*).
const (
	sIFMT   = 0xf000
	sIFBLK  = 0x6000
	sIFCHR  = 0x2000
	sIFDIR  = 0x4000
	sIFIFO  = 0x1000
	sIFLNK  = 0xa000
	sIFREG  = 0x8000
	sIFSOCK = 0xc000
	sISGID  = 0x400
	sISUID  = 0x800
	sISVTX  = 0x200
)

// newFileInfo is gc's fillFileStatFromSys.
func newFileInfo(st fileStat, name string) *fileInfo {
	fi := &fileInfo{name: basename(name), size: st.size, dev: st.dev, ino: st.ino,
		modTime: time.Unix(st.mtime[0], st.mtime[1])}
	fi.mode = FileMode(st.mode & 0777)
	switch st.mode & sIFMT {
	case sIFBLK:
		fi.mode |= ModeDevice
	case sIFCHR:
		fi.mode |= ModeDevice | ModeCharDevice
	case sIFDIR:
		fi.mode |= ModeDir
	case sIFIFO:
		fi.mode |= ModeNamedPipe
	case sIFLNK:
		fi.mode |= ModeSymlink
	case sIFSOCK:
		fi.mode |= ModeSocket
	}
	if st.mode&sISGID != 0 {
		fi.mode |= ModeSetgid
	}
	if st.mode&sISUID != 0 {
		fi.mode |= ModeSetuid
	}
	if st.mode&sISVTX != 0 {
		fi.mode |= ModeSticky
	}
	return fi
}

// basename removes trailing slashes and the leading directory name from
// path name, as gc's.
func basename(name string) string {
	i := len(name) - 1
	for ; i > 0 && name[i] == '/'; i-- {
		name = name[:i]
	}
	for i--; i >= 0; i-- {
		if name[i] == '/' {
			name = name[i+1:]
			break
		}
	}
	return name
}

// Stat returns a FileInfo describing the named file.
func Stat(name string) (FileInfo, error) {
	st, err := statName("stat", name, 0)
	if err != nil {
		return nil, err
	}
	return newFileInfo(st, name), nil
}

// Lstat returns a FileInfo describing the named file; a symbolic link is
// described itself.
func Lstat(name string) (FileInfo, error) {
	st, err := statName("lstat", name, atSymlinkNofollow)
	if err != nil {
		return nil, err
	}
	return newFileInfo(st, name), nil
}

// Stat returns the FileInfo structure describing file.
func (f *File) Stat() (FileInfo, error) {
	if f == nil {
		return nil, ErrInvalid
	}
	st, err := fstat(f)
	if err != nil {
		return nil, f.wrapErr("stat", err)
	}
	return newFileInfo(st, f.name), nil
}

// SameFile reports whether fi1 and fi2 describe the same file.
func SameFile(fi1, fi2 FileInfo) bool {
	a, ok1 := fi1.(*fileInfo)
	b, ok2 := fi2.(*fileInfo)
	if !ok1 || !ok2 {
		return false
	}
	return a.dev == b.dev && a.ino == b.ino
}

// dirEntry is a DirEntry of a directory read by getdents64, as gc's
// unixDirent.
type dirEntry struct {
	parent string
	name   string
	typ    FileMode
	info   FileInfo
}

func (d *dirEntry) Name() string   { return d.name }
func (d *dirEntry) IsDir() bool    { return d.typ.IsDir() }
func (d *dirEntry) Type() FileMode { return d.typ }
func (d *dirEntry) String() string { return fs.FormatDirEntry(d) }
func (d *dirEntry) Info() (FileInfo, error) {
	if d.info != nil {
		return d.info, nil
	}
	return Lstat(d.parent + "/" + d.name)
}

// dtype converts a dirent's d_type to a FileMode's type bits, as gc's
// dtToType; unknown is ^FileMode(0).
func dtype(t byte) FileMode {
	switch t {
	case 1: // DT_FIFO
		return ModeNamedPipe
	case 2: // DT_CHR
		return ModeDevice | ModeCharDevice
	case 4: // DT_DIR
		return ModeDir
	case 6: // DT_BLK
		return ModeDevice
	case 8: // DT_REG
		return 0
	case 10: // DT_LNK
		return ModeSymlink
	case 12: // DT_SOCK
		return ModeSocket
	}
	return ^FileMode(0)
}

// dirState is an open directory's entries not yet returned.
type dirState struct {
	entries []DirEntry
	read    bool
}

// readEntries reads all of f's remaining entries, in the kernel's order,
// without "." and "..".
func (f *File) readEntries() ([]*dirEntry, error) {
	buf := make([]byte, 8192)
	var out []*dirEntry
	for {
		n, err := f.fileCall(sysGetdents64, addr(buf), uintptr(len(buf)), 0)
		if err != nil {
			return out, &PathError{Op: "readdirent", Path: f.name, Err: err}
		}
		if n == 0 {
			return out, nil
		}
		for off := 0; off < n; {
			reclen := int(buf[off+16]) | int(buf[off+17])<<8
			typ := buf[off+18]
			name := buf[off+19 : off+reclen]
			if i := indexByte(name, 0); i >= 0 {
				name = name[:i]
			}
			off += reclen
			if s := string(name); s != "." && s != ".." {
				e := &dirEntry{parent: f.name, name: s, typ: dtype(typ)}
				if e.typ == ^FileMode(0) {
					// An unknown type: lstat it, as gc does.
					info, err := Lstat(f.name + "/" + s)
					if err != nil {
						if IsNotExist(err) {
							continue // removed since read
						}
						return out, err
					}
					e.typ, e.info = info.Mode().Type(), info
				}
				out = append(out, e)
			}
		}
	}
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// entries returns the entries of the directory f not yet returned, at
// most n if n > 0, as gc's readdir does.
func (f *File) entries(n int) ([]DirEntry, error) {
	if f == nil {
		return nil, ErrInvalid
	}
	if f.dir == nil {
		f.dir = &dirState{}
	}
	d := f.dir.(*dirState)
	if !d.read {
		es, err := f.readEntries()
		d.entries, d.read = make([]DirEntry, len(es)), true
		for i, e := range es {
			d.entries[i] = e
		}
		if err != nil {
			return nil, err
		}
	}
	es := d.entries
	if n > 0 {
		if len(es) == 0 {
			return nil, io.EOF
		}
		es = es[:min(n, len(es))]
	}
	d.entries = d.entries[len(es):]
	return es, nil
}

// ReadDir reads the contents of the directory associated with the file f
// and returns a slice of DirEntry values in directory order.
func (f *File) ReadDir(n int) ([]DirEntry, error) {
	es, err := f.entries(n)
	return append([]DirEntry(nil), es...), err
}

// Readdirnames reads the contents of the directory associated with file
// and returns a slice of up to n names of files in the directory.
func (f *File) Readdirnames(n int) ([]string, error) {
	es, err := f.entries(n)
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name()
	}
	return out, err
}

// Readdir reads the contents of the directory associated with file and
// returns a slice of up to n FileInfo values, as would be returned by
// Lstat, in directory order.
func (f *File) Readdir(n int) ([]FileInfo, error) {
	es, err := f.entries(n)
	out := make([]FileInfo, 0, len(es))
	for _, e := range es {
		info, ierr := e.Info()
		if ierr != nil {
			if IsNotExist(ierr) {
				continue
			}
			return out, ierr
		}
		out = append(out, info)
	}
	return out, err
}

// ReadDir reads the named directory, returning all its directory entries
// sorted by filename.
func ReadDir(name string) ([]DirEntry, error) {
	f, err := Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dirs, err := f.ReadDir(-1)
	slices.SortFunc(dirs, func(a, b DirEntry) int {
		switch x, y := a.Name(), b.Name(); {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	})
	return dirs, err
}

var errPatternHasSeparator = errors.New("pattern contains path separator")

// prefixAndSuffix splits pattern by the last wildcard "*", as gc's.
func prefixAndSuffix(pattern string) (prefix, suffix string, err error) {
	for i := 0; i < len(pattern); i++ {
		if IsPathSeparator(pattern[i]) {
			return "", "", errPatternHasSeparator
		}
	}
	if pos := lastIndexByte(pattern, '*'); pos != -1 {
		prefix, suffix = pattern[:pos], pattern[pos+1:]
	} else {
		prefix = pattern
	}
	return prefix, suffix, nil
}

func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// tempSeq is the state of the random part of temporary names. gc's comes
// from its runtime's generator; Aether's mixes the clock and the process
// ID into a counter, so names differ from run to run, as gc's.
var tempSeq uint64

func nextRandom() string {
	if tempSeq == 0 {
		tempSeq = uint64(time.Now().UnixNano()) ^ uint64(syscall.Getpid())<<32
	}
	tempSeq = tempSeq*6364136223846793005 + 1442695040888963407
	return uitoa(uint(uint32(tempSeq >> 32)))
}

func uitoa(v uint) string {
	var buf [20]byte
	i := len(buf) - 1
	for v >= 10 {
		buf[i] = byte('0' + v%10)
		i--
		v /= 10
	}
	buf[i] = byte('0' + v)
	return string(buf[i:])
}

func joinPath(dir, name string) string {
	if len(dir) > 0 && IsPathSeparator(dir[len(dir)-1]) {
		return dir + name
	}
	return dir + string(PathSeparator) + name
}

// CreateTemp creates a new temporary file in the directory dir, opens it
// for reading and writing, and returns the resulting file.
func CreateTemp(dir, pattern string) (*File, error) {
	if dir == "" {
		dir = TempDir()
	}
	prefix, suffix, err := prefixAndSuffix(pattern)
	if err != nil {
		return nil, &PathError{Op: "createtemp", Path: pattern, Err: err}
	}
	prefix = joinPath(dir, prefix)
	try := 0
	for {
		name := prefix + nextRandom() + suffix
		f, err := OpenFile(name, O_RDWR|O_CREATE|O_EXCL, 0600)
		if IsExist(err) {
			if try++; try < 10000 {
				continue
			}
			return nil, &PathError{Op: "createtemp", Path: prefix + "*" + suffix, Err: ErrExist}
		}
		return f, err
	}
}

// MkdirTemp creates a new temporary directory in the directory dir and
// returns the pathname of the new directory.
func MkdirTemp(dir, pattern string) (string, error) {
	if dir == "" {
		dir = TempDir()
	}
	prefix, suffix, err := prefixAndSuffix(pattern)
	if err != nil {
		return "", &PathError{Op: "mkdirtemp", Path: pattern, Err: err}
	}
	prefix = joinPath(dir, prefix)
	try := 0
	for {
		name := prefix + nextRandom() + suffix
		err := Mkdir(name, 0700)
		if err == nil {
			return name, nil
		}
		if IsExist(err) {
			if try++; try < 10000 {
				continue
			}
			return "", &PathError{Op: "mkdirtemp", Path: dir + string(PathSeparator) + prefix + "*" + suffix, Err: ErrExist}
		}
		if IsNotExist(err) {
			if _, err := Stat(dir); IsNotExist(err) {
				return "", err
			}
		}
		return "", err
	}
}

// RemoveAll removes path and any children it contains. It removes
// everything it can but returns the first error it encounters. If the
// path does not exist, RemoveAll returns nil.
func RemoveAll(path string) error {
	if path == "" {
		return nil
	}
	if path == "." || len(path) >= 2 && path[len(path)-2:] == "/." {
		return &PathError{Op: "RemoveAll", Path: path, Err: syscall.Errno(einval)}
	}
	err := Remove(path)
	if err == nil || IsNotExist(err) {
		return nil
	}
	fi, serr := Lstat(path)
	if serr != nil {
		if IsNotExist(serr) {
			return nil
		}
		return serr
	}
	if !fi.IsDir() {
		return err
	}
	es, rerr := ReadDir(path)
	var first error
	for _, e := range es {
		if err := RemoveAll(joinPath(path, e.Name())); err != nil && first == nil {
			first = err
		}
	}
	if rerr != nil && first == nil {
		first = rerr
	}
	if err := Remove(path); err != nil && !IsNotExist(err) && first == nil {
		first = err
	}
	return first
}

const sysReadlinkat = 267

// Readlink returns the destination of the named symbolic link.
func Readlink(name string) (string, error) {
	p, err := cstring(name)
	if err != nil {
		return "", &PathError{Op: "readlink", Path: name, Err: err}
	}
	for n := 128; ; n *= 2 {
		b := make([]byte, n)
		r, err := call(sysReadlinkat, atFDCWD, addr(p), addr(b), uintptr(n))
		if err != nil {
			return "", &PathError{Op: "readlink", Path: name, Err: err}
		}
		if r < n {
			return string(b[:r]), nil
		}
	}
}
