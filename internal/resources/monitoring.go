package resources

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

var (
	serviceMonitorGVK = schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"}
	prometheusRuleGVK = schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: "PrometheusRule"}
)

// serviceMonitor builds a ServiceMonitor that selects Services by component label.
func serviceMonitor(cfg *costv1alpha1.CostManagementServiceConfig, name, portName string, components []string) *unstructured.Unstructured {
	matchExpressions := make([]any, len(components))
	for i, c := range components {
		matchExpressions[i] = c
	}

	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(serviceMonitorGVK)
	sm.SetName(name)
	sm.SetNamespace(cfg.Namespace)
	sm.SetLabels(Labels(cfg, "monitoring"))

	_ = unstructured.SetNestedSlice(sm.Object, []any{
		map[string]any{
			"port":     portName,
			"path":     "/metrics",
			"interval": "30s",
		},
	}, "spec", "endpoints")
	_ = unstructured.SetNestedField(sm.Object, map[string]any{
		"matchLabels": map[string]any{
			"app.kubernetes.io/managed-by": "koku-service-operator",
			"app.kubernetes.io/instance":   cfg.Name,
		},
		"matchExpressions": []any{
			map[string]any{
				"key":      "app.kubernetes.io/component",
				"operator": "In",
				"values":   matchExpressions,
			},
		},
	}, "spec", "selector")
	// Target only the CR's own namespace.
	_ = unstructured.SetNestedSlice(sm.Object, []any{cfg.Namespace}, "spec", "namespaceSelector", "matchNames")

	return sm
}

// AppServiceMonitor watches all application services that expose metrics on port 9000.
func AppServiceMonitor(cfg *costv1alpha1.CostManagementServiceConfig) *unstructured.Unstructured {
	components := []string{"cost-management-api", "cost-processor", "listener", "ingress"}
	if costv1alpha1.ROSEnabled(cfg) {
		components = append(components, "ros-api")
	}
	return serviceMonitor(cfg, cfg.Name+"-app-metrics", "metrics", components)
}

// KruizeServiceMonitor watches Kruize which exposes metrics on port 8080.
func KruizeServiceMonitor(cfg *costv1alpha1.CostManagementServiceConfig) *unstructured.Unstructured {
	return serviceMonitor(cfg, cfg.Name+"-kruize-metrics", "metrics", []string{"ros-optimization"})
}

// OperatorServiceMonitor watches the controller-manager metrics endpoint.
// The manager service is in the operator's own namespace (not the CR namespace)
// so we create this monitor in the operator namespace and rely on the caller
// to apply it cluster-scoped or in the manager namespace.
func OperatorServiceMonitor(cfg *costv1alpha1.CostManagementServiceConfig) *unstructured.Unstructured {
	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(serviceMonitorGVK)
	sm.SetName(cfg.Name + "-operator-metrics")
	sm.SetNamespace(cfg.Namespace)
	sm.SetLabels(Labels(cfg, "monitoring"))

	_ = unstructured.SetNestedSlice(sm.Object, []any{
		map[string]any{
			"port":     "https",
			"path":     "/metrics",
			"interval": "30s",
			"scheme":   "https",
			"tlsConfig": map[string]any{
				"insecureSkipVerify": true,
			},
		},
	}, "spec", "endpoints")
	_ = unstructured.SetNestedStringMap(sm.Object, map[string]string{
		"control-plane": "controller-manager",
	}, "spec", "selector", "matchLabels")

	return sm
}

// PrometheusRules returns a PrometheusRule with the five alert rules for the
// cost management operator. The caller applies it in the CR's namespace.
func PrometheusRules(cfg *costv1alpha1.CostManagementServiceConfig) *unstructured.Unstructured {
	instance := cfg.Name
	ns := cfg.Namespace

	rules := []any{
		// 1 — migration stuck / failed
		map[string]any{
			"alert": "CostManagementMigrationFailed",
			"expr":  `kube_job_status_failed{namespace="` + ns + `",job_name=~"` + instance + `-(koku|ros|rbac)-migrate"} > 0`,
			"for":   "1m",
			"labels": map[string]any{
				"severity": "critical",
				"instance": instance,
			},
			"annotations": map[string]any{
				"summary":     "Cost Management migration job failed",
				"description": "Migration job {{ $labels.job_name }} has failed. Schema upgrades are blocked.",
			},
		},
		// 2 — operator degraded (condition)
		map[string]any{
			"alert": "CostManagementDegraded",
			"expr": `kube_customresource_status_condition{` +
				`customresource_kind="CostManagementServiceConfig",` +
				`customresource_name="` + instance + `",` +
				`namespace="` + ns + `",` +
				`condition="Degraded",status="true"} == 1`,
			"for": "5m",
			"labels": map[string]any{
				"severity": "critical",
				"instance": instance,
			},
			"annotations": map[string]any{
				"summary":     "Cost Management operator is degraded",
				"description": "The CostManagementServiceConfig {{ $labels.customresource_name }} has been in Degraded state for 5 minutes.",
			},
		},
		// 3 — schema not up to date
		map[string]any{
			"alert": "CostManagementSchemaOutOfDate",
			"expr": `kube_customresource_status_condition{` +
				`customresource_kind="CostManagementServiceConfig",` +
				`customresource_name="` + instance + `",` +
				`namespace="` + ns + `",` +
				`condition="SchemaUpToDate",status="false"} == 1`,
			"for": "15m",
			"labels": map[string]any{
				"severity": "warning",
				"instance": instance,
			},
			"annotations": map[string]any{
				"summary":     "Cost Management schema migrations pending",
				"description": "Database schema is not up to date for {{ $labels.customresource_name }}. Migrations may be stuck.",
			},
		},
		// 4 — koku API unavailable
		map[string]any{
			"alert": "CostManagementAPIDown",
			"expr":  `up{job="` + instance + `-koku-api",namespace="` + ns + `"} == 0`,
			"for":   "5m",
			"labels": map[string]any{
				"severity": "critical",
				"instance": instance,
			},
			"annotations": map[string]any{
				"summary":     "Cost Management API is unreachable",
				"description": "The koku-api metrics endpoint has been unreachable for 5 minutes.",
			},
		},
		// 5 — operator not progressing (stuck reconcile)
		map[string]any{
			"alert": "CostManagementNotProgressing",
			"expr": `kube_customresource_status_condition{` +
				`customresource_kind="CostManagementServiceConfig",` +
				`customresource_name="` + instance + `",` +
				`namespace="` + ns + `",` +
				`condition="Available",status="false"} == 1`,
			"for": "30m",
			"labels": map[string]any{
				"severity": "warning",
				"instance": instance,
			},
			"annotations": map[string]any{
				"summary":     "Cost Management stack is not available",
				"description": "CostManagementServiceConfig {{ $labels.customresource_name }} has not become Available in 30 minutes.",
			},
		},
	}

	pr := &unstructured.Unstructured{}
	pr.SetGroupVersionKind(prometheusRuleGVK)
	pr.SetName(cfg.Name + "-alerts")
	pr.SetNamespace(cfg.Namespace)
	pr.SetLabels(Labels(cfg, "monitoring"))

	_ = unstructured.SetNestedSlice(pr.Object, []any{
		map[string]any{
			"name":  "cost-management.rules",
			"rules": rules,
		},
	}, "spec", "groups")

	return pr
}
