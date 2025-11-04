package prediction

import (
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
	startLineRe  *regexp.Regexp
}

func NewPredictionService(logger *slog.Logger) *PredictionService {
	return &PredictionService{
		logger: logger,

		// ловим: ТБ/ТМ 2.5 (со/без скобок), Ф1/Ф2 -1.5 (со/без скобок), П1/П2, X, 1X, 12, X2, ОЗ
		outcomeRe: regexp.MustCompile(`(?i)\b(
			Т[БМ]\s*\(?\s*\d+(?:[.,]\d+)?\s*\)? |
			Ф[12]\s*\(?\s*[-+]?\d+(?:[.,]\d+)?\s*\)? |
			П[12] |
			1X | 12 | X2 | ОЗ | X
		)\b`),

		coefRe: regexp.MustCompile(`~?\s*\d+(?:[.,]\d+)?`),

		capperLineRe: regexp.MustCompile(`^Каппер\s*-\s*([^\s,]+)(?:\s+добавил)?[,;]?\s*$`),
		teamsLineRe:  regexp.MustCompile(`^\s*.+\s-\s.+,\s*$`),
		startLineRe:  regexp.MustCompile(`(?i)^Начало\s+матча\s+(.+)$`),
	}
}
func (p *PredictionService) FormatBetMessage(
	sport, league, date, teams, outcome, coef string,
) string {

	var b strings.Builder

	// Заголовок
	if sport != "" {
		fmt.Fprintln(&b, sport)
	}
	if league != "" {
		fmt.Fprintln(&b, league)
	}
	if sport != "" || league != "" {
		fmt.Fprintln(&b)
	}

	// Основной блок
	fmt.Fprintf(&b, "🕓 %s\n", strings.TrimSpace(date))
	fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(teams))

	// Исход
	if strings.TrimSpace(outcome) == "" {
		outcome = "—"
	}
	fmt.Fprintf(&b, "🎯 %s\n", strings.TrimSpace(outcome))

	// Коэффициент
	coef = strings.TrimSpace(coef)
	if coef == "" {
		coef = "?"
	}

	fmt.Fprintf(&b, "📈 Кф: %s", coef)

	return b.String()
}

func (p *PredictionService) GetOutcomeOnly(capper, teams, baseURL string) (string, error) {
	url := fmt.Sprintf("%s%s/bets?_pjax=%%23profile", strings.TrimRight(baseURL, "/")+"/", capper)

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("не удалось загрузить страницу: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("статус %d при загрузке %s", resp.StatusCode, url)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга HTML: %w", err)
	}

	// Подготовим искомые команды
	a, b := splitTeams(teams)
	if a == "" || b == "" {
		return "", fmt.Errorf("не удалось разделить команды: %q", teams)
	}
	na, nb := normalizeName(a), normalizeName(b)

	var (
		outcome string
		found   bool
	)

	doc.Find(".UserBet").EachWithBreak(func(i int, bet *goquery.Selection) bool {
		// Собираем названия команд из .sides span
		var left, right []string
		bet.Find(".sides span").Each(func(i int, s *goquery.Selection) {
			txt := strings.TrimSpace(s.Text())
			if txt != "" {
				if len(left) == 0 {
					left = append(left, txt)
				} else {
					right = append(right, txt)
				}
			}
		})
		team1 := strings.Join(left, " ")
		team2 := strings.Join(right, " ")

		// Нормализуем
		nsides1 := normalizeName(team1)
		nsides2 := normalizeName(team2)

		// Совпадение: обе искомые команды присутствуют (порядок неважен)
		match := (strings.Contains(nsides1, na) && strings.Contains(nsides2, nb)) ||
			(strings.Contains(nsides1, nb) && strings.Contains(nsides2, na))
		if !match {
			return true // continue
		}

		// 1) приоритет — mobile-ячейка исхода
		if m := strings.TrimSpace(bet.Find(".exspres .col-6.d-block.d-md-none.order-1").First().Text()); m != "" {
			outcome = strings.TrimSpace(p.outcomeRe.FindString(normSpaces(m)))
		}

		// 2) фолбэк — ищем в .exspres целиком
		if outcome == "" {
			block := normSpaces(bet.Find(".exspres").Text())
			outcome = strings.TrimSpace(p.outcomeRe.FindString(block))
		}

		// 3) крайний фолбэк — по всей карточке
		if outcome == "" {
			all := normSpaces(bet.Text())
			outcome = strings.TrimSpace(p.outcomeRe.FindString(all))
		}

		found = true
		return false // stop
	})

	if !found {
		return "", fmt.Errorf("ставка для матча %q не найдена", teams)
	}
	if outcome == "" {
		return "", fmt.Errorf("исход не найден для матча %q", teams)
	}
	return outcome, nil
}
func normSpaces(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, ",")
	s = strings.ReplaceAll(s, "—", "-")
	s = strings.ReplaceAll(s, "–", "-")
	s = strings.ReplaceAll(s, "−", "-")
	s = strings.ReplaceAll(s, "\u00A0", " ")
	s = strings.Join(strings.Fields(s), " ")
	return strings.ToLower(s)
}

