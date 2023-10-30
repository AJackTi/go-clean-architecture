package config

import (
	"fmt"

	"github.com/ilyakaznacheev/cleanenv"
)

type (
	// Config -.
	Config struct {
		App   `yaml:"app"`
		HTTP  `yaml:"http"`
		Log   `yaml:"logger"`
		PG    `yaml:"postgres"`
		S3AWS `yaml:"s3aws"`
	}

	// App -.
	App struct {
		Name    string `env-required:"true" yaml:"name"    env:"APP_NAME"`
		Env     string `env-required:"true" yaml:"env"     env:"APP_ENV"`
		Version string `env-required:"true" yaml:"version" env:"APP_VERSION"`
	}

	// HTTP -.
	HTTP struct {
		Port string `env-required:"true" yaml:"port" env:"HTTP_PORT"`
		Cors *bool  `env-required:"true" yaml:"cors" env:"HTTP_CORS"`
	}

	// Log -.
	Log struct {
		Level string `env-required:"true" yaml:"log_level"   env:"LOG_LEVEL"`
	}

	// PG -.
	PG struct {
		DbName   string `env-required:"true"   yaml:"db_name"    env:"PG_DB_NAME"`
		Host     string `env-required:"true"   yaml:"host"       env:"PG_HOST"`
		Port     string `env-required:"true"   yaml:"port"       env:"PG_PORT"`
		User     string `env-required:"true"   yaml:"user"       env:"PG_USER"`
		Password string `env-required:"true"   yaml:"password"   env:"PG_PASSWORD"`
		SSLMode  string `env-required:"true"   yaml:"sslmode"    env:"PG_SSLMODE"`
	}

	// S3 AWS -.
	S3AWS struct {
		KeyID      string `env-required:"true"   yaml:"key_id"             env:"KEY_ID"`
		AccessKey  string `env-required:"true"   yaml:"access_key"         env:"ACCESS_KEY"`
		Region     string `env-required:"true"   yaml:"region"             env:"REGION"`
		ACL        string `env-required:"true"   yaml:"acl"                env:"ACL"`
		Bucket     string `env-required:"true"   yaml:"bucket"             env:"BUCKET"`
		PathAvatar string `env-required:"true"   yaml:"path_avatar"        env:"PATH_AVATAR"`
	}
)

const ENV_PROD = "production"

// NewConfig returns app config.
func NewConfig() (*Config, error) {
	cfg := &Config{}

	err := cleanenv.ReadConfig("./config/config.yml", cfg)
	if err != nil {
		return nil, fmt.Errorf("read base config error: %w", err)
	}
	if cfg.App.Env == ENV_PROD {
		// overwrite some values from /config/config.production.yml
		err := cleanenv.ReadConfig("./config/config.production.yml", cfg)
		if err != nil {
			return nil, fmt.Errorf("read production config error: %w", err)
		}
	}

	// lastly, overwrite value from environment variable
	err = cleanenv.ReadEnv(cfg)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}
