package config

import (
	"embed"
	"fmt"
	"log/slog"
	"strings"

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
  target  interface{}
  configLoader *koanf.Koanf
  fname  string
  fs *embed.FS
  logger *slog.Logger
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
		return nil
	}

	if c.fs != nil {
		return c.loadFromFs()
	}

	return c.loadFromLocal()
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
    conf.logger = slog.Default();
  }

  return conf.load()
}
