package v1

import (
	"fmt"

	utils "github.com/vitistack/ipam-operator/internal/utils"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getSecret(annotations map[string]string, service *corev1.Service, c client.Client) (*corev1.Secret, error) {
	var secret *corev1.Secret
	var err error

	if annotations["ipam.vitistack.io/secret"] == "default" {
		secret, err = utils.GetDefaultSecret(c)
		if err != nil {
			return nil, fmt.Errorf("failed to get default secret. Error: %w", err)
		}
	} else {
		secret, err = utils.GetCustomSecret(c, service.Namespace, annotations)
		if err != nil {
			return nil, fmt.Errorf("failed to get custom secret. Error: %w", err)
		}
	}

	return secret, nil
}
