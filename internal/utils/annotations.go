package utils

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func SetDefaultIpamAnnotations(annotations map[string]string) map[string]string {

	// Set default annotations for missing IPAM annotations
	if _, exists := annotations["ipam.vitistack.io/addresses"]; !exists {
		annotations["ipam.vitistack.io/addresses"] = ""
	}
	if _, exists := annotations["ipam.vitistack.io/deny-external-cleanup"]; !exists {
		annotations["ipam.vitistack.io/deny-external-cleanup"] = "false"
	}
	if _, exists := annotations["ipam.vitistack.io/allow-local-shared-ip"]; !exists {
		annotations["ipam.vitistack.io/allow-local-shared-ip"] = "pre-shared-key-" + GenerateRandomString(32)
	}
	if _, exists := annotations["ipam.vitistack.io/ip-family"]; !exists {
		annotations["ipam.vitistack.io/ip-family"] = "ipv4"
	}
	if _, exists := annotations["ipam.vitistack.io/retention-period-days"]; !exists {
		annotations["ipam.vitistack.io/retention-period-days"] = "0"
	}
	if _, exists := annotations["ipam.vitistack.io/secret"]; !exists {
		annotations["ipam.vitistack.io/secret"] = DefaultSecretName
	}
	if _, exists := annotations["ipam.vitistack.io/zone"]; !exists {
		annotations["ipam.vitistack.io/zone"] = "hnet-private"
	}

	// Set Metallb annotations from IPAM annotations values
	annotations["metallb.io/address-pool"] = annotations["ipam.vitistack.io/zone"]
	annotations["metallb.io/loadBalancerIPs"] = annotations["ipam.vitistack.io/addresses"]
	annotations["metallb.io/allow-shared-ip"] = annotations["ipam.vitistack.io/allow-local-shared-ip"]

	return annotations
}

func UpdateAddressAnnotation(annotations map[string]string, prefix string) map[string]string {

	if annotations["ipam.vitistack.io/addresses"] == "" {
		annotations["ipam.vitistack.io/addresses"] = strings.Split(prefix, "/")[0]
	} else {
		annotations["ipam.vitistack.io/addresses"] = strings.Split(prefix, "/")[0] + "," + annotations["ipam.vitistack.io/addresses"]
	}

	annotations["metallb.io/loadBalancerIPs"] = annotations["ipam.vitistack.io/addresses"]

	return annotations
}

func ValidateAnnotations(annotations map[string]string) error {
	errs := validation.ValidateAnnotations(annotations, field.NewPath("metadata", "annotations"))
	if len(errs) > 0 {
		return fmt.Errorf("invalid annotations: %v", errs)
	}
	return nil
}

func EqualSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func FilterMapByPrefix(input map[string]string, prefix string) map[string]string {
	result := make(map[string]string)
	for key, value := range input {
		if strings.HasPrefix(key, prefix) {
			result[key] = value
		}
	}
	return result
}
