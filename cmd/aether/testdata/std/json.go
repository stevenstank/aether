package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type Color int

func (c Color) MarshalText() ([]byte, error) { return []byte(fmt.Sprintf("c%d", int(c))), nil }
func (c *Color) UnmarshalText(b []byte) error {
	var n int
	_, err := fmt.Sscanf(string(b), "c%d", &n)
	*c = Color(n)
	return err
}

type Temp float64

func (t Temp) MarshalJSON() ([]byte, error) { return []byte(fmt.Sprintf(`"%.1fC"`, float64(t))), nil }
func (t *Temp) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"C`)
	var f float64
	_, err := fmt.Sscan(s, &f)
	*t = Temp(f)
	return err
}

type Base struct {
	ID      int       `json:"id"`
	Created time.Time `json:"created"`
}

type Address struct {
	City string `json:"city"`
	Zip  string `json:"zip,omitempty"`
}

type Person struct {
	Base
	Name     string          `json:"name"`
	Age      int             `json:"age,omitempty"`
	Email    *string         `json:"email"`
	Tags     []string        `json:"tags"`
	Scores   map[string]int  `json:"scores,omitempty"`
	Addr     *Address        `json:"address,omitempty"`
	Secret   string          `json:"-"`
	Count    int64           `json:"count,string"`
	Raw      json.RawMessage `json:"raw,omitempty"`
	Data     []byte          `json:"data"`
	Colors   map[Color]bool  `json:"colors,omitempty"`
	Temp     Temp            `json:"temp"`
	Extra    any             `json:"extra"`
	Nested   [2]Address      `json:"nested"`
	internal int
	Ratio    float64        `json:"ratio"`
	ByID     map[int]string `json:"by_id,omitempty"`
}

func show(label string, v any) {
	b, err := json.Marshal(v)
	fmt.Printf("%s: %s %v\n", label, b, err)
}

func main() {
	email := "ada@example.org"
	p := Person{
		Base:   Base{ID: 7, Created: time.Date(2026, 3, 14, 15, 9, 26, 535000000, time.UTC)},
		Name:   "Ada <Lovelace> & co",
		Email:  &email,
		Tags:   []string{"math", "poet"},
		Scores: map[string]int{"z": 1, "a": 2, "m": 3},
		Addr:   &Address{City: "London"},
		Secret: "hidden",
		Count:  1234567890123,
		Raw:    json.RawMessage(`{"pre":"computed"}`),
		Data:   []byte{0, 1, 2, 250, 255},
		Colors: map[Color]bool{3: true, 1: false},
		Temp:   21.25,
		Extra:  map[string]any{"list": []any{1, "two", nil, true, 2.5}},
		Ratio:  1.0 / 3,
		ByID:   map[int]string{10: "ten", 2: "two", -1: "neg"},
	}
	b, err := json.MarshalIndent(p, "", "  ")
	fmt.Println(string(b), err)

	var q Person
	err = json.Unmarshal(b, &q)
	fmt.Println(err, q.Name, q.ID, q.Created.Format(time.RFC3339Nano), *q.Email, q.Tags, q.Scores["m"], q.Addr.City,
		q.Secret == "", q.Count, string(q.Raw), q.Data, q.Colors, q.Temp, q.Extra, q.Ratio, q.ByID)

	show("nil slice/map/ptr", struct {
		S []int
		M map[string]int
		P *int
		I any
	}{})
	show("empty", struct {
		S []int          `json:"s"`
		M map[string]int `json:"m"`
	}{[]int{}, map[string]int{}})
	show("floats", []float64{0, -0.0, 1e21, 1e20, 1.5e-7, 123456789.125, math.MaxFloat64, 1e-320})
	show("ints", []any{int8(-128), uint64(math.MaxUint64), int64(math.MinInt64), uintptr(9)})
	show("strings", []string{"  ", "tab\tnew\nline", "quote\"back\\", "\x00\x1f", "héllo 世界", "<script>"})
	show("nan", math.NaN())
	show("chan", make(chan int))
	show("bool keys", map[bool]int{true: 1})

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent(">", "\t")
	enc.Encode(map[string]string{"html": "<b>&</b>"})
	fmt.Print(buf.String())

	var anyv any
	err = json.Unmarshal([]byte(`{"a": [1, 2.5, "x", null, {"b": false}], "n": 1e3}`), &anyv)
	fmt.Printf("%v %T %v\n", anyv, anyv.(map[string]any)["n"], err)

	dec := json.NewDecoder(strings.NewReader(`{"n": 12345678901234567890} {"n": 1.5} [1,2] "tail"`))
	dec.UseNumber()
	for {
		var v any
		if err := dec.Decode(&v); err != nil {
			fmt.Println("decode end:", err)
			break
		}
		fmt.Printf("%v ", v)
	}
	fmt.Println()

	errs := []string{`{"name": 5}`, `{"name": "x",}`, `[1, 2`, `{"age": 1.5}`, `{"count": "abc"}`, `{"temp": "hot"}`, `nul`, `{"age": 99999999999999999999}`}
	for _, in := range errs {
		var x Person
		err := json.Unmarshal([]byte(in), &x)
		var se *json.SyntaxError
		var te *json.UnmarshalTypeError
		fmt.Printf("%-28s %v | syntax=%v type=%v\n", in, err, errors.As(err, &se), errors.As(err, &te))
	}
	d2 := json.NewDecoder(strings.NewReader(`{"name": "x", "bogus": 1}`))
	d2.DisallowUnknownFields()
	var x Person
	fmt.Println(d2.Decode(&x))
	fmt.Println(json.Unmarshal([]byte(`{}`), nil), json.Unmarshal([]byte(`{}`), x))

	// Decoding into existing values merges.
	existing := Person{Name: "keep", Tags: []string{"old"}, Scores: map[string]int{"old": 1}}
	json.Unmarshal([]byte(`{"tags": ["new"], "scores": {"new": 2}, "address": {"zip": "1"}}`), &existing)
	fmt.Println(existing.Name, existing.Tags, existing.Scores, *existing.Addr)

	var out bytes.Buffer
	fmt.Println(json.Indent(&out, []byte(`{"a":[1,{"b":2}],"c":{}}`), "", "  "), out.String())
	out.Reset()
	fmt.Println(json.Compact(&out, []byte("{ \"a\" : [ 1 , 2 ] }")), out.String(), json.Valid([]byte(`{"x":}`)), json.Valid([]byte(`[]`)))
	var arr [3]int
	var sl []int
	var ip *int
	var iface fmt.Stringer
	fmt.Println(json.Unmarshal([]byte(`[1,2,3,4]`), &arr), arr, json.Unmarshal([]byte(`[5]`), &sl), sl,
		json.Unmarshal([]byte(`7`), &ip), *ip, json.Unmarshal([]byte(`"s"`), &iface))
	type Emb struct {
		*Address
		Name string
	}
	show("embedded ptr", Emb{Address: &Address{City: "Paris", Zip: "75"}, Name: "e"})
	show("embedded nil", Emb{Name: "e"})
	var e Emb
	fmt.Println(json.Unmarshal([]byte(`{"city":"Rome","Name":"r"}`), &e), e.Address.City, e.Name)
	show("anon", struct {
		A, B int
		C    struct{ D string } `json:"c"`
	}{1, 2, struct{ D string }{"d"}})
	show("time", time.Date(2001, 2, 3, 4, 5, 6, 0, time.FixedZone("X", 3600)))
	show("duration", 90*time.Second)
	show("number", json.Number("12.50"))
}
