package util

import "example.com/basic/service"

func Format(s string) string {
	return service.Name() + ": " + s
}
