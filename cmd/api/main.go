package main

import (
	"log"

	"github.com/nidao003/k12edu-backend/internal/config"
	"github.com/nidao003/k12edu-backend/internal/httpapi"
)

func main() {
	cfg := config.Load()
	log.Printf("k12edu backend listening on %s", cfg.Addr)
	if err := httpapi.NewRouter().Run(cfg.Addr); err != nil {
		log.Fatal(err)
	}
}
