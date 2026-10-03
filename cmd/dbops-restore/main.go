package main

import (
	"context"
	"flag"
	"github.com/aimdotsh/dbops/internal/platformbackup"
	"log"
)

func main() {
	source := flag.String("snapshot", "", "snapshot directory")
	dest := flag.String("destination", "", "new data directory (must not exist)")
	flag.Parse()
	if *source == "" || *dest == "" {
		log.Fatal("--snapshot and --destination are required")
	}
	if err := platformbackup.Restore(context.Background(), *source, *dest); err != nil {
		log.Fatal(err)
	}
	log.Print("SQLite snapshot restored; configure original master key and software storage before starting the server")
}
