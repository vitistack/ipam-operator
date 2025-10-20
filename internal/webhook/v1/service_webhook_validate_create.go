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

	utils "github.com/vitistack/ipam-operator/internal/utils"
)

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Service.
func (v *ServiceCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {

	// Initialize Error Object
	var err error

	service, ok := obj.(*corev1.Service)
	if !ok {
		return nil, fmt.Errorf("expected a Service object but got %T", obj)
	}

	// Get the admission request from the context!
	req, _ := admission.RequestFromContext(ctx)

	// Detect dry run mode
	if *req.DryRun {
		servicelog.Info("Validate Create: Dry run mode detected, skipping validate create for Service:", "name", service.GetName())
		return nil, nil
	}

	servicelog.Info("Validate Create: Started for Service", "name", service.GetName())

	// Do not validate if the service type is not LoadBalancer
	if service.Spec.Type != LoadBalancer {
		servicelog.Info("Validate Create: Not Validating Service due to wrong .spec.type", "name", service.GetName(), "type", service.Spec.Type)
		return nil, nil
	}

	// Get kube-system namespace uid for cluster identification
	clusterId, err := getClusterID(v.Client)
	if err != nil {
		servicelog.Info("Validate Create: Failed to get cluster ID")
		return nil, err
	}

	// Get namespace uid for Service namespace identification
	namespaceId, err := getNameSpaceID(v.Client, service)
	if err != nil {
		servicelog.Info("Validate Create: Failed to get namespace ID")
		return nil, err
	}

	// Get Service annotations
	annotations := service.GetAnnotations()

	// Get Secret
	secret, err := getSecret(annotations, service, v.Client)
	if err != nil {
		servicelog.Info("Validate Crate: Failed to get secret", "error", err)
		return nil, err
	}

	// Validate addresses against IPAM API
	addrSlice := strings.Split(annotations["ipam.vitistack.io/addresses"], ",")
	validatedAddresses := make([]string, 0, len(addrSlice))
	var validateFailed bool

	for _, addr := range addrSlice {
		servicelog.Info("Validate Create: Validating IP-address:", "name", service.GetName(), "ip", addr)
		if err := updateAddressIpamAPI(addr, annotations, service, secret, clusterId, namespaceId); err != nil {
			servicelog.Info("Validate Create: IP-address failed!", "name", service.GetName(), "ip", addr, "error", err)
			validateFailed = true
			break
		}
		validatedAddresses = append(validatedAddresses, addr)
	}

	if validateFailed {
		for _, addr := range validatedAddresses {
			if err := removeAddressIpamAPI(addr, annotations, service, secret, clusterId, namespaceId); err != nil {
				servicelog.Info("Validate Create: Delete Validated IP-address failed!", "name", service.GetName(), "ip", addr, "Error", err)
			}
		}
		return nil, fmt.Errorf("validation create failed for service %s, Please verify f.ex secret", service.GetName())
	}

	if err := utils.AddIpAddressesToPool(v.Client, annotations, addrSlice); err != nil {
		servicelog.Info("Unable to add IP-addresses to pool", "name", service.GetName(), "pool", annotations["ipam.vitistack.io/zone"], "Error", err)
		return nil, err
	}

	servicelog.Info("Validate Create: Completed for Service", "name", service.GetName())

	return nil, nil
}
