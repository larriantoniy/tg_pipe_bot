package parse

import (
	"bufio"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// валидатор отсутствия исхода (ставки типа Ф1/П1/ТБ и т.д.)
var outcomeRe = regexp.MustCompile(`(?i)\b(Ф[12]\s*\([^)]*\)|П[12]\b|(?:^|\W)X(?:$|\W)|\b1X\b|\b12\b|\bX2\b|Т[БМ]\s*\d+(\.\d+)?|\bОЗ\b|\bобе забьют\b)`)

// Каппер из первой строки: "Каппер - NeNaZavode добавил,"
var capperLineRe = regexp.MustCompile(`(?i)^Каппер\s*-\s*([^,]+?)\s*добавил\b`)

// Линия с командами — ищем строку с " - " и запятой на конце (как в примере)
var teamsLineRe = regexp.MustCompile(`^\s*.+\s-\s.+,\s*$`)

// Линия даты: "Начало матча 02 ноября 23:30" (оставим как есть)
var startLineRe = regexp.MustCompile(`(?i)^Начало\s+матча\s+(.+)$`)

// Маркер строго нужного типа сообщения
const newForecastMarker = "Новый прогноз - -"

// ExtractCapperAndMatch парсит ТОЛЬКО сообщения строго заданного формата.
// Возвращает ошибку, если формат не совпадает или найден исход (ставка).
func ExtractCapperAndMatch(message string) (capper string, teams string, date string, err error) {
	// нормализуем переносы строк
	msg := strings.ReplaceAll(message, "\r\n", "\n")
	msg = strings.ReplaceAll(msg, "\r", "\n")

	// обязательный маркер "Новый прогноз - -"
	if !strings.Contains(msg, newForecastMarker) {
		return "", "", "", errors.New("пропуск: отсутствует строка 'Новый прогноз - -'")
	}

	// исход (тип ставки) НЕ должен быть указан
	if outcomeRe.FindStringIndex(msg) != nil {
		return "", "", "", errors.New("пропуск: в сообщении найден исход (Ф/П/ТБ/ТМ/1X/12/X2/ОЗ)")
	}

	sc := bufio.NewScanner(strings.NewReader(msg))
	var capperFound, teamsFound, dateFound bool

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		// 1) каппер
		if !capperFound {
			if m := capperLineRe.FindStringSubmatch(line); len(m) == 2 {
				capper = strings.TrimSpace(m[1])
				capperFound = true
				continue
			}
		}

		// 2) команды (строка вида "Team A - Team B,")
		if !teamsFound && teamsLineRe.MatchString(line) {
			// убираем хвостовую запятую
			teams = strings.TrimRight(line, ", ")
			teamsFound = true
			continue
		}

		// 3) дата/время из "Начало матча ..."
		if !dateFound {
			if m := startLineRe.FindStringSubmatch(line); len(m) == 2 {
				date = strings.TrimSpace(m[1]) // например: "02 ноября 23:30"
				dateFound = true
				continue
			}
		}
	}

	if err := sc.Err(); err != nil {
		return "", "", "", fmt.Errorf("ошибка сканирования: %w", err)
	}

	if !capperFound {
		return "", "", "", errors.New("не удалось извлечь имя каппера")
	}
	if !teamsFound {
		return "", "", "", errors.New("не удалось извлечь команды матча")
	}
	if !dateFound {
		return "", "", "", errors.New("не удалось извлечь дату/время начала матча")
	}

	return capper, teams, date, nil
}
