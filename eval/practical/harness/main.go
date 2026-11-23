// Command harness builds every evaluation program with gc (the reference)
// and with Aether, runs both on identical inputs, and compares standard
// output, standard error, and exit status.
//
// It is built and run with the gc toolchain (see ../run.sh). Only the
// programs under ../programs are compiled by Aether.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

var (
	flagAether  = flag.String("aether", "", "path to the aether binary (required)")
	flagRoot    = flag.String("root", ".", "eval/practical directory")
	flagOut     = flag.String("out", "results", "output directory (replaced)")
	flagOnly    = flag.String("only", "", "comma-separated program directory prefixes to run (default all)")
	flagModes   = flag.String("modes", "default,O0", "comma-separated Aether modes: default, O0")
	flagTimeout = flag.Duration("timeout", 5*time.Minute, "per-run timeout")
	flagRepeat  = flag.Int("gcrepeat", 2, "runs of the gc binary per case, to detect nondeterministic programs")
)

type caseSpec struct {
	Name  string
	Stdin string // path relative to the program's inputs/, or "" for /dev/null
	Args  []string
}

type runResult struct {
	Stdout, Stderr []byte
	Exit           int
	TimedOut       bool
	Elapsed        time.Duration
	Err            error // failure to start
}

type row struct {
	Program, Mode, Case string
	Status              string
	GcExit, AeExit      string
	GcTime, AeTime      time.Duration
	Detail              string
}

type buildRow struct {
	Program, Mode string
	Status        string // OK, UNSUPPORTED, INVALID, INTERNAL, TOOLCHAIN, USAGE, FAILED
	Exit          int
	Elapsed       time.Duration
	Diag          string
}

func main() {
	flag.Parse()
	if *flagAether == "" {
		fatalf("-aether is required")
	}
	aether, err := filepath.Abs(*flagAether)
	check(err)
	root, err := filepath.Abs(*flagRoot)
	check(err)
	out, err := filepath.Abs(*flagOut)
	check(err)
	check(os.RemoveAll(out))
	check(os.MkdirAll(out, 0o755))

	progDirs, err := filepath.Glob(filepath.Join(root, "programs", "[0-9][0-9]-*"))
	check(err)
	sort.Strings(progDirs)
	var only []string
	if *flagOnly != "" {
		only = strings.Split(*flagOnly, ",")
	}
	modes := strings.Split(*flagModes, ",")

	var rows []row
	var builds []buildRow
	for _, dir := range progDirs {
		name := filepath.Base(dir)
		if len(only) > 0 && !hasPrefixAny(name, only) {
			continue
		}
		cases, err := readCases(filepath.Join(dir, "cases.txt"))
		check(err)
		pout := filepath.Join(out, name)
		check(os.MkdirAll(pout, 0o755))

		fmt.Printf("== %s (%d cases)\n", name, len(cases))
		gcBin := filepath.Join(pout, "gc.bin")
		gb := buildGc(dir, gcBin)
		builds = append(builds, gb)
		logf(pout, "build-gc.log", gb.Diag)
		fmt.Printf("   gc build: %s (%v)\n", gb.Status, gb.Elapsed.Round(time.Millisecond))
		if gb.Status != "OK" {
			for _, c := range cases {
				rows = append(rows, row{Program: name, Mode: "-", Case: c.Name, Status: "HARNESS-GC-BUILD"})
			}
			continue
		}

		// Reference runs.
		gcRuns := map[string]runResult{}
		gcNondet := map[string]string{}
		for _, c := range cases {
			var first runResult
			for i := 0; i < max(1, *flagRepeat); i++ {
				r := runBin(gcBin, dir, c)
				if i == 0 {
					first = r
					continue
				}
				if r.TimedOut || first.TimedOut {
					break
				}
				if !bytes.Equal(r.Stdout, first.Stdout) || r.Exit != first.Exit || !stderrMatches(first.Stderr, r.Stderr) {
					gcNondet[c.Name] = "gc runs differ between repetitions"
				}
			}
			gcRuns[c.Name] = first
			save(pout, "gc", c.Name, first)
		}

		for _, mode := range modes {
			aeBin := filepath.Join(pout, "aether-"+mode+".bin")
			ab := buildAether(aether, dir, aeBin, mode)
			ab.Program = name
			builds = append(builds, ab)
			logf(pout, "build-aether-"+mode+".log", ab.Diag)
			fmt.Printf("   aether build (%s): %s (%v)\n", mode, ab.Status, ab.Elapsed.Round(time.Millisecond))
			for _, c := range cases {
				g := gcRuns[c.Name]
				rw := row{Program: name, Mode: mode, Case: c.Name, GcExit: exitStr(g), GcTime: g.Elapsed}
				if ab.Status != "OK" {
					rw.Status = "NOT-BUILT"
					rw.Detail = "aether build: " + ab.Status
					rows = append(rows, rw)
					continue
				}
				a := runBin(aeBin, dir, c)
				save(pout, "aether-"+mode, c.Name, a)
				rw.AeExit, rw.AeTime = exitStr(a), a.Elapsed
				rw.Status, rw.Detail = compare(g, a)
				if why, ok := gcNondet[c.Name]; ok {
					rw.Status, rw.Detail = "HARNESS-GC-NONDETERMINISTIC", why
				}
				if g.TimedOut {
					rw.Status, rw.Detail = "HARNESS-GC-TIMEOUT", "the gc binary timed out"
				}
				if strings.HasPrefix(rw.Status, "FAIL") || rw.Status == "TIMEOUT" {
					logf(pout, fmt.Sprintf("diff-%s-%s.txt", mode, c.Name), explain(g, a))
				}
				fmt.Printf("   %-8s %-18s %-14s gc %-4s %8v | aether %-4s %8v  %s\n", mode, c.Name, rw.Status,
					rw.GcExit, rw.GcTime.Round(time.Millisecond), rw.AeExit, rw.AeTime.Round(time.Millisecond), rw.Detail)
				rows = append(rows, rw)
			}
		}
	}
	writeTSV(out, rows, builds)
	writeSummary(out, root, aether, rows, builds)
	fmt.Printf("\nwrote %s/summary.md, results.tsv, builds.tsv\n", out)
}

