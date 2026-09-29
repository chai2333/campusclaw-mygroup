package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"campusclaw/internal/auth"
	"campusclaw/internal/config"
	"campusclaw/internal/db"
	"campusclaw/internal/materials"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	conn, err := db.Open(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	startCtx, startCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer startCancel()
	if err := db.Wait(startCtx, conn); err != nil {
		log.Fatal(err)
	}
	if err := db.Migrate(startCtx, conn); err != nil {
		log.Fatal(err)
	}
	if err := db.Seed(startCtx, conn, cfg.TeacherPassword, cfg.StudentPassword, cfg.UploadDir); err != nil {
		log.Fatal(err)
	}
	authSvc, err := auth.New(conn, cfg)
	if err != nil {
		log.Fatal(err)
	}
	matSvc := &materials.Service{DB: conn, Dir: cfg.UploadDir, MaxBytes: int64(cfg.MaxUploadBytes)}
	if err := matSvc.Reconcile(startCtx); err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		auth.JSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if !matSvc.Ready(ctx) {
			auth.Error(w, 503, "not_ready")
			return
		}
		auth.JSON(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("POST /api/login", authSvc.Login)
	mux.HandleFunc("GET /api/me", authSvc.Require(authSvc.Me))
	mux.HandleFunc("POST /api/logout", authSvc.Require(authSvc.Logout))
	mux.HandleFunc("GET /api/materials", authSvc.Class(matSvc.List))
	mux.HandleFunc("POST /api/materials", authSvc.Teacher(matSvc.Upload))
	mux.HandleFunc("GET /api/materials/{id}", authSvc.Class(matSvc.Detail))
	mux.HandleFunc("GET /api/materials/{id}/file", authSvc.Class(matSvc.Download))
	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		log.Fatal(server.ListenAndServe())
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	_ = server.Shutdown(shutCtx)
}
