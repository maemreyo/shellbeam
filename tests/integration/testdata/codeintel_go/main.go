package main

import (
	"fmt"

	"example.com/codeintelfixture/lib"
)

func render(name string) string {
	greeter := lib.NewGreeter(name)
	return greeter.Message()
}

func main() {
	fmt.Println("Việt Nam 😊", render("Thế giới"))
}

func helperOne() {}
func helperTwo() {}
