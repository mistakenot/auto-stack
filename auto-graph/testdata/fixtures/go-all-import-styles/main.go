package main

import (
	"fmt"

	"example.com/styles/pkg"

	_ "example.com/styles/pkg"

	. "example.com/styles/pkg"

	alias "example.com/styles/pkg"
)

func main() {
	fmt.Println(pkg.Hello())
	fmt.Println(Hello())
	fmt.Println(alias.Hello())
}
