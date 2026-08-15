package resources

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

// Shared PodSpec helpers used by workload builders across this package.

// CronJob defaults shared by all operator-managed CronJobs (Kruize partitions,
// ROS partition cleaner, RBAC Keycloak sync). Centralised so scheduling and
// retry behaviour stays consistent and future tuning only touches one place.
var (
	CronJobConcurrencyForbid       = batchv1.ForbidConcurrent
	CronJobRestartOnFailure        = corev1.RestartPolicyOnFailure
	CronJobActiveDeadlineSeconds   = int64(300)
	CronJobBackoffLimit            = int32(3)
	CronJobSuccessHistoryLimit     = int32(3)
	CronJobFailedHistoryLimit      = int32(3)
	CronJobStartingDeadlineSeconds = int64(900)
)

func nonRootPodSC() *corev1.PodSecurityContext {
	nonRoot := true
	return &corev1.PodSecurityContext{
		RunAsNonRoot: &nonRoot,
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func pullPolicy(cfg *costv1alpha1.CostManagementServiceConfig) corev1.PullPolicy {
	if cfg.Spec.Global.PullPolicy != "" {
		return cfg.Spec.Global.PullPolicy
	}
	return corev1.PullIfNotPresent
}

// imagePullSecrets returns global.imagePullSecrets for Pod specs so private
// registry credentials (e.g. registry.redhat.io pull secrets) are applied to
// every workload the operator creates.
func imagePullSecrets(cfg *costv1alpha1.CostManagementServiceConfig) []corev1.LocalObjectReference {
	return cfg.Spec.Global.ImagePullSecrets
}
