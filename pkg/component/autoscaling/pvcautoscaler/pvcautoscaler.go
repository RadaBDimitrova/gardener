// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package pvcautoscaler

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/component"
	"github.com/gardener/gardener/pkg/resourcemanager/controller/garbagecollector/references"
	"github.com/gardener/gardener/pkg/utils"
	gardenerutils "github.com/gardener/gardener/pkg/utils/gardener"
	kubernetesutils "github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	"github.com/go-logr/logr"
)

const (
	// PVCAutoscalerManagedResourceNameName is the name of the PVCAutoscaler managed resource.
	PVCAutoscalerManagedResourceName = name

	name               = "pvc-autoscaler"
	serviceAccountName = name
	roleName           = name
	clusterRoleName    = name
)

// Values keeps values for the PVCAutoscaler.
type Values struct {
	// Image is the image of the PVCAutoscaler.
	Image string
	// PriorityClassName is the name of the priority class of the PVCAutoscaler.
	PriorityClassName string
}

type pvcautoscaler struct {
	client    client.Client
	namespace string
	values    Values
	log       logr.Logger
}

// NewPVCAutoscaler creates a new instance of PVCAutoscaler.
func NewPVCAutoscaler(
	client client.Client,
	namespace string,
	values Values,
	log logr.Logger,
) component.DeployWaiter {
	return &pvcautoscaler{
		client:    client,
		namespace: namespace,
		values:    values,
		log:       log,
	}
}

func (p *pvcautoscaler) Deploy(ctx context.Context) error {
	var (
		registry            = managedresources.NewRegistry(kubernetes.SeedScheme, kubernetes.SeedCodec, kubernetes.SeedSerializer)
		serviceAccount      = p.serviceAccount()
		clusterRoles        = p.clusterRoles()
		clusterRoleBindings = p.clusterRoleBindings()
		role                = p.role()
		roleBinding         = p.roleBinding()
		service             = p.service()
		deployment          = p.deployment()
	)

	utilruntime.Must(references.InjectAnnotations(deployment))

	resources := []client.Object{
		serviceAccount,
		role,
		roleBinding,
		service,
		deployment,
	}

	for _, cr := range clusterRoles {
		resources = append(resources, cr)
	}

	for _, crb := range clusterRoleBindings {
		resources = append(resources, crb)
	}

	serializedResources, err := registry.AddAllAndSerialize(resources...)
	if err != nil {
		return err
	}

	return managedresources.CreateForSeed(ctx, p.client, p.namespace, PVCAutoscalerManagedResourceName, false, serializedResources)
}

func (p *pvcautoscaler) Destroy(ctx context.Context) error {
	managedResourceList := &resourcesv1alpha1.ManagedResourceList{}
	if err := p.client.List(ctx, managedResourceList,
		client.MatchingLabels{"test-delete": "true"}); err == nil {
		for _, mr := range managedResourceList.Items {
			p.log.Info("Deleting ManagedResource", "name", mr.Name, "namespace", mr.Namespace)
			if err := p.client.Delete(ctx, &mr); err != nil {
				return fmt.Errorf("failed to delete ManagedResource %s/%s: %w", mr.Namespace, mr.Name, err)
			}
		}
		p.log.Info("All ManagedResources deleted")
		if err := managedresources.WaitUntilListDeleted(ctx, p.client, managedResourceList, client.MatchingLabels{"test-delete": "true"}); err != nil {
			return fmt.Errorf("failed to wait for deletion of ManagedResources: %w", err)
		}
		p.log.Info("All ManagedResources deletion confirmed")
	}

	if err := p.client.Get(ctx, client.ObjectKey{Namespace: p.namespace, Name: PVCAutoscalerManagedResourceName}, &resourcesv1alpha1.ManagedResource{}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	statefulSetInfo := func(ctx context.Context, namespace string, name string) ([]string, *resource.Quantity, error) {
		var pvcNames []string
		statefulSet := &appsv1.StatefulSet{}
		if err := p.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, statefulSet); err != nil {
			return nil, nil, err
		}

		for _, pvc := range statefulSet.Spec.VolumeClaimTemplates {
			pvcNames = append(pvcNames, pvc.Name)
		}

		defaultStorage := statefulSet.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage()
		return pvcNames, defaultStorage, nil
	}

	observabilityManagedResourceList := &resourcesv1alpha1.ManagedResourceList{}
	if err := p.client.List(ctx, observabilityManagedResourceList,
		client.MatchingLabels{v1beta1constants.LabelWithPVCAutoscaler: v1beta1constants.PVCAutoscalerEnabled}); err != nil {
		return fmt.Errorf("failed to list ManagedResources: %w", err)
	}
	p.log.Info("Resizing data volumes if storage is less than default")
	for _, mr := range observabilityManagedResourceList.Items {
		shootPVCNames, defaultStorage, err := statefulSetInfo(ctx, mr.Namespace, mr.Name)
		if err != nil {
			return err
		}
		for _, pvcName := range shootPVCNames {
			p.log.Info("Resizing shoot data volume if storage is less than default", "namespace", mr.Namespace)
			kubernetesutils.ResizeOrDeleteDataVolumeIfStorageNotTheSame(ctx, p.client, &mr, pvcName, mr.Name, defaultStorage, true, p.log)
		}
	}

	return managedresources.DeleteForSeed(ctx, p.client, p.namespace, PVCAutoscalerManagedResourceName)
}

