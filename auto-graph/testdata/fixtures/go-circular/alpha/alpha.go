package alpha

import "example.com/circular/beta"

func Hello() string {
	return "alpha says " + beta.World()
}
