package config

import (
	"io/fs"
	"testing"
)

func TestLoadConfigFromFS(t *testing.T) {
	type args struct {
		file fs.FS
	}
}

func TestLoadConfigFromEnv(t *testing.T) {}
