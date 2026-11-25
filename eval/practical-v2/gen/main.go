// Command gen writes the large, deterministic fixtures (inputs/gen-*) of
// the v2 evaluation programs. It uses its own linear congruential
// generator, so output does not depend on math/rand. Run from this
// directory: go run .
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type lcg struct{ s uint64 }

func (l *lcg) next() uint64 {
	l.s = l.s*6364136223846793005 + 1442695040888963407
	return l.s >> 33
}

func (l *lcg) intn(n int) int { return int(l.next() % uint64(n)) }

func (l *lcg) pick(xs []string) string { return xs[l.intn(len(xs))] }

var words = strings.Fields(`the of and to in is that for it as with was on be by this are from or have an
they which one you were all we her she there would their will when who him been has more if no out do so
can what up said about other into than its time only could new them man some these then two first may any
like now my such make over our even most me state after also made many did must before back see through way
where get much go well your know should down work year because come people just say each those take day good
how long very own being three under might great still while last us part too place right came life around
compiler runtime garbage collector goroutine channel interface pointer slice escape register allocation
lexer parser token grammar optimizer inline loop vector scheduler stack heap mutex atomic buffer cache
café naïve façade über straße 東京 数据 données ελληνικά`)

func write(path string, fill func(w *bufio.Writer)) {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	w := bufio.NewWriterSize(f, 1<<16)
	fill(w)
	if err := w.Flush(); err != nil {
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
	st, _ := os.Stat(path)
	fmt.Printf("%-60s %9d bytes\n", path, st.Size())
}

func sentence(r *lcg, n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = r.pick(words)
	}
	s := strings.Join(parts, " ")
	first, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(first)) + s[size:] + "."
}

func prog(name string) string { return filepath.Join("..", "programs", name, "inputs") }

