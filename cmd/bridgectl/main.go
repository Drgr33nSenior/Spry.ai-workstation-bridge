package main

import (
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/cli"
	"os"
)

func main() { os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr)) }
