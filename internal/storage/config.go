package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Theme string `json:"theme,omitempty"`
	Model string `json:"model,omitempty"`
	Vim   bool   `json:"vim,omitempty"`
	// Appearance overrides the auto-detected terminal brightness used to pick
	// the light/dark variant of each theme. "" or "auto" follows the terminal,
	// "light" / "dark" force a specific variant.
	Appearance string `json:"appearance,omitempty"`
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "tuai")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func LoadConfig() (Config, error) {
	var cfg Config
	p, err := configPath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	return cfg, json.Unmarshal(data, &cfg)
}

func SaveConfig(cfg Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
