package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// AppConfig holds application configuration
type AppConfig struct {
	// General settings
	DefaultDestDir   string   `json:"defaultDestDir"`
	LastUsedPaths    []string `json:"lastUsedPaths"`
	MaxLastUsedPaths int      `json:"maxLastUsedPaths"`

	// Network settings
	KnownHosts  map[string]string `json:"knownHosts"`
	ScanTimeout int               `json:"scanTimeout"`
	CommonPorts []int             `json:"commonPorts"`

	// SSH settings
	DefaultKeyType string `json:"defaultKeyType"`
	DefaultKeyBits int    `json:"defaultKeyBits"`

	// UI settings
	DebugMode   bool   `json:"debugMode"`
	ColorScheme string `json:"colorScheme"`

	// Internal
	configPath string
	mutex      sync.RWMutex
}

var (
	config     *AppConfig
	configOnce sync.Once
)

// GetConfig returns the singleton AppConfig instance
func GetConfig() *AppConfig {
	configOnce.Do(func() {
		// Default values
		config = &AppConfig{
			DefaultDestDir:   "/home/user",
			MaxLastUsedPaths: 10,
			KnownHosts:       make(map[string]string),
			ScanTimeout:      60,
			DefaultKeyType:   "ed25519",
			DefaultKeyBits:   4096,
			ColorScheme:      "default",
			CommonPorts:      []int{22, 80, 443, 445, 3389, 8080, 8443},
		}

		err := config.Load()
		if err != nil {
			// Log error but continue with defaults
			if os.IsNotExist(err) {
				// This is normal on first run
				config.Save()
			} else {
				Error("Error loading config: %v", err)
			}
		}
	})

	return config
}

// Load config from file
func (c *AppConfig) Load() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Get config path
	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	c.configPath = configPath

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return err
	}

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	// Parse JSON
	err = json.Unmarshal(data, c)
	if err != nil {
		return fmt.Errorf("invalid config file format: %v", err)
	}

	return nil
}

// Save config to file
func (c *AppConfig) Save() error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// Get config path if not set
	if c.configPath == "" {
		configPath, err := getConfigPath()
		if err != nil {
			return err
		}
		c.configPath = configPath
	}

	// Create config directory if it doesn't exist
	configDir := filepath.Dir(c.configPath)
	err := os.MkdirAll(configDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	// Marshal config to JSON
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	// Write to file
	err = os.WriteFile(c.configPath, data, 0600)
	if err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	return nil
}

// AddRecentPath adds a path to the recent paths list
func (c *AppConfig) AddRecentPath(path string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check if path already exists
	for i, p := range c.LastUsedPaths {
		if p == path {
			// Move to front if not already
			if i > 0 {
				// Remove from current position
				c.LastUsedPaths = append(c.LastUsedPaths[:i], c.LastUsedPaths[i+1:]...)
				// Add to front
				c.LastUsedPaths = append([]string{path}, c.LastUsedPaths...)
			}
			return
		}
	}

	// Add new path to front
	c.LastUsedPaths = append([]string{path}, c.LastUsedPaths...)

	// Trim list if needed
	if len(c.LastUsedPaths) > c.MaxLastUsedPaths {
		c.LastUsedPaths = c.LastUsedPaths[:c.MaxLastUsedPaths]
	}

	// Save changes asynchronously
	go func() {
		if err := c.Save(); err != nil {
			// Log the error, but don't block the main thread
			Error("Failed to save config: %v", err)
		}
	}()
}

// AddKnownHost adds or updates a known host
func (c *AppConfig) AddKnownHost(ip, hostname string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.KnownHosts[ip] = hostname
	c.Save()
}

// GetKnownHostname returns the hostname for a known IP
func (c *AppConfig) GetKnownHostname(ip string) string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if hostname, ok := c.KnownHosts[ip]; ok {
		return hostname
	}
	return ""
}

// Get config file path
func getConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %v", err)
	}

	return filepath.Join(home, ".file-leaper", "config.json"), nil
}
