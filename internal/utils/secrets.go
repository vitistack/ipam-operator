package utils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetDefaultSecret(d client.Client) (*corev1.Secret, error) {

	// Check if secret exists in the "ipam" namespace
	secret := &corev1.Secret{}
	retrieveSecret := d.Get(context.TODO(), types.NamespacedName{
		Name:      "default",
		Namespace: "ipam-system",
	}, secret)

	// If not, create a default secret
	if retrieveSecret != nil {

		// Create a new secret object

		randomSecret := GenerateRandomString(32)

		newSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default",
				Namespace: "ipam-system",
			},
			Data:      map[string][]byte{"secret": []byte(randomSecret)},
			Type:      corev1.SecretTypeOpaque,
			Immutable: func(b bool) *bool { return &b }(true),
		}

		// Create secret in the "ipam-system" namespace
		if err := d.Create(context.TODO(), newSecret); err != nil {
			return nil, fmt.Errorf("failed to create default secret")
		}
		return newSecret, nil
	}

	return secret, nil

}

func GetCustomSecret(d client.Client, namespace string, annotations map[string]string) (*corev1.Secret, error) {

	// Check if secret exists in the "ipam" namespace
	secret := &corev1.Secret{}
	retrieveSecret := d.Get(context.TODO(), types.NamespacedName{
		Name:      annotations["ipam.vitistack.io/secret"],
		Namespace: namespace,
	}, secret)

	if retrieveSecret != nil {
		return nil, fmt.Errorf("failed to get custom secret %s in namespace %s", annotations["ipam.vitistack.io/secret"], namespace)
	}

	_, exists := secret.Data["secret"]
	if !exists {
		return nil, fmt.Errorf("secret %s in namespace %s does not contain 'secret' key", secret.GetName(), secret.GetNamespace())
	}

	if secret.Type != corev1.SecretTypeOpaque {
		return nil, fmt.Errorf("secret %s in namespace %s is not of type Opaque", secret.GetName(), secret.GetNamespace())
	}

	return secret, nil

}
