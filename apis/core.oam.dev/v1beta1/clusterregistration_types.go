/*
Copyright 2021 The KubeVela Authors.

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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/oam-dev/kubevela/apis/core.oam.dev/condition"
)

// ClusterRegistrationSpec defines the desired state of ClusterRegistration
type ClusterRegistrationSpec struct {
	// ClusterName is the name of the cluster to register
	// If empty, defaults to metadata.name
	// +optional
	ClusterName string `json:"clusterName,omitempty"`

	// Alias is a human-readable name for the cluster
	// +optional
	Alias string `json:"alias,omitempty"`

	// Kubeconfig is the raw kubeconfig content for the cluster to be joined
	// This is a simplified POC approach where we just paste the kubeconfig directly
	// +required
	Kubeconfig string `json:"kubeconfig"`

	// CreateNamespace specifies the namespace to create in the managed cluster
	// Defaults to "vela-system"
	// +optional
	CreateNamespace string `json:"createNamespace,omitempty"`

	// Labels are custom labels to add to the cluster
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// ClusterRegistrationPhase defines the phase of cluster registration
type ClusterRegistrationPhase string

const (
	// ClusterRegistrationPhasePending means the registration is pending
	ClusterRegistrationPhasePending ClusterRegistrationPhase = "Pending"
	// ClusterRegistrationPhaseProgressing means the registration is in progress
	ClusterRegistrationPhaseProgressing ClusterRegistrationPhase = "Progressing"
	// ClusterRegistrationPhaseReady means the cluster is successfully registered
	ClusterRegistrationPhaseReady ClusterRegistrationPhase = "Ready"
	// ClusterRegistrationPhaseFailed means the registration failed
	ClusterRegistrationPhaseFailed ClusterRegistrationPhase = "Failed"
)

// ClusterInfo contains information about the registered cluster
type ClusterInfo struct {
	// Endpoint is the API server endpoint
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// CredentialType is the type of credential used
	// +optional
	CredentialType string `json:"credentialType,omitempty"`

	// Version is the Kubernetes version of the cluster
	// +optional
	Version string `json:"version,omitempty"`
}

// ClusterRegistrationStatus defines the observed state of ClusterRegistration
type ClusterRegistrationStatus struct {
	// Phase represents the current phase of cluster registration
	// +optional
	Phase ClusterRegistrationPhase `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the cluster registration state
	// +optional
	Conditions []condition.Condition `json:"conditions,omitempty"`

	// ClusterInfo contains information about the registered cluster
	// +optional
	ClusterInfo *ClusterInfo `json:"clusterInfo,omitempty"`

	// Message provides additional information about the current state
	// +optional
	Message string `json:"message,omitempty"`

	// ObservedGeneration is the most recent generation observed for this ClusterRegistration
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastReconcileTime is the last time the cluster registration was reconciled
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`
}

// ClusterRegistration is the Schema for the clusterregistrations API
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={oam},shortName=clusterreg
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="CLUSTER",type=string,JSONPath=`.spec.clusterName`
// +kubebuilder:printcolumn:name="PHASE",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="ENDPOINT",type=string,JSONPath=`.status.clusterInfo.endpoint`
// +kubebuilder:printcolumn:name="AGE",type=date,JSONPath=`.metadata.creationTimestamp`
type ClusterRegistration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterRegistrationSpec   `json:"spec,omitempty"`
	Status ClusterRegistrationStatus `json:"status,omitempty"`
}

// ClusterRegistrationList contains a list of ClusterRegistration
// +kubebuilder:object:root=true
type ClusterRegistrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterRegistration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterRegistration{}, &ClusterRegistrationList{})
}
