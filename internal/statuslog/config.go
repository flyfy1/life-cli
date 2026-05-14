package statuslog

import (
	"os"
	"path/filepath"
)

const defaultAPIURL = "https://api.integ.life"

type Config struct {
	APIURL   string
	APIToken string
	DBPath   string
}

func LoadConfig() Config {
	return Config{
		APIURL:   firstNonEmpty(os.Getenv("STATUSLOG_API_URL"), os.Getenv("LIFE_API_URL"), defaultAPIURL),
		APIToken: firstNonEmpty(os.Getenv("STATUSLOG_API_TOKEN"), os.Getenv("LIFE_API_TOKEN")),
		DBPath:   firstNonEmpty(os.Getenv("STATUSLOG_DB_PATH"), os.Getenv("LIFE_DB_PATH"), defaultDBPath()),
	}
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".statuslog/statuslog.db"
	}
	return filepath.Join(home, ".statuslog", "statuslog.db")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
