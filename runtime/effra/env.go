package effra

import "os"

func Env(name string) Effect[string] {
	return func(*FiberContext) Exit[string] { return Succeed(os.Getenv(name)) }
}
