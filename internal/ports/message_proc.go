package ports

import "github.com/larriantoniy/tg_user_bot/internal/domain"

type MessageProc interface {
	Process(msg domain.Message) error
}
