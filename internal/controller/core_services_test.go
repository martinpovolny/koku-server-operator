package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
	"github.com/project-koku/koku-service-operator/internal/resources"
)

func TestReconcileCoreServices_ROSOff_APINotReady(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	r := &CostManagementServiceConfigReconciler{
		Client:   fakeClientWithApplySupport(scheme),
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	result, err := r.reconcileCoreServices(context.Background(), cfg)
	if err != nil {
		t.Fatalf("reconcileCoreServices: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected non-zero RequeueAfter while API is not ready")
	}

	mustExist(t, r.Client, testNamespace, resources.NameRBACAPI(cfg), &appsv1.Deployment{})
	mustExist(t, r.Client, testNamespace, resources.NameRBACAPI(cfg), &corev1.Service{})
	mustExist(t, r.Client, testNamespace, resources.NameRBACWorker(cfg), &appsv1.Deployment{})
	mustExist(t, r.Client, testNamespace, resources.NameKokuAPI(cfg), &appsv1.Deployment{})
	mustExist(t, r.Client, testNamespace, resources.NameKokuAPI(cfg), &corev1.Service{})
	mustExist(t, r.Client, testNamespace, resources.NameMasu(cfg), &appsv1.Deployment{})
	mustExist(t, r.Client, testNamespace, resources.NameMasu(cfg), &corev1.Service{})
	mustExist(t, r.Client, testNamespace, resources.NameListener(cfg), &appsv1.Deployment{})

	mustNotExist(t, r.Client, testNamespace, resources.NameKruize(cfg), &appsv1.Deployment{})
	mustNotExist(t, r.Client, "", resources.NameKruizeClusterRole(cfg), &rbacv1.ClusterRole{})

	cond := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionAvailable)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "WaitingForAPI" {
		t.Fatalf("expected Available=False WaitingForAPI, got %+v", cond)
	}
}

func TestReconcileCoreServices_ROSOn_APINotReady(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	cfg.Spec.ROS.Enabled = boolPtr(true)

	r := &CostManagementServiceConfigReconciler{
		Client:   fakeClientWithApplySupport(scheme),
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	result, err := r.reconcileCoreServices(context.Background(), cfg)
	if err != nil {
		t.Fatalf("reconcileCoreServices: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected non-zero RequeueAfter while API is not ready")
	}

	mustExist(t, r.Client, testNamespace, resources.NameKruizeServiceAccount(cfg), &corev1.ServiceAccount{})
	mustExist(t, r.Client, testNamespace, resources.NameKruizeConfigMap(cfg), &corev1.ConfigMap{})
	mustExist(t, r.Client, testNamespace, resources.NameKruize(cfg), &appsv1.Deployment{})
	mustExist(t, r.Client, testNamespace, resources.NameKruize(cfg), &corev1.Service{})
	mustExist(t, r.Client, "", resources.NameKruizeClusterRole(cfg), &rbacv1.ClusterRole{})
	mustExist(t, r.Client, "", resources.NameKruizeClusterRole(cfg), &rbacv1.ClusterRoleBinding{})

	mustExist(t, r.Client, testNamespace, resources.NameCdappConfigMap(cfg), &corev1.ConfigMap{})
	mustExist(t, r.Client, testNamespace, resources.NameROSServiceAccount(cfg), &corev1.ServiceAccount{})
	mustExist(t, r.Client, testNamespace, resources.NameKokuAPI(cfg), &appsv1.Deployment{})

	cond := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionAvailable)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "WaitingForAPI" {
		t.Fatalf("expected Available=False WaitingForAPI, got %+v", cond)
	}
}

func TestReconcileCoreServices_ROSOff_APIReady(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	c := fakeClientPreservingStatus(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}

	result, err := r.reconcileCoreServices(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("first pass should requeue while API is not ready")
	}

	markDeploymentReady(t, c, testNamespace, resources.NameKokuAPI(cfg))
	markDeploymentReady(t, c, testNamespace, resources.NameMasu(cfg))
	markDeploymentReady(t, c, testNamespace, resources.NameListener(cfg))

	result, err = r.reconcileCoreServices(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected zero Result when API is ready, got %+v", result)
	}
	if !apimeta.IsStatusConditionTrue(cfg.Status.Conditions, costv1alpha1.ConditionAvailable) {
		t.Fatal("expected Available=True")
	}
	cond := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionAvailable)
	if cond == nil || cond.Reason != "KokuAvailable" {
		t.Fatalf("expected Available reason KokuAvailable, got %+v", cond)
	}
}

func TestReconcileCoreServices_WaitsForMasuAfterAPI(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	c := fakeClientPreservingStatus(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}
	if _, err := r.reconcileCoreServices(context.Background(), cfg); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	markDeploymentReady(t, c, testNamespace, resources.NameKokuAPI(cfg))

	result, err := r.reconcileCoreServices(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected requeue while Masu is not ready")
	}
	cond := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionAvailable)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "WaitingForMasu" {
		t.Fatalf("expected Available=False WaitingForMasu, got %+v", cond)
	}
}

