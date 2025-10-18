package v1

import (
	"fmt"
	"strconv"

	"github.com/vitistack/ipam-api/pkg/models/apicontracts"
	utils "github.com/vitistack/ipam-operator/internal/utils"

	corev1 "k8s.io/api/core/v1"
)

func updateSecret(keepPrefixes []string, oldAnnotations map[string]string, newAnnotations map[string]string, oldService *corev1.Service, newService *corev1.Service, oldSecret *corev1.Secret, newSecret *corev1.Secret, clusterId string, namespaceId string) error {

	if (oldAnnotations["ipam.vitistack.io/secret"] != newAnnotations["ipam.vitistack.io/secret"]) && len(keepPrefixes) > 0 {
		for index, addr := range keepPrefixes {
			servicelog.Info("Validate Update: Change secret for IP-address", "name", newService.GetName(), "ip", addr)
			if _, err := updateSecretIpamAPI(addr, newAnnotations, newService, oldSecret, newSecret, clusterId, namespaceId); err != nil {
				servicelog.Info("Validate Update: Change secret for IP-address failed!", "name", newService.GetName(), "ip", addr, "error", err)
				if index == 1 {
					if _, err := updateSecretIpamAPI(keepPrefixes[0], oldAnnotations, newService, newSecret, oldSecret, clusterId, namespaceId); err != nil {
						servicelog.Info("Validate Update: Failed to revert (best effort) first address after second address secret update failure", "service", newService.GetName(), "ip", keepPrefixes[0], "Error", err)
						return err
					}
				}
				return err
			}
		}
	}

	return nil
}

func updateSecretIpamAPI(ipAddress string, annotations map[string]string, service *corev1.Service, oldSecret *corev1.Secret, newSecret *corev1.Secret, clusterId string, namespaceId string) (apicontracts.IpamApiResponse, error) {

	// Convert retentionPeriodDays from string to int
	retentionPeriodDays := annotations["ipam.vitistack.io/retention-period-days"]
	retentionPeriodDaysToInt, err := strconv.Atoi(retentionPeriodDays)
	if err != nil {
		return apicontracts.IpamApiResponse{}, fmt.Errorf("not able to convert byte retentionPeriodDays to Integer")
	}

	// Convert denyExternalCleanup from string to bool
	denyExternalCleanup := annotations["ipam.vitistack.io/deny-external-cleanup"]
	denyExternalCleanupToBool, err := strconv.ParseBool(denyExternalCleanup)
	if err != nil {
		return apicontracts.IpamApiResponse{}, fmt.Errorf("not able to convert string denyExternalCleanup to Bool for Service %s", service.GetName())
	}

	// Determine IP Family
	var ipFamily string
	if utils.IsIPv4(ipAddress) {
		ipFamily = IPv4Family
	} else if utils.IsIPv6(ipAddress) {
		ipFamily = IPv6Family
	} else {
		return apicontracts.IpamApiResponse{}, fmt.Errorf("invalid IP address format for Service %s", service.GetName())
	}

	// Create validate object for IPAM API
	requestAddrObject := apicontracts.IpamApiRequest{
		Secret:    string(oldSecret.Data["secret"]),
		NewSecret: string(newSecret.Data["secret"]),
		Zone:      annotations["ipam.vitistack.io/zone"],
		IpFamily:  ipFamily,
		Address:   ipAddress,
		Service: apicontracts.Service{
			ServiceName:         service.GetName(),
			NamespaceId:         namespaceId,
			ClusterId:           clusterId,
			RetentionPeriodDays: retentionPeriodDaysToInt,
			DenyExternalCleanup: denyExternalCleanupToBool,
		},
	}

	// Validate IP from IPAM API
	var responseAddrObject apicontracts.IpamApiResponse
	responseAddrObject, err = utils.RequestIP(requestAddrObject)
	if err != nil {
		return apicontracts.IpamApiResponse{}, fmt.Errorf("failed to update secret for address-family %s for Service %s: %w", ipFamily, service.GetName(), err)
	}

	return responseAddrObject, nil
}
