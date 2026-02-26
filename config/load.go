package config

import (
	"embed"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"unicode"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/fs"
	"github.com/knadh/koanf/v2"
)

// PostLoad is for postloafing ahaha.
type PostLoad interface {
	PostLoad() error
}

type config struct {
	target       interface{}
	configLoader *koanf.Koanf
	fname        string
	fs           *embed.FS
	logger       *slog.Logger
}

func (c *config) loadFromFs() error {
	if err := c.configLoader.Load(
		fs.Provider(*c.fs, c.fname),
		toml.Parser(),
	); err != nil {
		return fmt.Errorf("error loading config file from fs: %w", err)
	}

	return nil
}

func (c *config) loadFromLocal() error {
	if err := c.configLoader.Load(
		file.Provider(c.fname),
		toml.Parser(),
	); err != nil {
		return fmt.Errorf("error loading config file from file: %w", err)
	}

	return nil
}

func (c *config) loadFile() error {
	if c.fname == "" {
		env := os.Getenv("APP_ENV")

		switch env {
		case "local":
			c.fname = "config.local.toml"
		case "dev", "development":
			c.fname = "config.dev.toml"
		case "test", "testing":
			c.fname = "config.test.toml"
		case "staging", "stage":
			c.fname = "config.staging.toml"
		case "prod", "production":
			c.fname = "config.prod.toml"
		default:
			c.fname = "config.dev.toml"
		}
	}

	if c.fname == "" {
		return nil
	}

	if c.fs != nil {
		return c.loadFromFs()
	}

	return c.loadFromLocal()
}

func camelToSnake(str string) string {
	runes := []rune(str)
	var b strings.Builder

	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) && unicode.IsLower(runes[i-1]) {
			b.WriteRune('_')
		}
		b.WriteRune(unicode.ToUpper(r))
	}

	return b.String()
}

func toEnvName(path string) string {
	segments := strings.Split(path, ".")
	for i, seg := range segments {
		segments[i] = camelToSnake(seg)
	}
	return strings.Join(segments, "_")
}

func (c *config) walkStructForEnv(t reflect.Type, prefix string, allowList map[string]string) {
	if t.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("koanf")

		if tag == "" || tag == "-" {
			continue
		}

		path := tag
		if prefix != "" {
			path = prefix + "." + path
		}

		ft := field.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}

		if ft.Kind() != reflect.Struct {
			c.walkStructForEnv(ft, path, allowList)
		} else {
			allowList[toEnvName(path)] = path
		}

	}

}

func (c *config) buildEnvAllowList(target any) map[string]string {
	allowList := make(map[string]string)
	t := reflect.TypeOf(target)

	for t.Kind() == reflect.Ptr {
		t.Elem()
	}

	c.walkStructForEnv(t, "", allowList)
	return allowList
}

// LoadFromEnv load value from env.
func (c *config) LoadFromEnv() error {
	allowList := c.buildEnvAllowList(c.target)
	c.logger.Info("loading config from environment", "allowlist", allowList)
	return nil
}

func (c *config) load() error {
	c.configLoader = koanf.New(".")

	if err := c.loadFile(); err != nil {
		return fmt.Errorf("error loading file: %w", err)
	}

	if err := c.configLoader.Load(env.Provider("", ".", func(s string) string {
		return strings.ReplaceAll(strings.ToLower(s), "_", ".")
	}), nil); err != nil {
		return fmt.Errorf("error loading env variables: %w", err)
	}

	if err := c.configLoader.Unmarshal("", c.target); err != nil {
		return fmt.Errorf("error while unmarshalling env vars: %w", err)
	}

	if postLoad, ok := c.target.(PostLoad); ok {
		if err := postLoad.PostLoad(); err != nil {
			return fmt.Errorf("error while running post load: %w", err)
		}
	}

	return nil
}

// Load will load the configuration
func Load(target interface{}, options ...Option) error {
	conf := config{
		target: target,
	}

	for _, opt := range options {
		if err := opt(&conf); err != nil {
			return err
		}
	}

	if conf.logger == nil {
		conf.logger = slog.Default()
	}

	return conf.load()
}
