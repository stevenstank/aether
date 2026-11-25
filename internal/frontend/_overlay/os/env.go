package os

// The environment, as gc's syscall keeps it (env_unix.go): envs are the
// "key=value" strings, "" for one unset, and env, made on first use, maps
// each key to its first string, the others of the same key unset. A
// program's goroutines never run in parallel (D-049), so they need no lock.
var (
	envs   []string
	envMap map[string]int
)

func copyenv() {
	if envMap != nil {
		return
	}
	envMap = make(map[string]int)
	for i, s := range envs {
		for j := 0; j < len(s); j++ {
			if s[j] == '=' {
				key := s[:j]
				if _, ok := envMap[key]; !ok {
					envMap[key] = i // first mention of key
				} else {
					envs[i] = ""
				}
				break
			}
		}
	}
}

// Getenv returns the value of the environment variable key, "" if it is
// not set.
func Getenv(key string) string {
	v, _ := LookupEnv(key)
	return v
}

// LookupEnv returns the value of the environment variable key and whether
// it is set.
func LookupEnv(key string) (string, bool) {
	copyenv()
	if len(key) == 0 {
		return "", false
	}
	i, ok := envMap[key]
	if !ok {
		return "", false
	}
	s := envs[i]
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[i+1:], true
		}
	}
	return "", false
}

// Setenv sets the environment variable key to value.
func Setenv(key, value string) error {
	copyenv()
	if len(key) == 0 {
		return NewSyscallError("setenv", errno(einval))
	}
	for i := 0; i < len(key); i++ {
		if key[i] == '=' || key[i] == 0 {
			return NewSyscallError("setenv", errno(einval))
		}
	}
	for i := 0; i < len(value); i++ {
		if value[i] == 0 {
			return NewSyscallError("setenv", errno(einval))
		}
	}
	i, ok := envMap[key]
	kv := key + "=" + value
	if ok {
		envs[i] = kv
	} else {
		i = len(envs)
		envs = append(envs, kv)
	}
	envMap[key] = i
	return nil
}

// Unsetenv unsets the environment variable key.
func Unsetenv(key string) error {
	copyenv()
	if i, ok := envMap[key]; ok {
		envs[i] = ""
		delete(envMap, key)
	}
	return nil
}

// Clearenv deletes all environment variables.
func Clearenv() {
	copyenv()
	envMap = make(map[string]int)
	envs = []string{}
}

// Environ returns the environment, "key=value" strings.
func Environ() []string {
	copyenv()
	a := make([]string, 0, len(envs))
	for _, env := range envs {
		if env != "" {
			a = append(a, env)
		}
	}
	return a
}

// The rest is gc's, from $GOROOT/src/os/env.go.

// Expand replaces ${var} or $var in the string based on the mapping function.
// For example, [os.ExpandEnv](s) is equivalent to [os.Expand](s, [os.Getenv]).
func Expand(s string, mapping func(string) string) string {
	var buf []byte
	// ${} is all ASCII, so bytes are fine for this operation.
	i := 0
	for j := 0; j < len(s); j++ {
		if s[j] == '$' && j+1 < len(s) {
			if buf == nil {
				buf = make([]byte, 0, 2*len(s))
			}
			buf = append(buf, s[i:j]...)
			name, w := getShellName(s[j+1:])
			if name == "" && w > 0 {
				// Encountered invalid syntax; eat the
				// characters.
			} else if name == "" {
				// Valid syntax, but $ was not followed by a
				// name. Leave the dollar character untouched.
				buf = append(buf, s[j])
			} else {
				buf = append(buf, mapping(name)...)
			}
			j += w
			i = j + 1
		}
	}
	if buf == nil {
		return s
	}
	return string(buf) + s[i:]
}

// ExpandEnv replaces ${var} or $var in the string according to the values
// of the current environment variables. References to undefined
// variables are replaced by the empty string.
func ExpandEnv(s string) string {
	return Expand(s, Getenv)
}

// isShellSpecialVar reports whether the character identifies a special
// shell variable such as $*.
func isShellSpecialVar(c uint8) bool {
	switch c {
	case '*', '#', '$', '@', '!', '?', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

// isAlphaNum reports whether the byte is an ASCII letter, number, or underscore.
func isAlphaNum(c uint8) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// getShellName returns the name that begins the string and the number of bytes
// consumed to extract it. If the name is enclosed in {}, it's part of a ${}
// expansion and two more bytes are needed than the length of the name.
func getShellName(s string) (string, int) {
	switch {
	case s[0] == '{':
		if len(s) > 2 && isShellSpecialVar(s[1]) && s[2] == '}' {
			return s[1:2], 3
		}
		// Scan to closing brace
		for i := 1; i < len(s); i++ {
			if s[i] == '}' {
				if i == 1 {
					return "", 2 // Bad syntax; eat "${}"
				}
				return s[1:i], i + 1
			}
		}
		return "", 1 // Bad syntax; eat "${"
	case isShellSpecialVar(s[0]):
		return s[0:1], 1
	}
	// Scan alphanumerics.
	var i int
	for i = 0; i < len(s) && isAlphaNum(s[i]); i++ {
	}
	return s[:i], i
}
