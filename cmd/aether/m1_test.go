package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stevenstank/aether/internal/amd64"
)

// The Milestone 1 acceptance test (docs/ROADMAP.md §Milestone 1 acceptance
// test). It runs the aether binary as a user would, and checks P1-P6.

// cmdResult is the outcome of a command run by the harness.
type cmdResult struct {
	stdout, stderr string
	code           int
}

// command runs name with args in dir. A nil env inherits the test's
// environment.
func command(t *testing.T, dir string, env []string, name string, args ...string) cmdResult {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	r := cmdResult{}
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("%s %v: %v", name, args, err)
		}
		r.code = exit.ExitCode()
	}
	r.stdout, r.stderr = stdout.String(), stderr.String()
	return r
}

// aetherBinary builds the aether command into a temporary directory.
func aetherBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "aether")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// gnuTool is how the harness itself resolves and identifies a tool,
// independently of internal/toolchain.
func gnuTool(t *testing.T, name, versionFlag, prefix string) (path, version string) {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not found", name)
	}
	if path, err = filepath.Abs(path); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path, versionFlag).Output()
	if err != nil {
		t.Fatal(err)
	}
	version, _, _ = strings.Cut(string(out), "\n")
	if !strings.HasPrefix(version, prefix) {
		t.Fatalf("%s is not %s: %q", path, prefix, version)
	}
	return path, version
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// workDir returns the WORK= directory printed by aether build -work.
func workDir(t *testing.T, stderr string) string {
	t.Helper()
	work := strings.TrimPrefix(firstLine(stderr, "WORK="), "WORK=")
	if work == "" {
		t.Fatalf("no WORK= line:\n%s", stderr)
	}
	t.Cleanup(func() { os.RemoveAll(work) })
	return work
}

// logCommands returns the commands in a -x log, without comments, the cd
// line, and the WORK= line.
func logCommands(log string) []string {
	var cmds []string
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		if !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "cd ") && !strings.HasPrefix(line, "WORK=") {
			cmds = append(cmds, line)
		}
	}
	return cmds
}

