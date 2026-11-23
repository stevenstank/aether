// Command workload runs a set of compute- and allocation-heavy kernels and
// prints a checksum for each, so different builds can be compared.
//
// Usage: workload [-scale N] [kernel ...]
// Kernels: sieve trees matmul nbody maps sort strings (default: all).
package main

import (
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
)

var scale = 1

// lcg is a small deterministic generator (Knuth's MMIX constants).
type lcg uint64

func (r *lcg) next() uint64 {
	*r = *r*6364136223846793005 + 1442695040888963407
	return uint64(*r >> 11)
}

func sieve() string {
	n := 2_000_000 * scale
	composite := make([]bool, n+1)
	count, sum := 0, 0
	for i := 2; i <= n; i++ {
		if composite[i] {
			continue
		}
		count++
		sum += i
		for j := i * i; j <= n; j += i {
			composite[j] = true
		}
	}
	return fmt.Sprintf("primes<=%d: %d, sum %d", n, count, sum)
}

type tree struct{ left, right *tree }

func bottomUp(depth int) *tree {
	if depth <= 0 {
		return &tree{}
	}
	return &tree{bottomUp(depth - 1), bottomUp(depth - 1)}
}

func (t *tree) check() int {
	if t.left == nil {
		return 1
	}
	return 1 + t.left.check() + t.right.check()
}

func trees() string {
	maxDepth := 14 + scale
	long := bottomUp(maxDepth)
	var b strings.Builder
	for d := 4; d <= maxDepth; d += 2 {
		iters := 1 << (maxDepth - d + 4)
		chk := 0
		for i := 0; i < iters; i++ {
			chk += bottomUp(d).check()
		}
		fmt.Fprintf(&b, "%d:%d ", d, chk)
	}
	fmt.Fprintf(&b, "long:%d", long.check())
	return b.String()
}

func matmul() string {
	n := 120 * scale
	a := make([][]float64, n)
	b := make([][]float64, n)
	c := make([][]float64, n)
	for i := 0; i < n; i++ {
		a[i], b[i], c[i] = make([]float64, n), make([]float64, n), make([]float64, n)
		for j := 0; j < n; j++ {
			a[i][j] = float64(i*j%7) - 3
			b[i][j] = float64((i+2*j)%5) / 4
		}
	}
	for i := 0; i < n; i++ {
		for k := 0; k < n; k++ {
			aik := a[i][k]
			for j := 0; j < n; j++ {
				c[i][j] += aik * b[k][j]
			}
		}
	}
	trace, sum := 0.0, 0.0
	for i := 0; i < n; i++ {
		trace += c[i][i]
		for j := 0; j < n; j++ {
			sum += c[i][j]
		}
	}
	return fmt.Sprintf("n=%d trace=%.3f sum=%.3f", n, trace, sum)
}

type body struct{ x, y, z, vx, vy, vz, mass float64 }

const (
	solarMass   = 4 * math.Pi * math.Pi
	daysPerYear = 365.24
)

func energy(bs []body) float64 {
	e := 0.0
	for i, b := range bs {
		e += 0.5 * b.mass * (b.vx*b.vx + b.vy*b.vy + b.vz*b.vz)
		for _, c := range bs[i+1:] {
			dx, dy, dz := b.x-c.x, b.y-c.y, b.z-c.z
			e -= b.mass * c.mass / math.Sqrt(dx*dx+dy*dy+dz*dz)
		}
	}
	return e
}

func nbody() string {
	bs := []body{
		{0, 0, 0, 0, 0, 0, solarMass},
		{4.84143144246472090e+00, -1.16032004402742839e+00, -1.03622044471123109e-01,
			1.66007664274403694e-03 * daysPerYear, 7.69901118419740425e-03 * daysPerYear,
			-6.90460016972063023e-05 * daysPerYear, 9.54791938424326609e-04 * solarMass},
		{8.34336671824457987e+00, 4.12479856412430479e+00, -4.03523417114321381e-01,
			-2.76742510726862411e-03 * daysPerYear, 4.99852801234917238e-03 * daysPerYear,
			2.30417297573763929e-05 * daysPerYear, 2.85885980666130812e-04 * solarMass},
		{1.28943695621391310e+01, -1.51111514016986312e+01, -2.23307578892655734e-01,
			2.96460137564761618e-03 * daysPerYear, 2.37847173959480950e-03 * daysPerYear,
			-2.96589568540237556e-05 * daysPerYear, 4.36624404335156298e-05 * solarMass},
		{1.53796971148509165e+01, -2.59193146099879641e+01, 1.79258772950371181e-01,
			2.68067772490389322e-03 * daysPerYear, 1.62824170038242295e-03 * daysPerYear,
			-9.51592254519715870e-05 * daysPerYear, 5.15138902046611451e-05 * solarMass},
	}
	var px, py, pz float64
	for _, b := range bs {
		px += b.vx * b.mass
		py += b.vy * b.mass
		pz += b.vz * b.mass
	}
	bs[0].vx, bs[0].vy, bs[0].vz = -px/solarMass, -py/solarMass, -pz/solarMass
	before := energy(bs)
	steps := 200_000 * scale
	const dt = 0.01
	for s := 0; s < steps; s++ {
		for i := range bs {
			bi := &bs[i]
			for j := i + 1; j < len(bs); j++ {
				bj := &bs[j]
				dx, dy, dz := bi.x-bj.x, bi.y-bj.y, bi.z-bj.z
				d2 := dx*dx + dy*dy + dz*dz
				mag := dt / (d2 * math.Sqrt(d2))
				bi.vx -= dx * bj.mass * mag
				bi.vy -= dy * bj.mass * mag
				bi.vz -= dz * bj.mass * mag
				bj.vx += dx * bi.mass * mag
				bj.vy += dy * bi.mass * mag
				bj.vz += dz * bi.mass * mag
			}
		}
		for i := range bs {
			bs[i].x += dt * bs[i].vx
			bs[i].y += dt * bs[i].vy
			bs[i].z += dt * bs[i].vz
		}
	}
	return fmt.Sprintf("steps=%d energy %.9f -> %.9f", steps, before, energy(bs))
}

