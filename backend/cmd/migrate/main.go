// migrate is a standalone CLI tool that imports legacy SQLite data into the
// new MySQL schema. It preserves user/class/material IDs, password hashes,
// timestamps, content order, and upload files. Old sessions are never migrated.
//
// Usage:
//
//	migrate --sqlite /path/to/campusclaw.sqlite3 --uploads /path/to/uploads
//
// It requires the same DB_* and SESSION_SECRET environment variables as the API.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"campusclaw/internal/config"
	"campusclaw/internal/db"
	"campusclaw/internal/migrate"
)

func main() {
	sqlitePath := flag.String("sqlite", "", "path to legacy SQLite database")
	uploadsDir := flag.String("uploads", "", "path to legacy uploads directory")
	flag.Parse()
	if *sqlitePath == "" || *uploadsDir == "" {
		fmt.Fprintln(os.Stderr, "usage: migrate --sqlite PATH --uploads PATH")
		os.Exit(2)
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	conn, err := db.Open(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := db.Wait(ctx, conn); err != nil {
		log.Fatal(err)
	}
	if err := db.Migrate(ctx, conn); err != nil {
		log.Fatal(err)
	}
	report, err := migrate.Run(ctx, *sqlitePath, *uploadsDir, conn, cfg.UploadDir)
	if err != nil {
		log.Fatalf("migration failed: %v", err)
	}
	fmt.Printf("migration complete: classes=%d users=%d materials=%d chunks=%d files=%d anomalies=%d\n",
		report.Classes, report.Users, report.Materials, report.Chunks, report.Files, len(report.Anomalies))
	for _, a := range report.Anomalies {
		fmt.Printf("  anomaly: %s\n", a)
	}
}
