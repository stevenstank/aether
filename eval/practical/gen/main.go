// Command genfixtures writes the large, deterministic input fixtures of the
// practical evaluation into ../programs/*/inputs. It is run with the gc
// toolchain; it is not a program under test.
//
// Usage (from eval/practical): go run ./gen   (inside gen: go run .)
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// rng is a 64-bit LCG, so the fixtures never depend on math/rand's
// algorithm across Go versions.
type rng uint64

func (r *rng) next() uint64 {
	*r = *r*6364136223846793005 + 1442695040888963407
	return uint64(*r >> 17)
}
func (r *rng) intn(n int) int { return int(r.next() % uint64(n)) }

var root = "../programs"

func create(prog, name string, fill func(w *bufio.Writer)) {
	path := filepath.Join(root, prog, "inputs", name)
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	w := bufio.NewWriter(f)
	fill(w)
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	f.Close()
	st, _ := os.Stat(path)
	fmt.Printf("%-40s %9d bytes\n", path, st.Size())
}

var vocab = strings.Fields(`the of and to in is was for on that with as by at from his her
it an were are which this be or has had not but their its they one all
river mountain city harbor engine signal winter garden market letter
ancient quiet bright careful sudden distant golden broken hidden
café naïve über straße ελλάδα 東京 don't it's o'clock rock'n'roll`)

