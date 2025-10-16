package v1

import (
	"fmt"
	"strings"

	utils "github.com/vitistack/ipam-operator/internal/utils"
	corev1 "k8s.io/api/core/v1"
)

func ReconcileAnnotationAddresses(annotations map[string]string, service *corev1.Service) error {

	// Validate if addresses is valid IPs
	if annotations["ipam.vitistack.io/addresses"] != "" {
		for ip := range strings.SplitSeq(annotations["ipam.vitistack.io/addresses"], ",") {
			if !utils.IsValidIp(ip) {
				return fmt.Errorf("invalid ip-address detected for Service: %s: %s", service.GetName(), ip)
			}
		}
	}

	// Validate if two or more addresses is within same addressFamily
	if annotations["ipam.vitistack.io/addresses"] != "" {
		sliceIps := strings.Split(annotations["ipam.vitistack.io/addresses"], ",")
		var sliceIPv4Ips []string
		var sliceIPv6Ips []string
		for _, ip := range sliceIps {
			if strings.Contains(ip, ".") {
				sliceIPv4Ips = append(sliceIPv4Ips, ip)
			}
			if strings.Contains(ip, ":") {
				sliceIPv6Ips = append(sliceIPv6Ips, ip)
			}
		}
		if len(sliceIPv4Ips) > 1 || len(sliceIPv6Ips) > 1 {
			return fmt.Errorf("metallb supports only one (1) address pr ip-family")
		}
	}

	// Validate if IP-address family is valid
	if err := utils.ValidIpAddressFamiliy(annotations); err != nil {
		return fmt.Errorf("illegal ip-address family %v", err)
	}

	// Set .spec.ipFamilyPolicy according to ip-family annotation
	var ipFamily corev1.IPFamilyPolicy
	switch annotations["ipam.vitistack.io/ip-family"] {
	case IPv4Family:
		ipFamily = corev1.IPFamilyPolicySingleStack
		service.Spec.IPFamilyPolicy = &ipFamily
	case IPv6Family:
		ipFamily = corev1.IPFamilyPolicySingleStack
		service.Spec.IPFamilyPolicy = &ipFamily
	case DualFamily:
		ipFamily = corev1.IPFamilyPolicyRequireDualStack
		service.Spec.IPFamilyPolicy = &ipFamily
	}

	// Clear .spec.ClusterIP to avoid conflicts with pre-set IP-addresses
	service.Spec.ClusterIP = ""

	return nil

}