func main() {
	// 01-logstat: 60,000 access-log lines over two days with some noise.
	write(filepath.Join(prog("01-logstat"), "gen-access.log"), func(w *bufio.Writer) {
		r := &lcg{s: 1}
		methods := []string{"GET", "GET", "GET", "GET", "POST", "PUT", "DELETE", "PATCH"}
		paths := []string{"/", "/health", "/api/users/%d", "/api/users/%d/orders", "/api/orders/%d/items/%d",
			"/static/app.%d.js", "/api/search?q=w%d", "/api/sessions", "/admin/reports/%d"}
		agents := []string{"curl/8.4.0", "Mozilla/5.0 (X11; Linux x86_64)", "kube-probe/1.29", "Go-http-client/1.1", "python-requests/2.31"}
		sec := int64(1773446400) // 2026-03-14T00:00:00Z
		for i := 0; i < 60000; i++ {
			sec += int64(r.intn(6))
			if r.intn(997) == 0 {
				fmt.Fprintf(w, "corrupted line %d %s\n", i, r.pick(words))
				continue
			}
			p := r.pick(paths)
			if strings.Contains(p, "%d") {
				args := []any{r.intn(5000)}
				if strings.Count(p, "%d") == 2 {
					args = append(args, r.intn(20))
				}
				p = fmt.Sprintf(p, args...)
			}
			status := 200
			switch x := r.intn(100); {
			case x < 3:
				status = 500 + r.intn(4)
			case x < 10:
				status = 404
			case x < 13:
				status = 401
			case x < 20:
				status = 304
			case x < 25:
				status = 201
			}
			size := "-"
			if status != 304 {
				size = fmt.Sprint(r.intn(50000))
			}
			lat := fmt.Sprintf("%d.%03dms", r.intn(300), r.intn(1000))
			if r.intn(50) == 0 {
				lat = fmt.Sprintf("%d.%ds", 1+r.intn(5), r.intn(10))
			}
			frac := ""
			if r.intn(2) == 0 {
				frac = fmt.Sprintf(".%03d", r.intn(1000))
			}
			ts := fmt.Sprintf("2026-03-%02dT%02d:%02d:%02d%sZ", 14+(sec-1773446400)/86400, (sec/3600)%24, (sec/60)%60, sec%60, frac)
			fmt.Fprintf(w, "%s 10.%d.%d.%d %s %s %d %s %s \"%s\"\n", ts, r.intn(4), r.intn(256), r.intn(256),
				r.pick(methods), p, status, size, lat, r.pick(agents))
		}
	})

	// 02-markdown: a ~1 MB document of varied block structure.
	write(filepath.Join(prog("02-markdown"), "gen-book.md"), func(w *bufio.Writer) {
		r := &lcg{s: 2}
		for ch := 1; ch <= 300; ch++ {
			fmt.Fprintf(w, "# Chapter %d: %s\n\n", ch, sentence(r, 3))
			for sec := 1; sec <= 4; sec++ {
				fmt.Fprintf(w, "## Section %d.%d %s\n\n", ch, sec, r.pick(words))
				for p := 0; p < 3; p++ {
					s := sentence(r, 12+r.intn(30))
					ws := strings.Fields(s)
					for k := range ws {
						switch r.intn(25) {
						case 0:
							ws[k] = "**" + ws[k] + "**"
						case 1:
							ws[k] = "*" + ws[k] + "*"
						case 2:
							ws[k] = "`" + ws[k] + "`"
						case 3:
							ws[k] = fmt.Sprintf("[%s](https://example.org/%d)", ws[k], r.intn(1000))
						}
					}
					w.WriteString(strings.Join(ws, " ") + "\n\n")
				}
				switch r.intn(5) {
				case 0:
					for k := 0; k < 3+r.intn(5); k++ {
						fmt.Fprintf(w, "- %s\n", sentence(r, 5))
						if r.intn(3) == 0 {
							fmt.Fprintf(w, "  - nested %s\n", r.pick(words))
						}
					}
					w.WriteString("\n")
				case 1:
					fmt.Fprintf(w, "```go\nfunc f%d(x int) int {\n\treturn x * %d // <%s>\n}\n```\n\n", ch*10+sec, r.intn(100), r.pick(words))
				case 2:
					fmt.Fprintf(w, "> %s\n> %s\n\n", sentence(r, 8), sentence(r, 6))
				case 3:
					w.WriteString("| key | value | note |\n|:---|---:|:---:|\n")
					for k := 0; k < 4; k++ {
						fmt.Fprintf(w, "| %s | %d | %s |\n", r.pick(words), r.intn(100000), r.pick(words))
					}
					w.WriteString("\n")
				case 4:
					for k := 1; k <= 3; k++ {
						fmt.Fprintf(w, "%d. %s\n", k, sentence(r, 4))
					}
					w.WriteString("\n")
				}
			}
		}
	})

	// 03-tmpl: a catalog of 4,000 products.
	write(filepath.Join(prog("03-tmpl"), "gen-catalog.json"), func(w *bufio.Writer) {
		r := &lcg{s: 3}
		w.WriteString("{\"title\": \"Catalog <2026>\", \"currency\": \"$\", \"products\": [\n")
		for i := 0; i < 4000; i++ {
			if i > 0 {
				w.WriteString(",\n")
			}
			var tags []string
			for k := 0; k < r.intn(4); k++ {
				tags = append(tags, fmt.Sprintf("%q", r.pick(words)))
			}
			fmt.Fprintf(w, "  {\"sku\": \"P-%05d\", \"name\": %q, \"price\": %d.%02d, \"stock\": %d, \"featured\": %v, \"tags\": [%s], \"added\": \"2025-%02d-%02dT10:00:00Z\"}",
				i, sentence(r, 2+r.intn(5)), r.intn(5000), r.intn(100), r.intn(50), r.intn(7) == 0, strings.Join(tags, ", "), 1+r.intn(12), 1+r.intn(28))
		}
		w.WriteString("\n]}\n")
	})

	// 04-regex: ~2 MB of mixed log-like text.
	write(filepath.Join(prog("04-regex"), "gen-text.txt"), func(w *bufio.Writer) {
		r := &lcg{s: 4}
		for i := 0; i < 30000; i++ {
			switch r.intn(4) {
			case 0:
				fmt.Fprintf(w, "2026-%02d-%02d %02d:%02d:%02d %s user=%s ip=%d.%d.%d.%d %s\n", 1+r.intn(12), 1+r.intn(28),
					r.intn(24), r.intn(60), r.intn(60), r.pick([]string{"INFO", "WARN", "ERROR", "DEBUG"}), r.pick(words),
					r.intn(256), r.intn(256), r.intn(256), r.intn(256), sentence(r, 4))
			case 1:
				fmt.Fprintf(w, "contact %s.%s@%s.example.com or call +1-%03d-%03d-%04d\n", r.pick(words), r.pick(words),
					r.pick(words), r.intn(1000), r.intn(1000), r.intn(10000))
			default:
				w.WriteString(sentence(r, 6+r.intn(14)) + "\n")
			}
		}
	})

	// 05-deps: a registry of 400 packages with layered dependencies.
	write(filepath.Join(prog("05-deps"), "gen-registry.json"), func(w *bufio.Writer) {
		r := &lcg{s: 5}
		w.WriteString("{\"packages\": {\n")
		const n = 400
		for p := 0; p < n; p++ {
			if p > 0 {
				w.WriteString(",\n")
			}
			fmt.Fprintf(w, "  \"pkg%03d\": {", p)
			nv := 1 + r.intn(6)
			for v := 0; v < nv; v++ {
				if v > 0 {
					w.WriteString(", ")
				}
				major, minor := v/3+1, v%3
				fmt.Fprintf(w, "\"%d.%d.%d\": {\"deps\": {", major, minor, r.intn(4))
				nd := 0
				if p+1 < n {
					nd = r.intn(4)
				}
				seen := map[int]bool{}
				first := true
				for d := 0; d < nd; d++ {
					dep := p + 1 + r.intn(min(40, n-p-1))
					if seen[dep] {
						continue
					}
					seen[dep] = true
					cons := []string{"^1", "^1.1", ">=1.0.0 <3", "~1.0", "1.x || 2.x", "*", "^2"}[r.intn(7)]
					if !first {
						w.WriteString(", ")
					}
					first = false
					fmt.Fprintf(w, "\"pkg%03d\": %q", dep, cons)
				}
				w.WriteString("}}")
			}
			w.WriteString("}")
		}
		w.WriteString("\n}}\n")
	})
	write(filepath.Join(prog("05-deps"), "gen-manifest.json"), func(w *bufio.Writer) {
		w.WriteString(`{"name": "big-app", "dependencies": {"pkg000": "*", "pkg001": "*", "pkg002": "^1", "pkg003": "*", "pkg010": "*"}}` + "\n")
	})

	// 06-compress: text, CSV, and pseudo-random binary inputs.
	write(filepath.Join(prog("06-compress"), "gen-text.txt"), func(w *bufio.Writer) {
		r := &lcg{s: 6}
		total := 0
		for total < 1<<20 {
			s := sentence(r, 5+r.intn(20)) + "\n"
			total += len(s)
			w.WriteString(s)
		}
	})
	write(filepath.Join(prog("06-compress"), "gen-table.csv"), func(w *bufio.Writer) {
		r := &lcg{s: 7}
		w.WriteString("id,date,region,product,quantity,unit_price\n")
		for i := 0; i < 12000; i++ {
			fmt.Fprintf(w, "%d,2026-%02d-%02d,%s,%s,%d,%d.%02d\n", i, 1+r.intn(12), 1+r.intn(28),
				[]string{"north", "south", "east", "west"}[r.intn(4)], r.pick(words), 1+r.intn(40), r.intn(900), r.intn(100))
		}
	})
	write(filepath.Join(prog("06-compress"), "gen-noise.bin"), func(w *bufio.Writer) {
		r := &lcg{s: 8}
		for i := 0; i < 1<<18; i++ {
			w.WriteByte(byte(r.next()))
		}
	})

	// 08-minidb: a long workload script.
	write(filepath.Join(prog("08-minidb"), "gen-load.sql"), func(w *bufio.Writer) {
		r := &lcg{s: 9}
		w.WriteString("CREATE TABLE orders (id INT PRIMARY KEY, customer TEXT NOT NULL, region TEXT, amount FLOAT, qty INT, ref TEXT UNIQUE);\n")
		w.WriteString("CREATE INDEX orders_customer ON orders (customer);\nCREATE INDEX orders_amount ON orders (amount);\nCREATE INDEX orders_qty ON orders (qty);\n")
		regions := []string{"'north'", "'south'", "'east'", "'west'", "NULL"}
		id := 0
		for batch := 0; batch < 200; batch++ {
			if batch%10 == 0 {
				w.WriteString("BEGIN;\n")
			}
			w.WriteString("INSERT INTO orders VALUES ")
			for k := 0; k < 100; k++ {
				id++
				if k > 0 {
					w.WriteString(", ")
				}
				fmt.Fprintf(w, "(%d, 'cust%04d', %s, %d.%02d, %d, 'R%07d')", id, r.intn(3000), r.pick(regions), r.intn(10000), r.intn(100), 1+r.intn(50), id*7919%10000000)
			}
			w.WriteString(";\n")
			switch batch % 10 {
			case 3:
				fmt.Fprintf(w, "UPDATE orders SET amount = amount * 1.05, qty = qty + 1 WHERE customer = 'cust%04d';\n", r.intn(3000))
			case 5:
				fmt.Fprintf(w, "DELETE FROM orders WHERE qty = %d AND amount < %d;\n", 1+r.intn(50), r.intn(5000))
			case 7:
				fmt.Fprintf(w, "INSERT INTO orders VALUES (%d, 'dup', NULL, 1, 1, 'X');\n", 1+r.intn(id))
			case 9:
				if batch%20 == 9 {
					w.WriteString("ROLLBACK;\n")
				} else {
					w.WriteString("COMMIT;\n")
				}
			}
		}
		w.WriteString("SELECT COUNT(*), SUM(qty), ROUND(SUM(amount), 2), MIN(amount), MAX(amount) FROM orders;\n")
		w.WriteString("SELECT region, COUNT(*) AS n, ROUND(AVG(amount), 2) AS avg FROM orders GROUP BY region ORDER BY n DESC, region;\n")
		w.WriteString("SELECT customer, COUNT(*) AS n, SUM(qty) FROM orders GROUP BY customer HAVING COUNT(*) >= 5 ORDER BY n DESC, customer LIMIT 10;\n")
		for q := 0; q < 300; q++ {
			fmt.Fprintf(w, "SELECT id, amount FROM orders WHERE customer = 'cust%04d' ORDER BY id LIMIT 3;\n", r.intn(3000))
		}
		w.WriteString("SELECT COUNT(*) FROM orders WHERE amount BETWEEN 1000 AND 2000;\n")
		w.WriteString("EXPLAIN SELECT * FROM orders WHERE amount > 9990;\n")
		w.WriteString("SELECT id, customer, amount FROM orders WHERE amount > 9990 ORDER BY amount DESC, id;\n")
		w.WriteString("DELETE FROM orders WHERE id % 3 = 0;\nUPDATE orders SET region = 'central' WHERE region IS NULL;\n")
		w.WriteString("SHOW TABLES;\nCHECKPOINT;\nSHOW TABLES;\n")
		w.WriteString("SELECT region, COUNT(*) FROM orders GROUP BY region ORDER BY region;\n")
		w.WriteString("DELETE FROM orders WHERE amount < 5000;\nSHOW TABLES;\nSELECT COUNT(*) FROM orders;\n")
	})
}
