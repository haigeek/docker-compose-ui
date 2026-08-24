package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"compose-ui/internal/api"
	"compose-ui/internal/app"
	"compose-ui/internal/dockerx"
	"compose-ui/internal/safe"
)

func main() {
	addr := getenv("COMPOSE_UI_ADDR", ":8227")
	timeout := getenvDuration("COMPOSE_UI_REDEPLOY_TIMEOUT", 120*time.Second)
	authUser := getenv("COMPOSE_UI_BASIC_AUTH_USER", "admin")
	authPass := getenv("COMPOSE_UI_BASIC_AUTH_PASS", "admin")
	enableProjectManagement := getenvBool("COMPOSE_UI_ENABLE_PROJECT_MANAGEMENT", false)

	dockerClient, err := dockerx.New()
	if err != nil {
		log.Fatalf("failed to init docker client: %v", err)
	}
	defer dockerClient.Close()

	appSvc := app.NewService(dockerClient, safe.NewFileStore(), timeout)
	log.Printf("compose-ui server listening on %s", addr)
	if err := api.Run(addr, appSvc, authUser, authPass, enableProjectManagement); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getenvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