func main() {
	if _, err := os.Stat(root); err != nil {
		root = "programs" // run from eval/practical
		if _, err := os.Stat(root); err != nil {
			fmt.Fprintln(os.Stderr, "genfixtures: run from eval/practical or eval/practical/gen")
			os.Exit(1)
		}
	}

	create("01-wordfreq", "gen-words.txt", func(w *bufio.Writer) {
		r := rng(1)
		for line := 0; line < 40000; line++ {
			n := 3 + r.intn(12)
			for i := 0; i < n; i++ {
				// Zipf-ish: favor early words.
				k := r.intn(len(vocab)) * r.intn(len(vocab)) / len(vocab)
				word := vocab[k]
				if r.intn(10) == 0 {
					word = strings.ToUpper(word[:1]) + word[1:]
				}
				w.WriteString(word)
				switch r.intn(12) {
				case 0:
					w.WriteString(", ")
				case 1:
					w.WriteString(". ")
				case 2:
					w.WriteString(" — ")
				default:
					w.WriteString(" ")
				}
			}
			w.WriteString("\n")
		}
	})

	create("02-calc", "gen-exprs.txt", func(w *bufio.Writer) {
		r := rng(2)
		vars := []string{}
		var gen func(depth int) string
		gen = func(depth int) string {
			if depth == 0 || r.intn(4) == 0 {
				switch {
				case len(vars) > 0 && r.intn(3) == 0:
					return vars[r.intn(len(vars))]
				case r.intn(5) == 0:
					return fmt.Sprintf("%d.%02d", r.intn(100), r.intn(100))
				}
				return fmt.Sprint(r.intn(1000))
			}
			switch r.intn(9) {
			case 0:
				return "(" + gen(depth-1) + ")"
			case 1:
				return "-" + gen(depth-1)
			case 2:
				return []string{"abs", "sqrt", "floor", "ceil", "round"}[r.intn(5)] + "(" + gen(depth-1) + ")"
			case 3:
				return fmt.Sprintf("max(%s, %s, %s)", gen(depth-1), gen(depth-1), gen(depth-1))
			case 4:
				return fmt.Sprintf("hypot(%s, %s)", gen(depth-1), gen(depth-1))
			}
			ops := []string{"+", "-", "*", "/", "%", "^"}
			op := ops[r.intn(len(ops))]
			if op == "^" {
				return gen(depth-1) + " ^ " + fmt.Sprint(r.intn(4))
			}
			return gen(depth-1) + " " + op + " " + gen(depth-1)
		}
		for i := 0; i < 20000; i++ {
			if r.intn(4) == 0 {
				name := fmt.Sprintf("v%d", r.intn(50))
				fmt.Fprintf(w, "%s = %s\n", name, gen(4))
				vars = append(vars, name)
			} else {
				fmt.Fprintln(w, gen(5))
			}
			if i%5000 == 4999 {
				w.WriteString("vars\n")
			}
		}
	})

	create("03-ledger", "gen-ledger.csv", func(w *bufio.Writer) {
		r := rng(3)
		names := []string{"alice", "bob", "carol", "dave", "erin", "frank", "grace", "heidi",
			"ivan", "judy", "mallory", "niaj", "olivia", "peggy", "rupert", "sybil"}
		w.WriteString("date,account,kind,amount,to\n")
		for i := 0; i < 30000; i++ {
			d := fmt.Sprintf("2025-%02d-%02d", 1+r.intn(12), 1+r.intn(28))
			a := names[r.intn(len(names))]
			amt := fmt.Sprintf("%d.%02d", r.intn(2000), r.intn(100))
			switch k := r.intn(10); {
			case k < 4:
				fmt.Fprintf(w, "%s,%s,deposit,%s\n", d, a, amt)
			case k < 8:
				fmt.Fprintf(w, "%s,%s,withdraw,%s\n", d, a, amt)
			case k < 9:
				fmt.Fprintf(w, "%s,%s,transfer,%s,%s\n", d, a, amt, names[r.intn(len(names))])
			default:
				fmt.Fprintf(w, "%s,%s,deposit,%d.%03d\n", d, a, r.intn(10), r.intn(1000)) // invalid
			}
		}
	})

	create("04-textkit", "gen-text.txt", func(w *bufio.Writer) {
		r := rng(4)
		word := func() string { return vocab[r.intn(len(vocab))] }
		sentence := func(n int) string {
			ws := make([]string, n)
			for i := range ws {
				ws[i] = word()
			}
			return strings.Join(ws, " ")
		}
		for i := 0; i < 4000; i++ {
			switch r.intn(10) {
			case 0:
				fmt.Fprintf(w, "reverse %s\n", sentence(8))
			case 1:
				fmt.Fprintf(w, "palindrome %s\n", sentence(3))
			case 2:
				fmt.Fprintf(w, "anagrams %s\n", sentence(12))
			case 3:
				fmt.Fprintf(w, "distance %s %s\n", sentence(1)+word(), word()+word())
			case 4:
				fmt.Fprintf(w, "lcs %s%s%s %s%s\n", word(), word(), word(), word(), word())
			case 5:
				fmt.Fprintf(w, "find %s %s\n", word()[:1], sentence(20))
			case 6:
				fmt.Fprintf(w, "rle %s\n", strings.Repeat(word()[:1], 1+r.intn(30))+word())
			case 7:
				fmt.Fprintf(w, "wrap %d %s\n", 10+r.intn(30), sentence(30))
			case 8:
				fmt.Fprintf(w, "title %s\n", sentence(6))
			default:
				fmt.Fprintf(w, "stats %s\n", sentence(10))
			}
		}
	})

	graph := func(name, header string, nodes, edges int, dag bool, seed uint64) {
		create("06-graph", name, func(w *bufio.Writer) {
			r := rng(seed)
			fmt.Fprintln(w, header)
			// A ring (or chain, for the DAG) keeps most nodes connected.
			for i := 0; i+1 < nodes; i++ {
				fmt.Fprintf(w, "n%d n%d %d\n", i, i+1, 1+r.intn(100))
			}
			for e := 0; e < edges; e++ {
				a, b := r.intn(nodes), r.intn(nodes)
				if dag && a > b {
					a, b = b, a
				}
				if a == b {
					continue
				}
				fmt.Fprintf(w, "n%d n%d %d\n", a, b, 1+r.intn(1000))
			}
			if !dag {
				fmt.Fprintf(w, "n%d n0 %d\n", nodes-1, 1+r.intn(100))
			}
		})
	}
	graph("gen-graph.txt", "directed", 5000, 25000, false, 6)
	graph("gen-ugraph.txt", "undirected", 4000, 16000, false, 7)
	graph("gen-dag.txt", "directed", 3000, 12000, true, 8)

	create("09-pipeline", "gen-access.log", func(w *bufio.Writer) {
		r := rng(9)
		services := []string{"auth", "checkout", "payments", "search", "catalog", "profile"}
		paths := []string{"/login", "/cart", "/pay", "/search", "/item", "/order", "/me", "/logout"}
		levels := []string{"DEBUG", "INFO", "INFO", "INFO", "INFO", "WARN", "ERROR"}
		sec := 0
		for i := 0; i < 60000; i++ {
			sec += r.intn(4)
			if r.intn(997) == 0 {
				w.WriteString("garbage line that cannot be parsed\n")
				continue
			}
			st := 200
			switch k := r.intn(100); {
			case k < 3:
				st = 500 + r.intn(4)
			case k < 8:
				st = 404
			case k < 12:
				st = 201
			}
			lat := 5 + r.intn(50)
			if r.intn(20) == 0 {
				lat += r.intn(3000)
			}
			fmt.Fprintf(w, "2026-03-%02dT%02d:%02d:%02dZ %s %s %d %d %s\n",
				1+sec/86400, sec/3600%24, sec/60%60, sec%60,
				levels[r.intn(len(levels))], services[r.intn(len(services))], lat, st, paths[r.intn(len(paths))])
		}
	})

	create("10-kvstore", "gen-kv.txt", func(w *bufio.Writer) {
		r := rng(10)
		clients := []string{"main", "c1", "c2", "c3"}
		inTxn := ""
		for i := 0; i < 20000; i++ {
			c := clients[r.intn(len(clients))]
			if inTxn != "" && r.intn(3) > 0 {
				c = inTxn
			}
			prefix := ""
			if c != "main" {
				prefix = c + ": "
			}
			key := fmt.Sprintf("key:%03d", r.intn(400))
			switch k := r.intn(20); {
			case k < 7:
				fmt.Fprintf(w, "%sSET %s \"value %d\"\n", prefix, key, r.intn(100000))
			case k < 11:
				fmt.Fprintf(w, "%sGET %s\n", prefix, key)
			case k < 13:
				fmt.Fprintf(w, "%sDEL %s\n", prefix, key)
			case k < 15:
				fmt.Fprintf(w, "%sINCR ctr:%d %d\n", prefix, r.intn(10), r.intn(100)-50)
			case k < 16:
				fmt.Fprintf(w, "%sSCAN key:%d \"\" 5\n", prefix, r.intn(4))
			case k < 17 && inTxn == "":
				fmt.Fprintf(w, "%sBEGIN\n", prefix)
				inTxn = c
			case k < 18 && inTxn == c:
				fmt.Fprintf(w, "%s%s\n", prefix, []string{"COMMIT", "ROLLBACK"}[r.intn(2)])
				inTxn = ""
			default:
				fmt.Fprintf(w, "%sDBSIZE\n", prefix)
			}
			if i%2500 == 2499 && inTxn == "" {
				w.WriteString("CHECKPOINT\n")
			}
		}
		if inTxn != "" {
			p := ""
			if inTxn != "main" {
				p = inTxn + ": "
			}
			fmt.Fprintf(w, "%sCOMMIT\n", p)
		}
		w.WriteString("LOAD 16 200\nSTATS\nCHECKPOINT\n")
	})
}
