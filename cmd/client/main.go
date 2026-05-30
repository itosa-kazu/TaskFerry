package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/itosa-kazu/TaskFerry/internal/client"
)

func main() {
	cfg := client.Config{
		Addr:          getenv("TASKFERRY_CLIENT_ADDR", "AGENTCHAT_CLIENT_ADDR", "127.0.0.1:4318"),
		ClientID:      getenv("TASKFERRY_CLIENT_ID", "AGENTCHAT_CLIENT_ID", "client_dev"),
		DeviceID:      getenv("TASKFERRY_DEVICE_ID", "AGENTCHAT_DEVICE_ID", "device_dev"),
		OwnerID:       getenv("TASKFERRY_OWNER_ID", "AGENTCHAT_OWNER_ID", getenv("TASKFERRY_CLIENT_ID", "AGENTCHAT_CLIENT_ID", "owner_dev")),
		RelayHTTP:     getenv("TASKFERRY_RELAY_HTTP", "AGENTCHAT_RELAY_HTTP", "http://127.0.0.1:8080"),
		RelayWS:       getenv("TASKFERRY_RELAY_WS", "AGENTCHAT_RELAY_WS", "ws://127.0.0.1:8080/v1/ws"),
		RelayToken:    getenv("TASKFERRY_RELAY_TOKEN", "AGENTCHAT_RELAY_TOKEN", ""),
		LocalAPIToken: getenv("TASKFERRY_LOCAL_API_TOKEN", "AGENTCHAT_LOCAL_API_TOKEN", ""),
	}
	dbPath := getenv("TASKFERRY_CLIENT_DB", "AGENTCHAT_CLIENT_DB", filepath.Join(".taskferry", cfg.ClientID+".db"))
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatal(err)
	}
	store, err := client.OpenStore(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	server := client.NewServer(cfg, store)
	server.StartRelayLoop()
	log.Printf("taskferry local client listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, server.Routes()))
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
