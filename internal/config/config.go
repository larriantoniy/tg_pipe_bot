package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config хранит настройки приложения
type Config struct {
	APIID          int32
	APIHash        string
	BasePredictCh  string
	BasePredictUrl string
	Env            string `yaml:"env" env-required:"true"`
}

// Load читает настройки из переменных окружения
func Load() (*Config, error) {
	path := fetchConfigPath()
	cfg := MustLoadPath(path)
	apiIDStr := os.Getenv("TELEGRAM_API_ID")
	apiHash := os.Getenv("TELEGRAM_API_HASH")
	basePredictCh := os.Getenv("BASE_PREDICT_CH")
	basePredictUrl := os.Getenv("BASE_PREDICTION_URL")

	if apiIDStr == "" || apiHash == "" || basePredictUrl == "" || basePredictCh == "" {
		return nil, fmt.Errorf("TELEGRAM_API_ID, TELEGRAM_API_HASH , BASE_PREDICT_CH , BASE_PREDICTION_URL должны быть заданы")
	}

	apiID, err := strconv.Atoi(apiIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid TELEGRAM_API_ID: %w", err)
	}
	apiID32 := int32(apiID)

	return &Config{
		APIID:          apiID32,
		APIHash:        apiHash,
		Env:            cfg.Env, // <— добавили
		BasePredictUrl: basePredictUrl,
		BasePredictCh:  basePredictCh,
	}, nil
}

func MustLoadPath(configPath string) *Config {
	// check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		panic("config file does not exist: " + configPath)
	}

	var cfg Config
	///

	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		panic("cannot read config: " + err.Error())
	}

	return &cfg
}

// fetchConfigPath fetches config path from command line flag or environment variable.
// Priority: flag > env > default.
// Default value is empty string.
func fetchConfigPath() string {
	var res string

	flag.StringVar(&res, "config", "", "path to config file")
	flag.Parse()

	if res == "" {
		res = os.Getenv("CONFIG_PATH")
	}

	return res
}