var timeoutWaitForManagedResources = 2 * time.Minute

func (p *pvcautoscaler) Wait(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeoutWaitForManagedResources)
	defer cancel()

	return managedresources.WaitUntilHealthy(timeoutCtx, p.client, p.namespace, PVCAutoscalerManagedResourceName)
}

func (p *pvcautoscaler) WaitCleanup(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeoutWaitForManagedResources)
	defer cancel()

	return managedresources.WaitUntilDeleted(timeoutCtx, p.client, p.namespace, PVCAutoscalerManagedResourceName)
}

func getLabels() map[string]string {
	return map[string]string{
		v1beta1constants.LabelApp: name,
	}
}

func (p *pvcautoscaler) serviceAccount() *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: p.namespace,
			Labels:    getLabels(),
		},
		AutomountServiceAccountToken: ptr.To(false),
	}
}

func (p *pvcautoscaler) clusterRoles() []*rbacv1.ClusterRole {
	return []*rbacv1.ClusterRole{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "pvc-autoscaler-autoscaling-persistentvolumeclaimautoscaler-editor-role",
				Labels: getLabels(),
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"autoscaling.gardener.cloud"},
					Resources: []string{"persistentvolumeclaimautoscalers"},
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
				},
				{
					APIGroups: []string{"autoscaling.gardener.cloud"},
					Resources: []string{"persistentvolumeclaimautoscalers/status"},
					Verbs:     []string{"get"},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "pvc-autoscaler-autoscaling-persistentvolumeclaimautoscaler-viewer-role",
				Labels: getLabels(),
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"autoscaling.gardener.cloud"},
					Resources: []string{"persistentvolumeclaimautoscalers"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"autoscaling.gardener.cloud"},
					Resources: []string{"persistentvolumeclaimautoscalers/status"},
					Verbs:     []string{"get"},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "pvc-autoscaler-manager-role",
				Labels: getLabels(),
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"events"},
					Verbs:     []string{"create", "patch"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"persistentvolumeclaims"},
					Verbs:     []string{"get", "list", "patch", "update", "watch"},
				},
				{
					APIGroups: []string{""},
					Resources: []string{"persistentvolumeclaims/status"},
					Verbs:     []string{"get"},
				},
				{
					APIGroups: []string{"autoscaling.gardener.cloud"},
					Resources: []string{"persistentvolumeclaimautoscalers"},
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
				},
				{
					APIGroups: []string{"autoscaling.gardener.cloud"},
					Resources: []string{"persistentvolumeclaimautoscalers/finalizers"},
					Verbs:     []string{"update"},
				},
				{
					APIGroups: []string{"autoscaling.gardener.cloud"},
					Resources: []string{"persistentvolumeclaimautoscalers/status"},
					Verbs:     []string{"get", "patch", "update"},
				},
				{
					APIGroups: []string{"storage.k8s.io"},
					Resources: []string{"storageclasses"},
					Verbs:     []string{"get", "list", "watch"},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "pvc-autoscaler-metrics-reader",
				Labels: getLabels(),
			},
			Rules: []rbacv1.PolicyRule{
				{
					NonResourceURLs: []string{"/metrics"},
					Verbs:           []string{"get"},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "pvc-autoscaler-proxy-role",
				Labels: getLabels(),
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"authentication.k8s.io"},
					Resources: []string{"tokenreviews"},
					Verbs:     []string{"create"},
				},
				{
					APIGroups: []string{"authorization.k8s.io"},
					Resources: []string{"subjectaccessreviews"},
					Verbs:     []string{"create"},
				},
			},
		},
	}
}

