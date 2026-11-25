// The environment, gc's $GOROOT/src/syscall/env_unix.go without its locks
// (an Aether program's goroutines never run in parallel, D-049) and its
// runtime notifications (D-054): envs are the "key=value" strings, "" for
// one unset, and env, made on first use, maps each key to its first
// string, the others of the same key unset.

package syscall

var (
	env map[string]int

	envs []string = runtimeEnvs()
)

// envc and env are the program's environment strings, as execve passed
// them: intrinsics (D-051).
func envc() int
func envString(i int) string

func runtimeEnvs() []string {
	a := make([]string, envc())
	for i := range a {
		a[i] = envString(i)
	}
	return a
}

func copyenv() {
	if env != nil {
		return
	}
	env = make(map[string]int)
	for i, s := range envs {
		for j := 0; j < len(s); j++ {
			if s[j] == '=' {
				key := s[:j]
				if _, ok := env[key]; !ok {
					env[key] = i // first mention of key
				} else {
					envs[i] = ""
				}
				break
			}
		}
	}
}

func Unsetenv(key string) error {
	copyenv()
	if i, ok := env[key]; ok {
		envs[i] = ""
		delete(env, key)
	}
	return nil
}

func Getenv(key string) (value string, found bool) {
	copyenv()
	if len(key) == 0 {
		return "", false
	}
	i, ok := env[key]
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

func Setenv(key, value string) error {
	copyenv()
	if len(key) == 0 {
		return EINVAL
	}
	for i := 0; i < len(key); i++ {
		if key[i] == '=' || key[i] == 0 {
			return EINVAL
		}
	}
	for i := 0; i < len(value); i++ {
		if value[i] == 0 {
			return EINVAL
		}
	}
	i, ok := env[key]
	kv := key + "=" + value
	if ok {
		envs[i] = kv
	} else {
		i = len(envs)
		envs = append(envs, kv)
	}
	env[key] = i
	return nil
}

func Clearenv() {
	copyenv()
	env = make(map[string]int)
	envs = []string{}
}

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
