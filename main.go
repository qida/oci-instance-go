package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"time"
)

func main() {
	var t int
	flag.IntVar(&t, "t", 10, "Number of minutes between retries")
	flag.Parse()

	logFile, err := os.OpenFile("oci-instance-go.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Failed to open log file: %v", err)
	} else {
		log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Printf("Error loading config: %v", err)
		return
	}

	notifyStatus(cfg, fmt.Sprintf("**脚本启动**\n\n**重试间隔**: %d 分钟\n**区域**: %s\n**规格**: %s\n**OCPUs**: %.1f\n**内存**: %.1f GB",
		t, cfg.Region, cfg.Shape, cfg.OCPUS, cfg.MemoryInGbs))

	if run() {
		log.Println("Instance created successfully, exiting periodic task...")
		return
	}
	if t == 0 {
		return
	}
	log.Printf("Starting script with %v minutes delay.", t)
	for range time.Tick(time.Duration(t) * time.Minute) {
		if run() {
			log.Println("Instance created successfully, exiting periodic task...")
			break
		}
		//加入随机延时，避免对系统资源造成过大压力
		time.Sleep(time.Duration(rand.Intn(30)) * time.Second)
	}
}