func (p *pvcautoscaler) clusterRoleBindings() []*rbacv1.ClusterRoleBinding {
	return []*rbacv1.ClusterRoleBinding{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "pvc-autoscaler-manager-rolebinding",
				Labels: getLabels(),
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     "pvc-autoscaler-manager-role",
			},
			Subjects: []rbacv1.Subject{{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      serviceAccountName,
				Namespace: p.namespace,
			}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "pvc-autoscaler-proxy-rolebinding",
				Labels: getLabels(),
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     "pvc-autoscaler-proxy-role",
			},
			Subjects: []rbacv1.Subject{{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      serviceAccountName,
				Namespace: p.namespace,
			}},
		},
	}
}

func (p *pvcautoscaler) role() *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-autoscaler-leader-election-role",
			Namespace: p.namespace,
			Labels:    getLabels(),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"create", "patch"},
			},
		},
	}
}

func (p *pvcautoscaler) roleBinding() *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-autoscaler-leader-election-rolebinding",
			Namespace: p.namespace,
			Labels:    getLabels(),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      serviceAccountName,
				Namespace: p.namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     "pvc-autoscaler-leader-election-role",
		},
	}
}

func (p *pvcautoscaler) service() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-autoscaler-controller-manager-metrics-service",
			Namespace: p.namespace,
			Labels: utils.MergeStringMaps(getLabels(), map[string]string{
				"control-plane": "controller-manager",
			}),
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Port:       8443,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromString("https"),
				},
			},
			Selector: map[string]string{
				"control-plane": "controller-manager",
			},
		},
	}
}

func (p *pvcautoscaler) deployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      v1beta1constants.DeploymentNamePVCAutoscaler,
			Namespace: p.namespace,
			Labels: utils.MergeStringMaps(getLabels(), map[string]string{
				resourcesv1alpha1.HighAvailabilityConfigType: resourcesv1alpha1.HighAvailabilityConfigTypeController,
				"control-plane": "controller-manager",
			}),
		},
		Spec: appsv1.DeploymentSpec{
			RevisionHistoryLimit: ptr.To[int32](2),
			Replicas:             ptr.To[int32](1),
			Selector: &metav1.LabelSelector{
				MatchLabels: utils.MergeStringMaps(getLabels(), map[string]string{
					"control-plane": "controller-manager",
				}),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: utils.MergeStringMaps(getLabels(), map[string]string{
						v1beta1constants.LabelNetworkPolicyToDNS:                   v1beta1constants.LabelNetworkPolicyAllowed,
						v1beta1constants.LabelNetworkPolicyToRuntimeAPIServer:      v1beta1constants.LabelNetworkPolicyAllowed,
						gardenerutils.NetworkPolicyLabel("prometheus-cache", 9090): v1beta1constants.LabelNetworkPolicyAllowed,
						"control-plane": "controller-manager",
					}),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: serviceAccountName,
					PriorityClassName:  p.values.PriorityClassName,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						RunAsUser:    ptr.To[int64](65532),
						RunAsGroup:   ptr.To[int64](65532),
						FSGroup:      ptr.To[int64](65532),
					},
					Containers: []corev1.Container{
						{
							Name:            name,
							Image:           p.values.Image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Args: []string{
								"--health-probe-bind-address=:8081",
								"--metrics-bind-address=127.0.0.1:8080",
								"--leader-elect",
								"--interval=30s",
								"--prometheus-address=http://prometheus-cache.garden.svc.cluster.local:80",
							},
							Env: []corev1.EnvVar{
								{
									Name:  "ENABLE_WEBHOOKS",
									Value: "false",
								},
								{
									Name: "NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "metadata.namespace",
										},
									},
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
							},
						},
					},
				},
			},
		},
	}
}
