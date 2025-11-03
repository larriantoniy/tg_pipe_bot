package tdlib

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/larriantoniy/tg_pipe_bot/internal/domain"
	"github.com/larriantoniy/tg_pipe_bot/internal/ports"
	"github.com/zelenin/go-tdlib/client"
)

// TDLibClient реализует ports.TelegramClient через go-tdlib
type TDLibClient struct {
	client *client.Client
	logger *slog.Logger
	selfId int64
}

// NewClient создаёт и авторизует TDLib клиента
func NewClient(apiID int32, apiHash string, logger *slog.Logger) (ports.TelegramClient, error) {
	// Параметры TDLib
	tdParams := &client.SetTdlibParametersRequest{
		ApiId:              apiID,
		ApiHash:            apiHash,
		SystemLanguageCode: "en",
		DeviceModel:        "GoUserBot",
		ApplicationVersion: "0.1",
		UseMessageDatabase: true,
		UseFileDatabase:    true,
		DatabaseDirectory:  "./tdlib-db",
		FilesDirectory:     "./tdlib-files",
	}
	if _, err := client.SetLogVerbosityLevel(&client.SetLogVerbosityLevelRequest{
		NewVerbosityLevel: 1,
	}); err != nil {
		logger.Error("TDLib SetLogVerbosity level", "error", err)
	}
	// Авторизатор и CLI-интерактор
	authorizer := client.ClientAuthorizer(tdParams)
	go client.CliInteractor(authorizer)

	// Создаём клиента
	tdClient, err := client.NewClient(authorizer)
	if err != nil {
		logger.Error("TDLib NewClient error", "error", err)
		return nil, err
	}
	// Получаем информацию о себе (боте) — понадобится для GetChatMember
	me, err := tdClient.GetMe()
	if err != nil {
		logger.Error("GetMe failed", "error", err)
		return nil, err
	}
	logger.Info("TDLib client initialized and authorized", "self_id", me.Id)
	return &TDLibClient{client: tdClient, logger: logger, selfId: me.Id}, nil
}

// JoinChannel подписывается на публичный канал по его username, если ещё не подписан
func (t *TDLibClient) JoinChannel(username string) error {
	// Ищем чат по username
	chat, err := t.client.SearchPublicChat(&client.SearchPublicChatRequest{
		Username: username,
	})
	if err != nil {
		t.logger.Error("SearchPublicChat failed", "username", username, "error", err)
		return err
	}

	// Пытаемся подписаться
	_, err = t.client.JoinChat(&client.JoinChatRequest{
		ChatId: chat.Id,
	})
	if err != nil {
		// Telegram вернёт ошибку, если уже в канале — можно логировать как инфо
		t.logger.Error("JoinChat failed", "chat_id", chat.Id, "error", err)
		return err
	}

	t.logger.Info("Joined channel", "channel", username)
	return nil
}

// Listen возвращает канал доменных сообщений из TDLib и запускает обработку обновлений
func (t *TDLibClient) Listen() (<-chan domain.Message, error) {
	out := make(chan domain.Message)

	// Получаем слушатель обновлений
	listener := t.client.GetListener()
	go func() {
		defer close(out)
		for update := range listener.Updates {
			t.logger.Debug("Received new message")
			if upd, ok := update.(*client.UpdateNewMessage); ok {
				_, err := t.ProcessUpdateNewMessage(out, upd)
				if err != nil {
					t.logger.Error("Error process UpdateNewMessage msg content type", "upd MessageContentType", upd.Message.Content.MessageContentType())
				}
			}
		}
	}()

	return out, nil
}

func (t *TDLibClient) GetAdminChannelsSimple() (map[string]string, error) {
	const prefix = "Слив Платок " // вот тут ключевое изменение

	res := make(map[string]string)

	chats, err := t.client.GetChats(&client.GetChatsRequest{Limit: 1000})
	if err != nil {
		return nil, fmt.Errorf("failed to get chats: %w", err)
	}

	lp := strings.ToLower(prefix)

	for _, chatID := range chats.ChatIds {
		chat, err := t.client.GetChat(&client.GetChatRequest{ChatId: chatID})
		if err != nil {
			continue
		}

		// Только супергруппы/каналы
		if _, ok := chat.Type.(*client.ChatTypeSupergroup); !ok {
			continue
		}

		title := strings.TrimSpace(chat.Title)
		if !strings.HasPrefix(strings.ToLower(title), lp) {
			continue
		}

		// Имя каппера после префикса
		name := strings.TrimSpace(title[len(prefix):])
		name = strings.TrimRight(name, ",.;: \t")
		if name == "" {
			continue
		}

		res[name] = fmt.Sprintf("%d", chat.Id)
	}

	return res, nil
}

func normalizeCapper(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "ё", "е")
	return s
}

func (t *TDLibClient) getChatTitle(chatID int64) (string, error) {
	chat, err := t.client.GetChat(&client.GetChatRequest{
		ChatId: chatID,
	})
	if err != nil {
		return "", err
	}

	return chat.Title, nil
}

func (t *TDLibClient) ProcessUpdateNewMessage(out chan domain.Message, upd *client.UpdateNewMessage) (<-chan domain.Message, error) {
	chatName, err := t.getChatTitle(upd.Message.ChatId)
	if err != nil {
		t.logger.Info("Error getting chat title", err)
		chatName = ""
	}
	switch content := upd.Message.Content.(type) {
	case *client.MessageText:
		return t.processMessageText(out, content, upd.Message.ChatId, chatName)
	default:
		t.logger.Debug("cant switch type update", "upd message MessageContentType()", upd.Message.Content.MessageContentType())
		return out, nil
	}
}

func (t *TDLibClient) processMessageText(out chan domain.Message, msg *client.MessageText, msgChatId int64, ChatName string) (<-chan domain.Message, error) {
	out <- domain.Message{
		ChatID:   msgChatId,
		Text:     msg.Text.Text,
		ChatName: ChatName,
	}
	return out, nil
}
func (t *TDLibClient) SendMessage(chatID int64, text string) error {

	// Формируем контент сообщения
	content := &client.InputMessageText{
		Text: &client.FormattedText{
			Text: text,
		},
		ClearDraft: true,
	}
	t.logger.Debug("Sending message", "chat_id", chatID, "content", content)
	// Отправляем
	_, err := t.client.SendMessage(&client.SendMessageRequest{
		ChatId:              chatID,
		InputMessageContent: content,
	})

	if err != nil {
		t.logger.Error("SendMessage failed",
			"chatID", chatID,
			"error", err,
		)
		return err
	}

	t.logger.Info("Message sent",
		"chatID", chatID,
		"text", text,
	)

	return nil
}
