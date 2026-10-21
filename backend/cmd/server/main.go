package main

import (
	"campusclaw/internal/mcp"
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"campusclaw/internal/apidocs"
	"campusclaw/internal/ask"
	"campusclaw/internal/assistant"
	"campusclaw/internal/auth"
	"campusclaw/internal/config"
	"campusclaw/internal/conversation"
	"campusclaw/internal/db"
	"campusclaw/internal/dialogue"
	"campusclaw/internal/materials"
	"campusclaw/internal/retrieval"
	"campusclaw/internal/tutor"
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
	if err := db.Repair(startCtx, conn); err != nil {
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
	vectorReady := func(context.Context) bool { return !cfg.VectorEnabled() }
	mux := http.NewServeMux()
	apidocs.Register(mux)
	// @Route GET /health
	// @Summary 存活检查
	// @Role public
	// @Response 200 Health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		auth.JSON(w, 200, map[string]string{"status": "ok"})
	})
	// @Route GET /ready
	// @Summary 本地依赖就绪检查
	// @Role public
	// @Response 200 Ready
	// @Error 503 not_ready / vector_not_ready
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if !matSvc.Ready(ctx) {
			auth.Error(w, 503, "not_ready")
			return
		}
		if !vectorReady(ctx) {
			auth.Error(w, 503, "vector_not_ready")
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
	tutorSvc := &tutor.Service{DB: conn}
	mux.HandleFunc("GET /api/agents", authSvc.Class(tutorSvc.Agents))
	mux.HandleFunc("GET /api/agents/{id}", authSvc.Class(tutorSvc.Agent))
	mux.HandleFunc("POST /api/agents", authSvc.Teacher(tutorSvc.CreateAgent))
	mux.HandleFunc("PUT /api/agents/{id}", authSvc.Teacher(tutorSvc.UpdateAgent))
	mux.HandleFunc("GET /api/skills", authSvc.Class(tutorSvc.Skills))
	mux.HandleFunc("GET /api/skills/{id}", authSvc.Class(tutorSvc.GetSkill))
	mux.HandleFunc("POST /api/skills", authSvc.Teacher(tutorSvc.CreateSkill))
	mux.HandleFunc("POST /api/skills/preview", authSvc.Teacher(tutorSvc.Preview))
	mux.HandleFunc("GET /api/skills/{id}/export", authSvc.Teacher(tutorSvc.Export))
	mux.HandleFunc("PUT /api/skills/{id}", authSvc.Teacher(tutorSvc.UpdateSkill))
	mux.HandleFunc("DELETE /api/skills/{id}", authSvc.Teacher(tutorSvc.RemoveSkill))
	assistantSvc := &assistant.Service{DB: conn}
	mux.HandleFunc("GET /api/assistant/prompt", authSvc.Teacher(assistantSvc.GetPrompt))
	mux.HandleFunc("PUT /api/assistant/prompt", authSvc.Teacher(assistantSvc.PutPrompt))
	mux.HandleFunc("GET /api/assistant/skills/guided-problem-solving", authSvc.Teacher(assistantSvc.GetSkill))
	mux.HandleFunc("PUT /api/assistant/skills/guided-problem-solving", authSvc.Teacher(assistantSvc.PutSkill))
	mcpSvc := &mcp.Service{DB: conn, Secret: cfg.SessionSecret, DefaultToken: cfg.MCPAuthToken, DefaultURL: cfg.MCPDefaultURL, AllowedURLs: strings.Split(cfg.MCPAllowedURLs, ","), MaxRounds: cfg.MCPMaxRounds, MaxTurnCalls: cfg.MCPMaxTurnCalls, MaxConversationCalls: cfg.MCPMaxConversationCalls}
	mux.HandleFunc("GET /api/mcp/servers", authSvc.Teacher(mcpSvc.Servers))
	mux.HandleFunc("POST /api/mcp/servers", authSvc.Teacher(mcpSvc.Create))
	mux.HandleFunc("PUT /api/mcp/servers/{id}", authSvc.Teacher(mcpSvc.Update))
	mux.HandleFunc("DELETE /api/mcp/servers/{id}", authSvc.Teacher(mcpSvc.Remove))
	mux.HandleFunc("POST /api/mcp/servers/{id}/connect", authSvc.Teacher(mcpSvc.ConnectHTTP))
	mux.HandleFunc("POST /api/mcp/servers/{id}/disconnect", authSvc.Teacher(mcpSvc.DisconnectHTTP))
	mux.HandleFunc("GET /api/mcp/servers/{id}/tools", authSvc.Teacher(mcpSvc.ToolsHTTP))
	mux.HandleFunc("POST /api/mcp/servers/{id}/tools/approve", authSvc.Teacher(mcpSvc.ApproveHTTP))
	mux.HandleFunc("GET /api/mcp/tool-calls", authSvc.Teacher(mcpSvc.AuditHTTP))
	mux.HandleFunc("GET /api/mcp/tool-changes", authSvc.Teacher(mcpSvc.ChangesHTTP))
	conversationSvc := &conversation.Service{DB: conn}
	var dialogueGateway dialogue.Gateway = dialogue.UnavailableGateway{}
	if cfg.DialogueEnabled() {
		configuredGateway, err := dialogue.NewHTTPGateway(cfg.DialogueBaseURL, cfg.DialogueModel, cfg.DialogueAPIKey, time.Duration(cfg.DialogueTimeout)*time.Second)
		if err != nil {
			log.Fatal(err)
		}
		dialogueGateway = configuredGateway
	}
	retrievalSvc := &retrieval.Service{DB: conn}
	indexer := &retrieval.Indexer{DB: conn, Dimension: cfg.EmbeddingDimension}
	if cfg.VectorEnabled() {
		embedder := &retrieval.HTTPEmbedder{
			BaseURL:   cfg.EmbeddingBaseURL,
			Model:     cfg.EmbeddingModel,
			APIKey:    cfg.EmbeddingAPIKey,
			Dimension: cfg.EmbeddingDimension,
			HTTP:      &http.Client{Timeout: time.Duration(cfg.EmbeddingTimeout) * time.Second},
		}
		vectorIndex := &retrieval.QdrantIndex{
			BaseURL:    cfg.QdrantBaseURL,
			Collection: cfg.QdrantCollection,
			APIKey:     cfg.QdrantAPIKey,
			HTTP:       &http.Client{Timeout: time.Duration(cfg.QdrantTimeout) * time.Second},
		}
		vectorReady = func(ctx context.Context) bool {
			return vectorIndex.EnsureCollection(ctx, cfg.EmbeddingDimension) == nil
		}
		retrievalSvc.Embedder, retrievalSvc.Vectors = embedder, vectorIndex
		indexer.Embedder, indexer.Vectors = embedder, vectorIndex
		if err := vectorIndex.EnsureCollection(startCtx, cfg.EmbeddingDimension); err != nil {
			log.Printf("vector collection unavailable at startup: %v", err)
		} else {
			if err := indexer.Backfill(startCtx); err != nil {
				log.Printf("knowledge backfill incomplete: %v", err)
			}
			if err := indexer.Reconcile(startCtx); err != nil {
				log.Printf("vector reconciliation incomplete: %v", err)
			}
		}
	}
	matSvc.Indexer = indexer
	retrievalHTTP := &retrieval.HTTPService{DB: conn, Retriever: retrievalSvc, Indexer: indexer}
	mux.HandleFunc("POST /api/retrieval/search", authSvc.Class(retrievalHTTP.Search))
	mux.HandleFunc("POST /api/materials/{id}/reindex", authSvc.Teacher(retrievalHTTP.Rebuild))
	askSvc := &ask.Service{
		Retriever: retrievalSvc, Gateway: dialogue.ExtractiveGateway{}, Model: cfg.DialogueModel,
		MaxTokens: cfg.DialogueMaxOutputTokens, Timeout: time.Duration(cfg.DialogueTimeout) * time.Second,
	}
	if cfg.DialogueEnabled() {
		askSvc.Gateway = dialogueGateway
	}
	mux.HandleFunc("POST /api/ask", authSvc.Class(askSvc.Ask))
	conversationSvc.Orch = &conversation.Orchestrator{
		MCP:                mcpSvc,
		Retriever:          retrievalSvc,
		Gateway:            dialogueGateway,
		Model:              cfg.DialogueModel,
		MaxTokens:          cfg.DialogueMaxOutputTokens,
		HistoryMaxMessages: cfg.HistoryMaxMessages,
		HistoryMaxChars:    cfg.HistoryMaxCharacters,
		Timeout:            time.Duration(cfg.DialogueTimeout) * time.Second,
	}
	mux.HandleFunc("POST /api/conversations", conversation.Student(authSvc, conversationSvc.CreateConversation))
	mux.HandleFunc("GET /api/conversations", conversation.Student(authSvc, conversationSvc.ListConversations))
	mux.HandleFunc("GET /api/conversations/{id}", conversation.Student(authSvc, conversationSvc.GetConversation))
	mux.HandleFunc("POST /api/conversations/{id}/messages", conversation.Student(authSvc, conversationSvc.PostMessage))
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
