package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	ActiveAccount string            `json:"active_account"`
	Accounts      map[string]string `json:"accounts"`
}

var configMutex sync.Mutex

func getConfigPath() string {
	if dir := os.Getenv("MARVA_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "config.json")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	marvaDir := filepath.Join(dir, "marva")
	_ = os.MkdirAll(marvaDir, 0755)
	return filepath.Join(marvaDir, "config.json")
}

func loadConfig() (*Config, error) {
	configMutex.Lock()
	defer configMutex.Unlock()

	path := getConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{
				Accounts: make(map[string]string),
			}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Accounts == nil {
		cfg.Accounts = make(map[string]string)
	}
	return &cfg, nil
}

func saveConfig(cfg *Config) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	path := getConfigPath()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

type DynamicRoundTripper struct {
	next http.RoundTripper
}

func (rt *DynamicRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.ActiveAccount == "" {
		return nil, fmt.Errorf("no active account selected")
	}

	token, exists := cfg.Accounts[cfg.ActiveAccount]
	if !exists || token == "" {
		return nil, fmt.Errorf("active account %s has no valid token", cfg.ActiveAccount)
	}

	// Set the Authorization header dynamically
	req.Header.Set("Authorization", "Bearer "+token)

	return rt.next.RoundTrip(req)
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &DynamicRoundTripper{
			next: http.DefaultTransport,
		},
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  marva-cli auth login --user <username>")
	fmt.Println("  marva-cli auth switch --user <username>")
	fmt.Println("  marva-cli api user")
}

func parseUserFlag(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		if args[i] == "--user" && i+1 < len(args) {
			return args[i+1], nil
		}
		if len(args[i]) > 7 && args[i][:7] == "--user=" {
			return args[i][7:], nil
		}
	}
	return "", fmt.Errorf("missing --user flag")
}

func handleLogin(args []string) error { 
	username, err := parseUserFlag(args)
	if err != nil {
		return err
	}

	var token string
	fmt.Print("Enter token: ")
	var input string
	_, err = fmt.Scanln(&input)
	if err != nil || input == "" {
		token = "token-" + username
		fmt.Printf("\nNo token entered or non-interactive shell. Using mock token: %s\n", token)
	} else {
		token = input
	}

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	cfg.Accounts[username] = token
	cfg.ActiveAccount = username

	err = saveConfig(cfg)
	if err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Successfully logged in as %s\n", username)
	return nil
}

func handleSwitch(args []string) error {
	username, err := parseUserFlag(args)
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	token, exists := cfg.Accounts[username]
	if !exists || token == "" {
		return fmt.Errorf("account %s is not authenticated. Please login first", username)
	}

	cfg.ActiveAccount = username
	err = saveConfig(cfg)
	if err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Switched active account to %s\n", username)
	return nil
}

func handleAPIUser() error {
	client := newHTTPClient()

	apiURL := os.Getenv("MARVA_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}

	req, err := http.NewRequest("GET", apiURL+"/user", nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	log.Printf("Making request to %s using active account...", req.URL)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API Error (Status %d): %s", resp.StatusCode, string(body))
	}

	fmt.Println(string(body))
	return nil
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	cmd := os.Args[1]
	switch cmd {
	case "auth":
		if len(os.Args) < 3 {
			printUsage()
			os.Exit(1)
		}
		subCmd := os.Args[2]
		switch subCmd {
		case "login":
			err = handleLogin(os.Args[3:])
		case "switch":
			err = handleSwitch(os.Args[3:])
		default:
			printUsage()
			os.Exit(1)
		}
	case "api":
		if len(os.Args) < 3 {
			printUsage()
			os.Exit(1)
		}
		subCmd := os.Args[2]
		switch subCmd {
		case "user":
			err = handleAPIUser()
		default:
			printUsage()
			os.Exit(1)
		}
	default:
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}