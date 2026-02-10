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

package clusterregistration

import (
	"context"
	"os"
	"time"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/oam-dev/kubevela/apis/core.oam.dev/condition"
	"github.com/oam-dev/kubevela/apis/core.oam.dev/v1beta1"
	"github.com/oam-dev/kubevela/pkg/multicluster"
)

const (
	clusterRegistrationFinalizer = "clusterregistration.core.oam.dev/finalizer"
)

// Reconciler reconciles a ClusterRegistration object
type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile implements the reconciliation logic for ClusterRegistration
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.InfoS("Reconciling ClusterRegistration", "name", req.Name, "namespace", req.Namespace)

	// Fetch the ClusterRegistration instance
	clusterReg := &v1beta1.ClusterRegistration{}
	if err := r.Get(ctx, req.NamespacedName, clusterReg); err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("ClusterRegistration not found, may have been deleted", "name", req.Name)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get ClusterRegistration", "name", req.Name)
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !clusterReg.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, clusterReg)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(clusterReg, clusterRegistrationFinalizer) {
		controllerutil.AddFinalizer(clusterReg, clusterRegistrationFinalizer)
		if err := r.Update(ctx, clusterReg); err != nil {
			klog.ErrorS(err, "Failed to add finalizer", "name", clusterReg.Name)
			return ctrl.Result{}, err
		}
		// Requeue to continue with registration
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if already successfully registered and no spec changes
	// This prevents infinite reconciliation loops
	if clusterReg.Status.Phase == v1beta1.ClusterRegistrationPhaseReady &&
		clusterReg.Status.ObservedGeneration == clusterReg.Generation {
		klog.V(4).InfoS("ClusterRegistration already in Ready state with no changes, skipping",
			"name", clusterReg.Name, "generation", clusterReg.Generation)
		return ctrl.Result{}, nil
	}

	// Get cluster name (default to metadata.name if not specified)
	clusterName := clusterReg.Spec.ClusterName
	if clusterName == "" {
		clusterName = clusterReg.Name
	}

	// Validate cluster name
	if clusterName == multicluster.ClusterLocalName {
		err := errors.New("cluster name cannot be 'local', it is reserved")
		r.updateFailedStatus(ctx, clusterReg, err.Error())
		return ctrl.Result{}, nil // Don't return error to avoid requeue
	}

	// Update status to Progressing only if not already progressing
	if clusterReg.Status.Phase != v1beta1.ClusterRegistrationPhaseProgressing &&
		clusterReg.Status.Phase != v1beta1.ClusterRegistrationPhaseReady {
		clusterReg.Status.Phase = v1beta1.ClusterRegistrationPhaseProgressing
		clusterReg.Status.Message = "Registering cluster..."
		if err := r.Status().Update(ctx, clusterReg); err != nil {
			klog.ErrorS(err, "Failed to update status to Progressing", "name", clusterReg.Name)
			return ctrl.Result{}, err
		}
		// Return here to let the status update trigger the next reconcile
		return ctrl.Result{}, nil
	}

	// Write kubeconfig to a temporary file
	tmpfile, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		klog.ErrorS(err, "Failed to create temporary kubeconfig file", "name", clusterReg.Name)
		r.updateFailedStatus(ctx, clusterReg, "Failed to create temporary file: "+err.Error())
		return ctrl.Result{}, nil
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(clusterReg.Spec.Kubeconfig)); err != nil {
		klog.ErrorS(err, "Failed to write kubeconfig to temporary file", "name", clusterReg.Name)
		r.updateFailedStatus(ctx, clusterReg, "Failed to write kubeconfig: "+err.Error())
		return ctrl.Result{}, nil
	}
	tmpfile.Close()

	// Load and validate the kubeconfig
	clusterConfig, err := multicluster.LoadKubeClusterConfigFromFile(tmpfile.Name())
	if err != nil {
		klog.ErrorS(err, "Failed to load kubeconfig", "name", clusterReg.Name)
		r.updateFailedStatus(ctx, clusterReg, "Failed to load kubeconfig: "+err.Error())
		return ctrl.Result{}, nil
	}

	// Set create namespace (default to vela-system)
	createNamespace := clusterReg.Spec.CreateNamespace
	if createNamespace == "" {
		createNamespace = "vela-system"
	}

	// Configure cluster
	if err := clusterConfig.SetClusterName(clusterName).SetCreateNamespace(createNamespace).Validate(); err != nil {
		klog.ErrorS(err, "Cluster configuration validation failed", "name", clusterReg.Name)
		r.updateFailedStatus(ctx, clusterReg, "Validation failed: "+err.Error())
		return ctrl.Result{}, nil
	}

	// Register the cluster by creating/updating the secret
	// RegisterByVelaSecret is idempotent - it creates or updates the secret
	if err := clusterConfig.RegisterByVelaSecret(ctx, r.Client); err != nil {
		klog.ErrorS(err, "Failed to register cluster", "name", clusterReg.Name, "clusterName", clusterName)
		r.updateFailedStatus(ctx, clusterReg, "Failed to register cluster: "+err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	klog.InfoS("Successfully registered cluster", "name", clusterReg.Name, "clusterName", clusterName, "endpoint", clusterConfig.Cluster.Server)

	// Set cluster info
	credentialType := "X509Certificate"
	if len(clusterConfig.AuthInfo.Token) > 0 || clusterConfig.AuthInfo.Exec != nil {
		credentialType = "ServiceAccountToken"
	}

	// Update status to Ready
	clusterReg.Status.Phase = v1beta1.ClusterRegistrationPhaseReady
	clusterReg.Status.Message = "Cluster successfully registered"
	clusterReg.Status.ObservedGeneration = clusterReg.Generation
	now := metav1.Now()
	clusterReg.Status.LastReconcileTime = &now
	clusterReg.Status.ClusterInfo = &v1beta1.ClusterInfo{
		Endpoint:       clusterConfig.Cluster.Server,
		CredentialType: credentialType,
	}

	// Set condition (replace, don't append to avoid growing list)
	readyCondition := condition.Available()
	readyCondition.Message = "Cluster successfully registered and ready"
	clusterReg.Status.Conditions = []condition.Condition{readyCondition}

	if err := r.Status().Update(ctx, clusterReg); err != nil {
		klog.ErrorS(err, "Failed to update status to Ready", "name", clusterReg.Name)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) handleDeletion(ctx context.Context, clusterReg *v1beta1.ClusterRegistration) (ctrl.Result, error) {
	klog.InfoS("Handling deletion of ClusterRegistration", "name", clusterReg.Name)

	if controllerutil.ContainsFinalizer(clusterReg, clusterRegistrationFinalizer) {
		// Get cluster name
		clusterName := clusterReg.Spec.ClusterName
		if clusterName == "" {
			clusterName = clusterReg.Name
		}

		// Delete the cluster secret
		klog.InfoS("Detaching cluster", "name", clusterReg.Name, "clusterName", clusterName)
		if err := multicluster.DetachCluster(ctx, r.Client, clusterName); err != nil && !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to detach cluster", "name", clusterReg.Name, "clusterName", clusterName)
			return ctrl.Result{}, err
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(clusterReg, clusterRegistrationFinalizer)
		if err := r.Update(ctx, clusterReg); err != nil {
			klog.ErrorS(err, "Failed to remove finalizer", "name", clusterReg.Name)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) updateFailedStatus(ctx context.Context, clusterReg *v1beta1.ClusterRegistration, message string) {
	clusterReg.Status.Phase = v1beta1.ClusterRegistrationPhaseFailed
	clusterReg.Status.Message = message
	clusterReg.Status.ObservedGeneration = clusterReg.Generation
	now := metav1.Now()
	clusterReg.Status.LastReconcileTime = &now

	failedCondition := condition.Unavailable()
	failedCondition.Message = message
	// Replace conditions instead of appending to prevent infinite growth
	clusterReg.Status.Conditions = []condition.Condition{failedCondition}

	if err := r.Status().Update(ctx, clusterReg); err != nil {
		klog.ErrorS(err, "Failed to update failed status", "name", clusterReg.Name)
	}
}

// SetupWithManager sets up the controller with the Manager
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.ClusterRegistration{}).
		Complete(r)
}

// Setup adds a controller that reconciles ClusterRegistration
func Setup(mgr ctrl.Manager, args any) error {
	reconciler := &Reconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	return reconciler.SetupWithManager(mgr)
}
