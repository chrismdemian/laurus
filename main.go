package main

import (
	"os"

	"github.com/chrismdemian/quercus/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
