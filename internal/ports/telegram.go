package ports

import (
	"github.com/larriantoniy/tg_pipe_bot/internal/domain"
	"github.com/zelenin/go-tdlib/client"
)

// TelegramClient определяет интерфейс для работы с Telegram
// Реализуется конкретными адаптерами (TDLib, Bot API и т.д.).
type TelegramClient interface {
	// Listen возвращает канал доменных сообщений
	Listen() (<-chan domain.Message, error)
	ProcessUpdateNewMessage(out chan domain.Message, upd *client.UpdateNewMessage) (<-chan domain.Message, error)
	GetAdminChannels() (map[string]string, error)
	SendMessage(chatID int64, text string) error
}
