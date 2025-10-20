package v1

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getClusterID(c client.Client) (string, error) {
	getClusterNamespace := &corev1.Namespace{}
	if err := c.Get(context.TODO(), types.NamespacedName{Name: "kube-system"}, getClusterNamespace); err != nil {
		return "", fmt.Errorf("failed to get kube-system namespace: %w", err)
	}
	return string(getClusterNamespace.GetUID()), nil
}

func getNameSpaceID(c client.Client, service *corev1.Service) (string, error) {
	getNamespace := &corev1.Namespace{}
	if err := c.Get(context.TODO(), types.NamespacedName{Name: service.Namespace}, getNamespace); err != nil {
		return "", fmt.Errorf("failed to get service namespace: %w", err)
	}
	return string(getNamespace.GetUID()), nil
}
