package main

import (
	"log"

	"vibe-talk/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
