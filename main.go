package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
)

func main() {
	var t int
	flag.IntVar(&t, "t", 10, "Number of minutes between retries")
	flag.Parse()

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
		return false
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
				log.Fatal("Service limit exceeded. Please check your OCI tenancy quotas and existing instances. Exiting.")
			}
			return false
		}
		log.Println("Domain out of capacity: ", domain)
		notifyStatus(cfg, fmt.Sprintf("可用域 %s 容量不足，尝试下一个...", domain))
	}
	handleFailure(cfg)
	return false
}
func ListAvailabilityDomains(client identity.IdentityClient, compartmentId string) ([]string, error) {
	req := identity.ListAvailabilityDomainsRequest{CompartmentId: common.String(compartmentId)}

	resp, err := client.ListAvailabilityDomains(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("failed to list availability domains: %w", err)
	}

	var domainNames []string
	for _, item := range resp.Items {
		domainNames = append(domainNames, *item.Name)
	}
	return domainNames, nil
}

func ListInstances(client core.ComputeClient, compartmentId string) ([]core.Instance, error) {
	req := core.ListInstancesRequest{Page: common.String(""),
		Limit:         common.Int(78),
		SortBy:        core.ListInstancesSortByTimecreated,
		SortOrder:     core.ListInstancesSortOrderAsc,
		CompartmentId: common.String(compartmentId)}

	resp, err := client.ListInstances(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}

	return resp.Items, nil
}

func checkExistingInstances(cfg config, instances []core.Instance) string {
	shape := cfg.Shape
	maxInstances := cfg.MaxInstances
	var displayNames []string
	var states []core.InstanceLifecycleStateEnum
	for _, instance := range instances {
		if *instance.Shape == shape && instance.LifecycleState != core.InstanceLifecycleStateTerminated {
			displayNames = append(displayNames, *instance.DisplayName)
			states = append(states, instance.LifecycleState)
		}
	}

	if len(displayNames) < maxInstances {
		return ""
	}

	msg := fmt.Sprintf("Already have an instance(s) %v in state(s) (respectively) %v. User: %v\n", displayNames, states, cfg.UserID)
	return msg
}

func createInstance(client core.ComputeClient, cfg config, domain string) (core.LaunchInstanceResponse, error) {
	req := core.LaunchInstanceRequest{
		LaunchInstanceDetails: core.LaunchInstanceDetails{
			Metadata:           map[string]string{"ssh_authorized_keys": cfg.SSHPublicKey},
			Shape:              &cfg.Shape,
			CompartmentId:      &cfg.TenancyID,
			DisplayName:        common.String("instance-" + time.Now().Format("20060102-1504")),
			AvailabilityDomain: &domain,
			SourceDetails:      buildSourceDetails(cfg),
			CreateVnicDetails: &core.CreateVnicDetails{
				AssignPublicIp:         common.Bool(false),
				SubnetId:               &cfg.SubnetID,
				AssignPrivateDnsRecord: common.Bool(true),
			},
			AgentConfig: &core.LaunchInstanceAgentConfigDetails{
				PluginsConfig: []core.InstanceAgentPluginConfigDetails{
					{
						Name:         common.String("Compute Instance Monitoring"),
						DesiredState: core.InstanceAgentPluginConfigDetailsDesiredStateEnabled,
					},
				},
				IsMonitoringDisabled: common.Bool(false),
				IsManagementDisabled: common.Bool(false),
			},
			DefinedTags:  make(map[string]map[string]interface{}),
			FreeformTags: make(map[string]string),
			InstanceOptions: &core.InstanceOptions{
				AreLegacyImdsEndpointsDisabled: common.Bool(false),
			},
			AvailabilityConfig: &core.LaunchInstanceAvailabilityConfigDetails{
				RecoveryAction: core.LaunchInstanceAvailabilityConfigDetailsRecoveryActionRestoreInstance,
			},
			ShapeConfig: &core.LaunchInstanceShapeConfigDetails{
				Ocpus:       &cfg.OCPUS,
				MemoryInGBs: &cfg.MemoryInGbs,
			},
		},
	}
	return client.LaunchInstance(context.Background(), req)
}

func handleSuccess(cfg config, domain string) {
	log.Println("Instance created")
	notifySuccess(cfg, domain)
}

func handleFailure(cfg config) {
	log.Println("Failed to create instance")
	notifyFailure(cfg)
}
