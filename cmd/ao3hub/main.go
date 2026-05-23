package main

import (
	"fmt"
	"log"
	"os"

	"ao3hub/internal/app"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println(app.Version)
		return
	}

	srv, err := app.New()
	if err != nil {
		log.Fatal(err)
	}
	if err := srv.Run(); err != nil {
		log.Fatal(fmt.Errorf("server stopped: %w", err))
	}
}
