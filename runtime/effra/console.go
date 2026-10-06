package effra

import "fmt"

func Println(message string) Effect[Unit] {
	return func(*FiberContext) Exit[Unit] { fmt.Println(message); return Succeed(Unit{}) }
}
