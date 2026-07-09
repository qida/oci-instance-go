package main

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
)

func run() bool {
	cfg, err := loadConfig()
	if err != nil {
		log.Printf("Error loading config: %v", err)
		return false
	}

	err = cfg.validate()
	if err != nil {
		log.Printf("Error validating config: %v", err)
		return false
	}

	cp, err := cfg.buildConfigProvider()
	if err != nil {
		log.Printf("Error building config provider: %v", err)
		return false
	}

	coreClient, err := core.NewComputeClientWithConfigurationProvider(cp)
	if err != nil {
		log.Printf("Error creating compute client: %v", err)
		return false
	}

	identityClient, err := identity.NewIdentityClientWithConfigurationProvider(cp)
	if err != nil {
		log.Printf("Error creating identity client: %v", err)
		return false
	}

	if len(cfg.AvailabilityDomains) == 0 {
		cfg.AvailabilityDomains, err = ListAvailabilityDomains(identityClient, cfg.TenancyID)
		if err != nil {
			log.Printf("Error listing availability domains: %v", err)
			return false
		}
	}

	instances, err := ListInstances(coreClient, cfg.TenancyID)
	if err != nil {
		log.Printf("Error listing instances: %v", err)
		return false
	}
	existingInstances := checkExistingInstances(cfg, instances)
	if existingInstances != "" {
		log.Println(existingInstances)
		notifyStatus(cfg, existingInstances)
		// 已存在实例，无需创建
		return true
	}

	for _, domain := range cfg.AvailabilityDomains {
		log.Println("Trying domain: ", domain)
		notifyStatus(cfg, fmt.Sprintf("正在尝试可用域: %s", domain))
		resp, err := createInstance(coreClient, cfg, domain)
		if err == nil {
			handleSuccess(cfg, domain)
			return true
		}

		time.Sleep(time.Duration(rand.Intn(30)) * time.Second)

		if !strings.Contains(err.Error(), "Out of host capacity") {
			if resp.HTTPResponse() != nil {
				body, _ := io.ReadAll(resp.HTTPResponse().Body)
				log.Printf("Something went wrong in domain %s: %s, Body: %s, Error: %v", domain, resp.HTTPResponse().Status, string(body), err)
				notifyError(cfg, domain, fmt.Sprintf("%s: %s", resp.HTTPResponse().Status, err.Error()))
			} else {
				log.Printf("Error creating instance in domain %s: %v", domain, err)
				notifyError(cfg, domain, err.Error())
			}
			// LimitExceeded 是配额问题，重试无意义，直接退出
			if strings.Contains(err.Error(), "LimitExceeded") {
				notifyFatal(cfg, "服务配额超限 (LimitExceeded)，请检查 OCI 账户配额和现有实例。程序退出。")
				// log.Fatal("Service limit exceeded. Please check your OCI tenancy quotas and existing instances. Exiting.")
			}
			// return false
			continue
		}
		log.Println("Domain out of capacity: ", domain)
		notifyStatus(cfg, fmt.Sprintf("可用域 %s 容量不足，尝试下一个...", domain))
	}
	handleFailure(cfg)
	return false
}

func handleSuccess(cfg config, domain string) {
	log.Println("Instance created")
	notifySuccess(cfg, domain)
}

func handleFailure(cfg config) {
	log.Println("Failed to create instance")
	notifyFailure(cfg)
}
