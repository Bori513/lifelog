package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Bori513/lifelog/internal/database"
	"github.com/Bori513/lifelog/internal/web"
)

const defaultDataDir = "./data"
const defaultAddr = ":8080"

func main() {
	dataDir := os.Getenv("LIFELOG_DATA_DIR")
	if dataDir == "" {
		dataDir = defaultDataDir
	}

	db, err := database.Open(dataDir)
	if err != nil {
		log.Fatalf("initialize LifeLog: %v", err)
	}
	defer db.Close()
	addr := os.Getenv("LIFELOG_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	secureCookies := truthy(os.Getenv("LIFELOG_SECURE_COOKIES"))
	app, err := web.New(db, secureCookies, log.Default())
	if err != nil {
		log.Fatalf("initialize web application: %v", err)
	}
	server := &http.Server{Addr: addr, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("LifeLog listening on %s (data: %s)", addr, dataDir)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve LifeLog: %v", err)
	}
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
