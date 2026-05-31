package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/itosa-kazu/TaskFerry/internal/relay"
)

func main() {
	addr := getenv("TASKFERRY_RELAY_ADDR", "AGENTCHAT_RELAY_ADDR", "127.0.0.1:8080")
	dbPath := getenv("TASKFERRY_RELAY_DB", "AGENTCHAT_RELAY_DB", filepath.Join(".taskferry", "relay.db"))
	token := getenv("TASKFERRY_RELAY_TOKEN", "AGENTCHAT_RELAY_TOKEN", "")
	opsToken := getenv("TASKFERRY_OPS_TOKEN", "AGENTCHAT_OPS_TOKEN", "")
	signupEnabled := getenvBool("TASKFERRY_SIGNUP_ENABLED", "AGENTCHAT_SIGNUP_ENABLED", true)
	signupLimit := getenvInt("TASKFERRY_SIGNUP_LIMIT_PER_HOUR", "AGENTCHAT_SIGNUP_LIMIT_PER_HOUR", 5)
	clientTokens, err := relay.ParseClientTokens(getenv("TASKFERRY_RELAY_CLIENT_TOKENS", "AGENTCHAT_RELAY_CLIENT_TOKENS", ""))
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatal(err)
	}
	store, err := relay.OpenStore(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	server := relay.NewServer(store, relay.AuthConfig{
		GlobalToken:        token,
		ClientTokens:       clientTokens,
		OpsToken:           opsToken,
		SignupDisabled:     !signupEnabled,
		SignupLimitPerHour: signupLimit,
	})
	log.Printf("taskferry relay listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, server.Routes()))
}

func getenv(primary string, legacy string, fallback string) string {
	if value := os.Getenv(primary); value != "" {
		return value
	}
	if value := os.Getenv(legacy); value != "" {
		return value
	}
	return fallback
}

func getenvBool(primary string, legacy string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(getenv(primary, legacy, "")))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		log.Printf("invalid boolean for %s, using %t", primary, fallback)
		return fallback
	}
}

func getenvInt(primary string, legacy string, fallback int) int {
	value := strings.TrimSpace(getenv(primary, legacy, ""))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		log.Printf("invalid integer for %s, using %d", primary, fallback)
		return fallback
	}
	return parsed
}