func TestReconcileCoreServices_WaitsForListenerAfterMasu(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	c := fakeClientPreservingStatus(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}
	if _, err := r.reconcileCoreServices(context.Background(), cfg); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	markDeploymentReady(t, c, testNamespace, resources.NameKokuAPI(cfg))
	markDeploymentReady(t, c, testNamespace, resources.NameMasu(cfg))

	result, err := r.reconcileCoreServices(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected requeue while Listener is not ready")
	}
	cond := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionAvailable)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "WaitingForListener" {
		t.Fatalf("expected Available=False WaitingForListener, got %+v", cond)
	}
}

func TestReconcileCoreServices_ROSOn_WaitsForKruize(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	cfg.Spec.ROS.Enabled = boolPtr(true)
	c := fakeClientPreservingStatus(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}
	if _, err := r.reconcileCoreServices(context.Background(), cfg); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	markDeploymentReady(t, c, testNamespace, resources.NameKokuAPI(cfg))
	markDeploymentReady(t, c, testNamespace, resources.NameMasu(cfg))
	markDeploymentReady(t, c, testNamespace, resources.NameListener(cfg))

	result, err := r.reconcileCoreServices(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected requeue while Kruize is not ready")
	}
	cond := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionAvailable)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "WaitingForKruize" {
		t.Fatalf("expected Available=False WaitingForKruize, got %+v", cond)
	}
}

func TestReconcileCoreServices_MasuTimeoutSetsDegraded(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	c := fakeClientPreservingStatus(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}
	if _, err := r.reconcileCoreServices(context.Background(), cfg); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	markDeploymentReady(t, c, testNamespace, resources.NameKokuAPI(cfg))

	apimeta.RemoveStatusCondition(&cfg.Status.Conditions, costv1alpha1.ConditionAvailable)
	apimeta.SetStatusCondition(&cfg.Status.Conditions, metav1.Condition{
		Type:               costv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "WaitingForMasu",
		Message:            "waiting for Masu",
		LastTransitionTime: metav1.NewTime(time.Now().Add(-6 * time.Minute)),
	})

	result, err := r.reconcileCoreServices(context.Background(), cfg)
	if err != nil {
		t.Fatalf("timeout pass: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected requeue after readiness timeout")
	}
	if result.RequeueAfter < requeueSlow {
		t.Errorf("backoff RequeueAfter = %v, want at least %v", result.RequeueAfter, requeueSlow)
	}
	deg := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionDegraded)
	if deg == nil || deg.Status != metav1.ConditionTrue || deg.Reason != "DeploymentNotReady" {
		t.Fatalf("expected Degraded=True DeploymentNotReady, got %+v", deg)
	}
	if !strings.Contains(strings.ToLower(deg.Message), "masu") {
		t.Errorf("Degraded message %q should name Masu", deg.Message)
	}
	if cfg.Status.Phase != costv1alpha1.PhaseDegraded {
		t.Errorf("Phase = %q, want %q", cfg.Status.Phase, costv1alpha1.PhaseDegraded)
	}
}

func TestReconcileCoreServices_ReasonChangeResetsTimeoutClock(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	c := fakeClientPreservingStatus(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}
	if _, err := r.reconcileCoreServices(context.Background(), cfg); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	markDeploymentReady(t, c, testNamespace, resources.NameKokuAPI(cfg))

	// Stale API wait must not make Masu look like it already timed out.
	apimeta.RemoveStatusCondition(&cfg.Status.Conditions, costv1alpha1.ConditionAvailable)
	apimeta.SetStatusCondition(&cfg.Status.Conditions, metav1.Condition{
		Type:               costv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "WaitingForAPI",
		LastTransitionTime: metav1.NewTime(time.Now().Add(-6 * time.Minute)),
	})

	result, err := r.reconcileCoreServices(context.Background(), cfg)
	if err != nil {
		t.Fatalf("masu wait pass: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected requeue while Masu is not ready")
	}
	cond := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionAvailable)
	if cond == nil || cond.Reason != "WaitingForMasu" {
		t.Fatalf("expected WaitingForMasu, got %+v", cond)
	}
	deg := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionDegraded)
	if deg != nil && deg.Status == metav1.ConditionTrue {
		t.Fatalf("Degraded should stay unset when the not-ready component just changed, got %+v", deg)
	}
}

func mustExist(t *testing.T, c client.Client, ns, name string, obj client.Object) {
	t.Helper()
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, obj); err != nil {
		t.Fatalf("expected %s/%s to exist: %v", ns, name, err)
	}
}

func mustNotExist(t *testing.T, c client.Client, ns, name string, obj client.Object) {
	t.Helper()
	err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, obj)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected %s/%s to be absent, got err=%v", ns, name, err)
	}
}
