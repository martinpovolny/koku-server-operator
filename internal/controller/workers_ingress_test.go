package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"

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
	// reconcileWorkers applies Ingress Deployment/Service and returns without waiting.
	if !result.IsZero() {
		t.Fatalf("expected zero result (no requeue), got %+v", result)
	}
	mustExist(t, r.Client, testNamespace, resources.NameIngress(cfg), &appsv1.Deployment{})
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
}
