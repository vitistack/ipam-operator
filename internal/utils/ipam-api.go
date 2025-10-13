package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/vitistack/ipam-api/pkg/models/apicontracts"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DualFamily = "dual"
	IPAMApiUrl = "IPAM_API_URL"
	IPv6Family = "ipv6"
	IPv4Family = "ipv4"
)

func RequestIP(request apicontracts.IpamApiRequest) (apicontracts.IpamApiResponse, error) {

	// Get OS Environment Variable for IPAM API
	envVar := IPAMApiUrl
	ipamApiUrl := os.Getenv(envVar)
	if ipamApiUrl == "" {
		return apicontracts.IpamApiResponse{}, fmt.Errorf("environment variable for IPAM-API %s was not found", envVar)
	}

	// Update request object with prefix and correct ip-family
	if strings.Contains(request.Address, ".") {
		if !strings.Contains(request.Address, "/32") {
			request.Address = request.Address + "/32"
			request.IpFamily = IPv4Family
		}
	}
	if strings.Contains(request.Address, ":") {
		if !strings.Contains(request.Address, "/128") {
			request.Address = request.Address + "/128"
			request.IpFamily = IPv6Family
		}
	}

	// Marshal the struct to JSON
	jsonData, err := json.Marshal(request)
	if err != nil {
		return apicontracts.IpamApiResponse{}, fmt.Errorf("fail to struct request to Json: %v", err)
	}

	// Make the HTTP POST request
	req, err := http.NewRequest("POST", ipamApiUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return apicontracts.IpamApiResponse{}, fmt.Errorf("fail to create new http request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return apicontracts.IpamApiResponse{}, fmt.Errorf("fail to send request to: %v , error: %v", ipamApiUrl, err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("error closing response body: %s\n", err)
		}
	}()

	// Decode the response (optional)
	var responseData apicontracts.IpamApiResponse
	err = json.NewDecoder(resp.Body).Decode(&responseData)
	if err != nil {
		return apicontracts.IpamApiResponse{}, fmt.Errorf("fail to decode response %v", err)
	}

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return responseData, fmt.Errorf("%s", responseData.Message)
	}

	return responseData, nil

}

func DeleteIP(request apicontracts.IpamApiRequest) (apicontracts.IpamApiResponse, error) {

	// Get OS Environment Variable for IPAM API
	envVar := IPAMApiUrl
	ipamApiUrl := os.Getenv(envVar)
	if ipamApiUrl == "" {
		return apicontracts.IpamApiResponse{}, fmt.Errorf("environment variable %s was not found", envVar)
	}

	// Update request object with prefix and ip-family if missing
	if strings.Contains(request.Address, ".") {
		if !strings.Contains(request.Address, "/32") {
			request.Address = request.Address + "/32"
			request.IpFamily = IPv4Family
		}
	}
	if strings.Contains(request.Address, ":") {
		if !strings.Contains(request.Address, "/128") {
			request.Address = request.Address + "/128"
			request.IpFamily = IPv6Family
		}
	}

	// Marshal the struct to JSON
	jsonData, err := json.Marshal(request)
	if err != nil {
		return apicontracts.IpamApiResponse{}, err
	}

	// Make the HTTP POST request
	req, err := http.NewRequest("DELETE", ipamApiUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return apicontracts.IpamApiResponse{}, err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return apicontracts.IpamApiResponse{}, err
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("error closing response body: %s\n", err)
		}
	}()

	// Decode the response (optional)
	var responseData apicontracts.IpamApiResponse
	err = json.NewDecoder(resp.Body).Decode(&responseData)
	if err != nil {
		return apicontracts.IpamApiResponse{}, err
	}

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return responseData, fmt.Errorf("%s", responseData.Message)
	}

	return responseData, nil

}

