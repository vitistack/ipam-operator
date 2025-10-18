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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"reflect"

	utils "github.com/vitistack/ipam-operator/internal/utils"

	"github.com/vitistack/ipam-api/pkg/models/apicontracts"
)

const (
	DualFamily   = "dual"
	IPv4Family   = "ipv4"
	IPv6Family   = "ipv6"
	LoadBalancer = "LoadBalancer"
)

// nolint:unused
// log is for logging in this package.
var servicelog = logf.Log.WithName("ipam-operator")

// SetupServiceWebhookWithManager registers the webhook for Service in the manager.
func SetupServiceWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1.Service{}).
		WithValidator(&ServiceCustomValidator{Client: mgr.GetClient()}).
		WithDefaulter(&ServiceCustomDefaulter{Client: mgr.GetClient()}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate--v1-service,mutating=true,failurePolicy=Ignore,sideEffects=NoneOnDryRun,groups="",resources=services,verbs=create;update,versions=v1,name=mservice-v1.kb.io,admissionReviewVersions=v1

// ServiceCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Service when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.

type ServiceCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
	Client client.Client
}

var _ webhook.CustomDefaulter = &ServiceCustomDefaulter{}

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
	if err := ValidateServiceSpec(ctx, d.Client, service, string(req.Operation)); err != nil {
		servicelog.Info("Mutation: Dry run .spec Failed:", "name", service.GetName(), "error", err)
		return err
	}

	// Check if Metallb Controller is actually running
	if err := ValidateMetallbOperator(ctx, d.Client); err != nil {
		servicelog.Info("Mutation: Metallb operator is not available. Please make sure Metallb is installed and ready.")
		return err
	}

	// Get kube-system namespace uid for cluster identification
	clusterId, err := GetClusterID(d.Client)
	if err != nil {
		servicelog.Info("Mutation: Failed to get cluster ID")
		return err
	}

	// Get namespace uid for Service namespace identification
	namespaceId, err := GetNameSpaceID(d.Client, service)
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
	if err := ReconcileAnnotationAddresses(annotations, service); err != nil {
		servicelog.Info("Mutation: Reconcile of ip-family annotation failed", "name", service.GetName(), "Error:", err)
		return err
	}

	// Validate annotations boundaries
	if err := utils.ValidateAnnotations(service.GetAnnotations()); err != nil {
		servicelog.Info("Mutation: Invalid annotations for Service", "name", service.GetName(), "Error", err)
		return err
	}

	// Get Secret
	secret, err := GetSecret(annotations, service, d.Client)
	if err != nil {
		servicelog.Info("Mutation: Failed to get secret", "error", err)
		return err
	}

	// Request Addresses from IPAM API
	ipFamily := annotations["ipam.vitistack.io/ip-family"]
	var retrievedIPv4Address, retrievedIPv6Address apicontracts.IpamApiResponse

	if !strings.Contains(annotations["ipam.vitistack.io/addresses"], ".") && (ipFamily == IPv4Family || ipFamily == DualFamily) {
		retrievedIPv4Address, err = GetAddressIpamAPI(IPv4Family, annotations, service, secret, clusterId, namespaceId)
		if err != nil {
			servicelog.Info("Mutation:", "Message", err)
			return err
		}
		annotations = utils.UpdateAddressAnnotation(annotations, retrievedIPv4Address.Address)
	}

	if !strings.Contains(annotations["ipam.vitistack.io/addresses"], ":") && (ipFamily == IPv6Family || ipFamily == DualFamily) {
		retrievedIPv6Address, err = GetAddressIpamAPI(IPv6Family, annotations, service, secret, clusterId, namespaceId)
		if err != nil {
			servicelog.Info("Mutation:", "Message", err)
			if retrievedIPv4Address.Address != "" {
				if err := RemoveAddressIpamAPI(retrievedIPv4Address.Address, annotations, service, secret, clusterId, namespaceId); err != nil {
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

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate--v1-service,mutating=false,failurePolicy=Ignore,sideEffects=None,groups="",resources=services,verbs=create;update;delete,versions=v1,name=vservice-v1.kb.io,admissionReviewVersions=v1

// ServiceCustomValidator struct is responsible for validating the Service resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ServiceCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
	Client client.Client
}

var _ webhook.CustomValidator = &ServiceCustomValidator{}

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
	clusterId, err := GetClusterID(v.Client)
	if err != nil {
		servicelog.Info("Validate Create: Failed to get cluster ID")
		return nil, err
	}

	// Get namespace uid for Service namespace identification
	namespaceId, err := GetNameSpaceID(v.Client, service)
	if err != nil {
		servicelog.Info("Validate Create: Failed to get namespace ID")
		return nil, err
	}

	// Get Service annotations
	annotations := service.GetAnnotations()

	// Get Secret
	secret, err := GetSecret(annotations, service, v.Client)
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
		if _, err := UpdateAddressIpamAPI(addr, annotations, service, secret, clusterId, namespaceId); err != nil {
			servicelog.Info("Validate Create: IP-address failed!", "name", service.GetName(), "ip", addr, "error", err)
			validateFailed = true
			break
		}
		validatedAddresses = append(validatedAddresses, addr)
	}

	if validateFailed {
		for _, addr := range validatedAddresses {
			if err := RemoveAddressIpamAPI(addr, annotations, service, secret, clusterId, namespaceId); err != nil {
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
	clusterId, err := GetClusterID(v.Client)
	if err != nil {
		servicelog.Info("Validate Update: Failed to get cluster ID")
		return nil, err
	}

	// Get namespace uid for Service namespace identification
	namespaceId, err := GetNameSpaceID(v.Client, oldService)
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

	oldSecret, err := GetSecret(oldAnnotations, oldService, v.Client)
	if err != nil {
		servicelog.Info("Validate Update: Failed to get old secret", "error", err)
		return nil, err
	}

	newSecret, err := GetSecret(newAnnotations, newService, v.Client)
	if err != nil {
		servicelog.Info("Validate Update: Failed to get new secret", "error", err)
		return nil, err
	}

	// Return an error if oldPrefixes and newPrefixes does not belong to the same zone
	if (oldAnnotations["ipam.vitistack.io/zone"] != newAnnotations["ipam.vitistack.io/zone"]) && len(keepPrefixes) > 0 {
		if len(newPrefixes) > 0 {
			if err := RemoveAddressIpamAPI(newPrefixes[0], newAnnotations, newService, newSecret, clusterId, namespaceId); err != nil {
				servicelog.Info("Validate Update: Failed to remove (best effort) newly requested address after zone change detected", "service", newService.GetName(), "ip", newPrefixes[0], "Error", err)
			}
		}
		servicelog.Info("Validate Update: Change of zone is prohibited while requesting keeping old addresses", "service", newService.GetName())
		return nil, fmt.Errorf("change of zone is prohibited while keeping old addresses")
	}

	// Validate newPrefixes
	for _, addr := range newPrefixes {
		servicelog.Info("Validate Update: Validating new IP-address:", "name", newService.GetName(), "ip", addr)
		if _, err := UpdateAddressIpamAPI(addr, newAnnotations, newService, newSecret, clusterId, namespaceId); err != nil {
			servicelog.Info("Validate Update: Validate IP-address failed!", "name", newService.GetName(), "ip", addr, "error", err)
			if err := RemoveAddressIpamAPI(newPrefixes[0], newAnnotations, newService, newSecret, clusterId, namespaceId); err != nil {
				servicelog.Info("Validate Update: Failed to remove (best effort) newly requested address after zone change detected", "service", newService.GetName(), "ip", newPrefixes[0], "Error", err)
			}
			return nil, err
		}
	}

	// Update Secret for prefixes to keep
	if err := UpdateSecret(keepPrefixes, oldAnnotations, newAnnotations, oldService, newService, oldSecret, newSecret, clusterId, namespaceId); err != nil {
		for _, addr := range newPrefixes {
			if err := RemoveAddressIpamAPI(addr, newAnnotations, newService, newSecret, clusterId, namespaceId); err != nil {
				servicelog.Info("Validate Update: Failed to remove newly acquired IP-address", "service", newService.GetName(), "ip", addr, "Error", err)
			}
		}
		return nil, err
	}

	// Remove old addresses that are not needed anymore
	for _, addr := range removePrefixes {
		servicelog.Info("Validate Update: Remove old ip-address", "service", newService.GetName(), "ip", addr)
		if err := RemoveAddressIpamAPI(addr, oldAnnotations, oldService, oldSecret, clusterId, namespaceId); err != nil {
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

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Service.
func (v *ServiceCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	service, ok := obj.(*corev1.Service)
	if !ok {
		return nil, fmt.Errorf("expected a Service object but got %T", obj)
	}

	// TODO(user): fill in your validation logic upon object deletion.

	// Do not Validate if the service type is not LoadBalancer.

	if service.Spec.Type != LoadBalancer {
		servicelog.Info("Validate Delete: wrong .spec.type, ABORT further actions", "name", service.GetName(), "type", service.Spec.Type)
		return nil, nil
	}

	// Check if Metallb Controller is actually running
	var podList corev1.PodList
	podSelector := client.MatchingLabels{"app": "metallb", "component": "controller"}
	if err := v.Client.List(ctx, &podList, client.InNamespace("metallb-system"), podSelector); err != nil {
		servicelog.Info("Failed to list Pods: Error: %v", err)
		return nil, fmt.Errorf("failed to list Pods: %w", err)
	}
	var podRunning bool
	if len(podList.Items) > 0 {
		for _, pod := range podList.Items {
			if pod.Status.Phase == corev1.PodRunning {
				podRunning = true
			}
		}
	}
	if !podRunning {
		servicelog.Info("Metallb operator is not available. Please make sure Metallb is installed and running.")
		return nil, fmt.Errorf("metallb operator is not available. Please make sure Metallb is installed and running")
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
	err = utils.RemoveIPAddressesFromPool(v.Client, annotations, addresses)

	if err != nil {
		return nil, fmt.Errorf("failed to remove IP addresses from IPAddressPool: %w", err)
	} else {
		servicelog.Info("Removed addresses successful:", "name", service.GetName(), "Addresses", addresses)
	}

	servicelog.Info("Validation for Service upon deletion completed:", "name", service.GetName())

	return nil, nil
}
