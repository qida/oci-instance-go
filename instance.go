package main

import (
	"context"
	"fmt"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
)

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
			Metadata:      map[string]string{"ssh_authorized_keys": cfg.SSHPublicKey},
			Shape:         &cfg.Shape,
			CompartmentId: &cfg.TenancyID,
			// DisplayName:        common.String(cfg.InstanceName + "-" + time.Now().Format("20060102-1504")),
			DisplayName:        common.String(cfg.InstanceName),
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
