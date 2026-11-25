package interp

import (
	"os"
	"slices"
	"strings"
	"sync"
)

// hostCPUFMA is ir.SysCPUFMA's value (D-053): 1 if the host's processor and
// kernel provide AVX and FMA, as the "avx" and "fma" flags of
// /proc/cpuinfo say (Linux lists "avx" only if the kernel saves the AVX
// state, which internal/cpu's XGETBV check requires), else 0.
var hostCPUFMA = sync.OnceValue(func() int64 {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	for line := range strings.Lines(string(data)) {
		name, flags, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) != "flags" {
			continue
		}
		f := strings.Fields(flags)
		if slices.Contains(f, "avx") && slices.Contains(f, "fma") {
			return 1
		}
		return 0
	}
	return 0
})
