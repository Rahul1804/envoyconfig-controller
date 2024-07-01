package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	meshv1alpha1 "github.com/example/envoy-controller/api/v1alpha1"
)

const envoyConfigFinalizer = "finalizer.mesh.example.com"

type EnvoyConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=mesh.example.com,resources=envoyconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mesh.example.com,resources=envoyconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mesh.example.com,resources=envoyconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *EnvoyConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the EnvoyConfig instance
	envoyConfig := &meshv1alpha1.EnvoyConfig{}
	err := r.Get(ctx, req.NamespacedName, envoyConfig)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return. Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "unable to fetch EnvoyConfig")
		return ctrl.Result{}, err
	}

	// Check if the EnvoyConfig instance is marked for deletion
	if envoyConfig.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so add the finalizer if it doesn't exist
		if !containsString(envoyConfig.ObjectMeta.Finalizers, envoyConfigFinalizer) {
			envoyConfig.ObjectMeta.Finalizers = append(envoyConfig.ObjectMeta.Finalizers, envoyConfigFinalizer)
			if err := r.Update(ctx, envoyConfig); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if containsString(envoyConfig.ObjectMeta.Finalizers, envoyConfigFinalizer) {
			// Handle any cleanup logic here
			if err := r.cleanupResources(ctx, envoyConfig); err != nil {
				return ctrl.Result{}, err
			}

			// Remove the finalizer and update the object
			envoyConfig.ObjectMeta.Finalizers = removeString(envoyConfig.ObjectMeta.Finalizers, envoyConfigFinalizer)
			if err := r.Update(ctx, envoyConfig); err != nil {
				return ctrl.Result{}, err
			}
		}
		// Stop reconciliation as the object is being deleted
		return ctrl.Result{}, nil
	}

	// Define your Envoy proxy configuration logic here
	configMapName := types.NamespacedName{
		Name:      envoyConfig.Name + "-config",
		Namespace: envoyConfig.Namespace,
	}

	configMap := &corev1.ConfigMap{}
	err = r.Get(ctx, configMapName, configMap)
	if err != nil && errors.IsNotFound(err) {
		// Define a new ConfigMap
		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName.Name,
				Namespace: configMapName.Namespace,
			},
			Data: map[string]string{
				"envoy.yaml": envoyConfig.Spec.Config,
			},
		}
		// Set EnvoyConfig instance as the owner and controller
		if err := controllerutil.SetControllerReference(envoyConfig, configMap, r.Scheme); err != nil {
			log.Error(err, "unable to set controller reference for ConfigMap")
			return ctrl.Result{}, err
		}
		err = r.Create(ctx, configMap)
		if err != nil {
			log.Error(err, "unable to create ConfigMap")
			return ctrl.Result{}, err
		}
		log.Info("ConfigMap created", "name", configMapName.Name, "namespace", configMapName.Namespace)
	} else if err == nil {
		// Update the existing ConfigMap
		configMap.Data["envoy.yaml"] = envoyConfig.Spec.Config
		err = r.Update(ctx, configMap)
		if err != nil {
			log.Error(err, "unable to update ConfigMap")
			return ctrl.Result{}, err
		}
		log.Info("ConfigMap updated", "name", configMapName.Name, "namespace", configMapName.Namespace)
	} else {
		log.Error(err, "unable to fetch ConfigMap")
		return ctrl.Result{}, err
	}

	// Update the status if necessary
	envoyConfig.Status.Applied = true
	err = r.Status().Update(ctx, envoyConfig)
	if err != nil {
		log.Error(err, "unable to update EnvoyConfig status")
		return ctrl.Result{}, err
	}

	log.Info("EnvoyConfig reconciled", "name", req.NamespacedName)
	return ctrl.Result{}, nil
}

func (r *EnvoyConfigReconciler) cleanupResources(ctx context.Context, envoyConfig *meshv1alpha1.EnvoyConfig) error {
	configMapName := types.NamespacedName{
		Name:      envoyConfig.Name + "-config",
		Namespace: envoyConfig.Namespace,
	}
	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, configMapName, configMap)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get ConfigMap for cleanup: %w", err)
	}

	if err == nil {
		err = r.Delete(ctx, configMap)
		if err != nil {
			return fmt.Errorf("failed to delete ConfigMap: %w", err)
		}
	}

	return nil
}

func (r *EnvoyConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&meshv1alpha1.EnvoyConfig{}).
		Complete(r)
}

func containsString(slice []string, str string) bool {
	for _, item := range slice {
		if item == str {
			return true
		}
	}
	return false
}

func removeString(slice []string, str string) []string {
	var result []string
	for _, item := range slice {
		if item != str {
			result = append(result, item)
		}
	}
	return result
}
