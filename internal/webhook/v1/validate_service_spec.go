package v1

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func validateServiceSpec(ctx context.Context, c client.Client, service *corev1.Service, operation string) error {

	// Validate the Service using dry-run to ensure it can be created/updated successfully

	if operation == "CREATE" {
		dryRunService := service.DeepCopy()
		if err := c.Create(ctx, dryRunService, &client.CreateOptions{
			DryRun: []string{metav1.DryRunAll},
		}); err != nil {
			return fmt.Errorf("failed to dry run Service creation: %w", err)
		}
	} else {
		dryRunService := service.DeepCopy()
		if err := c.Update(ctx, dryRunService, &client.UpdateOptions{
			DryRun: []string{metav1.DryRunAll},
		}); err != nil {
			return fmt.Errorf("failed to dry run Service update: %w", err)
		}
	}
	return nil
}
