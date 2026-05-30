package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/itosa-kazu/TaskFerry/internal/relay"
)

func main() {
	addr := getenv("TASKFERRY_RELAY_ADDR", "AGENTCHAT_RELAY_ADDR", "127.0.0.1:8080")
	dbPath := getenv("TASKFERRY_RELAY_DB", "AGENTCHAT_RELAY_DB", filepath.Join(".taskferry", "relay.db"))
	token := getenv("TASKFERRY_RELAY_TOKEN", "AGENTCHAT_RELAY_TOKEN", "")
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
	server := relay.NewServer(store, relay.AuthConfig{GlobalToken: token, ClientTokens: clientTokens})
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
