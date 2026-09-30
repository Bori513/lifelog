package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Bori513/lifelog/internal/backup"
	"github.com/Bori513/lifelog/internal/database"
	"github.com/Bori513/lifelog/internal/web"
)

const defaultDataDir = "./data"
const defaultAddr = ":8080"
const shutdownTimeout = 10 * time.Second

func main() {
	backupMode, err := backupCommand(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	dataDir := os.Getenv("LIFELOG_DATA_DIR")
	if dataDir == "" {
		dataDir = defaultDataDir
	}
	if backupMode {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		name, err := createServerBackup(ctx, dataDir, os.Getenv("LIFELOG_BACKUP_DIR"))
		if err != nil {
			log.Fatalf("create server backup: %v", err)
		}
		fmt.Println(name)
		return
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
	app, err := web.NewConfigured(db, dataDir, os.Getenv("LIFELOG_BACKUP_DIR"), secureCookies, log.Default())
	if err != nil {
		log.Fatalf("initialize web application: %v", err)
	}
	server := &http.Server{Addr: addr, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("LifeLog listening on %s (data: %s)", addr, dataDir)
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve LifeLog: %v", err)
		}
		return
	case <-signalContext.Done():
		log.Print("shutting down LifeLog")
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("graceful shutdown: %v", err)
	}
}

func backupCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if len(args) == 1 && args[0] == "backup" {
		return true, nil
	}
	return false, errors.New("usage: lifelog [backup]")
}

func createServerBackup(ctx context.Context, dataDir, backupDir string) (string, error) {
	db, err := database.Open(dataDir)
	if err != nil {
		return "", fmt.Errorf("initialize LifeLog: %w", err)
	}
	defer db.Close()
	return backup.New(db, dataDir, backupDir).CreateServer(ctx)
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
