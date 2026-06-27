package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type ntfyPriority int

const (
	ntfyPriorityMin     ntfyPriority = 1
	ntfyPriorityDefault ntfyPriority = 3
	ntfyPriorityHigh    ntfyPriority = 4
	ntfyPriorityMax     ntfyPriority = 5
)

type ntfyTag string

const (
	tagStart   ntfyTag = "rocket"
	tagInfo    ntfyTag = "information_source"
	tagSuccess ntfyTag = "white_check_mark"
	tagWarning ntfyTag = "warning"
	tagError   ntfyTag = "x"
	tagFatal   ntfyTag = "skull"
)

// sendNTFY sends a notification to the ntfy server with the given title, message, priority and tags.
func sendNTFY(cfg config, title, message string, priority ntfyPriority, tags ...ntfyTag) error {
	if !cfg.NTFYEnabled || cfg.NTFYTopic == "" {
		return nil
	}

	// Build markdown message
	fullMessage := fmt.Sprintf("## %s\n\n%s", title, message)

	// Convert tags to strings
	tagStrs := make([]string, len(tags))
	for i, t := range tags {
		tagStrs[i] = string(t)
	}

	url := fmt.Sprintf("%s/%s", cfg.NTFYServer, cfg.NTFYTopic)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("POST", url, strings.NewReader(fullMessage))
	if err != nil {
		return fmt.Errorf("failed to create ntfy request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Markdown", "yes")
	req.Header.Set("Title", "OCI Instance Monitor")
	req.Header.Set("Priority", fmt.Sprintf("%d", priority))

	if len(tagStrs) > 0 {
		req.Header.Set("Tags", strings.Join(tagStrs, ","))
	}

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

	return nil
}

// notifyStatus sends an informational notification about script status.
func notifyStatus(cfg config, message string) {
	title := "OCI 抢机状态"
	if err := sendNTFY(cfg, title, message, ntfyPriorityDefault, tagInfo); err != nil {
		log.Printf("Failed to send NTFY notification: %v", err)
	}
}

// notifySuccess sends a success notification when instance is created.
func notifySuccess(cfg config, domain string) {
	message := fmt.Sprintf("**实例创建成功！**\n\n**区域**: %s\n**可用域**: %s\n**规格**: %s\n**OCPUs**: %.1f\n**内存**: %.1f GB",
		cfg.Region, domain, cfg.Shape, cfg.OCPUS, cfg.MemoryInGbs)
	if err := sendNTFY(cfg, "OCI 实例创建成功", message, ntfyPriorityMax, tagSuccess); err != nil {
		log.Printf("Failed to send NTFY notification: %v", err)
	}
}

// notifyFailure sends a failure notification when all domains have been tried.
func notifyFailure(cfg config) {
	message := fmt.Sprintf("**所有可用域尝试完毕，均无容量**\n\n**区域**: %s\n**规格**: %s\n**OCPUs**: %.1f\n**内存**: %.1f GB\n\n将持续重试...",
		cfg.Region, cfg.Shape, cfg.OCPUS, cfg.MemoryInGbs)
	if err := sendNTFY(cfg, "OCI 实例创建失败", message, ntfyPriorityHigh, tagWarning); err != nil {
		log.Printf("Failed to send NTFY notification: %v", err)
	}
}

// notifyError sends an error notification for non-capacity errors.
func notifyError(cfg config, domain string, errMsg string) {
	message := fmt.Sprintf("**可用域 %s 创建失败**\n\n**错误**: %s", domain, errMsg)
	if err := sendNTFY(cfg, "OCI 实例创建错误", message, ntfyPriorityHigh, tagError); err != nil {
		log.Printf("Failed to send NTFY notification: %v", err)
	}
}

// notifyFatal sends a fatal notification and is used for unrecoverable errors.
func notifyFatal(cfg config, message string) {
	fullMessage := fmt.Sprintf("**致命错误，程序退出**\n\n%s", message)
	if err := sendNTFY(cfg, "OCI 实例监控 - 致命错误", fullMessage, ntfyPriorityMax, tagFatal); err != nil {
		log.Printf("Failed to send NTFY notification: %v", err)
	}
}
