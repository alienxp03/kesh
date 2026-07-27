// Command kesh launches the Kesh terminal workspace picker.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/alienxp03/kesh/internal/app"
)

var version = "dev"

func main() {
	args := os.Args[1:]
	if versionRequested(args) {
		fmt.Println(version)
		return
	}
	if err := app.Run(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var usage *app.UsageError
		if errors.As(err, &usage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func versionRequested(args []string) bool {
	return len(args) == 1 && (args[0] == "-v" || args[0] == "--version")
}
