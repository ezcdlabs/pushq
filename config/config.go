package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const filename = ".pushq.json"

type Config struct {
	TestCommand string `json:"test_command"`
	MainBranch  string `json:"main_branch"`
}

func Load(repoRoot string) (*Config, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, filename))
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.MainBranch == "" {
		cfg.MainBranch = "main"
	}
	return &cfg, nil
}
