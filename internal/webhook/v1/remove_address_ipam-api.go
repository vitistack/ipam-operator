package v1

import (
	"fmt"
	"strconv"

	"github.com/vitistack/ipam-api/pkg/models/apicontracts"
	utils "github.com/vitistack/ipam-operator/internal/utils"

	corev1 "k8s.io/api/core/v1"
)

func RemoveAddressIpamAPI(ipAddress string, annotations map[string]string, service *corev1.Service, secret *corev1.Secret, clusterId string, namespaceId string) error {

	// Convert retentionPeriodDays from string to int
	retentionPeriodDays := annotations["ipam.vitistack.io/retention-period-days"]
	retentionPeriodDaysToInt, err := strconv.Atoi(retentionPeriodDays)
	if err != nil {
		return fmt.Errorf("not able to convert byte retentionPeriodDays to Integer")
	}

	// Convert denyExternalCleanup from string to bool
	denyExternalCleanup := annotations["ipam.vitistack.io/deny-external-cleanup"]
	denyExternalCleanupToBool, err := strconv.ParseBool(denyExternalCleanup)
	if err != nil {
		return fmt.Errorf("not able to convert string denyExternalCleanup to Bool for Service %s", service.GetName())
	}

	// Determine IP Family
	var ipFamily string
	if utils.IsIPv4(ipAddress) {
		ipFamily = "ipv4"
	} else if utils.IsIPv6(ipAddress) {
		ipFamily = "ipv6"
	} else {
		return fmt.Errorf("invalid IP address format for Service %s", service.GetName())
	}

	// Create remove object for IPAM API
	requestAddrObject := apicontracts.IpamApiRequest{
		Secret:   string(secret.Data["secret"]),
		Zone:     annotations["ipam.vitistack.io/zone"],
		IpFamily: ipFamily,
		Address:  ipAddress,
		Service: apicontracts.Service{
			ServiceName:         service.GetName(),
			NamespaceId:         namespaceId,
			ClusterId:           clusterId,
			RetentionPeriodDays: retentionPeriodDaysToInt,
			DenyExternalCleanup: denyExternalCleanupToBool,
		},
	}

	// Remove IP from IPAM API
	_, err = utils.DeleteIP(requestAddrObject)
	if err != nil {
		return fmt.Errorf("failed to remove IP for address-family %s for Service %s: %w", ipFamily, service.GetName(), err)
	}

	return nil
}
