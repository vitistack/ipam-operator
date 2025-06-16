package utils

import (
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metallbv1beta1 "go.universe.tf/metallb/api/v1beta1"
)

func AddIpAddressesToPool(d client.Client, annotations map[string]string, addresses []string) error {

	// Add prefixes to addresses if missing!
	for index, addr := range addresses {
		if strings.Contains(addr, ".") {
			if !strings.Contains(addr, "/32") {
				addresses[index] = addr + "/32"
			}
		}
		if strings.Contains(addr, ":") {
			if !strings.Contains(addr, "/128") {
				addresses[index] = addr + "/128"
			}
		}
	}

	// Check if IP Address-Pool exists
	ipAddressPool := &metallbv1beta1.IPAddressPool{}
	err := d.Get(context.TODO(), types.NamespacedName{
		Name:      annotations["ipam.vitistack.io/zone"],
		Namespace: "metallb-system",
	}, ipAddressPool)

	// CREATE if IP Address-Pool does not exists
	if err != nil {
		// Create IP Address Pool
		ipAddressPool.Name = annotations["ipam.vitistack.io/zone"]
		ipAddressPool.Namespace = "metallb-system"
		ipAddressPool.Spec.AvoidBuggyIPs = true
		autoAssign := false
		ipAddressPool.Spec.AutoAssign = &autoAssign
		ipAddressPool.Spec.Addresses = addresses
		err := d.Create(context.TODO(), ipAddressPool)
		if err != nil {
			return fmt.Errorf("failed to create IPAddressPool: %w", err)
		}
		return nil
	}

	// UPDATE if IP Address-Pool exists

	// Check if the addresses are already in the IPAddressPool
	// If the addresses are already in the pool, skip adding them
	unassignedAddresses := []string{}
	for _, addr := range addresses {
		if !slices.Contains(ipAddressPool.Spec.Addresses, addr) {
			unassignedAddresses = append(unassignedAddresses, addr)
		}
	}

	ipAddressPool.Spec.Addresses = append(ipAddressPool.Spec.Addresses, unassignedAddresses...)

	if err = d.Update(context.TODO(), ipAddressPool); err != nil {
		return fmt.Errorf("failed to update IPAddressPool: %w", err)
	}

	return nil
}

func GetIpAddressesToPool(d client.Client, annotations map[string]string) error {
	ipAddressPool := &metallbv1beta1.IPAddressPool{}
	err := d.Get(context.TODO(), types.NamespacedName{
		Name:      annotations["ipam.vitistack.io/zone"],
		Namespace: "metallb-system",
	}, ipAddressPool)

	if err != nil {
		return fmt.Errorf("failed to get IPAddressPool: %w", err)
	}

	return nil
}

func RemoveIPAddressesFromPool(d client.Client, annotations map[string]string, addresses []string) error {

	// Get the IPAddressPool by zone name from annotations
	ipAddressPool := &metallbv1beta1.IPAddressPool{}
	err := d.Get(context.TODO(), types.NamespacedName{
		Name:      annotations["ipam.vitistack.io/zone"],
		Namespace: "metallb-system",
	}, ipAddressPool)

	if err != nil {
		return fmt.Errorf("failed to get IPAddressPool: %s (error: %v)", annotations["ipam.vitistack.io/zone"], err)
	}

	// Check if the addresses is used by any Service
	allServices := &corev1.ServiceList{}
	err = d.List(context.TODO(), allServices, &client.ListOptions{})

	if err != nil {
		return fmt.Errorf("failed to list all Services: %v", err)
	}

	// Filter the services to only include those of type LoadBalancer
	filteredAllServices := []corev1.Service{}
	for _, svc := range allServices.Items {
		if svc.Spec.Type == "LoadBalancer" {
			filteredAllServices = append(filteredAllServices, svc)
		}
	}

	// Add all used Service IP addresses to a slice
	notInUseAddresses := []string{}
	for _, addr := range addresses {
		count := 0
		if strings.Contains(addr, "/32") {
			addr = addr[:len(addr)-3]
		}
		if strings.Contains(addr, "/128") {
			addr = addr[:len(addr)-4]
		}
		for _, svc := range filteredAllServices {
			for _, ingress := range svc.Status.LoadBalancer.Ingress {
				// Check if the address is in the Service Ingress addresses
				if ingress.IP == addr {
					count++
				}
			}
		}
		if count <= 1 {
			if strings.Contains(addr, ".") {
				addr = addr + "/32"
			}
			if strings.Contains(addr, ":") {
				addr = addr + "/128"
			}
			notInUseAddresses = append(notInUseAddresses, addr)
		}
	}

	// Create variable with IP address that should be removed!
	newIpAddressPool := removeAddressesHelper(ipAddressPool.Spec.Addresses, notInUseAddresses)

	// Delete IP-Address Pool if there is no more ip-addresses left in the pool!
	if len(newIpAddressPool) == 0 {
		if err = d.Delete(context.TODO(), ipAddressPool, &client.DeleteOptions{}); err != nil {
			return fmt.Errorf("failed to remove ip-address pool: %v", err)
		}
		return nil
	}

	// Update IP-address Pool otherwise!
	ipAddressPool.Spec.Addresses = newIpAddressPool

	if err = d.Update(context.TODO(), ipAddressPool); err != nil {
		return fmt.Errorf("failed to remove adresses from IPAddressPool: %v", err)
	}

	return nil

}

// removeAddressesHelper removes all occurrences of the given addresses from the src slice.
func removeAddressesHelper(src []string, toRemove []string) []string {
	removeMap := make(map[string]struct{}, len(toRemove))
	for _, addr := range toRemove {
		removeMap[addr] = struct{}{}
	}
	var result []string
	for _, addr := range src {
		if _, found := removeMap[addr]; !found {
			result = append(result, addr)
		}
	}
	return result
}
