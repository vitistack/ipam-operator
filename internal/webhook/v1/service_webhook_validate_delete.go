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

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Service.
func (v *ServiceCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	service, ok := obj.(*corev1.Service)
	if !ok {
		return nil, fmt.Errorf("expected a Service object but got %T", obj)
	}

	// Do not Validate if the service type is not LoadBalancer.
	if service.Spec.Type != LoadBalancer {
		servicelog.Info("Validate Delete: wrong .spec.type, ABORT further actions", "name", service.GetName(), "type", service.Spec.Type)
		return nil, nil
	}

	// Check if Metallb Controller is actually running
	if err := validateMetallbOperator(ctx, v.Client); err != nil {
		servicelog.Info("Mutation: Metallb operator is not available. Please make sure Metallb is installed and ready.")
		return nil, err
	}

	// Get Service annotations, need "ipam.vitistack.io/zone" to remove IP addresses from correct IPAddressPool
	annotations := service.GetAnnotations()

	// Add all Service addresses to a slice
	addresses := strings.Split(annotations["ipam.vitistack.io/addresses"], ",")

	// Delete Service from IPAM-API
	_, err := utils.DeleteMultiplePrefixes(v.Client, service, addresses)
	if err != nil {
		servicelog.Info("Removed addresses from IPAM-API failed", "service", service.GetName(), "error", err)
		return nil, err
	}

	// Remove Metallb Addresses from IPAddressPool
	if err := utils.RemoveIPAddressesFromPool(v.Client, annotations, addresses); err != nil {
		return nil, fmt.Errorf("failed to remove IP addresses from IPAddressPool: %w", err)
	}

	servicelog.Info("Removed addresses successful:", "name", service.GetName(), "Addresses", addresses)

	servicelog.Info("Validate Delete: Completed:", "name", service.GetName())

	return nil, nil
}