func RequestMultiplePrefixes(v client.Client, service *corev1.Service, prefixes []string) ([]string, error) {

	// Initialize err
	var err error

	// Get OS Environment Variable for IPAM API
	envVar := IPAMApiUrl
	ipamApiUrl := os.Getenv(envVar)
	if ipamApiUrl == "" {
		return nil, fmt.Errorf("environment variable %s was not found", envVar)
	}

	// Get kube-system namespace uid for cluster identification
	getClusterNamespace := &corev1.Namespace{}
	if err := v.Get(context.TODO(), types.NamespacedName{Name: "kube-system"}, getClusterNamespace); err != nil {
		return nil, fmt.Errorf("failed to get kube-system namespace: %v", err)
	}
	clusterId := getClusterNamespace.GetUID()

	// Get namespace uid for Service namespace identification
	getNamespace := &corev1.Namespace{}
	if err := v.Get(context.TODO(), types.NamespacedName{Name: service.Namespace}, getNamespace); err != nil {
		return nil, fmt.Errorf("failed to get service namespace: %v", err)
	}
	namespaceId := getNamespace.GetUID()

	// Get Service annotations
	annotations := service.GetAnnotations()

	// Get Secret
	var secret *corev1.Secret
	if annotations["ipam.vitistack.io/secret"] == "default" {
		secret, err = GetDefaultSecret(v)
		if err != nil {
			return nil, fmt.Errorf("failed to get default secret: %v", err)
		}
	} else {
		secret, err = GetCustomSecret(v, service.Namespace, annotations)
		if err != nil {
			return nil, fmt.Errorf("failed to get custom secret: %v", err)
		}
	}

	retentionPeriodDays := annotations["ipam.vitistack.io/retention-period-days"]
	retentionPeriodDaysToInt, err := strconv.Atoi(retentionPeriodDays)
	if err != nil {
		return nil, fmt.Errorf("not able to convert byte retentionPeriodDays to Integer")
	}

	denyExternalCleanup := annotations["ipam.vitistack.io/deny-external-cleanup"]
	denyExternalCleanupToBool, err := strconv.ParseBool(denyExternalCleanup)
	if err != nil {
		return nil, fmt.Errorf("not able to convert string denyExternalCleanup to Bool for Service %s", service.GetName())
	}

	// Request Object
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

	// Post new requests and return succeeded requests

	var succeededRequests []string
	var failedRequests error

	for _, addr := range prefixes {
		requestAddrObject.Address = addr
		if strings.Contains(addr, ".") {
			requestAddrObject.IpFamily = IPv4Family
		} else {
			requestAddrObject.IpFamily = IPv6Family
		}
		_, err := RequestIP(requestAddrObject)
		if err != nil {
			failedRequests = err
		} else {
			succeededRequests = append(succeededRequests, addr)
		}
	}

	return succeededRequests, failedRequests
}

func DeleteMultiplePrefixes(v client.Client, service *corev1.Service, prefixes []string) ([]string, error) {

	// Initialize err
	var err error

	// Get OS Environment Variable for IPAM API
	envVar := IPAMApiUrl
	ipamApiUrl := os.Getenv(envVar)
	if ipamApiUrl == "" {
		return nil, fmt.Errorf("environment variable %s was not found", envVar)
	}

	// Get kube-system namespace uid for cluster identification
	getClusterNamespace := &corev1.Namespace{}
	if err := v.Get(context.TODO(), types.NamespacedName{Name: "kube-system"}, getClusterNamespace); err != nil {
		return nil, fmt.Errorf("failed to get kube-system namespace: %v", err)
	}
	clusterId := getClusterNamespace.GetUID()

	// Get namespace uid for Service namespace identification
	getNamespace := &corev1.Namespace{}
	if err := v.Get(context.TODO(), types.NamespacedName{Name: service.Namespace}, getNamespace); err != nil {
		return nil, fmt.Errorf("failed to get service namespace: %v", err)
	}
	namespaceId := getNamespace.GetUID()

	// Get Service annotations
	annotations := service.GetAnnotations()

	// Get Secret
	var secret *corev1.Secret
	if annotations["ipam.vitistack.io/secret"] == "default" {
		secret, err = GetDefaultSecret(v)
		if err != nil {
			return nil, fmt.Errorf("failed to get default secret: %v", err)
		}
	} else {
		secret, err = GetCustomSecret(v, service.Namespace, annotations)
		if err != nil {
			return nil, fmt.Errorf("failed to get custom secret: %v", err)
		}
	}

	retentionPeriodDays := annotations["ipam.vitistack.io/retention-period-days"]
	retentionPeriodDaysToInt, err := strconv.Atoi(retentionPeriodDays)
	if err != nil {
		return nil, fmt.Errorf("not able to convert byte retentionPeriodDays to Integer")
	}

	denyExternalCleanup := annotations["ipam.vitistack.io/deny-external-cleanup"]
	denyExternalCleanupToBool, err := strconv.ParseBool(denyExternalCleanup)
	if err != nil {
		return nil, fmt.Errorf("not able to convert string denyExternalCleanup to Bool for Service %s", service.GetName())
	}

	// Request Object
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

	// Post new requests and return succeeded requests

	var succeededRequests []string
	var failedRequests error

	for _, addr := range prefixes {
		requestAddrObject.Address = addr
		if strings.Contains(addr, ".") {
			requestAddrObject.IpFamily = IPv4Family
		} else {
			requestAddrObject.IpFamily = IPv6Family
		}
		_, err := DeleteIP(requestAddrObject)
		if err != nil {
			failedRequests = err
		} else {
			succeededRequests = append(succeededRequests, addr)
		}
	}

	return succeededRequests, failedRequests
}

