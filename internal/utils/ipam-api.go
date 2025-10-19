package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"

	"strings"

	"github.com/vitistack/ipam-api/pkg/models/apicontracts"
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

func ValidIpAddressFamiliy(annotations map[string]string) error {

	validIpFamilies := []string{IPv4Family, IPv6Family, DualFamily}

	if !slices.Contains(validIpFamilies, annotations["ipam.vitistack.io/ip-family"]) {
		return fmt.Errorf("illegial specified ip-address family: %v , Valid Choices: ipv4,ipv6 & dual", annotations["ipam.vitistack.io/ip-family"])
	}

	return nil
}
