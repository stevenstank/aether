// Files by name: create, read, write, seek, positional I/O, truncation,
// directories, renames, removal, and gc's errors for each. The test runs it
// in a fresh directory holding in.txt, with os.Args[1] = "in.txt".
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

func check(what string, err error) {
	fmt.Printf("%s: %v", what, err)
	if err != nil {
		fmt.Printf(" (exist %v, notexist %v, is-notexist %v, is-exist %v)",
			os.IsExist(err), os.IsNotExist(err), errors.Is(err, os.ErrNotExist), errors.Is(err, os.ErrExist))
		var pe *os.PathError
		if errors.As(err, &pe) {
			fmt.Printf(" [path op %s path %q]", pe.Op, pe.Path)
		}
		var le *os.LinkError
		if errors.As(err, &le) {
			fmt.Printf(" [link op %s %q %q]", le.Op, le.Old, le.New)
		}
	}
	fmt.Println()
}

func main() {
	in, err := os.ReadFile(os.Args[1])
	check("read arg", err)
	fmt.Printf("%d bytes, %q\n", len(in), in[:20])

	_, err = os.ReadFile("missing.txt")
	check("read missing", err)
	_, err = os.ReadFile(".")
	check("read dir", err)
	_, err = os.Open("bad\x00name")
	check("open NUL", err)

	check("write", os.WriteFile("out.txt", []byte("0123456789\n"), 0o640))
	f, err := os.OpenFile("out.txt", os.O_RDWR, 0)
	check("open rw", err)
	buf := make([]byte, 4)
	n, err := f.ReadAt(buf, 3)
	fmt.Println("readat", n, string(buf))
	check("readat", err)
	n, err = f.ReadAt(buf, 9)
	fmt.Println("readat end", n, string(buf[:n]))
	check("readat end", err)
	_, err = f.ReadAt(buf, -1)
	check("readat negative", err)
	n, err = f.WriteAt([]byte("AB"), 4)
	fmt.Println("writeat", n)
	check("writeat", err)
	off, err := f.Seek(2, io.SeekStart)
	fmt.Println("seek", off)
	check("seek", err)
	_, err = f.Seek(-1, io.SeekStart)
	check("seek negative", err)
	rest, err := io.ReadAll(f)
	fmt.Printf("rest %q\n", rest)
	check("readall", err)
	check("truncate", f.Truncate(6))
	check("sync", f.Sync())
	off, _ = f.Seek(0, io.SeekEnd)
	fmt.Println("size", off)
	fd := f.Fd()
	check("close", f.Close())
	check("close again", f.Close())
	_, err = f.Write([]byte("x"))
	check("write closed", err)
	_, err = f.Read(buf)
	check("read closed", err)
	_, err = f.Seek(0, io.SeekStart)
	check("seek closed", err)
	fmt.Println("fd", fd > 2, f.Fd() == ^uintptr(0)) // gc's runtime holds descriptors of its own

	var nilf *os.File
	_, err = nilf.Read(buf)
	check("read nil", err)
	check("close nil", nilf.Close())

	_, err = os.OpenFile("out.txt", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	check("create excl", err)
	g, err := os.Create("out.txt")
	check("create", err)
	g.WriteString("short\n")
	g.Close()
	b, _ := os.ReadFile("out.txt")
	fmt.Printf("after create %q\n", b)

	a, _ := os.OpenFile("log.txt", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	a.WriteString("one\n")
	_, err = a.WriteAt([]byte("x"), 0)
	check("writeat append", err)
	a.WriteString("two\n")
	a.Close()
	a, _ = os.OpenFile("log.txt", os.O_WRONLY|os.O_APPEND, 0)
	fmt.Fprintf(a, "three %d\n", 3)
	a.Close()
	b, _ = os.ReadFile("log.txt")
	fmt.Printf("log %q\n", b)

	check("mkdir", os.Mkdir("d", 0o750))
	check("mkdir again", os.Mkdir("d", 0o750))
	check("mkdir under file", os.Mkdir("out.txt/x", 0o750))
	check("mkdirall", os.MkdirAll("d/e/f/", 0o750))
	check("mkdirall exists", os.MkdirAll("d/e", 0o750))
	check("mkdirall dot", os.MkdirAll("d/e/.", 0o750))
	check("mkdirall file", os.MkdirAll("out.txt", 0o750))
	check("mkdirall under file", os.MkdirAll("out.txt/y/z", 0o750))
	check("rename", os.Rename("out.txt", "d/moved.txt"))
	check("rename missing", os.Rename("nope", "d/x"))
	check("rename onto dir", os.Rename("log.txt", "d/e"))
	check("rename missing onto dir", os.Rename("nope", "d/e"))
	check("rename dir onto itself", os.Rename("d/e", "d/e"))
	check("rename dir", os.Rename("d/e", "d/g"))
	check("remove nonempty", os.Remove("d"))
	check("remove file", os.Remove("d/moved.txt"))
	check("remove dir", os.Remove("d/g/f"))
	check("remove missing", os.Remove("zz"))
	check("remove under file", os.Remove("log.txt/x"))
	check("write in missing dir", os.WriteFile("no/such/file", nil, 0o600))
}
