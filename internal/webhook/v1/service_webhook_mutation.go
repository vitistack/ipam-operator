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

	"github.com/vitistack/ipam-api/pkg/models/apicontracts"
)

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Service.
func (d *ServiceCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {

	// Check if the object is of type Service
	service, ok := obj.(*corev1.Service)

	if !ok {
		return fmt.Errorf("mutation: expected an Service object but got %T", obj)
	}

	// Get the admission request from the context!
	req, _ := admission.RequestFromContext(ctx)

	// Detect dry run mode
	if *req.DryRun {
		servicelog.Info("Mutation: Dry run mode detected, skipping mutating for Service:", "name", service.GetName())
		return nil
	}

	// Start Mutating
	servicelog.Info("Mutation: Started for Service", "name", service.GetName())

	// Do not mutate if the service type is not LoadBalancer.
	if service.Spec.Type != LoadBalancer && len(service.Status.LoadBalancer.Ingress) == 0 {
		servicelog.Info("Mutation: Not Mutating Service due to wrong .spec.type", "name", service.GetName(), "type", service.Spec.Type)
		return nil
	} else {
		// Force spec type to LoadBalancer
		service.Spec.Type = LoadBalancer
	}

	// DryRun the object to check if it pass dry run validation.
	servicelog.Info("Mutation: Dry run .spec Started:", "name", service.GetName())
	if err := validateServiceSpec(ctx, d.Client, service, string(req.Operation)); err != nil {
		servicelog.Info("Mutation: Dry run .spec Failed:", "name", service.GetName(), "error", err)
		return err
	}

	// Check if Metallb Controller is actually running
	if err := validateMetallbOperator(ctx, d.Client); err != nil {
		servicelog.Info("Mutation: Metallb operator is not available. Please make sure Metallb is installed and ready.")
		return err
	}

	// Get kube-system namespace uid for cluster identification
	clusterId, err := getClusterID(d.Client)
	if err != nil {
		servicelog.Info("Mutation: Failed to get cluster ID")
		return err
	}

	// Get namespace uid for Service namespace identification
	namespaceId, err := getNameSpaceID(d.Client, service)
	if err != nil {
		servicelog.Info("Mutation: Failed to get namespace ID")
		return err
	}

	// Get Service annotations
	annotations := service.GetAnnotations()

	// If annotations are nil, initialize them
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Set default annotations for missing IPAM annotations
	annotations = utils.SetDefaultIpamAnnotations(annotations)

	// Reconcile annotations related to addresses and ip-family
	if err := reconcileAnnotationAddresses(annotations, service); err != nil {
		servicelog.Info("Mutation: Reconcile of ip-family annotation failed", "name", service.GetName(), "Error:", err)
		return err
	}

	// Validate annotations boundaries
	if err := utils.ValidateAnnotations(service.GetAnnotations()); err != nil {
		servicelog.Info("Mutation: Invalid annotations for Service", "name", service.GetName(), "Error", err)
		return err
	}

	// Get Secret
	secret, err := getSecret(annotations, service, d.Client)
	if err != nil {
		servicelog.Info("Mutation: Failed to get secret", "error", err)
		return err
	}

	// Request Addresses from IPAM API
	ipFamily := annotations["ipam.vitistack.io/ip-family"]
	var retrievedIPv4Address, retrievedIPv6Address apicontracts.IpamApiResponse

	if !strings.Contains(annotations["ipam.vitistack.io/addresses"], ".") && (ipFamily == IPv4Family || ipFamily == DualFamily) {
		retrievedIPv4Address, err = getAddressIpamAPI(IPv4Family, annotations, service, secret, clusterId, namespaceId)
		if err != nil {
			servicelog.Info("Mutation:", "Message", err)
			return err
		}
		annotations = utils.UpdateAddressAnnotation(annotations, retrievedIPv4Address.Address)
	}

	if !strings.Contains(annotations["ipam.vitistack.io/addresses"], ":") && (ipFamily == IPv6Family || ipFamily == DualFamily) {
		retrievedIPv6Address, err = getAddressIpamAPI(IPv6Family, annotations, service, secret, clusterId, namespaceId)
		if err != nil {
			servicelog.Info("Mutation:", "Message", err)
			if retrievedIPv4Address.Address != "" {
				if err := removeAddressIpamAPI(retrievedIPv4Address.Address, annotations, service, secret, clusterId, namespaceId); err != nil {
					servicelog.Info("Mutation: Failed to remove IPv4 address after IPv6 request failure for Service:", "name", service.GetName(), "ip", retrievedIPv4Address.Address, "Error", err)
				}
			}
			return err
		}
		annotations = utils.UpdateAddressAnnotation(annotations, retrievedIPv6Address.Address)
	}

	// Update annotations
	annotations["ipam.vitistack.io/addresses"] = strings.ReplaceAll(annotations["ipam.vitistack.io/addresses"], " ", "")
	service.SetAnnotations(annotations)

	servicelog.Info("Mutation: Completed for Service", "name", service.GetName())

	return nil
}
