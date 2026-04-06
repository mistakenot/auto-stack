package daemoninstall

import "runtime"

func runtimeGOOS() string {
	return runtime.GOOS
}
