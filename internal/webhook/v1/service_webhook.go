/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// nolint:unused
// log is for logging in this package.
var servicelog = logf.Log.WithName("ipam-operator")

// SetupServiceWebhookWithManager registers the webhook for Service in the manager.
func SetupServiceWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1.Service{}).
		WithValidator(&ServiceCustomValidator{Client: mgr.GetClient()}).
		WithDefaulter(&ServiceCustomDefaulter{Client: mgr.GetClient()}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate--v1-service,mutating=true,failurePolicy=Ignore,sideEffects=NoneOnDryRun,groups="",resources=services,verbs=create;update,versions=v1,name=mservice-v1.kb.io,admissionReviewVersions=v1

// ServiceCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Service when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.

type ServiceCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
	Client client.Client
}

var _ webhook.CustomDefaulter = &ServiceCustomDefaulter{}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate--v1-service,mutating=false,failurePolicy=Ignore,sideEffects=None,groups="",resources=services,verbs=create;update;delete,versions=v1,name=vservice-v1.kb.io,admissionReviewVersions=v1

// ServiceCustomValidator struct is responsible for validating the Service resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ServiceCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
	Client client.Client
}

var _ webhook.CustomValidator = &ServiceCustomValidator{}
