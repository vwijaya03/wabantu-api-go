package realtime

import "fmt"

// LogfTo adapts a structured logger (msg, kv...) to the printf-style Logf.
func LogfTo(log func(msg string, kv ...any)) func(string, ...any) {
	return func(f string, a ...any) { log(fmt.Sprintf(f, a...)) }
}
