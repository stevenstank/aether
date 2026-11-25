// timefmt checks gc's time compiled by Aether (D-054): parsing,
// formatting, arithmetic, Duration, zones from the system's zoneinfo, and
// Time's comparison and use as a map key.
package main

import (
	"fmt"
	"sort"
	"time"
)

func main() {
	layouts := []string{time.RFC3339, time.RFC3339Nano, time.RFC1123Z, time.Kitchen, time.DateTime, "2006-01-02 15:04:05.000 MST", "Jan _2 06 3:04PM"}
	t, err := time.Parse(time.RFC3339Nano, "2026-03-01T10:15:02.123456789+05:30")
	fmt.Println(t, err)
	for _, l := range layouts {
		s := t.Format(l)
		back, err := time.Parse(l, s)
		fmt.Printf("%q %v %v\n", s, back.Equal(t), err != nil)
	}
	for _, bad := range []string{"2026-13-01T00:00:00Z", "x", "2026-02-30T00:00:00Z", "2026-03-01T25:00:00Z"} {
		_, err := time.Parse(time.RFC3339, bad)
		fmt.Println(err)
	}
	u := t.UTC()
	fmt.Println(u, u.Unix(), u.UnixNano(), u.Weekday(), u.YearDay(), u.Truncate(time.Hour), u.Round(time.Minute))
	y, w := u.ISOWeek()
	fmt.Println(y, w, u.AddDate(0, 1, -1), u.Add(-36*time.Hour).Before(u), u.Sub(u.AddDate(-1, 0, 0)))
	for _, d := range []time.Duration{0, 1, 999, 1500 * time.Microsecond, 90 * time.Minute, -2*time.Hour - time.Nanosecond, 1<<63 - 1} {
		fmt.Println(d, d.Seconds(), d.Truncate(time.Second), d.Round(time.Millisecond), d.Abs())
	}
	for _, s := range []string{"1h2m3.5s", "-1.5h", "300ms", "2us", "1µs", "bogus", "1d"} {
		d, err := time.ParseDuration(s)
		fmt.Println(d, err)
	}
	for _, name := range []string{"UTC", "America/New_York", "Asia/Tokyo", "Nowhere/City"} {
		loc, err := time.LoadLocation(name)
		if err != nil {
			fmt.Println(err)
			continue
		}
		in := time.Date(2026, 7, 4, 12, 0, 0, 0, loc)
		zn, off := in.Zone()
		fmt.Println(loc, in, zn, off, in.In(time.UTC), in.IsDST())
	}
	fixed := time.FixedZone("X", -3*3600)
	fmt.Println(time.Date(2026, 1, 1, 0, 0, 0, 0, fixed).Format(time.RFC822Z))
	counts := map[time.Time]int{}
	for _, s := range []string{"2026-03-01T10:15:02Z", "2026-03-01T10:45:00Z", "2026-03-01T11:00:00Z"} {
		p, _ := time.Parse(time.RFC3339, s)
		counts[p.Truncate(time.Hour)]++
	}
	keys := make([]time.Time, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })
	for _, k := range keys {
		fmt.Println(k.Format("15:04"), counts[k])
	}
	fmt.Println(time.Unix(1e9, 0).UTC(), time.UnixMilli(-1).UTC(), time.Time{}.IsZero(), time.Month(13), time.Saturday)
	now := time.Now()
	fmt.Println(now.After(t), time.Since(now) < time.Minute, now.Round(0).Equal(now))
}
