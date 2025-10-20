package utils

import (
	"slices"
)

func PrefixSlicesDiff(oldServicePrefixes, newServicePrefixes []string) (newPrefixes, keepPrefixes, removePrefixes []string) {

	// Create a slice to hold addresses which should be added
	newPrefixes = []string{}
	for _, newAddr := range newServicePrefixes {
		if !slices.Contains(oldServicePrefixes, newAddr) {
			newPrefixes = append(newPrefixes, newAddr)
		}
	}

	// Create a slice to hold addresses which should be updated
	keepPrefixes = []string{}
	for _, newAddr := range newServicePrefixes {
		if slices.Contains(oldServicePrefixes, newAddr) {
			keepPrefixes = append(keepPrefixes, newAddr)
		}
	}

	// Create a slice to hold addresses which should be removed
	removePrefixes = []string{}
	for _, oldAddr := range oldServicePrefixes {
		if !slices.Contains(newServicePrefixes, oldAddr) {
			removePrefixes = append(removePrefixes, oldAddr)
		}
	}

	return newPrefixes, keepPrefixes, removePrefixes
}