// --- helpers ---

func normalizeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, ",")
	s = strings.ReplaceAll(s, "\u00A0", " ")
	s = strings.ReplaceAll(s, "—", "-")
	s = strings.ReplaceAll(s, "–", "-")
	s = strings.ReplaceAll(s, "−", "-")
	s = strings.ReplaceAll(s, " - ", "-")
	s = strings.Join(strings.Fields(s), " ")
	return strings.ToLower(s)
}

func splitTeams(teams string) (string, string) {
	t := strings.ReplaceAll(teams, "—", "-")
	t = strings.ReplaceAll(t, "–", "-")
	t = strings.ReplaceAll(t, "−", "-")
	parts := strings.Split(t, "-")
	if len(parts) >= 2 {
		a := strings.TrimSpace(parts[0])
		b := strings.TrimSpace(strings.Join(parts[1:], "-"))
		return a, b
	}
	return strings.TrimSpace(teams), ""
}

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
func (p *PredictionService) ExtractCapperAndMatch(message string) (
	capper string,
	sport string,
	league string,
	teams string,
	date string,
	coef string,
	err error,
) {
	// normalize
	msg := strings.ReplaceAll(message, "\r\n", "\n")
	msg = strings.ReplaceAll(msg, "\r", "\n")

	lines := []string{}
	for _, l := range strings.Split(msg, "\n") {
		trim := strings.TrimSpace(l)
		if trim != "" {
			lines = append(lines, trim)
		}
	}

	// We expect at least 7 meaningful lines
	if len(lines) < 7 {
		return "", "", "", "", "", "", errors.New("неполное сообщение")
	}

	// 1) Каппер
	if m := p.capperLineRe.FindStringSubmatch(lines[0]); len(m) == 2 {
		capper = m[1]
	} else {
		return "", "", "", "", "", "", errors.New("неверная строка каппера")
	}

	// 2) Проверка маркера
	if lines[1] != "Новый прогноз - -" {
		return "", "", "", "", "", "", errors.New("ожидался маркер 'Новый прогноз - -'")
	}

	// 3) спорт
	sport = lines[2]

	// 4) лига
	league = lines[3]

	// 5) команды
	if !p.teamsLineRe.MatchString(lines[4]) {
		return "", "", "", "", "", "", fmt.Errorf("некорректная строка команд: %s", lines[4])
	}
	teams = strings.TrimRight(lines[4], ", ")

	// 6) дата
	if m := p.startLineRe.FindStringSubmatch(lines[5]); len(m) == 2 {
		date = strings.TrimSpace(m[1]) // "05 ноября 15:15"
	} else {
		return "", "", "", "", "", "", errors.New("неверная строка даты")
	}

	// 7) коэффициент на последней строке
	coef = p.coefRe.FindString(lines[6])
	if coef == "" {
		return "", "", "", "", "", "", errors.New("не найден коэффициент")
	}

	return capper, sport, league, teams, date, coef, nil
}

func (p *PredictionService) GetFormatedPrediction(msg domain.Message, baseURL string) (string, string, error) {
	// 1) Достаём capper / teams / sport / league/ date из текста входящего сообщения
	if msg.Text == "" {
		return "", "", errors.New("пустое сообщение")
	}
	capper, sport, league, teams, date, coef, err := p.ExtractCapperAndMatch(msg.Text)
	if err != nil {
		p.logger.Error("extract capper/match failed", "err", err)
		return "", "", err
	}
	p.logger.Warn("GetFormatedPrediction AFTER ExtractCapperAndMatch", "capper", capper, " sport", sport, "league", league, "teams", teams, "date", date, "coef", coef)

	// 2) Парсим сайт каппера и находим исход и кф
	outcome, err := p.GetOutcomeOnly(capper, teams, strings.TrimRight(baseURL, "/")+"/")
	if err != nil {
		p.logger.Error("fetch forecast failed", "capper", capper, "teams", teams, "date", date, "err", err)
		return "", "", err
	}
	p.logger.Warn("GetFormatedPrediction AFTER GETOUTCOME ONLY", "outcome", outcome)

	// 4) Формируем финальный текст сообщения
	formatted := p.FormatBetMessage(teams, date, sport, league, outcome, coef)

	return capper, formatted, nil
}
