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
	startLineRe  *regexp.Regexp
}

func NewPredictionService(logger *slog.Logger) *PredictionService {
	return &PredictionService{
		logger:       logger,
		outcomeRe:    regexp.MustCompile(`(?i)(Т[БМ]\s*\([^)]*\)|Ф[12]\s*\([^)]*\)|П[12]|X|1X|12|X2)`), // вытаскиваем исход (ТБ/ТМ/Ф1/... )
		coefRe:       regexp.MustCompile(`(~\d+(\.\d+)?|\b\d+(\.\d+)?\b)`),                             // вытаскиваем коэффициент "~2", "2.05" и т.п.
		capperLineRe: regexp.MustCompile(`^Каппер\s*-\s*([^\s,]+)(?:\s+добавил)?[,;]?\s*$`),
		teamsLineRe:  regexp.MustCompile(`^\s*.+\s-\s.+,\s*$`), // Линия с командами — ищем строку с " - " и запятой на конце (как в примере)
		startLineRe:  regexp.MustCompile(`(?i)^Начало\s+матча\s+(.+)$`),
	}
}

func (p *PredictionService) FormatBetMessage(teams, date, sport, league, outcome, coef string) string {
	// 1) дата
	parts := strings.Fields(date)
	day := ""
	month := ""
	timeStr := ""

	if len(parts) >= 3 {
		day = parts[0]
		month = monthNum(parts[1]) // твоя функция: "ноября" -> "11"
		timeStr = parts[len(parts)-1]
	} else {
		// фолбэк: вернём как есть
		timeStr = date
	}

	dateFormatted := timeStr
	if day != "" && month != "" {
		dateFormatted = fmt.Sprintf("%s.%s — %s", day, month, timeStr)
	}

	// 2) команды: нормализуем короткий дефис на длинное тире
	teamsClean := strings.ReplaceAll(teams, " - ", " — ")
	teamsClean = strings.ReplaceAll(teamsClean, "-", "—") // если без пробелов

	// 3) исход и коэффициент
	outcome = strings.TrimSpace(outcome)
	if outcome == "" {
		outcome = "—"
	}

	coef = strings.TrimSpace(coef)
	if coef == "" {
		coef = "?"
	} else if !strings.HasPrefix(coef, "~") {
		coef = "~" + coef
	}

	var b strings.Builder

	// 4) заголовок: спорт + лига (если есть)
	if sport != "" {
		fmt.Fprintln(&b, sport)
	}
	if league != "" {
		fmt.Fprintln(&b, league)
	}
	if sport != "" || league != "" {
		fmt.Fprintln(&b) // пустая строка
	}

	// 5) тело
	fmt.Fprintf(&b, "🕓 %s\n%s\n\n", dateFormatted, teamsClean)
	fmt.Fprintf(&b, "🎯 %s\n", outcome)
	fmt.Fprintf(&b, "📈 Кф: %s", coef)

	return b.String()
}

// --- ВСПОМОГАТЕЛЬНОЕ ---