func UpdateMultiplePrefixes(v client.Client, oldService *corev1.Service, newService *corev1.Service, prefixes []string) ([]string, error) {

	// Initialize err
	var err error

	// Get OS Environment Variable for IPAM API
	envVar := IPAMApiUrl
	ipamApiUrl := os.Getenv(envVar)
	if ipamApiUrl == "" {
		return nil, fmt.Errorf("environment variable %s was not found", envVar)
	}

	// Get kube-system namespace uid for cluster identification
	getClusterNamespace := &corev1.Namespace{}
	if err := v.Get(context.TODO(), types.NamespacedName{Name: "kube-system"}, getClusterNamespace); err != nil {
		return nil, fmt.Errorf("failed to get kube-system namespace: %v", err)
	}
	clusterId := getClusterNamespace.GetUID()

	// Get namespace uid for Service namespace identification
	getNamespace := &corev1.Namespace{}
	if err := v.Get(context.TODO(), types.NamespacedName{Name: newService.Namespace}, getNamespace); err != nil {
		return nil, fmt.Errorf("failed to get service namespace: %v", err)
	}
	namespaceId := getNamespace.GetUID()

	// Get Service annotations
	oldAnnotations := oldService.GetAnnotations()
	newAnnotations := newService.GetAnnotations()

	// Get Old Secret
	var oldSecret *corev1.Secret
	if oldAnnotations["ipam.vitistack.io/secret"] == "default" {
		oldSecret, err = GetDefaultSecret(v)
		if err != nil {
			return nil, fmt.Errorf("failed to get default secret: %v", err)
		}
	} else {
		oldSecret, err = GetCustomSecret(v, oldService.Namespace, oldAnnotations)
		if err != nil {
			return nil, fmt.Errorf("failed to get custom secret: %v", err)
		}
	}

	// Get New Secret
	var newSecret *corev1.Secret
	if newAnnotations["ipam.vitistack.io/secret"] == "default" {
		newSecret, err = GetDefaultSecret(v)
		if err != nil {
			return nil, fmt.Errorf("failed to get default secret: %v", err)
		}
	} else {
		newSecret, err = GetCustomSecret(v, newService.Namespace, newAnnotations)
		if err != nil {
			return nil, fmt.Errorf("failed to get custom secret: %v", err)
		}
	}

	// Convert annotations before request
	retentionPeriodDays := newAnnotations["ipam.vitistack.io/retention-period-days"]
	retentionPeriodDaysToInt, err := strconv.Atoi(retentionPeriodDays)
	if err != nil {
		return nil, fmt.Errorf("not able to convert byte retentionPeriodDays to Integer")
	}

	denyExternalCleanup := newAnnotations["ipam.vitistack.io/deny-external-cleanup"]
	denyExternalCleanupToBool, err := strconv.ParseBool(denyExternalCleanup)
	if err != nil {
		return nil, fmt.Errorf("not able to convert string denyExternalCleanup to Bool for Service %s", newService.GetName())
	}

	// Request Object
	requestAddrObject := apicontracts.IpamApiRequest{
		Secret:    string(oldSecret.Data["secret"]),
		NewSecret: string(newSecret.Data["secret"]),
		Zone:      newAnnotations["ipam.vitistack.io/zone"],
		IpFamily:  newAnnotations["ipam.vitistack.io/ip-family"],
		Service: apicontracts.Service{
			ServiceName:         newService.GetName(),
			NamespaceId:         string(namespaceId),
			ClusterId:           string(clusterId),
			RetentionPeriodDays: retentionPeriodDaysToInt,
			DenyExternalCleanup: denyExternalCleanupToBool,
		},
	}

	// Post new requests and return succeeded requests

	var succeededRequests []string
	var failedRequests error

	for _, addr := range prefixes {
		requestAddrObject.Address = addr
		_, err := RequestIP(requestAddrObject)
		if err != nil {
			failedRequests = err
		} else {
			succeededRequests = append(succeededRequests, addr)
		}
	}

	return succeededRequests, failedRequests
}

func ValidIpAddressFamiliy(annotations map[string]string) error {

	validIpFamilies := []string{IPv4Family, IPv6Family, DualFamily}

	if !slices.Contains(validIpFamilies, annotations["ipam.vitistack.io/ip-family"]) {
		return fmt.Errorf("illegial specified ip-address family: %v , Valid Choices: ipv4,ipv6 & dual", annotations["ipam.vitistack.io/ip-family"])
	}

	return nil
}
