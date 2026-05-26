package beta

import "example.com/circular/alpha"

func World() string {
	return "beta says " + alpha.Hello()
}