func hasPrefixAny(s string, ps []string) bool {
	for _, p := range ps {
		if strings.HasPrefix(s, strings.TrimSpace(p)) {
			return true
		}
	}
	return false
}

func readCases(path string) ([]caseSpec, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cs []caseSpec
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 2 {
			return nil, fmt.Errorf("%s: bad line %q", path, line)
		}
		c := caseSpec{Name: strings.TrimSpace(parts[0]), Stdin: strings.TrimSpace(parts[1])}
		if c.Stdin == "-" {
			c.Stdin = ""
		}
		if len(parts) == 3 {
			c.Args = strings.Fields(parts[2])
		}
		cs = append(cs, c)
	}
	return cs, sc.Err()
}

func command(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

func buildGc(dir, out string) buildRow {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := command(ctx, dir, "go", "build", "-o", out, ".")
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS=", "CGO_ENABLED=0")
	start := time.Now()
	b, err := cmd.CombinedOutput()
	br := buildRow{Program: filepath.Base(dir), Mode: "gc", Elapsed: time.Since(start), Diag: string(b)}
	br.Status = "OK"
	if err != nil {
		br.Status, br.Exit = "FAILED", cmd.ProcessState.ExitCode()
	}
	return br
}

// buildAether compiles the program's main-package files (the *.go files
// directly in dir). Imported packages of the program's module are found by
// Aether through the program's go.mod.
func buildAether(aether, dir, out, mode string) buildRow {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	sort.Strings(files)
	args := []string{"build"}
	if mode == "O0" {
		args = append(args, "-O0")
	}
	args = append(args, "-o", out)
	for _, f := range files {
		args = append(args, filepath.Base(f))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	cmd := command(ctx, dir, aether, args...)
	start := time.Now()
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stderr, &stderr
	err := cmd.Run()
	br := buildRow{Mode: mode, Elapsed: time.Since(start), Diag: "$ aether " + strings.Join(args, " ") + "\n" + stderr.String()}
	if err == nil {
		br.Status = "OK"
		return br
	}
	br.Exit = cmd.ProcessState.ExitCode()
	switch br.Exit {
	case 1:
		br.Status = "INVALID"
	case 2:
		br.Status = "USAGE"
	case 3:
		br.Status = "UNSUPPORTED"
	case 4:
		br.Status = "INTERNAL"
	case 5:
		br.Status = "TOOLCHAIN"
	default:
		br.Status = fmt.Sprintf("FAILED(exit %d)", br.Exit)
	}
	return br
}

func runBin(bin, dir string, c caseSpec) runResult {
	ctx, cancel := context.WithTimeout(context.Background(), *flagTimeout)
	defer cancel()
	cmd := command(ctx, dir, bin, c.Args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	if c.Stdin != "" {
		f, err := os.Open(filepath.Join(dir, "inputs", c.Stdin))
		if err != nil {
			return runResult{Err: err, Exit: -1}
		}
		defer f.Close()
		cmd.Stdin = f
	}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	start := time.Now()
	err := cmd.Run()
	r := runResult{Stdout: so.Bytes(), Stderr: se.Bytes(), Elapsed: time.Since(start)}
	if ctx.Err() == context.DeadlineExceeded {
		r.TimedOut = true
		r.Exit = -1
		return r
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			r.Exit = 128 + int(ws.Signal())
		} else {
			r.Exit = ee.ExitCode()
		}
	default:
		r.Err, r.Exit = err, -1
	}
	return r
}

func exitStr(r runResult) string {
	switch {
	case r.TimedOut:
		return "T/O"
	case r.Err != nil:
		return "ERR"
	}
	return fmt.Sprint(r.Exit)
}

// panicPrefix applies the comparison rule of docs/SEMANTICS.md (V-3, V-10):
// when gc's stderr contains a "panic: " or "fatal error: " line, only the
// text up to the end of that line, plus following tab-indented lines, is
// compared; gc's goroutine traceback is not. ok is false when the rule does
// not apply.
func panicPrefix(gc []byte) (q []byte, ok bool) {
	i := bytes.Index(gc, []byte("panic: "))
	j := bytes.Index(gc, []byte("fatal error: "))
	if i < 0 || (j >= 0 && j < i) {
		i = j
	}
	if i < 0 {
		return nil, false
	}
	end := bytes.IndexByte(gc[i:], '\n')
	if end < 0 {
		return gc, true
	}
	end += i + 1
	for end < len(gc) && gc[end] == '\t' {
		n := bytes.IndexByte(gc[end:], '\n')
		if n < 0 {
			end = len(gc)
			break
		}
		end += n + 1
	}
	return gc[:end], true
}

func stderrMatches(gc, ae []byte) bool {
	if bytes.Equal(gc, ae) {
		return true
	}
	q, ok := panicPrefix(gc)
	return ok && bytes.Equal(q, ae)
}

func compare(g, a runResult) (status, detail string) {
	switch {
	case a.Err != nil:
		return "FAIL-START", a.Err.Error()
	case a.TimedOut:
		return "TIMEOUT", fmt.Sprintf("aether binary exceeded %v", *flagTimeout)
	}
	var bad []string
	if g.Exit != a.Exit {
		bad = append(bad, fmt.Sprintf("exit %d vs %d", g.Exit, a.Exit))
	}
	if !bytes.Equal(g.Stdout, a.Stdout) {
		bad = append(bad, "stdout differs")
	}
	normalized := false
	if !bytes.Equal(g.Stderr, a.Stderr) {
		if stderrMatches(g.Stderr, a.Stderr) {
			normalized = true
		} else {
			bad = append(bad, "stderr differs")
		}
	}
	if len(bad) > 0 {
		return "FAIL", strings.Join(bad, "; ")
	}
	if normalized {
		return "PASS", "stderr compared up to the panic line (V-3/V-10 rule)"
	}
	return "PASS", ""
}

func firstDiff(a, b []byte) string {
	la := strings.SplitAfter(string(a), "\n")
	lb := strings.SplitAfter(string(b), "\n")
	for i := 0; i < max(len(la), len(lb)); i++ {
		var x, y string
		if i < len(la) {
			x = la[i]
		}
		if i < len(lb) {
			y = lb[i]
		}
		if x != y {
			var sb strings.Builder
			fmt.Fprintf(&sb, "first difference at line %d (gc %d lines, aether %d lines)\n", i+1, len(la), len(lb))
			for k := max(0, i-3); k < min(i+4, max(len(la), len(lb))); k++ {
				if k < len(la) {
					fmt.Fprintf(&sb, "  gc     %5d: %q\n", k+1, la[k])
				}
				if k < len(lb) {
					fmt.Fprintf(&sb, "  aether %5d: %q\n", k+1, lb[k])
				}
			}
			return sb.String()
		}
	}
	return "identical\n"
}

func explain(g, a runResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "exit: gc %s, aether %s\n\n-- stdout --\n%s\n-- stderr --\n%s", exitStr(g), exitStr(a),
		firstDiff(g.Stdout, a.Stdout), firstDiff(g.Stderr, a.Stderr))
	return sb.String()
}

func save(dir, who, cname string, r runResult) {
	_ = os.WriteFile(filepath.Join(dir, cname+"."+who+".stdout"), r.Stdout, 0o644)
	_ = os.WriteFile(filepath.Join(dir, cname+"."+who+".stderr"), r.Stderr, 0o644)
}

func logf(dir, name, text string) { _ = os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644) }