func TestMilestone1(t *testing.T) {
	if testing.Short() {
		t.Skip("builds with gc and GNU binutils")
	}
	requireTools(t)
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	asPath, asVersion := gnuTool(t, "as", "--version", "GNU assembler")
	ldPath, ldVersion := gnuTool(t, "ld", "-v", "GNU ld")
	t.Logf("as: %s (%s); ld: %s (%s)", asPath, asVersion, ldPath, ldVersion)
	bin := aetherBinary(t)
	corpusPath := corpusFile("s1_03_milestone1")
	src := readFile(t, corpusPath)

	// Step 1: the gc reference, built elsewhere and used only as the oracle.
	refDir := t.TempDir()
	writeFile(t, filepath.Join(refDir, "m1_arith.go"), src)
	refEnv := append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=")
	if r := command(t, refDir, refEnv, gobin, "build", "-o", "ref", "m1_arith.go"); r.code != 0 {
		t.Fatalf("gc build: %s", r.stderr)
	}
	ref := runExe(t, filepath.Join(refDir, "ref"))
	if want := (result{stderr: "-6 42\n6 -9223372036854775808\n"}); ref != want {
		t.Fatalf("gc reference %+v differs from SEMANTICS.md case S1-03 %+v", ref, want)
	}

	// Step 2: check and build in a fresh directory.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "m1_arith.go"), src)
	if r := command(t, dir, nil, bin, "check", "m1_arith.go"); r.code != 0 || r.stderr != "" || r.stdout != "" {
		t.Fatalf("P1: aether check: %+v", r)
	}
	noFile(t, filepath.Join(dir, "out"))
	b := command(t, dir, nil, bin, "build", "-x", "-work", "-o", "out", "m1_arith.go")
	if b.code != 0 || b.stdout != "" {
		t.Fatalf("aether build: %+v", b)
	}
	work := workDir(t, b.stderr)
	out := readFile(t, filepath.Join(dir, "out"))

	// P1: accepted, checked, and lowered by Aether, in-process.
	prog, ds := compile([]source{{corpusPath, src}})
	if len(ds) > 0 {
		t.Fatalf("P1: %v", ds)
	}
	if got, want := prog.String(), string(readFile(t, strings.TrimSuffix(corpusPath, ".go")+".ir")); got != want {
		t.Errorf("P1: IR differs from the reviewed golden file:\n%s", got)
	}
	if interp, _, err := interpret(corpusPath, src); err != nil || interp != ref {
		t.Errorf("P1: interpreter %+v (%v), gc %+v", interp, err, ref)
	}

	// P2: Aether's backend emitted the program's instructions.
	asm, err := amd64.Emit(prog)
	if err != nil {
		t.Fatal(err)
	}
	progS := readFile(t, filepath.Join(work, "prog.s"))
	if !bytes.Equal(asm, progS) {
		t.Error("P2: prog.s differs from the in-process backend output")
	}
	if !bytes.Contains(progS, []byte("\nmain.main:\n")) {
		t.Error("P2: prog.s does not define main.main")
	}
	rtDir := filepath.Join("..", "..", "internal", "amd64", "rt")
	rtFiles, _ := filepath.Glob(filepath.Join(rtDir, "*.s"))
	if len(rtFiles) == 0 {
		t.Fatal("P2: no runtime sources in the repository")
	}
	for _, f := range rtFiles {
		repo := readFile(t, f)
		if !bytes.Equal(repo, readFile(t, filepath.Join(work, filepath.Base(f)))) {
			t.Errorf("P2: work directory %s differs from the repository", filepath.Base(f))
		}
		if bytes.Contains(repo, []byte("main.main:")) {
			t.Errorf("P2: runtime %s defines main.main", f)
		}
	}

	// P3: the harness rebuilds the executable itself from the in-process
	// backend output and the repository runtime, with GNU as and ld only.
	re := t.TempDir()
	writeFile(t, filepath.Join(re, "prog.s"), asm)
	objs := []string{"prog.o"}
	harnessCmds := []string{"LC_ALL=C " + asPath + " --64 -o prog.o prog.s"}
	for _, f := range rtFiles {
		name := filepath.Base(f)
		writeFile(t, filepath.Join(re, name), readFile(t, f))
		obj := strings.TrimSuffix(name, ".s") + ".o"
		objs = append(objs, obj)
		harnessCmds = append(harnessCmds, "LC_ALL=C "+asPath+" --64 -o "+obj+" "+name)
	}
	for i, obj := range objs {
		srcName := strings.TrimSuffix(obj, ".o") + ".s"
		if r := command(t, re, []string{"LC_ALL=C"}, asPath, "--64", "-o", obj, srcName); r.code != 0 {
			t.Fatalf("P3: as %s: %s", objs[i], r.stderr)
		}
	}
	ldArgs := append([]string{"-static", "-nostdlib", "-e", "_start", "-o", "a.out"}, objs...)
	harnessCmds = append(harnessCmds, "LC_ALL=C "+ldPath+" "+strings.Join(ldArgs, " "))
	if r := command(t, re, []string{"LC_ALL=C"}, ldPath, ldArgs...); r.code != 0 {
		t.Fatalf("P3: ld: %s", r.stderr)
	}
	if !bytes.Equal(readFile(t, filepath.Join(re, "a.out")), out) {
		t.Error("P3: the independently reconstructed executable differs from out")
	}

	// P4: the executable behaves like gc's and the interpreter's.
	if got := runExe(t, filepath.Join(dir, "out")); got != ref {
		t.Errorf("P4: out %+v, gc %+v", got, ref)
	}

	// Supporting evidence: the command log matches the harness's commands,
	// after the two identification commands.
	wantLog := append([]string{"LC_ALL=C " + asPath + " --version", "LC_ALL=C " + ldPath + " -V"}, harnessCmds...)
	if got := logCommands(b.stderr); !slices.Equal(got, wantLog) {
		t.Errorf("-x log commands:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(wantLog, "\n"))
	}
	for _, v := range []string{"# as version: " + asVersion, "# ld version: " + ldVersion} {
		if !strings.Contains(b.stderr, v) {
			t.Errorf("-x log lacks %q", v)
		}
	}
	checkELF(t, filepath.Join(dir, "out"), filepath.Join(refDir, "ref"))
	checkDisassembly(t, filepath.Join(work, "prog.o"), filepath.Join(dir, "out"), asm)

	// Supporting evidence: a rebuild with PATH holding only as and ld.
	restricted := t.TempDir()
	for name, path := range map[string]string{"as": asPath, "ld": ldPath} {
		if err := os.Symlink(path, filepath.Join(restricted, name)); err != nil {
			t.Fatal(err)
		}
	}
	env := append(os.Environ(), "PATH="+restricted)
	if r := command(t, dir, env, bin, "build", "-o", "out2", "m1_arith.go"); r.code != 0 {
		t.Errorf("restricted PATH build: %+v", r)
	} else if !bytes.Equal(readFile(t, filepath.Join(dir, "out2")), out) {
		t.Error("restricted PATH build differs")
	}
}

// checkELF records the ELF evidence: a static x86-64 executable without gc's
// sections, defining only the expected symbols. The gc reference must have
// those sections, which shows the inspection can see them.
func checkELF(t *testing.T, out, ref string) {
	t.Helper()
	header := command(t, "", nil, "readelf", "-h", out).stdout
	for _, want := range []string{"ELF64", "EXEC (Executable file)", "Advanced Micro Devices X86-64"} {
		if !strings.Contains(header, want) {
			t.Errorf("readelf -h lacks %q:\n%s", want, header)
		}
	}
	if d := command(t, "", nil, "readelf", "-d", out).stdout; !strings.Contains(d, "There is no dynamic section") {
		t.Errorf("out is not statically linked:\n%s", d)
	}
	if l := command(t, "", nil, "readelf", "-l", out).stdout; strings.Contains(l, "INTERP") {
		t.Errorf("out requests an interpreter:\n%s", l)
	}
	gcSections := []string{".go.buildinfo", ".note.go.buildid", ".gopclntab"}
	outSections := command(t, "", nil, "readelf", "-S", "-W", out).stdout
	refSections := command(t, "", nil, "readelf", "-S", "-W", ref).stdout
	for _, s := range gcSections {
		if strings.Contains(outSections, s) {
			t.Errorf("out has gc section %s", s)
		}
		if !strings.Contains(refSections, s) {
			t.Errorf("control: the gc binary lacks %s", s)
		}
	}
	var defined []string
	for _, line := range strings.Split(strings.TrimSpace(command(t, "", nil, "nm", out).stdout), "\n") {
		if f := strings.Fields(line); len(f) == 3 {
			defined = append(defined, f[2])
		}
	}
	slices.Sort(defined)
	want := []string{"__bss_start", "_edata", "_end", "_start", "aether.mainstack", "aether.printbool", "aether.printint", "aether.printnl", "aether.printsp", "aether.stacklimit", "aether.stackoverflow", "aether.write", "main.main"}
	if !slices.Equal(defined, want) {
		t.Errorf("nm: defined symbols %v, want %v", defined, want)
	}
}

// checkDisassembly checks that the main.main instructions in prog.o and in
// the executable are, in order, the instructions of prog.s.
func checkDisassembly(t *testing.T, obj, exe string, asm []byte) {
	t.Helper()
	var want []string
	inMain := false
	for _, line := range strings.Split(string(asm), "\n") {
		switch {
		case line == "main.main:":
			inMain = true
		case strings.HasPrefix(line, "\t.size\tmain.main"):
			inMain = false
		case inMain && strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "\t#") && !strings.HasPrefix(line, "\t."):
			want = append(want, mnemonic(strings.Fields(line)[0]))
		}
	}
	if len(want) < 10 {
		t.Fatalf("found only %d instructions of main.main in prog.s", len(want))
	}
	for _, file := range []string{obj, exe} {
		got := disassembledMain(t, file)
		if len(got) < len(want) || !slices.Equal(got[:len(want)], want) {
			t.Errorf("objdump %s: main.main is\n%v\nwant\n%v", file, got, want)
			continue
		}
		for _, extra := range got[len(want):] {
			if !strings.Contains(extra, "nop") {
				t.Errorf("objdump %s: unexpected instruction %q after main.main's code", file, extra)
			}
		}
	}
}

