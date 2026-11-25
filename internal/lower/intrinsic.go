package lower

import (
	"go/ast"
	"go/types"

	"github.com/stevenstank/aether/internal/frontend"
	"github.com/stevenstank/aether/internal/ir"
)

// intrinsicKey returns the key of fd in frontend.Intrinsics: its package
// path and name.
func intrinsicKey(info *types.Info, fd *ast.FuncDecl) string {
	obj := info.Defs[fd.Name]
	if obj == nil || fd.Recv != nil {
		return ""
	}
	return obj.Pkg().Path() + "." + obj.Name()
}

// reached reports whether the program, pruned to what main reaches,
// declares the intrinsic key.
func reached(pkg *frontend.Package, key string) bool {
	for _, f := range pkg.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body == nil && intrinsicKey(pkg.Info, fd) == key {
				return true
			}
		}
	}
	return false
}

// yield puts the running goroutine at the run queue's tail, after the
// others, and runs the next, unless there is no other to run.
func (l *lowerer) yield(pos ir.Pos) {
	sc, m := l.sched(), mb{l, pos}
	other := m.when(m.cmp(ir.Eq, l.fn.GLoad(l.b, sc.runnext, pos), m.nilPtr()))
	queued := m.when(m.cmp(ir.Eq, l.fn.GLoad(l.b, sc.runqHead, pos), m.nilPtr()))
	done := l.fn.NewBlock()
	m.join(done)
	l.b = queued
	m.join(other)
	l.runqPut(m.v("g", l.fn.Call(l.b, sc.getg, nil, pos)), pos)
	l.fn.Call(l.b, sc.park, nil, pos)
	m.join(done)
}

