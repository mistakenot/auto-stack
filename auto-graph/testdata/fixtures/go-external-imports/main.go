package main

import (
	"fmt"
	"net/http"

	"github.com/other/pkg"
)

func main() {
	fmt.Println(http.StatusOK)
	pkg.Do()
}
