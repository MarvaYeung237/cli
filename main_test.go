package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

func TestConfigAndSwitch(t *testing.T) {
	// Set up a temporary config directory
	tmpDir, err := os.MkdirTemp("", "marva-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("MARVA_CONFIG_DIR", tmpDir)
	defer os.Unsetenv("MARVA_CONFIG_DIR")

	// 1. Mock two accounts
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	cfg.Accounts["account-a"] = "token-a"
	cfg.Accounts["account-b"] = "token-b"
	cfg.ActiveAccount = "account-a"

	err = saveConfig(cfg)
	if err != nil {
		t.Fatalf("saveConfig failed: %v", err)
	}

	// 2. Verify initial active account
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if cfg.ActiveAccount != "account-a" {
		t.Errorf("expected active account to be account-a, got %s", cfg.ActiveAccount)
	}

	// 3. Switch active account to account-b
	err = handleSwitch([]string{"--user", "account-b"})
	if err != nil {
		t.Fatalf("handleSwitch failed: %v", err)
	}

	// Verify active account is updated in config
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if cfg.ActiveAccount != "account-b" {
		t.Errorf("expected active account to be account-b, got %s", cfg.ActiveAccount)
	}

	// 4. Test DynamicRoundTripper resolves the new token
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		expectedHeader := "Bearer token-b"
		if authHeader != expectedHeader {
			t.Errorf("expected Authorization header %q, got %q", expectedHeader, authHeader)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"user": "account-b"}`))
	}))
	defer server.Close()

	client := newHTTPClient()
	req, err := http.NewRequestWithContext(context.Background(), "GET", server.URL+"/user", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status OK, got %d", resp.StatusCode)
	}
}

func TestConcurrentAccess(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "marva-test-concurrent")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.Setenv("MARVA_CONFIG_DIR", tmpDir)
	defer os.Unsetenv("MARVA_CONFIG_DIR")

	cfg, _ := loadConfig()
	cfg.Accounts["account-a"] = "token-a"
	cfg.Accounts["account-b"] = "token-b"
	cfg.ActiveAccount = "account-a"
	_ = saveConfig(cfg)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			user := "account-a"
			if id%2 == 0 {
				user = "account-b"
			}
			_ = handleSwitch([]string{"--user", user})
			
			// Read config and verify it doesn't crash
			_, err := loadConfig()
			if err != nil {
				t.Errorf("concurrent loadConfig failed: %v", err)
			}
		}(i)
	}
	wg.Wait()
}