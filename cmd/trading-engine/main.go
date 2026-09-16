package main

import (
	"log"

	"github.com/ErenKarakus1/Trading-Engine/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
