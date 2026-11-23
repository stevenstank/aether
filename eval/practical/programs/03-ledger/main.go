// Command ledger reads bank transactions as CSV from standard input and
// prints per-account balances and a monthly summary.
//
// Input columns: date (YYYY-MM-DD), account, kind (deposit, withdraw,
// transfer), amount (decimal with up to two fraction digits), and for a
// transfer the destination account. The first line is a header.
//
// Invalid records are reported and skipped. A withdrawal or transfer that
// would overdraw an account is rejected. Exit status: 0 if every record was
// applied, 1 if some were rejected, 2 if the input could not be read.
package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Cents is an amount of money in hundredths.
type Cents int64

func (c Cents) String() string {
	sign := ""
	if c < 0 {
		sign = "-"
		c = -c
	}
	whole := strconv.FormatInt(int64(c/100), 10)
	// Group thousands.
	var b strings.Builder
	for i, d := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return fmt.Sprintf("%s%s.%02d", sign, b.String(), int64(c%100))
}

func parseCents(s string) (Cents, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty amount")
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	if hasFrac && (len(frac) == 0 || len(frac) > 2) {
		return 0, fmt.Errorf("amount %q: want at most two decimal places", s)
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w < 0 {
		return 0, fmt.Errorf("amount %q: not a non-negative number", s)
	}
	if w > 1_000_000_000 {
		return 0, fmt.Errorf("amount %q: too large", s)
	}
	var f int64
	if hasFrac {
		for len(frac) < 2 {
			frac += "0"
		}
		f, err = strconv.ParseInt(frac, 10, 64)
		if err != nil || f < 0 {
			return 0, fmt.Errorf("amount %q: bad fraction", s)
		}
	}
	return Cents(w*100 + f), nil
}

type Date struct{ Y, M, D int }

var daysIn = [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

func parseDate(s string) (Date, error) {
	parts := strings.Split(s, "-")
	if len(parts) != 3 || len(parts[0]) != 4 || len(parts[1]) != 2 || len(parts[2]) != 2 {
		return Date{}, fmt.Errorf("date %q: want YYYY-MM-DD", s)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil {
			return Date{}, fmt.Errorf("date %q: %v", s, err)
		}
		n[i] = v
	}
	d := Date{n[0], n[1], n[2]}
	if d.M < 1 || d.M > 12 {
		return Date{}, fmt.Errorf("date %q: bad month", s)
	}
	max := daysIn[d.M-1]
	if d.M == 2 && (d.Y%4 == 0 && d.Y%100 != 0 || d.Y%400 == 0) {
		max = 29
	}
	if d.D < 1 || d.D > max {
		return Date{}, fmt.Errorf("date %q: bad day", s)
	}
	return d, nil
}

func (d Date) Less(o Date) bool {
	if d.Y != o.Y {
		return d.Y < o.Y
	}
	if d.M != o.M {
		return d.M < o.M
	}
	return d.D < o.D
}

func (d Date) Month() string { return fmt.Sprintf("%04d-%02d", d.Y, d.M) }

type Txn struct {
	Line    int
	Date    Date
	Account string
	Kind    string
	Amount  Cents
	To      string
}

type Account struct {
	Name     string
	Balance  Cents
	In, Out  Cents
	Count    int
	Rejected int
}

type Ledger struct {
	accounts map[string]*Account
	months   map[string][2]Cents // deposits, withdrawals
}

var ErrInsufficient = errors.New("insufficient funds")

func (l *Ledger) account(name string) *Account {
	a, ok := l.accounts[name]
	if !ok {
		a = &Account{Name: name}
		l.accounts[name] = a
	}
	return a
}

func (l *Ledger) Apply(t Txn) error {
	a := l.account(t.Account)
	m := l.months[t.Date.Month()]
	switch t.Kind {
	case "deposit":
		a.Balance += t.Amount
		a.In += t.Amount
		m[0] += t.Amount
	case "withdraw":
		if a.Balance < t.Amount {
			a.Rejected++
			return fmt.Errorf("withdraw %v from %s (balance %v): %w", t.Amount, a.Name, a.Balance, ErrInsufficient)
		}
		a.Balance -= t.Amount
		a.Out += t.Amount
		m[1] += t.Amount
	case "transfer":
		if t.To == t.Account {
			return fmt.Errorf("transfer from %s to itself", t.Account)
		}
		if a.Balance < t.Amount {
			a.Rejected++
			return fmt.Errorf("transfer %v from %s (balance %v): %w", t.Amount, a.Name, a.Balance, ErrInsufficient)
		}
		b := l.account(t.To)
		a.Balance -= t.Amount
		a.Out += t.Amount
		b.Balance += t.Amount
		b.In += t.Amount
		b.Count++
	}
	a.Count++
	l.months[t.Date.Month()] = m
	return nil
}

func parseRecord(line int, rec []string) (Txn, error) {
	if len(rec) < 4 {
		return Txn{}, fmt.Errorf("want at least 4 fields, got %d", len(rec))
	}
	d, err := parseDate(strings.TrimSpace(rec[0]))
	if err != nil {
		return Txn{}, err
	}
	acct := strings.TrimSpace(rec[1])
	if acct == "" {
		return Txn{}, errors.New("empty account")
	}
	kind := strings.ToLower(strings.TrimSpace(rec[2]))
	amt, err := parseCents(rec[3])
	if err != nil {
		return Txn{}, err
	}
	if amt == 0 {
		return Txn{}, errors.New("zero amount")
	}
	t := Txn{Line: line, Date: d, Account: acct, Kind: kind, Amount: amt}
	switch kind {
	case "deposit", "withdraw":
	case "transfer":
		if len(rec) < 5 || strings.TrimSpace(rec[4]) == "" {
			return Txn{}, errors.New("transfer without destination")
		}
		t.To = strings.TrimSpace(rec[4])
	default:
		return Txn{}, fmt.Errorf("unknown kind %q", rec[2])
	}
	return t, nil
}

func main() {
	r := csv.NewReader(os.Stdin)
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	r.Comment = '#'

	header, err := r.Read()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ledger: reading header:", err)
		os.Exit(2)
	}
	if len(header) < 4 || strings.ToLower(header[0]) != "date" {
		fmt.Fprintln(os.Stderr, "ledger: missing header line")
		os.Exit(2)
	}

	var txns []Txn
	bad := 0
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		line, _ := r.FieldPos(0)
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				fmt.Fprintf(os.Stderr, "ledger: fatal: %v\n", err)
				os.Exit(2)
			}
			fmt.Fprintln(os.Stderr, "ledger:", err)
			os.Exit(2)
		}
		t, err := parseRecord(line, rec)
		if err != nil {
			fmt.Printf("line %d: invalid record: %v\n", line, err)
			bad++
			continue
		}
		txns = append(txns, t)
	}

	// Apply in date order; records with the same date keep input order.
	sort.SliceStable(txns, func(i, j int) bool { return txns[i].Date.Less(txns[j].Date) })

	l := &Ledger{accounts: map[string]*Account{}, months: map[string][2]Cents{}}
	for _, t := range txns {
		if err := l.Apply(t); err != nil {
			fmt.Printf("line %d: rejected: %v\n", t.Line, err)
			bad++
		}
	}

	names := make([]string, 0, len(l.accounts))
	for n := range l.accounts {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Println()
	fmt.Printf("%-12s %14s %14s %14s %5s %4s\n", "ACCOUNT", "IN", "OUT", "BALANCE", "TXNS", "REJ")
	var total Cents
	for _, n := range names {
		a := l.accounts[n]
		total += a.Balance
		fmt.Printf("%-12s %14v %14v %14v %5d %4d\n", a.Name, a.In, a.Out, a.Balance, a.Count, a.Rejected)
	}
	fmt.Printf("%-12s %14s %14s %14v\n", "TOTAL", "", "", total)

	months := make([]string, 0, len(l.months))
	for m := range l.months {
		months = append(months, m)
	}
	sort.Strings(months)
	fmt.Println()
	for _, m := range months {
		v := l.months[m]
		fmt.Printf("%s  deposits %12v  withdrawals %12v  net %12v\n", m, v[0], v[1], v[0]-v[1])
	}
	if bad > 0 {
		fmt.Fprintf(os.Stderr, "ledger: %d record(s) invalid or rejected\n", bad)
		os.Exit(1)
	}
}
