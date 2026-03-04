package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Aaron-QL/PersonalDevopsPlatform/control-api/internal/config"
	"github.com/Aaron-QL/PersonalDevopsPlatform/control-api/internal/server"
	mongostore "github.com/Aaron-QL/PersonalDevopsPlatform/control-api/internal/store/mongo"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	logger, err := buildLogger(cfg.LogLevel)
	if err != nil {
		log.Fatalf("failed to build logger: %v", err)
	}
	defer logger.Sync()

	mongoClient, err := mongostore.NewClient(cfg.MongoURI)
	if err != nil {
		logger.Fatal("failed to connect to mongodb", zap.Error(err))
	}
	logger.Info("connected to mongodb", zap.String("uri", cfg.MongoURI))

	router := server.NewRouter(mongoClient)
	srv := server.New(cfg.ServerPort, router, logger)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Run(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	<-quit
	logger.Info("shutdown signal received")
	srv.Shutdown()
}

func buildLogger(level string) (*zap.Logger, error) {
	if level == "debug" {
		return zap.NewDevelopment()
	}
	return zap.NewProduction()
}
