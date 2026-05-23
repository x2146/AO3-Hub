package main

import (
	"fmt"
	"log"

	"ao3hub/internal/app"
)

func main() {
	srv, err := app.New()
	if err != nil {
		log.Fatal(err)
	}
	if err := srv.Run(); err != nil {
		log.Fatal(fmt.Errorf("server stopped: %w", err))
	}
}