// Пытаемся вытащить спорт/страну из "шапки" текста прогноза.
// Работает на строках вида: "Теннис ITF. Хамамацу. Женщины 04 нояб. 05:00 ... П1 ~2"
func extractSportCountry(text string) (sport, country string) {
	s := normSpaces(text)

	// 1) обрезаем по времени/маркеру "Платный прогноз", чтобы осталась шапка
	if i := strings.Index(s, "Платный прогноз"); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	if m := regexp.MustCompile(`^(.+?)\s+\d{1,2}\s?[а-яё]{3,5}\.? \d{2}:\d{2}\b`).FindStringSubmatch(s); len(m) == 2 {
		s = strings.TrimSpace(m[1]) // только шапка: "Теннис ITF. Хамамацу. Женщины"
	}

	// 2) спорт — первое слово (часто "Теннис", "Футбол", "Баскетбол", "Хоккей" и т.д.)
	if mm := regexp.MustCompile(`^([A-Za-zА-Яа-яЁё]+)`).FindStringSubmatch(s); len(mm) == 2 {
		sport = mm[1]
	}

	// 3) страна — явное упоминание или по городу/лиге из мини-словаря
	// явные страны
	for _, c := range []string{
		"США", "Россия", "Испания", "Германия", "Италия", "Франция",
		"Япония", "Китай", "Англия", "Великобритания", "Украина",
		"Беларусь", "Казахстан", "Бразилия",
	} {
		if strings.Contains(s, c) {
			country = c
			break
		}
	}
	// если не нашли — попробуем по городу/лигe
	if country == "" {
		lc := strings.ToLower(s)
		switch {
		case strings.Contains(lc, "хамамацу"), strings.Contains(lc, "hamamatsu"):
			country = "Япония"
		case strings.Contains(lc, "nba"):
			country = "США"
		case strings.Contains(lc, "khl"), strings.Contains(lc, "кхл"):
			country = "Россия"
			// дополняй по мере встреч
		}
	}

	return strings.TrimSpace(sport), strings.TrimSpace(country)
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

// вернёт исход (например: "Ф2 (-1.00)") и коэффициент (например: "~5")
func (p *PredictionService) GetOutcomeAndCoef(capper, teams, baseURL string) (outcome, coef string, err error) {
	url := fmt.Sprintf("%s%s/bets?_pjax=%%23profile", baseURL, capper)

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", "", fmt.Errorf("не удалось загрузить страницу: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("статус %d при загрузке %s", resp.StatusCode, url)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("ошибка парсинга HTML: %w", err)
	}

	teamA, teamB := splitTeams(teams)

	// исход: Ф1/Ф2(...), П1/П2, ТБ/ТМ(...), 1X/12/X2/ОЗ
	outcomeRe := regexp.MustCompile(`(?i)\b(Ф[12]\s*\([^)]+\)|П[12]|Т[БМ]\s*\([^)]+\)|1X|12|X2|ОЗ)\b`)
	// коэффициент: ~Число (поддержим и дробные с . или ,)
	coefRe := regexp.MustCompile(`~\s*\d+(?:[.,]\d+)?`)

	found := false
	doc.Find(".UserBet").EachWithBreak(func(i int, bet *goquery.Selection) bool {
		// Уточняем матч по колонке с командами
		sides := normSpaces(bet.Find(".sides").Text())
		if !(strings.Contains(sides, teamA) && strings.Contains(sides, teamB)) {
			return true // continue
		}

		// Берём весь текст блока — на мобиле исход/кф могут быть в других подпоколонках
		text := normSpaces(bet.Text())

		outcome = strings.TrimSpace(outcomeRe.FindString(text))
		coef = strings.TrimSpace(coefRe.FindString(text))

		if outcome != "" || coef != "" {
			found = true
			return false // stop
		}
		return true
	})

	if !found {
		return "", "", fmt.Errorf("ставка для матча '%s' не найдена", teams)
	}
	if outcome == "" && coef == "" {
		return "", "", fmt.Errorf("не удалось извлечь исход и коэффициент")
	}
	return outcome, coef, nil
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
func (p *PredictionService) ExtractCapperAndMatch(message string) (capper string, sport string, league string, teams string, date string, err error) {
	// нормализуем переносы строк
	msg := strings.ReplaceAll(message, "\r\n", "\n")
	msg = strings.ReplaceAll(msg, "\r", "\n")

	// обязательный маркер "Новый прогноз - -"
	if !strings.Contains(msg, newForecastMarker) {
		return "", "", "", "", "", errors.New("пропуск: отсутствует строка 'Новый прогноз - -'")
	}

	// исход (тип ставки) НЕ должен быть указан
	if p.outcomeRe.FindStringIndex(msg) != nil {
		return "", "", "", "", "", errors.New("пропуск: в сообщении найден исход (Ф/П/ТБ/ТМ/1X/12/X2/ОЗ)")
	}

	sc := bufio.NewScanner(strings.NewReader(msg))
	var (
		capperFound  bool
		markerPassed bool
		sportFound   bool
		leagueFound  bool
		teamsFound   bool
		dateFound    bool
	)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		// ловим сам маркер как отдельную строку
		if !markerPassed && line == newForecastMarker {
			markerPassed = true
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

		// 2) сразу после маркера: спорт и лига
		if markerPassed && !sportFound {
			sport = line // например: "Футбол", "Теннис", "Баскетбол"
			sportFound = true
			continue
		}
		if markerPassed && sportFound && !leagueFound {
			league = line // берём строку целиком, например: "Чемпионат Венесуэлы. Примера дивизион"
			leagueFound = true
			continue
		}

		// 3) команды (строка вида "Team A - Team B,")
		if !teamsFound && p.teamsLineRe.MatchString(line) {
			teams = strings.TrimRight(line, ", ")
			teamsFound = true
			continue
		}

		// 4) дата/время из "Начало матча ..."
		if !dateFound {
			if m := p.startLineRe.FindStringSubmatch(line); len(m) == 2 {
				date = strings.TrimSpace(m[1]) // например: "05 ноября 02:30"
				dateFound = true
				continue
			}
		}
	}

	if err := sc.Err(); err != nil {
		return "", "", "", "", "", fmt.Errorf("ошибка сканирования: %w", err)
	}

	// валидация
	if !capperFound {
		return "", "", "", "", "", errors.New("не удалось извлечь имя каппера")
	}
	if !sportFound {
		return "", "", "", "", "", errors.New("не удалось извлечь вид спорта")
	}
	if !leagueFound {
		return "", "", "", "", "", errors.New("не удалось извлечь лигу/турнир")
	}
	if !teamsFound {
		return "", "", "", "", "", errors.New("не удалось извлечь команды матча")
	}
	if !dateFound {
		return "", "", "", "", "", errors.New("не удалось извлечь дату/время начала матча")
	}

	return capper, sport, league, teams, date, nil
}

func (p *PredictionService) GetFormatedPrediction(msg domain.Message, baseURL string) (string, string, error) {
	// 1) Достаём capper / teams / sport / league/ date из текста входящего сообщения
	if msg.Text == "" {
		return "", "", errors.New("пустое сообщение")
	}
	capper, sport, league, teams, date, err := p.ExtractCapperAndMatch(msg.Text)
	if err != nil {
		p.logger.Error("extract capper/match failed", "err", err)
		return "", "", err
	}

	// 2) Парсим сайт каппера и находим исход и кф
	outcome, coef, err := p.GetOutcomeAndCoef(capper, teams, strings.TrimRight(baseURL, "/")+"/")
	if err != nil {
		p.logger.Error("fetch forecast failed", "capper", capper, "teams", teams, "date", date, "err", err)
		return "", "", err
	}

	// 4) Формируем финальный текст сообщения
	formatted := p.FormatBetMessage(teams, date, sport, league, outcome, coef)

	p.logger.Info("prediction formatted",
		"capper", capper,
		"teams", teams,
		"date", date,
	)

	return capper, formatted, nil
}
