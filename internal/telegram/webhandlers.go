package telegram

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"sync"
	"time"
)

//go:embed templates/*
var templatesFS embed.FS

type ConfigStore struct {
	mu      sync.RWMutex
	configs map[string]*storedConfig
}

type storedConfig struct {
	config    string
	qrCode    string
	expiresAt time.Time
}

func NewConfigStore() *ConfigStore {
	store := &ConfigStore{
		configs: make(map[string]*storedConfig),
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				// No logger available here, print to stderr
				fmt.Fprintf(os.Stderr, "Panic in config store cleanup goroutine: %v\n", r)
			}
		}()
		store.cleanupExpired()
	}()

	return store
}

func (cs *ConfigStore) Store(config string, qrCode string) string {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	hash := sha256.Sum256([]byte(config))
	configID := base64.URLEncoding.EncodeToString(hash[:16])

	cs.configs[configID] = &storedConfig{
		config:    config,
		qrCode:    qrCode,
		expiresAt: time.Now().Add(24 * time.Hour),
	}

	return configID
}

func (cs *ConfigStore) Get(configID string) (string, string, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	stored, exists := cs.configs[configID]
	if !exists {
		return "", "", false
	}

	if time.Now().After(stored.expiresAt) {
		return "", "", false
	}

	return stored.config, stored.qrCode, true
}

func (cs *ConfigStore) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		cs.mu.Lock()
		now := time.Now()
		for id, stored := range cs.configs {
			if now.After(stored.expiresAt) {
				delete(cs.configs, id)
			}
		}
		cs.mu.Unlock()
	}
}

func WGConnectHandler(store *ConfigStore) http.HandlerFunc {
	tmpl := template.Must(template.ParseFS(templatesFS, "templates/wg_connect.html"))

	return func(w http.ResponseWriter, r *http.Request) {
		configID := r.URL.Query().Get("id")
		if configID == "" {
			http.Error(w, "Missing config ID", http.StatusBadRequest)
			return
		}

		config, qrCode, exists := store.Get(configID)
		if !exists {
			http.Error(w, "Config not found or expired", http.StatusNotFound)
			return
		}

		encodedConfig := base64.StdEncoding.EncodeToString([]byte(config))

		data := map[string]interface{}{
			"Config":        config,
			"EncodedConfig": encodedConfig,
			"QRCode":        qrCode,
			"ConfigID":      configID,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, data); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}
}

func WGConfigDownloadHandler(store *ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		configID := r.URL.Path[len("/wg/config/"):]
		if configID == "" {
			http.Error(w, "Missing config ID", http.StatusBadRequest)
			return
		}

		config, _, exists := store.Get(configID)
		if !exists {
			http.Error(w, "Config not found or expired", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=wireguard.conf")
		_, _ = w.Write([]byte(config))
	}
}