// intrinsic generates the body of fd, a function an overlay file declares
// without one (D-049).
func (l *lowerer) intrinsic(fd *ast.FuncDecl) {
	obj := l.pkg.Info.Defs[fd.Name].(*types.Func)
	sig := obj.Type().(*types.Signature)
	l.fn = l.funcs[obj]
	b, pos := l.fn.Entry(), l.fn.Pos
	switch intrinsicKey(l.pkg.Info, fd) {
	case "math.Float32bits", "math.Float32frombits", "math.Float64bits", "math.Float64frombits",
		"internal/strconv.float32bits", "internal/strconv.float32frombits",
		"internal/strconv.float64bits", "internal/strconv.float64frombits":
		// The bits of the parameter, as the result's type: stored in a
		// memory object and loaded as the other type of the same size.
		in, out := sig.Params().At(0).Type(), sig.Results().At(0).Type()
		if sizes.Sizeof(in) != sizes.Sizeof(out) || len(l.fn.Params) != 1 || len(l.fn.Results) != 1 {
			l.fail(fd, "intrinsic %s of types %s and %s", obj.Name(), in, out)
		}
		x := l.fn.Param(b, 0, pos) // the parameter first (ABI-R1)
		m := l.fn.NewMem("bits", sizes.Sizeof(in), sizes.Alignof(in), pos)
		a, zero := l.fn.Addr(b, m, pos), l.fn.Const(b, 0, pos)
		l.fn.StM(b, a, zero, x, pos)
		l.fn.Ret(b, pos, l.fn.LdM(b, l.fn.Results[0], a, zero, pos))
	case "runtime.GC", "runtime.Gosched":
		// GC collects, then, as gc's blocks its caller while it runs,
		// yields as Gosched does.
		l.b = b
		if obj.Name() == "GC" {
			l.fn.Sys(l.b, ir.SysGC, ir.NoType, pos)
		}
		l.yield(pos)
		l.fn.Ret(l.b, pos)
	case "runtime.NumGoroutine":
		// Main and the goroutines its scheduler counts (sched.ngo).
		l.b = b
		sc := l.sched()
		l.fn.Ret(l.b, pos, l.fn.Binary(l.b, ir.Add, l.fn.GLoad(l.b, sc.ngo, pos), l.fn.Const(l.b, 1, pos), pos))
	case "os.Exit":
		l.fn.Sys(b, ir.SysExit, ir.NoType, pos, l.fn.Param(b, 0, pos))
		l.fn.Ret(b, pos)
	case "os.argc":
		l.fn.Ret(b, pos, l.fn.Sys(b, ir.SysArgc, ir.I64, pos))
	case "os.arg":
		i := l.fn.Param(b, 0, pos)
		l.fn.Ret(b, pos, l.fn.Sys(b, ir.SysArgPtr, ir.Ptr, pos, i), l.fn.Sys(b, ir.SysArgLen, ir.I64, pos, i))
	case "os.write", "os.read":
		// write(fd int, b []byte) int, and read: the slice's words are
		// its address, length, and capacity.
		fdv, p, n := l.fn.Param(b, 0, pos), l.fn.Param(b, 1, pos), l.fn.Param(b, 2, pos)
		kind := ir.SysWrite
		if obj.Name() == "read" {
			kind = ir.SysRead
		}
		l.fn.Ret(b, pos, l.fn.Sys(b, kind, ir.I64, pos, fdv, p, n))
	case "time.setChanTimer":
		// setChanTimer(c, t unsafe.Pointer) records the timer of a channel
		// (D-054).
		c, t := l.fn.Param(b, 0, pos), l.fn.Param(b, 1, pos)
		l.fn.StM(b, c, l.fn.Const(b, chTimer, pos), t, pos)
		l.fn.Ret(b, pos)
	case "os.syscall6", "syscall.rawSyscall6", "time.syscall6":
		// syscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) int (D-051).
		args := make([]*ir.Instr, 7)
		for i := range args {
			args[i] = l.fn.Param(b, i, pos)
		}
		l.fn.Ret(b, pos, l.fn.Sys(b, ir.SysCall, ir.I64, pos, args...))
	case "math/rand.runtime_rand":
		// runtime_rand() uint64, gc's runtime.rand (D-053).
		l.fn.Ret(b, pos, l.fn.Sys(b, ir.SysRand, ir.U64, pos))
	case "syscall.envc", "math.envc", "internal/godebug.envc", "runtime.envc":
		l.fn.Ret(b, pos, l.fn.Sys(b, ir.SysEnvc, ir.I64, pos))
	case "math.cpuFMA":
		// cpuFMA() int (D-053).
		l.fn.Ret(b, pos, l.fn.Sys(b, ir.SysCPUFMA, ir.I64, pos))
	case "syscall.envString", "math.env", "internal/godebug.env", "runtime.envString":
		i := l.fn.Param(b, 0, pos)
		l.fn.Ret(b, pos, l.fn.Sys(b, ir.SysEnvPtr, ir.Ptr, pos, i), l.fn.Sys(b, ir.SysEnvLen, ir.I64, pos, i))
	case "internal/reflectlite.implements":
		// implements(iface, t unsafe.Pointer) unsafe.Pointer: the itab of
		// the type t and the non-empty interface iface, by iface's .findK,
		// whose address its descriptor holds (reflect.go, D-050), or nil.
		iface, t := l.fn.Param(b, 0, pos), l.fn.Param(b, 1, pos)
		code := l.fn.LdM(b, ir.Ptr, iface, l.fn.Const(b, 96, pos), pos)
		find := &ir.Sig{Params: []ir.Type{ir.Ptr, ir.Ptr}, Results: []ir.Type{ir.Ptr}}
		l.fn.Ret(b, pos, l.fn.CallInd(b, find, code, []*ir.Instr{t, t}, pos))
	case "sync.fatal":
		l.fn.Panic(b, ir.PanicFatal, pos, l.fn.Param(b, 0, pos), l.fn.Param(b, 1, pos))
	default:
		l.fail(fd, "unknown intrinsic %s", obj.Name())
	}
	l.fn = nil
}
