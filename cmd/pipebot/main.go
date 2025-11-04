package main

import (
	"fmt"
	"log/slog"
	"math/rand"
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

	tdClient, err := tdlib.NewClient(logger, cfg)
	if err != nil {
		logger.Error("TDLib init failed", "error", err)
		os.Exit(1)
	}
	adminChans, err := tdClient.GetAdminChannelsSimple()
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
			rand.Seed(time.Now().UnixNano())
			dur := randDuration(10, 50)
			time.Sleep(dur)
			logger.Info("New message", "chat_id", msg.ChatID, "text", msg.Text, "duration", dur)
			capper, formatted, err := ps.GetFormatedPrediction(msg, cfg.BasePredictUrl)
			if err != nil {
				logger.Error("GetFormattedPrediction", "chat_id", msg.ChatID, "text", msg.Text, "error", err)
				continue
			}

			chatIdStr, ok := adminChans[capper]
			if !ok || chatIdStr == "" {
				logger.Error("No target channel for capper", "capper", capper)
				continue
			}
			chatId, err := strconv.ParseInt(chatIdStr, 10, 64)
			if err != nil {
				logger.Error("Invalid chat ID for capper", "chat_id str", chatIdStr, "err", err)
				continue
			}
			err = tdClient.SendMessage(chatId, formatted)
			if err != nil {
				logger.Error("SendMessage failed", "chat_name", msg.ChatName, "text", msg.Text, "error", err)
				continue
			}
		}
		logger.Warn("Listen exited — вероятно упало соединение, пробуем снова...")
	}
}
func randDuration(minSec, maxSec int) time.Duration {
	if maxSec <= minSec {
		return time.Duration(minSec) * time.Second
	}
	sec := rand.Intn(maxSec-minSec+1) + minSec
	// добавим миллисекундный джиттер 0..999ms чтобы интервалы выглядили менее ровными
	ms := rand.Intn(1000)
	return time.Duration(sec)*time.Second + time.Duration(ms)*time.Millisecond
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
