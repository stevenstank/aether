// Command lzh is a small compression utility with its own LZ77 + Huffman
// format. It can also compare itself against gzip.
//
//	lzh c [-level 0-6] < in > out
//	lzh d < in > out
//	lzh bench file ...
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"text/tabwriter"
)

func entropy(data []byte) float64 {
	if len(data) == 0 {
		return 0
	}
	var counts [256]int
	for _, b := range data {
		counts[b]++
	}
	h := 0.0
	for _, c := range counts {
		if c > 0 {
			p := float64(c) / float64(len(data))
			h -= p * math.Log2(p)
		}
	}
	return h
}

func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	zw.Name = "data"
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzip(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func ratio(a, b int) string {
	if a == 0 {
		return "-"
	}
	return fmt.Sprintf("%.3f", float64(b)/float64(a))
}

func bench(files []string, level int) error {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "file\tsize\tentropy\tlzh\tratio\tgzip-9\tratio\tblocks\tmatches\tlongest\tcrc32\tgzip sha256[:12]\tok")
	var firstErr error
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			fmt.Fprintf(tw, "%s\t(error: %v)\n", name, errors.Unwrap(err))
			continue
		}
		var st Stats
		packed := Compress(data, level, &st)
		back, err := Decompress(bytes.NewReader(packed), uint64(len(data)))
		ok := err == nil && bytes.Equal(back, data)
		gz, err := gzipBytes(data)
		if err != nil {
			return err
		}
		ungz, err := gunzip(gz)
		ok = ok && err == nil && bytes.Equal(ungz, data)
		sum := sha256.Sum256(gz)
		fmt.Fprintf(tw, "%s\t%d\t%.3f\t%d\t%s\t%d\t%s\t%d\t%d\t%d\t%08x\t%s\t%v\n",
			name, len(data), entropy(data), len(packed), ratio(len(data), len(packed)),
			len(gz), ratio(len(data), len(gz)), st.Blocks, st.Matches, st.LongestMatch,
			crc32.ChecksumIEEE(data), hex.EncodeToString(sum[:])[:12], ok)
	}
	tw.Flush()
	return firstErr
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: lzh c|d|bench [flags] [files]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	level := fs.Int("level", 4, "compression effort 0 (store literals) to 6")
	verbose := fs.Bool("v", false, "print statistics to stderr")
	limit := fs.Uint64("max", 1<<30, "largest output size accepted when decompressing")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	if *level < 0 || *level > 6 {
		fmt.Fprintf(os.Stderr, "lzh: level %d out of range 0-6\n", *level)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "c":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "lzh:", err)
			os.Exit(1)
		}
		var st Stats
		out := Compress(data, *level, &st)
		os.Stdout.Write(out)
		if *verbose {
			fmt.Fprintf(os.Stderr, "%d -> %d bytes (%s), %d literals, %d matches covering %d bytes, %d blocks\n",
				len(data), len(out), ratio(len(data), len(out)), st.Literals, st.Matches, st.MatchBytes, st.Blocks)
		}
	case "d":
		out, err := Decompress(os.Stdin, *limit)
		if err != nil {
			fmt.Fprintln(os.Stderr, "lzh: decompress:", err)
			var fe *FormatError
			if errors.As(err, &fe) && errors.Is(err, io.ErrUnexpectedEOF) {
				fmt.Fprintln(os.Stderr, "lzh: input is truncated")
			} else if errors.Is(err, ErrCorrupt) {
				fmt.Fprintln(os.Stderr, "lzh: input is corrupt")
			}
			os.Exit(1)
		}
		os.Stdout.Write(out)
	case "bench":
		if err := bench(fs.Args(), *level); err != nil {
			fmt.Fprintln(os.Stderr, "lzh:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "lzh: unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}
