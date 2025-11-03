package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/larriantoniy/tg_pipe_bot/internal/adapters/tdlib"
	"github.com/larriantoniy/tg_pipe_bot/internal/config"
	prediction "github.com/larriantoniy/tg_pipe_bot/internal/useCases"
)

const (
	envDev  = "dev"
	envProd = "prod"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// либо log.Fatalf, либо panic с читаемым сообщением
		fmt.Fprintf(os.Stderr, "ошибка загрузки конфига: %v\n", err)
		os.Exit(1)
	}
	logger := setupLogger(cfg.Env)
	ps := prediction.NewPredictionService(logger)

	tdClient, err := tdlib.NewClient(cfg.APIID, cfg.APIHash, logger)
	if err != nil {
		logger.Error("TDLib init failed", "error", err)
		os.Exit(1)
	}
	adminChans, err := tdClient.GetAdminChannels()
	if err != nil {
		logger.Error("TDLib get admin channels failed", "error", err)
	}
	for {
		updates, err := tdClient.Listen()
		if err != nil {
			logger.Error("Listen failed, retrying", "error", err)
			time.Sleep(time.Second) // можно увеличить backoff по желанию
			continue
		}

		for msg := range updates {

			logger.Info("New message", "chat_id", msg.ChatID, "text", msg.Text)
			capper, formatted, err := ps.GetFormatedPrediction(msg, cfg.BasePredictUrl)
			if err != nil {
				logger.Error("GetFormattedPrediction", "chat_id", msg.ChatID, "text", msg.Text, "error", err)
			}
			chatIdStr := adminChans[capper]
			chatId, err := strconv.ParseInt(chatIdStr, 10, 64)
			if err != nil {
				logger.Error("GetFormattedPrediction ", "chat_id str", chatIdStr, "err", err)
			}
			err = tdClient.SendMessage(chatId, formatted)
			if err != nil {
				logger.Error("SendMessage failed", "chat_name", msg.ChatName, "text", msg.Text, "error", err)
			}
		}

		logger.Warn("Listen exited — вероятно упало соединение, пробуем снова")
	}
}

func setupLogger(env string) *slog.Logger {
	var logger *slog.Logger

	switch env {
	case envDev:
		logger = slog.New(
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
		)
	case envProd:
		logger = slog.New(
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}),
		)
	}

	return logger
}
