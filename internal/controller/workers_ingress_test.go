package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
	"github.com/project-koku/koku-service-operator/internal/resources"
)

func TestReconcileWorkers_IngressNotReady(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	r := &CostManagementServiceConfigReconciler{
		Client:   fakeClientWithApplySupport(scheme),
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}
	result, err := r.reconcileWorkers(context.Background(), cfg)
	if err != nil {
		t.Fatalf("reconcileWorkers: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected requeue while Ingress Deployment is not ready")
	}
	mustExist(t, r.Client, testNamespace, resources.NameIngress(cfg), &appsv1.Deployment{})
	cond := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionIngressReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "WaitingForIngress" {
		t.Fatalf("expected IngressReady=False WaitingForIngress, got %+v", cond)
	}
}

func TestReconcileWorkers_IngressReady(t *testing.T) {
	scheme := ownershipScheme(t)
	cfg := minimalCR(testCRName, testNamespace)
	c := fakeClientPreservingStatus(scheme)
	r := &CostManagementServiceConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: &noopRecorder{},
	}
	if _, err := r.reconcileWorkers(context.Background(), cfg); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	markDeploymentReady(t, c, testNamespace, resources.NameIngress(cfg))
	result, err := r.reconcileWorkers(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected zero result once Ingress is ready, got %+v", result)
	}
	if !apimeta.IsStatusConditionTrue(cfg.Status.Conditions, costv1alpha1.ConditionIngressReady) {
		t.Fatal("expected IngressReady=True")
	}
}