func maps() string {
	n := 300_000 * scale
	m := make(map[string]int)
	for i := 0; i < n; i++ {
		m["key-"+strconv.Itoa(i*7919%n)] = i
	}
	for i := 0; i < n; i += 2 {
		delete(m, "key-"+strconv.Itoa(i))
	}
	sum := 0
	for k, v := range m {
		sum += v ^ len(k)
	}
	byLen := map[int]int{}
	for k := range m {
		byLen[len(k)]++
	}
	lens := make([]int, 0, len(byLen))
	for l := range byLen {
		lens = append(lens, l)
	}
	sort.Ints(lens)
	var parts []string
	for _, l := range lens {
		parts = append(parts, fmt.Sprintf("%d:%d", l, byLen[l]))
	}
	return fmt.Sprintf("len=%d sum=%d lens=[%s]", len(m), sum, strings.Join(parts, " "))
}

type rec struct {
	key   uint32
	name  string
	score float64
}

func sorting() string {
	n := 500_000 * scale
	r := lcg(42)
	xs := make([]int, n)
	for i := range xs {
		xs[i] = int(r.next() % 1_000_000)
	}
	slices.Sort(xs)
	uniq := slices.Compact(slices.Clone(xs))
	recs := make([]rec, n/5)
	for i := range recs {
		k := uint32(r.next() % 1000)
		recs[i] = rec{k, "r" + strconv.Itoa(i), float64(r.next()%10000) / 100}
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].key < recs[j].key })
	h := uint64(14695981039346656037)
	for _, rc := range recs[:1000] {
		for _, c := range []byte(rc.name) {
			h = (h ^ uint64(c)) * 1099511628211
		}
	}
	idx, found := slices.BinarySearch(xs, 500_000)
	return fmt.Sprintf("n=%d min=%d median=%d max=%d unique=%d search=%d,%v fnv=%x",
		n, xs[0], xs[n/2], xs[n-1], len(uniq), idx, found, h)
}

func stringsKernel() string {
	words := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	var b strings.Builder
	r := lcg(7)
	for i := 0; i < 200_000*scale; i++ {
		b.WriteString(words[r.next()%uint64(len(words))])
		b.WriteByte(' ')
	}
	text := b.String()
	fields := strings.Fields(text)
	upper := strings.ToUpper(text[:64])
	count := strings.Count(text, "eta")
	replaced := strings.ReplaceAll(text, "gamma", "Γ")
	return fmt.Sprintf("bytes=%d fields=%d eta=%d replaced=%d head=%q",
		len(text), len(fields), count, len(replaced), strings.TrimSpace(upper))
}

func main() {
	all := []string{"sieve", "trees", "matmul", "nbody", "maps", "sort", "strings"}
	kernels := map[string]func() string{
		"sieve": sieve, "trees": trees, "matmul": matmul, "nbody": nbody,
		"maps": maps, "sort": sorting, "strings": stringsKernel,
	}
	args := os.Args[1:]
	if len(args) >= 2 && args[0] == "-scale" {
		s, err := strconv.Atoi(args[1])
		if err != nil || s < 1 || s > 8 {
			fmt.Fprintf(os.Stderr, "workload: -scale must be 1..8, got %q\n", args[1])
			os.Exit(2)
		}
		scale = s
		args = args[2:]
	}
	if len(args) == 0 {
		args = all
	}
	for _, name := range args {
		k, ok := kernels[name]
		if !ok {
			fmt.Fprintf(os.Stderr, "workload: unknown kernel %q\n", name)
			os.Exit(2)
		}
		fmt.Printf("%-8s %s\n", name, k())
	}
}
