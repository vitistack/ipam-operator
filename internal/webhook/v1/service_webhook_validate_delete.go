/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	utils "github.com/vitistack/ipam-operator/internal/utils"
)

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Service.
func (v *serviceValidatorAdapter) ValidateDelete(ctx context.Context, obj *corev1.Service) (admission.Warnings, error) {
	service := obj

	servicelog.Info("Validate Delete: Started for Service", "name", service.GetName())

	// Get the admission request from the context!
	req, _ := admission.RequestFromContext(ctx)

	// Detect dry run mode
	if *req.DryRun {
		servicelog.Info("Validate Delete: Dry run mode detected, skipping validate delete for Service:", "name", service.GetName())
		return nil, nil
	}

	// Do not Validate if the service type is not LoadBalancer.
	if service.Spec.Type != LoadBalancer {
		servicelog.Info("Validate Delete: wrong .spec.type, ABORT further actions", "name", service.GetName(), "type", service.Spec.Type)
		return nil, nil
	}

	// Check if Metallb Controller is actually running
	if err := validateMetallbOperator(ctx, v.validator.Client); err != nil {
		servicelog.Info("Validate Delete: Metallb operator is not available. Please make sure Metallb is installed and ready.")
		return nil, err
	}

	// Get kube-system namespace uid for cluster identification
	clusterId, err := getClusterID(v.validator.Client)
	if err != nil {
		servicelog.Info("Validate Delete: Failed to get cluster ID")
		return nil, err
	}

	// Get namespace uid for Service namespace identification
	namespaceId, err := getNameSpaceID(v.validator.Client, service)
	if err != nil {
		servicelog.Info("Validate Delete: Failed to get namespace ID")
		return nil, err
	}

	// Get Service annotations, need "ipam.vitistack.io/zone" to remove IP addresses from correct IPAddressPool
	annotations := service.GetAnnotations()

	// Get Secret
	secret, err := getSecret(annotations, service, v.validator.Client)
	if err != nil {
		servicelog.Info("Validate Delete: Failed to get secret", "error", err)
		return nil, err
	}

	// Add all Service addresses to a slice
	addresses := strings.Split(annotations["ipam.vitistack.io/addresses"], ",")

	// Remove old addresses that are not needed anymore
	// for index, addr := range addresses {
	// 	servicelog.Info("Validate Delete: Remove ip-address from IPAM-API", "service", service.GetName(), "ip", addr)
	// 	if err := removeAddressIpamAPI(addr, annotations, service, secret, clusterId, namespaceId); err != nil {
	// 		servicelog.Info("Validate Delete: Failed to remove ip-address from IPAM-API", "service", service.GetName(), "ip", addr, "Error", err)
	// 		if index == 1 {
	// 			if err := updateAddressIpamAPI(addresses[0], annotations, service, secret, clusterId, namespaceId); err != nil {
	// 				servicelog.Info("Validate Delete: Failed to rollback ip-address in IPAM-API", "service", service.GetName(), "ip", addresses[0], "Error", err)
	// 				return nil, err
	// 			}
	// 		}
	// 		return nil, err
	// 	}
	// }
	for _, addr := range addresses {
		servicelog.Info("Validate Delete: Remove ip-address from IPAM-API", "service", service.GetName(), "ip", addr)
		err := removeAddressIpamAPI(addr, annotations, service, secret, clusterId, namespaceId)
		if err != nil && err.Error() == "no matching address found with the provided secret, zone and address" {
			servicelog.Info("Validate Delete: No matching address found in IPAM-API, skipping removal", "service", service.GetName(), "ip", addr)
		} else if err != nil {
			servicelog.Info("Validate Delete: Failed to remove ip-address from IPAM-API", "service", service.GetName(), "ip", addr, "Error", err)
		}
	}

	// Remove Metallb Addresses from IPAddressPool
	if utils.VerifyIpAddressesInPool(v.validator.Client, annotations) {
		if err := utils.RemoveIPAddressesFromPool(v.validator.Client, annotations, addresses); err != nil {
			for _, addr := range addresses {
				if err := updateAddressIpamAPI(addr, annotations, service, secret, clusterId, namespaceId); err != nil {
					servicelog.Info("Validate Delete: Failed to rollback ip-address in IPAM-API", "service", service.GetName(), "ip", addresses[0], "Error", err)
					return nil, err
				}
			}
			return nil, fmt.Errorf("failed to remove IP addresses from MetallB IPAddressPool: %w", err)
		}
		servicelog.Info("Validate Delete: Successfully removed IP addresses from MetallB IPAddressPool", "service", service.GetName(), "ip", addresses)
	} else {
		servicelog.Info("Validate Delete: IP addresses were not found in MetallB IPAddressPool", "service", service.GetName(), "ip", addresses)
	}

	servicelog.Info("Validate Delete: Completed:", "name", service.GetName())

	return nil, nil
}
