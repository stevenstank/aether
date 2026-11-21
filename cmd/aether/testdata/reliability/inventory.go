// An inventory report: records parsed from CSV-like text, grouped and
// sorted, then formatted with fmt's width, precision, and %v of structs,
// slices, maps, and pointers to structs. Ends in a panic from a
// goroutine-free invariant check, after deferred cleanup prints.
package main

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

type Item struct {
	SKU      string
	Name     string
	Category string
	Qty      int
	Price    float64
	Tags     []string
}

type Stats struct {
	Count int
	Units int
	Value float64
	Min   *Item
	Max   *Item
}

const data = `sku,name,category,qty,price,tags
A-100,Widget,hardware,40,2.50,small;metal
A-101,Gadget,hardware,12,12.99,
B-200,Notebook,stationery,200,1.25,paper
B-201,Pen (blue),stationery,500,0.40,ink;plastic
B-202,Pen (red),stationery,0,0.40,ink;plastic
C-300,Café mug,kitchen,30,7.00,ceramic
C-301,Teapot,kitchen,3,24.50,ceramic;large
C-302,Spoon,kitchen,120,0.95,metal
D-400,Gizmo,hardware,7,99.99,electronic
bad line without enough fields
E-500,Thing,misc,x,1.00,
`

type parseError struct {
	line int
	err  error
}

func (e *parseError) Error() string { return fmt.Sprintf("line %d: %v", e.line, e.err) }
func (e *parseError) Unwrap() error { return e.err }

func parse(text string) ([]Item, []error) {
	var items []Item
	var errs []error
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for n, l := range lines[1:] {
		f := strings.Split(l, ",")
		if len(f) != 6 {
			errs = append(errs, &parseError{n + 2, fmt.Errorf("%d fields", len(f))})
			continue
		}
		q, err := strconv.Atoi(f[3])
		if err != nil {
			errs = append(errs, &parseError{n + 2, err})
			continue
		}
		p, err := strconv.ParseFloat(f[4], 64)
		if err != nil {
			errs = append(errs, &parseError{n + 2, err})
			continue
		}
		var tags []string
		if f[5] != "" {
			tags = strings.Split(f[5], ";")
		}
		items = append(items, Item{f[0], f[1], f[2], q, p, tags})
	}
	return items, errs
}

func title(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

func main() {
	defer func() { fmt.Println("deferred: report finished") }()
	items, errs := parse(data)
	for _, e := range errs {
		fmt.Fprintln(os.Stdout, "skip:", e)
	}
	byCat := map[string]*Stats{}
	for i := range items {
		it := &items[i]
		s := byCat[it.Category]
		if s == nil {
			s = &Stats{Min: it, Max: it}
			byCat[it.Category] = s
		}
		s.Count++
		s.Units += it.Qty
		s.Value += float64(it.Qty) * it.Price
		if it.Price < s.Min.Price {
			s.Min = it
		}
		if it.Price > s.Max.Price {
			s.Max = it
		}
	}
	cats := make([]string, 0, len(byCat))
	for c := range byCat {
		cats = append(cats, c)
	}
	slices.SortFunc(cats, func(a, b string) int {
		switch va, vb := byCat[a].Value, byCat[b].Value; {
		case va > vb:
			return -1
		case va < vb:
			return 1
		}
		return 0
	})
	fmt.Printf("%-12s|%5s|%7s|%10s| %s\n", "category", "n", "units", "value", "cheapest..dearest")
	fmt.Println(strings.Repeat("-", 60))
	for _, c := range cats {
		s := byCat[c]
		fmt.Printf("%-12s|%5d|%7d|%10.2f| %s..%s\n", title(c), s.Count, s.Units, s.Value, s.Min.Name, s.Max.Name)
	}
	// Insertion sort keeps equal prices in order, as a stable sort does.
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Price < items[j-1].Price; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	for _, it := range items[:3] {
		fmt.Printf("%v\n", it)
	}
	fmt.Printf("%+v\n", items[len(items)-1])
	tagCount := map[string]int{}
	for _, it := range items {
		for _, t := range it.Tags {
			tagCount[t]++
		}
	}
	fmt.Println("tags:", tagCount)
	fmt.Printf("%q %x %08.3f %-6d| %t %c %U\n", items[0].Name, items[0].SKU, items[0].Price, items[0].Qty, len(items[0].Tags) > 0, 'é', 'é')
	fmt.Printf("%6.2f%% %e %g\n", 12.3456, 123456.789, 1e21)
	var sb strings.Builder
	for i, it := range items {
		if it.Qty == 0 {
			fmt.Fprintf(&sb, "[%d:%s out]", i, it.SKU)
		}
	}
	fmt.Println(sb.String(), len("Café"), len([]rune("Café")))
	total := 0.0
	for _, s := range byCat {
		total += s.Value
	}
	if total > 1000 {
		panic(fmt.Sprintf("inventory value %.2f over budget", total))
	}
}
