package config

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPPort string

	RedisURL      string
	RedisPassword string
	RedisDB       int

	DefaultRPS      int
	DefaultDuration time.Duration

	BAPPrivateKey  string
	BAPPublicKey   string
	BAPID          string
	BAPUniqueKeyID string

	CountryCode string
	Domain      string
	CityCode    string

	VerificationEnable bool

	SwaggerEnable bool

	RunsFSRoot   string
	RunsFSEnable bool

	BAPURI string

	CoreVersion string

	PipelineStageGapSeconds int

	MaxInFlight          int
	RunStoreBackend      string
	RunPayloadTTLSeconds int

	MongoURI           string
	MongoDB            string
	GlobalRPSLimit     int
	PerSessionRPSLimit int
	SessionTTLSeconds       int
	DiscoveryWaitTTLSeconds int
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		HTTPPort:          getEnv("HTTP_PORT", "8080"),
		RedisURL:          getEnv("REDIS_URL", "redis:6379"),
		RedisPassword:     os.Getenv("REDIS_PASSWORD"),
		RedisDB:           getEnvInt("REDIS_DB", 0),
		DefaultRPS:        getEnvInt("DEFAULT_RPS", 100),
		DefaultDuration:   getEnvDuration("DEFAULT_DURATION", "60s"),
		BAPPrivateKey:     os.Getenv("BAP_PRIVATE_KEY"),
		BAPPublicKey:      os.Getenv("BAP_PUBLIC_KEY"),
		BAPID:             os.Getenv("BAP_ID"),
		BAPUniqueKeyID:    os.Getenv("BAP_UNIQUE_KEY_ID"),
		CountryCode:       getEnv("COUNTRY_CODE", "IND"),
		Domain:            getEnv("DOMAIN", "nic2004:52110"),
		CityCode:          getEnv("CITY_CODE", "std:080"),
		VerificationEnable: getEnvBool("VERIFICATION_ENABLE", false),
		SwaggerEnable:      getEnvBool("SWAGGER_ENABLE", true),
		RunsFSRoot:              getEnv("RUNS_FS_ROOT", "./runs"),
		RunsFSEnable:            getEnvBool("RUNS_FS_ENABLE", false),
		BAPURI:                  getEnv("BAP_URI", ""),
		PipelineStageGapSeconds: getEnvInt("PIPELINE_STAGE_GAP_SECONDS", 5),
		CoreVersion:             getEnv("CORE_VERSION", "1.2.0"),
		MaxInFlight:             getEnvInt("MAX_IN_FLIGHT", 256),
		RunStoreBackend:         getEnv("RUN_STORE_BACKEND", "memory"),
		RunPayloadTTLSeconds:    getEnvInt("RUN_PAYLOAD_TTL_SECONDS", 600),
		MongoURI:                getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:                 getEnv("MONGO_DB", "load_tester"),
		GlobalRPSLimit:          getEnvInt("GLOBAL_RPS_LIMIT", 2000),
		PerSessionRPSLimit:      getEnvInt("PER_SESSION_RPS_LIMIT", 150),
		SessionTTLSeconds:       getEnvInt("SESSION_TTL_SECONDS", 3600),
		DiscoveryWaitTTLSeconds: getEnvInt("DISCOVERY_WAIT_TTL_SECONDS", 30),
	}

	if err := os.MkdirAll(cfg.RunsFSRoot, 0o755); err != nil {
		log.Printf("unable to create runs directory %s: %v", cfg.RunsFSRoot, err)
	}

	return cfg, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		i, err := strconv.Atoi(v)
		if err == nil {
			return i
		}
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		switch v {
		case "1", "true", "TRUE", "True":
			return true
		case "0", "false", "FALSE", "False":
			return false
		}
	}
	return def
}

func getEnvDuration(key, def string) time.Duration {
	raw := getEnv(key, def)
	d, err := time.ParseDuration(raw)
	if err != nil {
		return time.Second * 60
	}
	return d
}