// mnemonic drops the AT&T operand-size suffix q, which objdump omits where
// the operands make the size clear.
func mnemonic(m string) string { return strings.TrimSuffix(m, "q") }

func disassembledMain(t *testing.T, file string) []string {
	t.Helper()
	out := command(t, "", nil, "objdump", "-d", "--no-show-raw-insn", file).stdout
	var ms []string
	inMain := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasSuffix(line, "<main.main>:"):
			inMain = true
		case inMain && strings.TrimSpace(line) == "":
			return ms
		case inMain:
			if _, insn, ok := strings.Cut(line, ":\t"); ok {
				ms = append(ms, mnemonic(strings.Fields(insn)[0]))
			}
		}
	}
	return ms
}

// TestMilestone1Rejections is check P5: invalid and unsupported programs and
// a failing linker fail clearly, create nothing at the output path or in a
// work directory, and leave an existing output untouched.
func TestMilestone1Rejections(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the aether binary")
	}
	requireTools(t)
	bin := aetherBinary(t)
	m1 := readFile(t, corpusFile("s1_03_milestone1"))
	main := func(body string) string { return "package main\n\nfunc main() {\n" + body + "\n}\n" }
	tests := []struct {
		name, src string
		code      int
		feature   string
	}{
		{"division", main("x := 6\nprintln(x / 2)"), 3, "operator-div"},
		{"import", "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(1) }\n", 3, "import"},
		{"if", main("x := 1\nif x > 0 {\nprintln(x)\n}"), 3, "if"},
		{"two results", "package main\n\nfunc f() (int, int) { return 1, 2 }\n\nfunc main() { f() }\n", 3, "function-declaration"},
		{"string argument", main(`println("hi")`), 3, "println-argument-type"},
		{"float literal", main("var x int = 1.0\nprintln(x)"), 3, "float-literal"},
		{"package name", "package foo\n\nfunc main() {}\n", 3, "package-name"},
	}
	for _, f := range []string{"s1_n01_constant_overflow", "s1_n02_typed_constant_overflow", "s1_n03_unused_variable",
		"s1_n04_negative_constant_overflow", "s1_n05_missing_main"} {
		tests = append(tests, struct {
			name, src string
			code      int
			feature   string
		}{f, string(readFile(t, corpusFile(f))), 1, ""})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "m.go"), []byte(tt.src))
			r := command(t, dir, nil, bin, "build", "-work", "-o", "out", "m.go")
			if r.code != tt.code || r.stdout != "" {
				t.Fatalf("exit %d, want %d\n%s", r.code, tt.code, r.stderr)
			}
			class := map[int]string{1: "invalid", 3: "unsupported"}[tt.code]
			for _, line := range strings.Split(strings.TrimSpace(r.stderr), "\n") {
				file, rest, _ := strings.Cut(line, ": ")
				if parts := strings.Split(file, ":"); len(parts) != 3 || parts[0] != "m.go" || !strings.HasPrefix(rest, class+": ") {
					t.Errorf("diagnostic %q lacks a file:line:col position or class %s", line, class)
				}
			}
			if tt.feature != "" && !strings.Contains(r.stderr, "["+tt.feature+"]") {
				t.Errorf("no diagnostic names %s:\n%s", tt.feature, r.stderr)
			}
			// The gate stops before lowering, so no work directory exists.
			if strings.Contains(r.stderr, "WORK=") {
				t.Errorf("a work directory was created:\n%s", r.stderr)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Errorf("files created: %v", entries)
			}
		})
	}

	t.Run("failing ld", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "m.go"), m1)
		tools := t.TempDir()
		fakeTool(t, tools, "ld", ldVersion+"echo 'simulated link failure' >&2\nexit 1\n")
		env := append(os.Environ(), "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"))
		r := command(t, dir, env, bin, "build", "-o", "out", "m.go")
		if r.code != 5 || !strings.Contains(r.stderr, "tool: ld (") || !strings.Contains(r.stderr, "simulated link failure") {
			t.Errorf("exit %d\n%s", r.code, r.stderr)
		}
		noFile(t, filepath.Join(dir, "out"))
		existing := []byte("previous output\n")
		if err := os.WriteFile(filepath.Join(dir, "out"), existing, 0o600); err != nil {
			t.Fatal(err)
		}
		command(t, dir, env, bin, "build", "-o", "out", "m.go")
		if got := readFile(t, filepath.Join(dir, "out")); !bytes.Equal(got, existing) {
			t.Errorf("existing output changed to %q", got)
		}
	})
}

