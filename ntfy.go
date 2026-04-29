package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type ntfyMessage struct {
	Message  string   `json:"message"`
	Title    string   `json:"title,omitempty"`
	Priority int      `json:"priority"`
	Tags     []string `json:"tags,omitempty"`
}

func sendNTFYNotification(cfg config, success bool) error {
	if !cfg.NTFYEnabled || cfg.NTFYTopic == "" {
		return nil
	}
	title := "## ❌ OCI Instance Creation Failed"
	if success {
		title = "## ✅ OCI Instance Created Successfully!"
	}
	message := fmt.Sprintf("%s\n\n**Status**: %s\n**Region**: %s\n**Shape**: %s\n\n**Specifications:**\n- OCPUs: %.1f\n- Memory: %.1f GB",
		title,
		"Running", cfg.Region, cfg.Shape, cfg.OCPUS, cfg.MemoryInGbs)
	tags := []string{"cloud", "oci"}
	if success {
		tags = append(tags, "success")
	}

	url := fmt.Sprintf("%s/%s", cfg.NTFYServer, cfg.NTFYTopic)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("POST", url, strings.NewReader(message))
	if err != nil {
		return fmt.Errorf("failed to create ntfy request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Markdown", "yes")

	if cfg.NTFYToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.NTFYToken))
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send ntfy notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ntfy server returned status %d", resp.StatusCode)
	}

	log.Println("NTFY notification sent successfully")
	return nil
}

func replacePlaceholders(message string, cfg config) string {
	message = strings.ReplaceAll(message, "{region}", cfg.Region)
	message = strings.ReplaceAll(message, "{shape}", cfg.Shape)
	message = strings.ReplaceAll(message, "{ocpus}", fmt.Sprintf("%.1f", cfg.OCPUS))
	message = strings.ReplaceAll(message, "{memory}", fmt.Sprintf("%.1f", cfg.MemoryInGbs))
	return message
}
