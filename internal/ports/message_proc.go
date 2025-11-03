package ports

import "github.com/larriantoniy/tg_pipe_bot/internal/domain"

type MessageProc interface {
	Process(msg domain.Message) error
}
