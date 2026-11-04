// mutate-gui is a desktop application rendered directly with Gio/GPU.
// The original Go CLI still owns the authenticated local package service.
package main

import (
	"fmt"
	"os"

	"gioui.org/app"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Uso: mutate-gui http://127.0.0.1:<porta>/<sessao>/")
		os.Exit(2)
	}
	service, err := newClient(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	go func() {
		if err := runWindow(service); err != nil {
			fmt.Fprintln(os.Stderr, "Mutate GUI:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}
