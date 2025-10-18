package v1

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateMetallbOperator checks if the Metallb controller is running
func validateMetallbOperator(ctx context.Context, c client.Client) error {
	var podList corev1.PodList
	podSelector := client.MatchingLabels{"app": "metallb", "component": "controller"}

	if err := c.List(ctx, &podList, client.InNamespace("metallb-system"), podSelector); err != nil {
		return fmt.Errorf("failed to list Pods: %w", err)
	}

	var podRunning bool
	if len(podList.Items) > 0 {
		for _, pod := range podList.Items {
			if pod.Status.Phase == corev1.PodRunning {
				for _, condition := range pod.Status.Conditions {
					if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
						podRunning = true
						break
					}
				}
				if podRunning {
					break
				}
			}
		}
	}

	if !podRunning {
		return fmt.Errorf("metallb operator is not available. Please make sure Metallb is installed and running")
	}

	return nil
}
