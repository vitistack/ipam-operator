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
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

	// Get default Secret in namespace "ipam-system", if it does not exist, create it.
	secret, err := utils.GetDefaultSecret(d.Client)
	if err != nil {
		servicelog.Error(err, "Mutation: Failed to get or create default secret")
		return err
	} else {
		servicelog.Info("Mutation: Default secret found in namespace ipam-system")
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

	// Replace default secret with custom secret if specified in annotations
	if annotations["ipam.vitistack.io/secret"] != "default" {
		secret, err = utils.GetCustomSecret(d.Client, service.Namespace, annotations)
		if err != nil {
			servicelog.Info("Mutation: Failed to get Custom Secret for Service:", "name", service.GetName(), "secret", annotations["ipam.vitistack.io/secret"])
			return err
		}
	}

	// Request Addresses

	ipFamily := annotations["ipam.vitistack.io/ip-family"]

	retentionPeriodDays := annotations["ipam.vitistack.io/retention-period-days"]
	retentionPeriodDaysToInt, err := strconv.Atoi(retentionPeriodDays)
	if err != nil {
		servicelog.Info("Not able to convert byte retentionPeriodDays to Integer for Service:", "name", service.GetName())
		return fmt.Errorf("not able to convert byte retentionPeriodDays to Integer")
	}

	denyExternalCleanup := annotations["ipam.vitistack.io/deny-external-cleanup"]
	denyExternalCleanupToBool, err := strconv.ParseBool(denyExternalCleanup)
	if err != nil {
		servicelog.Info("Not able to convert string denyExternalCleanup to Bool for Service:", "name", service.GetName())
		return fmt.Errorf("not able to convert string denyExternalCleanup to Bool for Service %s", service.GetName())
	}

	requestAddrObject := apicontracts.IpamApiRequest{
		Secret:   string(secret.Data["secret"]),
		Zone:     annotations["ipam.vitistack.io/zone"],
		IpFamily: annotations["ipam.vitistack.io/ip-family"],
		Service: apicontracts.Service{
			ServiceName:         service.GetName(),
			NamespaceId:         string(namespaceId),
			ClusterId:           string(clusterId),
			RetentionPeriodDays: retentionPeriodDaysToInt,
			DenyExternalCleanup: denyExternalCleanupToBool,
		},
	}

	requestIPv4AddrObject := requestAddrObject
	var responseIPv4AddrObject apicontracts.IpamApiResponse

	requestIPv6AddrObject := requestAddrObject
	var responseIPv6AddrObject apicontracts.IpamApiResponse

	var retrievedAddress bool

	switch ipFamily {
	case DualFamily:
		if !strings.Contains(annotations["ipam.vitistack.io/addresses"], ".") {
			servicelog.Info("Request IPv4-address for Service:", "name", service.GetName())
			requestIPv4AddrObject.IpFamily = IPv4Family
			responseIPv4AddrObject, err = utils.RequestIP(requestIPv4AddrObject)
			if err != nil {
				servicelog.Info("Request IPv4-address failed!", "name", service.GetName(), "Message", err)
				return fmt.Errorf("request ipv4-address failed for Service: %s Message: %s", service.GetName(), err)
			}
			requestIPv4AddrObject.Address = responseIPv4AddrObject.Address
			servicelog.Info("Received IPv4-address for Service", "name", service.GetName(), "address", strings.Split(responseIPv4AddrObject.Address, "/")[0])
			retrievedAddress = true
			annotations = utils.UpdateAddressAnnotation(annotations, responseIPv4AddrObject.Address)
		}
		if !strings.Contains(annotations["ipam.vitistack.io/addresses"], ":") {
			servicelog.Info("Request IPv6-address for Service", "name", service.GetName())
			requestIPv6AddrObject.IpFamily = IPv6Family
			responseIPv6AddrObject, err = utils.RequestIP(requestIPv6AddrObject)
			if err != nil {
				servicelog.Info("Request IPv6-address failed!", "name", service.GetName(), "Message", responseIPv6AddrObject.Message)
				if retrievedAddress {
					servicelog.Info("Delete previously allocated IPv4-address for Service:", "name", service.GetName(), "ip", requestIPv4AddrObject.Address)
					_, err = utils.DeleteIP(requestIPv4AddrObject)
					if err != nil {
						servicelog.Info("Delete previously allocated IPv4-address failed for Service:", "name", service.GetName(), "Error", err)
						return fmt.Errorf("delete previously allocated IPv4-address failed for Service: %s Message: %s", service.GetName(), err)
					}
				}
				return fmt.Errorf("request ipv6-address failed for Service: %s Message: %s", service.GetName(), responseIPv6AddrObject.Message)
			}
			requestIPv6AddrObject.Address = responseIPv6AddrObject.Address
			servicelog.Info("Received IPv6-address for Service:", "name", service.GetName(), "address", strings.Split(responseIPv6AddrObject.Address, "/")[0])
			annotations = utils.UpdateAddressAnnotation(annotations, responseIPv6AddrObject.Address)
		}
	case IPv4Family:
		if !strings.Contains(annotations["ipam.vitistack.io/addresses"], ".") {
			servicelog.Info("Request IPv4-address for Service:", "name", service.GetName())
			responseIPv4AddrObject, err := utils.RequestIP(requestAddrObject)
			if err != nil {
				servicelog.Info("Request IPv4-address failed!", "name", service.GetName(), "Message", err)
				return fmt.Errorf("request ipv4-address failed for Service: %s Message: %s", service.GetName(), err)
			}
			requestIPv4AddrObject.Address = responseIPv4AddrObject.Address
			servicelog.Info("Received IPv4-address for Service:", "name", service.GetName(), "address", strings.Split(responseIPv4AddrObject.Address, "/")[0])
			annotations = utils.UpdateAddressAnnotation(annotations, responseIPv4AddrObject.Address)
		}
	case IPv6Family:
		if !strings.Contains(annotations["ipam.vitistack.io/addresses"], ":") {
			servicelog.Info("Request IPv6-address for Service:", "name", service.GetName())
			responseIPv6AddrObject, err := utils.RequestIP(requestAddrObject)
			if err != nil {
				servicelog.Info("Request IPv6-address failed!", "name", service.GetName(), "Message", err)
				return fmt.Errorf("request ipv6-address failed for Service: %s Message: %s", service.GetName(), err)
			}
			requestIPv6AddrObject.Address = responseIPv6AddrObject.Address
			servicelog.Info("Received IPv6-address for Service:", "name", service.GetName(), "address", strings.Split(responseIPv6AddrObject.Address, "/")[0])
			annotations = utils.UpdateAddressAnnotation(annotations, responseIPv6AddrObject.Address)
		}
	default:
		servicelog.Info("Invalid ipFamily detected for Service:", "name", service.GetName(), "ipFamily", ipFamily)
		return fmt.Errorf("invalid ipFamily detected for Service: %s: %s", service.GetName(), ipFamily)
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

	// TODO(user): fill in your validation logic upon object creation.

	servicelog.Info("Validate Create: Started for Service", "name", service.GetName())

	// Do not validate if the service type is not LoadBalancer
	if service.Spec.Type != LoadBalancer {
		servicelog.Info("Validate Create: Not Validating Service due to wrong .spec.type", "name", service.GetName(), "type", service.Spec.Type)
		return nil, nil
	}

	// Initialize Error Object
	var err error

	// Get kube-system namespace uid for cluster identification
	getClusterNamespace := &corev1.Namespace{}
	if err := v.Client.Get(context.TODO(), types.NamespacedName{Name: "kube-system"}, getClusterNamespace); err != nil {
		servicelog.Error(err, "Failed to get kube-system namespace")
	}
	clusterId := getClusterNamespace.GetUID()
	// servicelog.Info("Cluster UID for Service:", "name", service.GetName(), "uid", clusterId)

	// Get namespace uid for Service namespace identification
	getNamespace := &corev1.Namespace{}
	if err := v.Client.Get(context.TODO(), types.NamespacedName{Name: service.Namespace}, getNamespace); err != nil {
		servicelog.Error(err, "Failed to get service namespace")
	}
	namespaceId := getNamespace.GetUID()
	// servicelog.Info("Namespace UID for Service:", "name", service.GetName(), "uid", namespaceId)

	// Get Service annotations
	annotations := service.GetAnnotations()

	// Get Secret
	var secret *corev1.Secret
	if annotations["ipam.vitistack.io/secret"] == "default" {
		secret, err = utils.GetDefaultSecret(v.Client)
		if err != nil {
			servicelog.Error(err, "Failed to get default secret")
			return nil, err
		}
	} else {
		secret, err = utils.GetCustomSecret(v.Client, service.Namespace, annotations)
		if err != nil {
			servicelog.Error(err, "Failed to get custom secret")
			return nil, err
		}
	}

	// Create request object with pre-defined annotations
	retentionPeriodDays := annotations["ipam.vitistack.io/retention-period-days"]
	retentionPeriodDaysToInt, err := strconv.Atoi(retentionPeriodDays)
	if err != nil {
		servicelog.Info("Not able to convert byte retentionPeriodDays to Integer for Service:", "name", service.GetName())
		return nil, fmt.Errorf("not able to convert byte retentionPeriodDays to Integer")
	}

	denyExternalCleanup := annotations["ipam.vitistack.io/deny-external-cleanup"]
	denyExternalCleanupToBool, err := strconv.ParseBool(denyExternalCleanup)
	if err != nil {
		servicelog.Info("Not able to convert string denyExternalCleanup to Bool for Service:", "name", service.GetName())
		return nil, fmt.Errorf("not able to convert string denyExternalCleanup to Bool for Service %s", service.GetName())
	}

	requestAddrObject := apicontracts.IpamApiRequest{
		Secret:   string(secret.Data["secret"]),
		Zone:     annotations["ipam.vitistack.io/zone"],
		IpFamily: annotations["ipam.vitistack.io/ip-family"],
		Service: apicontracts.Service{
			ServiceName:         service.GetName(),
			NamespaceId:         string(namespaceId),
			ClusterId:           string(clusterId),
			RetentionPeriodDays: retentionPeriodDaysToInt,
			DenyExternalCleanup: denyExternalCleanupToBool,
		},
	}

	// Validate addresses against IPAM API
	addrSlice := strings.Split(annotations["ipam.vitistack.io/addresses"], ",")

	var validateFailed bool
	var validatedAddresses []string

	for _, addr := range addrSlice {
		requestAddrObject.Address = addr
		_, err := utils.RequestIP(requestAddrObject)
		if err != nil {
			servicelog.Info("Validate IP-address failed!", "name", service.GetName(), "ip", addr, "error", err)
			validateFailed = true
		} else {
			servicelog.Info("Validate IP-address succeeded!", "name", service.GetName(), "ip", addr)
			validatedAddresses = append(validatedAddresses, addr)
		}
	}

	// Remove Validated Addresses if validateFailed true
	if validateFailed {
		for _, addr := range validatedAddresses {
			_, err := utils.DeleteIP(requestAddrObject)
			if err != nil {
				servicelog.Info("Delete Validated IP-address failed!", "name", service.GetName(), "ip", addr, "Error", err)
			}
		}
		return nil, fmt.Errorf("validation create failed for service %s, Please verify f.ex secret!", service.GetName())
	}

	err = utils.AddIpAddressesToPool(v.Client, annotations, addrSlice)
	if err != nil {
		servicelog.Info("Unable to add IP-addresses to pool", "name", service.GetName(), "pool", annotations["ipam.vitistack.io/zone"], "Error", err)
		return nil, fmt.Errorf("unable to add IP-addresses to pool %s. Error: %s", annotations["ipam.vitistack.io/zone"], err)
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

	// TODO(user): fill in your validation logic upon object update.

	servicelog.Info("Validation Update Started for Service", "name", newService.GetName())

	// Do not validate if the service type is not LoadBalancer
	if oldService.Spec.Type != LoadBalancer && newService.Spec.Type != LoadBalancer {
		servicelog.Info("Not Validating Service due to wrong .spec.type", "name", newService.GetName(), "type", newService.Spec.Type)
		return nil, nil
	}

	// Set variable err
	var err error

	// Get the admission request from the context!
	req, _ := admission.RequestFromContext(ctx)

	// Detect dry run mode
	if *req.DryRun {
		servicelog.Info("Dry run mode detected, skipping validate update for Service:", "name", newService.GetName())
		return nil, nil
	}

	// Support changing .Spec.Type
	if oldService.Spec.Type != LoadBalancer {
		// Get Service annotations
		annotations := oldService.GetAnnotations()
		// If annotations are nil, initialize them
		if annotations == nil {
			annotations = make(map[string]string)
		}
		oldService.SetAnnotations(utils.SetDefaultIpamAnnotations(annotations))
	}

	// Get Service annotations from old and new Service objects
	oldAnnotations := utils.FilterMapByPrefix(oldService.GetAnnotations(), "ipam.vitistack.io/")
	newAnnotations := utils.FilterMapByPrefix(newService.GetAnnotations(), "ipam.vitistack.io/")

	// Validate Annotions
	compareAnnotations := reflect.DeepEqual(oldAnnotations, newAnnotations)
	if compareAnnotations {
		servicelog.Info("No changes in annotations, skipping update validation", "name", newService.GetName())
		servicelog.Info("Validation for Service upon update completed:", "name", newService.GetName())
		return nil, nil
	}

	// Split addresses from annotations to slices and append prefix
	newServicePrefixes := strings.Split(newAnnotations["ipam.vitistack.io/addresses"], ",")
	oldServicePrefixes := strings.Split(oldAnnotations["ipam.vitistack.io/addresses"], ",")

	// Create a slice to hold addresses which should be added
	newPrefixes := []string{}
	for _, newAddr := range newServicePrefixes {
		if !slices.Contains(oldServicePrefixes, newAddr) {
			newPrefixes = append(newPrefixes, newAddr)
		}
	}

	// Create a slice to hold addresses which should be updated
	keepPrefixes := []string{}
	for _, newAddr := range newServicePrefixes {
		if slices.Contains(oldServicePrefixes, newAddr) {
			keepPrefixes = append(keepPrefixes, newAddr)
		}
	}

	// Create a slice to hold addresses which should be removed
	removePrefixes := []string{}
	for _, oldAddr := range oldServicePrefixes {
		if !slices.Contains(newServicePrefixes, oldAddr) {
			removePrefixes = append(removePrefixes, oldAddr)
		}
	}

	// Return error if len(keepPrefixes) > 0 & change of adddress-family for newPrefixes
	if len(keepPrefixes) > 0 && len(newPrefixes) > 0 {
		if oldAnnotations["ipam.vitistack.io/zone"] != newAnnotations["ipam.vitistack.io/zone"] {
			servicelog.Info("Change of zone is prohibited while keeping addresses from another zone", "service", newService.GetName())
			if _, err := utils.DeleteMultiplePrefixes(v.Client, newService, newPrefixes); err != nil {
				servicelog.Info("Remove allocated ip-addresses failed:", "service", newService.GetName(), "Prefixes:", newPrefixes, "Error", err)
			}
			servicelog.Info("Remove allocated ip-addresses:", "service", newService.GetName(), "Prefixes:", newPrefixes)
			return nil, fmt.Errorf("change of zone is prohibited while keeping addresses from another zone")
		}
	}

	// Request new addresses
	var newPrefixesSucceeded []string
	if len(newPrefixes) > 0 {
		servicelog.Info("Validate new ip-addresses for Service:", "name", newService.GetName(), "Addresses", newPrefixes)
		newPrefixesSucceeded, err = utils.RequestMultiplePrefixes(v.Client, newService, newPrefixes)
		if err != nil {
			if len(newPrefixesSucceeded) == 0 {
				servicelog.Info("Validate failed for new requests:", "name", newService.GetName(), "Error", err)
			} else {
				servicelog.Info("Validate failed, delete succedeed requests:", "name", newService.GetName(), "Error", err)
				_, err := utils.DeleteMultiplePrefixes(v.Client, newService, newPrefixesSucceeded)
				if err != nil {
					servicelog.Info("Failed to delete succedeed requests:", "name", newService.GetName(), "Error", err)
				}
			}
			return nil, fmt.Errorf("failed to request new ip-addresses during validate update: %v", err)
		}
	}

	// Update Secret for addresses to keep
	var keepPrefixesSucceeded []string
	if len(keepPrefixes) > 0 {
		servicelog.Info("Update addresses to keep:", "name", newService.GetName(), "Addresses", keepPrefixes)
		keepPrefixesSucceeded, err = utils.UpdateMultiplePrefixes(v.Client, oldService, newService, keepPrefixes)
		if err != nil {
			if len(keepPrefixesSucceeded) == 0 {
				servicelog.Info("Update failed for addresses to keep:", "name", newService.GetName(), "Error", err)
				if _, err := utils.DeleteMultiplePrefixes(v.Client, newService, newPrefixesSucceeded); err != nil {
					servicelog.Info("Remove allocated ip-addresses failed:", "service", newService.GetName(), "Prefixes:", newPrefixesSucceeded, "Error", err)
				}
			} else {
				servicelog.Info("Delete requested new addresses:", "name", newService.GetName(), "Addresses", newPrefixesSucceeded)
				if _, err := utils.DeleteMultiplePrefixes(v.Client, newService, newPrefixesSucceeded); err != nil {
					servicelog.Info("Remove allocated ip-addresses failed:", "service", newService.GetName(), "Prefixes:", newPrefixesSucceeded, "Error", err)
				}
				servicelog.Info("Update failed, revert succeeded updates to keep:", "name", newService.GetName(), "Error", err)
				_, err := utils.UpdateMultiplePrefixes(v.Client, newService, oldService, keepPrefixesSucceeded)
				if err != nil {
					servicelog.Info("Failed to revert succeeded updates for addresses to keep:", "name", newService.GetName(), "Error", err)
				}
			}
			return nil, fmt.Errorf("failed to update existing addresses during validate update: %v", err)
		}
	}

	// Remove old addresses
	var removePrefixesSucceeded []string
	if len(removePrefixes) > 0 && removePrefixes[0] != "" {
		servicelog.Info("Remove addresses from IPAM-API", "name", newService.GetName(), "Addresses", removePrefixes)
		removePrefixesSucceeded, err = utils.DeleteMultiplePrefixes(v.Client, oldService, removePrefixes)
		if err != nil {
			if len(removePrefixesSucceeded) != 0 {
				servicelog.Info("Remove addresses failed from IPAM-API:", "name", newService.GetName(), "Error", err)
				servicelog.Info("Best effort reverting:", "name", newService.GetName(), "Error", err)
				if _, err := utils.RequestMultiplePrefixes(v.Client, oldService, removePrefixesSucceeded); err != nil {
					servicelog.Info("Revert of removed addresses failed:", "name", newService.GetName(), "Error", err)
				}
				if _, err := utils.DeleteMultiplePrefixes(v.Client, newService, newPrefixesSucceeded); err != nil {
					servicelog.Info("Remove allocated ip-addresses failed:", "service", newService.GetName(), "Prefixes:", newPrefixesSucceeded, "Error", err)
				}
				if _, err := utils.UpdateMultiplePrefixes(v.Client, newService, oldService, keepPrefixesSucceeded); err != nil {
					servicelog.Info("Failed to revert succeeded updates for addresses to keep:", "name", newService.GetName(), "Error", err)
				}
			}
			return nil, fmt.Errorf("failed to remove addresses during validate update: %v", err)
		}
	}

	// Update Metallb AddressPool
	if len(newPrefixes) > 0 {
		if err := utils.AddIpAddressesToPool(v.Client, newAnnotations, newPrefixes); err != nil {
			servicelog.Info("Unable to add new IP-addresses to pool", "name", newService.GetName(), "pool", newAnnotations["ipam.vitistack.io/zone"], "Error", err)
		}
	}
	if len(keepPrefixes) > 0 {
		if err := utils.AddIpAddressesToPool(v.Client, newAnnotations, keepPrefixes); err != nil {
			servicelog.Info("Unable to add existing IP-addresses to pool", "name", newService.GetName(), "pool", newAnnotations["ipam.vitistack.io/zone"], "Error", err)
		}
	}
	if len(removePrefixes) > 0 {
		if err := utils.RemoveIPAddressesFromPool(v.Client, oldAnnotations, removePrefixes); err != nil {
			servicelog.Info("Unable to remove old IP-addresses from pool", "name", newService.GetName(), "pool", oldAnnotations["ipam.vitistack.io/zone"], "Error", err)
		}
	}

	servicelog.Info("Validation for Service upon update completed:", "name", newService.GetName())

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
		servicelog.Info("Not Mutating Service due to wrong .spec.type", "name", service.GetName(), "type", service.Spec.Type)
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
