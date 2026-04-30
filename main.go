package main

import (
	"context"
	"flag"
	"fmt"
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
	flag.IntVar(&t, "t", 0, "Number of minutes between retries")
	flag.Parse()

	if t == 0 {
		run()
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
		return false
	}

	for _, domain := range cfg.AvailabilityDomains {
		log.Println("Trying domain: ", domain)
		resp, err := createInstance(coreClient, cfg, domain)
		if err == nil {
			handleSuccess(cfg)
			return true
		}
		//加入随机延时，避免对系统资源造成过大压力
		time.Sleep(time.Duration(rand.Intn(30)) * time.Second)
		if !strings.Contains(err.Error(), "Out of host capacity") {
			log.Println("Something went wrong: ", resp.HTTPResponse().Status)
			return false
		}
		log.Println("Domain out of capacity: ", domain)
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
						DesiredState: "ENABLED",
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

func handleSuccess(cfg config) {
	log.Println("Instance created")

	if err := sendNTFYNotification(cfg, true); err != nil {
		log.Printf("Failed to send NTFY notification: %v", err)
	}
}
func handleFailure(cfg config) {
	log.Println("Failed to create instance")
	if err := sendNTFYNotification(cfg, false); err != nil {
		log.Printf("Failed to send NTFY notification: %v", err)
	}
}
