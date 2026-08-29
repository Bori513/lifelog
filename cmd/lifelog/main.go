package main

import (
	"log"
	"os"

	"github.com/Bori513/lifelog/internal/database"
)

const defaultDataDir = "./data"

func main() {
	dataDir := os.Getenv("LIFELOG_DATA_DIR")
	if dataDir == "" {
		dataDir = defaultDataDir
	}

	db, err := database.Open(dataDir)
	if err != nil {
		log.Fatalf("initialize LifeLog: %v", err)
	}
	if err := db.Close(); err != nil {
		log.Fatalf("close LifeLog database: %v", err)
	}
	log.Printf("LifeLog database initialized in %s", dataDir)
}