// TestMilestone1Determinism is check P6: two builds that differ in the work
// directory's location, the current directory, the output path, and one
// unrelated environment variable produce byte-identical assembly and
// executables, with the same tools.
func TestMilestone1Determinism(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the aether binary")
	}
	requireTools(t)
	bin := aetherBinary(t)
	src := readFile(t, corpusFile("s1_03_milestone1"))
	type build struct {
		cwd, tmp, out string
		env           []string
	}
	var builds []build
	for i, extra := range []string{"", "AETHER_UNRELATED=1"} {
		b := build{cwd: t.TempDir(), tmp: t.TempDir(), out: filepath.Join(t.TempDir(), []string{"out", "other-name"}[i])}
		b.env = append(os.Environ(), "TMPDIR="+b.tmp)
		if extra != "" {
			b.env = append(b.env, extra)
		}
		writeFile(t, filepath.Join(b.cwd, "m1_arith.go"), src)
		builds = append(builds, b)
	}
	var progS, exe [][]byte
	var cmds [][]string
	for _, b := range builds {
		r := command(t, b.cwd, b.env, bin, "build", "-x", "-work", "-o", b.out, "m1_arith.go")
		if r.code != 0 {
			t.Fatalf("build: %+v", r)
		}
		work := workDir(t, r.stderr)
		if !strings.HasPrefix(work, b.tmp) {
			t.Errorf("work directory %s is not under TMPDIR %s", work, b.tmp)
		}
		progS = append(progS, readFile(t, filepath.Join(work, "prog.s")))
		exe = append(exe, readFile(t, b.out))
		var logged []string // commands and version lines: the controlled tools
		for _, line := range strings.Split(r.stderr, "\n") {
			if !strings.HasPrefix(line, "cd ") && !strings.HasPrefix(line, "WORK=") {
				logged = append(logged, line)
			}
		}
		cmds = append(cmds, logged)
	}
	if !slices.Equal(cmds[0], cmds[1]) {
		t.Errorf("tools or commands differ:\n%v\n%v", cmds[0], cmds[1])
	}
	if !bytes.Equal(progS[0], progS[1]) {
		t.Error("prog.s differs between builds")
	}
	if !bytes.Equal(exe[0], exe[1]) {
		t.Error("executables differ between builds")
	}
}
