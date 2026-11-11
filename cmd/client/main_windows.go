//go:build windows

package main

import (
	"fmt"
	"os"

	"github.com/sa1tbaby/go-network-stat/backend/app"
	"github.com/sa1tbaby/go-network-stat/frontend/desktop"
)

func main() {
	handled, err := app.HandleMode(os.Args[1:], desktop.PickFolderForBackend)
	if handled {
		if err != nil {
			if len(os.Args) > 1 && os.Args[1] == "--pause-monitor" {
				desktop.ShowError(err)
			} else {
				fmt.Fprintln(os.Stderr, err)
			}

			os.Exit(1)
		}

		return
	}

	desktop.Run()
}