func writeTSV(out string, rows []row, builds []buildRow) {
	var b strings.Builder
	b.WriteString("program\tmode\tcase\tstatus\tgc_exit\taether_exit\tgc_ms\taether_ms\tdetail\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\n", r.Program, r.Mode, r.Case, r.Status,
			r.GcExit, r.AeExit, r.GcTime.Milliseconds(), r.AeTime.Milliseconds(), r.Detail)
	}
	logf(out, "results.tsv", b.String())
	b.Reset()
	b.WriteString("program\tmode\tstatus\texit\tms\tfirst_diagnostic\n")
	for _, br := range builds {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%d\t%d\t%s\n", br.Program, br.Mode, br.Status, br.Exit, br.Elapsed.Milliseconds(), firstDiag(br.Diag))
	}
	logf(out, "builds.tsv", b.String())
}

// firstDiag returns the first diagnostic line of a build log.
func firstDiag(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l != "" && !strings.HasPrefix(l, "$ ") {
			return l
		}
	}
	return ""
}

func cmdOut(name string, args ...string) string {
	b, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
}

func cpuModel() string {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "unknown"
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "model name") {
			if _, v, ok := strings.Cut(l, ":"); ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return "unknown"
}

func writeSummary(out, root, aether string, rows []row, builds []buildRow) {
	var b strings.Builder
	repo := filepath.Join(root, "..", "..")
	commit := cmdOut("git", "-C", repo, "rev-parse", "--short", "HEAD")
	dirty := cmdOut("git", "-C", repo, "status", "--porcelain", "--", "cmd", "internal")
	if dirty != "" && dirty != "unavailable" {
		commit += " (compiler sources modified)"
	}
	fmt.Fprintf(&b, "# Practical evaluation results\n\n")
	fmt.Fprintf(&b, "Generated %s.\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "| Environment | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Aether commit | %s |\n| Aether binary | %s |\n| Go | %s |\n| GNU as | %s |\n| GNU ld | %s |\n| Kernel | %s |\n| CPU | %s (%d logical CPUs) |\n\n",
		commit, aether, cmdOut("go", "version"), cmdOut("as", "--version"), cmdOut("ld", "--version"),
		cmdOut("uname", "-srm"), cpuModel(), runtime.NumCPU())

	b.WriteString("## Builds\n\n| Program | Mode | Status | Time | First diagnostic |\n|---|---|---|---|---|\n")
	for _, br := range builds {
		fmt.Fprintf(&b, "| %s | %s | %s | %v | %s |\n", br.Program, br.Mode, br.Status,
			br.Elapsed.Round(time.Millisecond), strings.ReplaceAll(firstDiag(br.Diag), "|", "\\|"))
	}

	type key struct{ p, m string }
	counts := map[key]map[string]int{}
	var order []key
	for _, r := range rows {
		k := key{r.Program, r.Mode}
		if counts[k] == nil {
			counts[k] = map[string]int{}
			order = append(order, k)
		}
		counts[k][r.Status]++
	}
	b.WriteString("\n## Cases by program and mode\n\n| Program | Mode | Cases | Status counts |\n|---|---|---|---|\n")
	for _, k := range order {
		var parts []string
		total := 0
		var ss []string
		for s := range counts[k] {
			ss = append(ss, s)
		}
		sort.Strings(ss)
		for _, s := range ss {
			parts = append(parts, fmt.Sprintf("%s %d", s, counts[k][s]))
			total += counts[k][s]
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %s |\n", k.p, k.m, total, strings.Join(parts, ", "))
	}

	b.WriteString("\n## Every case\n\n| Program | Mode | Case | Status | gc exit | Aether exit | gc time | Aether time | Detail |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %v | %v | %s |\n", r.Program, r.Mode, r.Case, r.Status,
			r.GcExit, r.AeExit, r.GcTime.Round(time.Millisecond), r.AeTime.Round(time.Millisecond), r.Detail)
	}
	b.WriteString("\nTimes are single wall-clock measurements of one run, including process start; they are not benchmarks.\n")
	logf(out, "summary.md", b.String())
}

func check(err error) {
	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "harness: "+format+"\n", args...)
	os.Exit(2)
}
