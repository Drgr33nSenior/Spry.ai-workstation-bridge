package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/contract"
)

func main() {
	check := flag.Bool("check", false, "verify the checked-in contract is reproducible")
	flag.Parse()
	b, e := contract.Generate()
	if e != nil {
		panic(e)
	}
	if *check {
		old, e := os.ReadFile("api/openapi.json")
		if e != nil || !bytes.Equal(old, b) {
			fmt.Fprintln(os.Stderr, "OpenAPI generation drift: run make generate")
			os.Exit(1)
		}
		return
	}
	if e = os.MkdirAll("api", 0755); e != nil {
		panic(e)
	}
	if e = os.WriteFile("api/openapi.json", b, 0644); e != nil {
		panic(e)
	}
}
