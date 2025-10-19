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
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"reflect"

	utils "github.com/vitistack/ipam-operator/internal/utils"
)

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Service.
func (v *ServiceCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	newService, ok := newObj.(*corev1.Service)
	if !ok {
		return nil, fmt.Errorf("expected a Service object for the newObj but got %T", newObj)
	}

	oldService, ok := oldObj.(*corev1.Service)
	if !ok {
		return nil, fmt.Errorf("expected a Service object for the oldObj but got %T", oldObj)
	}

	// Get the admission request from the context!
	req, _ := admission.RequestFromContext(ctx)

	// Detect dry run mode
	if *req.DryRun {
		servicelog.Info("Validate Update: Dry run mode detected, skipping validate create for Service:", "name", oldService.GetName())
		return nil, nil
	}

	servicelog.Info("Validation Update: Started for Service", "name", newService.GetName())

	// Do not validate if the service type is not LoadBalancer
	if oldService.Spec.Type != LoadBalancer && newService.Spec.Type != LoadBalancer {
		servicelog.Info("Validate Update: Not Validating Service due to wrong .spec.type", "name", newService.GetName(), "type", newService.Spec.Type)
		return nil, nil
	}

	// Allow change of .spec.Type (corner case) after creation
	if oldService.Spec.Type != LoadBalancer {
		// Get Service annotations
		annotations := oldService.GetAnnotations()
		// If annotations are nil, initialize them
		if annotations == nil {
			annotations = make(map[string]string)
		}
		oldService.SetAnnotations(utils.SetDefaultIpamAnnotations(annotations))
	}

	// Get kube-system namespace uid for cluster identification
	clusterId, err := getClusterID(v.Client)
	if err != nil {
		servicelog.Info("Validate Update: Failed to get cluster ID")
		return nil, err
	}

	// Get namespace uid for Service namespace identification
	namespaceId, err := getNameSpaceID(v.Client, oldService)
	if err != nil {
		servicelog.Info("Validate Update: Failed to get namespace ID")
		return nil, err
	}

	// Get Service annotations from old and new Service objects
	oldAnnotations := utils.FilterMapByPrefix(oldService.GetAnnotations(), "ipam.vitistack.io/")
	newAnnotations := utils.FilterMapByPrefix(newService.GetAnnotations(), "ipam.vitistack.io/")

	// Validate Annotions
	compareAnnotations := reflect.DeepEqual(oldAnnotations, newAnnotations)
	if compareAnnotations {
		servicelog.Info("Validate Update: No changes in annotations, skipping further update validation", "name", newService.GetName())
		return nil, nil
	}

	// Split addresses from annotations to slices and append prefix
	newServicePrefixes := strings.Split(newAnnotations["ipam.vitistack.io/addresses"], ",")
	oldServicePrefixes := strings.Split(oldAnnotations["ipam.vitistack.io/addresses"], ",")

	// Get differences between old and new addresses
	newPrefixes, keepPrefixes, removePrefixes := utils.PrefixSlicesDiff(oldServicePrefixes, newServicePrefixes)

	// Get Secrets

	oldSecret, err := getSecret(oldAnnotations, oldService, v.Client)
	if err != nil {
		servicelog.Info("Validate Update: Failed to get old secret", "error", err)
		return nil, err
	}

	newSecret, err := getSecret(newAnnotations, newService, v.Client)
	if err != nil {
		servicelog.Info("Validate Update: Failed to get new secret", "error", err)
		return nil, err
	}

	// Return an error if oldPrefixes and newPrefixes does not belong to the same zone
	if (oldAnnotations["ipam.vitistack.io/zone"] != newAnnotations["ipam.vitistack.io/zone"]) && len(keepPrefixes) > 0 {
		if len(newPrefixes) > 0 {
			if err := removeAddressIpamAPI(newPrefixes[0], newAnnotations, newService, newSecret, clusterId, namespaceId); err != nil {
				servicelog.Info("Validate Update: Failed to remove (best effort) newly requested address after zone change detected", "service", newService.GetName(), "ip", newPrefixes[0], "Error", err)
			}
		}
		servicelog.Info("Validate Update: Change of zone is prohibited while requesting keeping old addresses", "service", newService.GetName())
		return nil, fmt.Errorf("change of zone is prohibited while keeping old addresses")
	}

	// Validate newPrefixes
	for _, addr := range newPrefixes {
		servicelog.Info("Validate Update: Validating new IP-address:", "name", newService.GetName(), "ip", addr)
		if err := updateAddressIpamAPI(addr, newAnnotations, newService, newSecret, clusterId, namespaceId); err != nil {
			servicelog.Info("Validate Update: Validate IP-address failed!", "name", newService.GetName(), "ip", addr, "error", err)
			if err := removeAddressIpamAPI(newPrefixes[0], newAnnotations, newService, newSecret, clusterId, namespaceId); err != nil {
				servicelog.Info("Validate Update: Failed to remove (best effort) newly requested address after zone change detected", "service", newService.GetName(), "ip", newPrefixes[0], "Error", err)
			}
			return nil, err
		}
	}

	// Update Secret for prefixes to keep
	if err := updateSecret(keepPrefixes, oldAnnotations, newAnnotations, newService, oldSecret, newSecret, clusterId, namespaceId); err != nil {
		for _, addr := range newPrefixes {
			if err := removeAddressIpamAPI(addr, newAnnotations, newService, newSecret, clusterId, namespaceId); err != nil {
				servicelog.Info("Validate Update: Failed to remove newly acquired IP-address", "service", newService.GetName(), "ip", addr, "Error", err)
			}
		}
		return nil, err
	}

	// Remove old addresses that are not needed anymore
	for _, addr := range removePrefixes {
		servicelog.Info("Validate Update: Remove old ip-address", "service", newService.GetName(), "ip", addr)
		if err := removeAddressIpamAPI(addr, oldAnnotations, oldService, oldSecret, clusterId, namespaceId); err != nil {
			servicelog.Info("Validate Update: Failed to remove old ip-address", "service", newService.GetName(), "ip", addr, "Error", err)
		}
	}

	// Update Metallb AddressPool
	if len(newPrefixes) > 0 {
		servicelog.Info("Validate Update: Add prefixes to Metallb Addresspool", "name", newService.GetName(), "pool", oldAnnotations["ipam.vitistack.io/zone"])
		if err := utils.AddIpAddressesToPool(v.Client, newAnnotations, newPrefixes); err != nil {
			servicelog.Info("Validate Update: Unable to add new IP-addresses to pool", "name", newService.GetName(), "pool", newAnnotations["ipam.vitistack.io/zone"], "Error", err)
		}
	}
	if len(removePrefixes) > 0 {
		servicelog.Info("Validate Update: Remove out-dated prefixes from Metallb Addresspool", "name", newService.GetName(), "pool", oldAnnotations["ipam.vitistack.io/zone"])
		if err := utils.RemoveIPAddressesFromPool(v.Client, oldAnnotations, removePrefixes); err != nil {
			servicelog.Info("Validate Update: Unable to remove old IP-addresses from pool", "name", newService.GetName(), "pool", oldAnnotations["ipam.vitistack.io/zone"], "Error", err)
		}
	}

	servicelog.Info("Validate Update: Completed:", "name", newService.GetName())

	return nil, nil
}
