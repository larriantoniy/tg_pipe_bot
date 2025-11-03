package prediction

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/larriantoniy/tg_pipe_bot/internal/domain"
)

type PredictionService struct {
	outcomeRe    *regexp.Regexp
	logger       *slog.Logger
	coefRe       *regexp.Regexp
	capperLineRe *regexp.Regexp
	teamsLineRe  *regexp.Regexp
}

func NewPredictionService(logger *slog.Logger) *PredictionService {
	return &PredictionService{
		logger:       logger,
		outcomeRe:    regexp.MustCompile(`(?i)(Т[БМ]\s*\([^)]*\)|Ф[12]\s*\([^)]*\)|П[12]|X|1X|12|X2)`), // вытаскиваем исход (ТБ/ТМ/Ф1/... )
		coefRe:       regexp.MustCompile(`(~\d+(\.\d+)?|\b\d+(\.\d+)?\b)`),                             // вытаскиваем коэффициент "~2", "2.05" и т.п.
		capperLineRe: regexp.MustCompile(`^Каппер\s*-\s*([^\s,]+)(?:\s+добавил)?[,;]?\s*$`),
		teamsLineRe:  regexp.MustCompile(`^\s*.+\s-\s.+,\s*$`), // Линия с командами — ищем строку с " - " и запятой на конце (как в примере)
	}
}

// FormatMessage форматирует прогноз в нужный вид
func (p *PredictionService) FormatMessage(sport string, country string, teams string, date string, forecast string) string {
	// Пример даты: "02 ноября 23:30"
	parts := strings.Split(date, " ")

	day := parts[0]
	month := monthNum(parts[1])
	timeStr := parts[len(parts)-1]
	dateFormatted := fmt.Sprintf("%s.%s — %s", day, month, timeStr)

	// исход
	outcome := p.outcomeRe.FindString(forecast)
	if outcome == "" {
		outcome = forecast
	}

	// коэффициент: сначала ищем `~число`, затем обычное
	coef := regexp.MustCompile(`~\s*\d+(\.\d+)?`).FindString(forecast)
	if coef == "" {
		coef = p.coefRe.FindString(forecast)
	}
	if coef == "" {
		coef = "?"
	}

	// просто используем входные sport и country как есть
	sportLine := fmt.Sprintf("%s %s", sport, country)

	return fmt.Sprintf(
		"%s\n\n🕓 %s\n%s\n\n🎯 %s\n📈 Кф: %s",
		sportLine,
		dateFormatted,
		teams,
		strings.TrimSpace(outcome),
		strings.TrimSpace(coef),
	)
}

// конвертация русских месяцев в число
func monthNum(m string) string {
	months := map[string]string{
		"января": "01", "февраля": "02", "марта": "03", "апреля": "04",
		"мая": "05", "июня": "06", "июля": "07", "августа": "08",
		"сентября": "09", "октября": "10", "ноября": "11", "декабря": "12",
	}
	return months[strings.ToLower(strings.TrimSpace(m))]
}

// GetForecast загружает страницу прогноза каппера и находит прогноз для заданного матча.
func (p *PredictionService) GetForecast(capper, teams, _ /*dateIgnored*/, baseURL string) (string, error) {
	// формируем URL
	url := fmt.Sprintf("%s%s/bets?_pjax=%%23profile", baseURL, capper)

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("не удалось загрузить страницу прогноза: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("получен неожиданный статус %d при загрузке %s", resp.StatusCode, url)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга HTML: %w", err)
	}

	teamA, teamB := splitTeams(teams)
	var forecast string

	// ищем первый блок, где присутствуют две команды
	doc.Find(".UserBet").EachWithBreak(func(i int, bet *goquery.Selection) bool {
		text := normSpaces(bet.Text())

		// если блок не содержит обе команды — пропускаем
		if !strings.Contains(text, teamA) || !strings.Contains(text, teamB) {
			return true // continue
		}

		// вырезаем команды
		text = strings.Replace(text, teamA, "", 1)
		text = strings.Replace(text, teamB, "", 1)

		forecast = strings.TrimSpace(text)
		return false // нашли — stop
	})

	if forecast == "" {
		return "", fmt.Errorf("прогноз для матча '%s' не найден на странице %s", teams, url)
	}

	return forecast, nil
}

// --- helpers ---

func normSpaces(s string) string {
	s = strings.ReplaceAll(s, "\u00A0", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func splitTeams(teams string) (string, string) {
	parts := strings.Split(teams, " - ")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	parts = strings.Split(teams, "-")
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return teams, ""
}

// валидатор отсутствия исхода (ставки типа Ф1/П1/ТБ и т.д.)
var outcomeRe = regexp.MustCompile(`(?i)\b(Ф[12]\s*\([^)]*\)|П[12]\b|(?:^|\W)X(?:$|\W)|\b1X\b|\b12\b|\bX2\b|Т[БМ]\s*\d+(\.\d+)?|\bОЗ\b|\bобе забьют\b)`)

// Линия даты: "Начало матча 02 ноября 23:30" (оставим как есть)
var startLineRe = regexp.MustCompile(`(?i)^Начало\s+матча\s+(.+)$`)

// Маркер строго нужного типа сообщения
const newForecastMarker = "Новый прогноз - -"

// ExtractCapperAndMatch парсит ТОЛЬКО сообщения строго заданного формата.
// / вида
//
// Каппер - NeNaZavode добавил,
// Новый прогноз - -
// Футбол
// Чемпионат Бразилии. Лига Кариока B2
// Рио-де-Жанейро - Серра Макаенсе,
// Начало матча 02 ноября 21:00
// КФ ~2, Ставка 400у.е.

// Возвращает ошибку, если формат не совпадает или найден исход (ставка).
func (p *PredictionService) ExtractCapperAndMatch(message string) (capper string, teams string, date string, err error) {
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
			if m := p.capperLineRe.FindStringSubmatch(line); len(m) == 2 {
				capper = strings.TrimSpace(m[1])
				capperFound = true
				continue
			}
		}

		// 2) команды (строка вида "Team A - Team B,")
		if !teamsFound && p.teamsLineRe.MatchString(line) {
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

func (p *PredictionService) GetFormatedPrediction(msg domain.Message, baseURL string) (string, string, error) {
	// 1) Достаём capper / teams / date из текста входящего сообщения
	if msg.Text == "" {
		return "", "", errors.New("пустое сообщение")
	}
	capper, teams, date, err := p.ExtractCapperAndMatch(msg.Text)
	if err != nil {
		p.logger.Error("extract capper/match failed", "err", err)
		return "", "", err
	}

	// 2) Парсим сайт каппера и находим конкретный прогноз под этот матч/дату
	forecast, err := p.GetForecast(capper, teams, date, strings.TrimRight(baseURL, "/")+"/")
	if err != nil {
		p.logger.Error("fetch forecast failed", "capper", capper, "teams", teams, "date", date, "err", err)
		return "", "", err
	}

	// 3) Нормализуем отображение команд (заменим дефис на тире, как в примере)
	teamsDisplay := strings.ReplaceAll(teams, " - ", " — ")

	// 4) Формируем финальный текст сообщения
	formatted := p.FormatMessage(teamsDisplay, date, forecast)

	p.logger.Info("prediction formatted",
		"capper", capper,
		"teams", teamsDisplay,
		"date", date,
	)

	return capper, formatted, nil
}
