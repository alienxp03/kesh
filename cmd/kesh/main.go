// Command kesh launches the Kesh terminal workspace picker.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/alienxp03/dotfiles/apps/kesh/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var usage *app.UsageError
		if errors.As(err, &usage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
