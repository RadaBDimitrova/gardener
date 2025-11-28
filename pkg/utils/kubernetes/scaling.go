// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"fmt"
	"time"

	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gardener/gardener/pkg/utils/retry"
)

// ScaleStatefulSet scales a StatefulSet.
func ScaleStatefulSet(ctx context.Context, c client.Client, key client.ObjectKey, replicas int32) error {
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
	}

	return scaleResource(ctx, c, statefulset, replicas)
}

// ScaleDeployment scales a Deployment.
func ScaleDeployment(ctx context.Context, c client.Client, key client.ObjectKey, replicas int32) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
	}

	return scaleResource(ctx, c, deployment, replicas)
}

// scaleResource scales resource's 'spec.replicas' to replicas count
func scaleResource(ctx context.Context, c client.Client, obj client.Object, replicas int32) error {
	patch := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas))
	return c.SubResource("scale").Patch(ctx, obj, client.RawPatch(types.MergePatchType, patch))
}

// WaitUntilDeploymentScaledToDesiredReplicas waits for the number of available replicas to be equal to the deployment's desired replicas count.
func WaitUntilDeploymentScaledToDesiredReplicas(ctx context.Context, client client.Client, key types.NamespacedName, desiredReplicas int32) error {
	return retry.UntilTimeout(ctx, 5*time.Second, 300*time.Second, func(ctx context.Context) (done bool, err error) {
		deployment := &appsv1.Deployment{}
		if err := client.Get(ctx, key, deployment); err != nil {
			return retry.SevereError(err)
		}

		if deployment.Generation != deployment.Status.ObservedGeneration {
			return retry.MinorError(fmt.Errorf("%q not observed at latest generation (%d/%d)", key.Name,
				deployment.Status.ObservedGeneration, deployment.Generation))
		}

		if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != desiredReplicas {
			return retry.SevereError(fmt.Errorf("waiting for deployment %q to scale failed. spec.replicas does not match the desired replicas", key.Name))
		}

		if deployment.Status.Replicas == desiredReplicas && deployment.Status.AvailableReplicas == desiredReplicas {
			return retry.Ok()
		}

		return retry.MinorError(fmt.Errorf("deployment %q currently has '%d' replicas. Desired: %d", key.Name, deployment.Status.AvailableReplicas, desiredReplicas))
	})
}

// WaitUntilStatefulSetScaledToDesiredReplicas waits for the number of available replicas to be equal to the StatefulSet's desired replicas count.
func WaitUntilStatefulSetScaledToDesiredReplicas(ctx context.Context, client client.Client, key types.NamespacedName, desiredReplicas int32) error {
	return retry.UntilTimeout(ctx, 5*time.Second, 300*time.Second, func(ctx context.Context) (done bool, err error) {
		statefulSet := &appsv1.StatefulSet{}
		if err := client.Get(ctx, key, statefulSet); err != nil {
			return retry.SevereError(err)
		}

		if statefulSet.Generation != statefulSet.Status.ObservedGeneration {
			return retry.MinorError(fmt.Errorf("statefulSet %q not observed at latest generation (%d/%d)", key.Name,
				statefulSet.Status.ObservedGeneration, statefulSet.Generation))
		}

		if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != desiredReplicas {
			if statefulSet.Spec.Replicas == nil {
				return retry.SevereError(fmt.Errorf("waiting for statefulSet %q to scale failed. spec.replicas is nill. Generation %d", key.Name, statefulSet.Generation))
			}
			return retry.SevereError(fmt.Errorf("waiting for statefulSet %q to scale failed. spec.replicas does not match the desired replicas", key.Name))
		}

		if statefulSet.Status.Replicas == desiredReplicas && statefulSet.Status.AvailableReplicas == desiredReplicas {
			return retry.Ok()
		}

		return retry.MinorError(fmt.Errorf("statefulSet %q currently has '%d' replicas. Desired: %d", key.Name, statefulSet.Status.AvailableReplicas, desiredReplicas))
	})
}

// ScaleStatefulSetAndWaitUntilScaled scales a StatefulSet and wait until is scaled.
func ScaleStatefulSetAndWaitUntilScaled(ctx context.Context, c client.Client, key client.ObjectKey, replicas int32) error {
	if err := ScaleStatefulSet(ctx, c, key, replicas); err != nil {
		return err
	}
	return WaitUntilStatefulSetScaledToDesiredReplicas(ctx, c, key, replicas)
}

// ResizeOrDeleteDataVolumeIfStorageNotTheSame updates the Vali/Prometheus PVC if passed storage value is not the same as the
// current one.
// Caution: If the passed storage capacity is less than the current one the existing PVC and its PV will be deleted.
func ResizeOrDeleteDataVolumeIfStorageNotTheSame(ctx context.Context, c client.Client, managedResource *resourcesv1alpha1.ManagedResource, pvcName string, statefulSetName string, storage *resource.Quantity, pvcAutoscalerEnabled bool, log logr.Logger) error {
	addOrRemoveIgnoreAnnotationFromManagedResource := func(addIgnoreAnnotation bool) error {
		// In order to not create the managed resource here first check if exists.
		if err := c.Get(ctx, client.ObjectKeyFromObject(managedResource), managedResource); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
			return nil
		}
		patch := client.MergeFrom(managedResource.DeepCopy())

		if addIgnoreAnnotation {
			metav1.SetMetaDataAnnotation(&managedResource.ObjectMeta, resourcesv1alpha1.Ignore, "true")
		} else {
			delete(managedResource.Annotations, resourcesv1alpha1.Ignore)
		}
		return c.Patch(ctx, managedResource, patch)
	}

	pvc := &corev1.PersistentVolumeClaim{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: managedResource.Namespace, Name: pvcName}, pvc); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		return addOrRemoveIgnoreAnnotationFromManagedResource(false)
	}

	// Check if we need resizing
	storageCmpResult := storage.Cmp(*pvc.Spec.Resources.Requests.Storage())
	log.Info("current pvc storage", "pvc", pvcName, "namespace", managedResource.Namespace, "currentStorage", pvc.Spec.Resources.Requests.Storage().String(), "desiredStorage", storage.String(), "comparisonResult", storageCmpResult)
	if storageCmpResult == 0 {
		return addOrRemoveIgnoreAnnotationFromManagedResource(false)
	}

	// Annotate managed resource to skip reconciliation.
	if err := addOrRemoveIgnoreAnnotationFromManagedResource(true); err != nil {
		return err
	}

	if err := ScaleStatefulSetAndWaitUntilScaled(ctx, c, client.ObjectKey{Namespace: v1beta1constants.GardenNamespace, Name: statefulSetName}, 0); client.IgnoreNotFound(err) != nil {
		return err
	}

	log.Info("resizing pvc", "pvc", pvcName, "namespace", managedResource.Namespace, "storage", storage.String())
	switch {
	case storageCmpResult > 0:
		patch := client.MergeFrom(pvc.DeepCopy())
		pvc.Spec.Resources.Requests = corev1.ResourceList{corev1.ResourceStorage: *storage}
		if err := c.Patch(ctx, pvc, patch); client.IgnoreNotFound(err) != nil {
			return err
		}
		log.Info("patching", "pvc", pvc.Name, "namespace", pvc.Namespace, "storage", storage.String())
	case storageCmpResult < 0:
		// if pvc-autoscaler is enabled we don't delete the PVC if it is smaller
		if !pvcAutoscalerEnabled {
			if err := client.IgnoreNotFound(c.Delete(ctx, pvc)); err != nil {
				return err
			}
		}
	}

	return addOrRemoveIgnoreAnnotationFromManagedResource(false)
}
