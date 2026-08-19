package controller

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
)

const (
	s3TestAccessKey = "AKIAIOSFODNN7EXAMPLE"
	s3TestSecretKey = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	s3TestSecret    = "s3-creds"
	listBucketsXML  = `<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Owner><ID>owner</ID><DisplayName>owner</DisplayName></Owner>
  <Buckets></Buckets>
</ListAllMyBucketsResult>`
)

func s3CredsSecret(name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Data: map[string][]byte{
			"access-key": []byte(s3TestAccessKey),
			"secret-key": []byte(s3TestSecretKey),
		},
	}
}

func objectStorageForServer(t *testing.T, srv *httptest.Server, secretName string) costv1alpha1.ObjectStorageConfig {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	useSSL := u.Scheme == "https"
	return costv1alpha1.ObjectStorageConfig{
		Endpoint:   host,
		Port:       int32(port),
		UseSSL:     &useSSL,
		SecretName: secretName,
		S3:         costv1alpha1.S3Options{Region: "us-east-1"},
	}
}

func fakeS3ListBuckets(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/" {
			http.Error(w, "not ListBuckets", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func bundledNoKafkaSpec() costv1alpha1.CostManagementServiceConfigSpec {
	return costv1alpha1.CostManagementServiceConfigSpec{
		Database: costv1alpha1.DatabaseConfig{Deploy: truePtr()},
		Cache:    costv1alpha1.CacheConfig{Deploy: truePtr()},
	}
}

func TestS3ListBucketsProbe(t *testing.T) {
	ctx := context.Background()

	t.Run("reachable", func(t *testing.T) {
		srv := httptest.NewServer(fakeS3ListBuckets(http.StatusOK, listBucketsXML))
		t.Cleanup(srv.Close)
		if err := s3ListBucketsProbe(ctx, srv.URL, "us-east-1", s3TestAccessKey, s3TestSecretKey, time.Second); err != nil {
			t.Fatalf("expected success, got %v", err)
		}
	})

	t.Run("403 AccessDenied", func(t *testing.T) {
		srv := httptest.NewServer(fakeS3ListBuckets(http.StatusForbidden, `<Error><Code>AccessDenied</Code><Message>denied</Message></Error>`))
		t.Cleanup(srv.Close)
		if err := s3ListBucketsProbe(ctx, srv.URL, "us-east-1", s3TestAccessKey, s3TestSecretKey, time.Second); err == nil {
			t.Fatal("expected error for HTTP 403")
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		if err := s3ListBucketsProbe(ctx, "http://"+localHost+":1", "us-east-1", s3TestAccessKey, s3TestSecretKey, 200*time.Millisecond); err == nil {
			t.Fatal("expected error for unreachable endpoint")
		}
	})

	t.Run("tls verify failure", func(t *testing.T) {
		srv := httptest.NewTLSServer(fakeS3ListBuckets(http.StatusOK, listBucketsXML))
		t.Cleanup(srv.Close)
		if err := s3ListBucketsProbe(ctx, srv.URL, "us-east-1", s3TestAccessKey, s3TestSecretKey, time.Second); err == nil {
			t.Fatal("expected TLS error")
		}
	})

	t.Run("endpoint missing scheme", func(t *testing.T) {
		if err := s3ListBucketsProbe(ctx, "s3.example.svc:443", "us-east-1", s3TestAccessKey, s3TestSecretKey, time.Second); err == nil {
			t.Fatal("expected error for endpoint without scheme")
		}
	})
}

func TestReconcileValidation_S3ListBucketsReachable(t *testing.T) {
	srv := httptest.NewServer(fakeS3ListBuckets(http.StatusOK, listBucketsXML))
	t.Cleanup(srv.Close)

	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: testCRName, Namespace: testNamespace},
		Spec:       bundledNoKafkaSpec(),
	}
	cfg.Spec.ObjectStorage = objectStorageForServer(t, srv, s3TestSecret)

	r := newValidationReconciler(t, s3CredsSecret(s3TestSecret))
	result, err := r.reconcileValidation(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Errorf("S3 probe must not block the pipeline, got %+v", result)
	}
	found := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionStorageReady)
	if found == nil || found.Status != metav1.ConditionTrue {
		t.Fatalf("expected StorageReady=True, got %+v", found)
	}
	if found.Reason != "StorageReachable" {
		t.Errorf("reason = %q, want StorageReachable", found.Reason)
	}
}

func TestReconcileValidation_S3ListBucketsUnreachableNonBlocking(t *testing.T) {
	useSSL := false
	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: testCRName, Namespace: testNamespace},
		Spec:       bundledNoKafkaSpec(),
	}
	cfg.Spec.ObjectStorage = costv1alpha1.ObjectStorageConfig{
		Endpoint:   localHost,
		Port:       1,
		UseSSL:     &useSSL,
		SecretName: s3TestSecret,
		S3:         costv1alpha1.S3Options{Region: "us-east-1"},
	}

	r := newValidationReconciler(t, s3CredsSecret(s3TestSecret))
	result, err := r.reconcileValidation(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Errorf("unreachable S3 must not block the pipeline, got %+v", result)
	}
	found := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionStorageReady)
	if found == nil || found.Status != metav1.ConditionFalse {
		t.Fatalf("expected StorageReady=False, got %+v", found)
	}
	if found.Reason != "StorageUnreachable" {
		t.Errorf("reason = %q, want StorageUnreachable", found.Reason)
	}
}

func TestReconcileValidation_S3ListBucketsForbidden(t *testing.T) {
	srv := httptest.NewServer(fakeS3ListBuckets(http.StatusForbidden, `<Error><Code>AccessDenied</Code><Message>denied</Message></Error>`))
	t.Cleanup(srv.Close)

	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: testCRName, Namespace: testNamespace},
		Spec:       bundledNoKafkaSpec(),
	}
	cfg.Spec.ObjectStorage = objectStorageForServer(t, srv, s3TestSecret)

	r := newValidationReconciler(t, s3CredsSecret(s3TestSecret))
	_, _ = r.reconcileValidation(context.Background(), cfg)

	found := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionStorageReady)
	if found == nil || found.Status != metav1.ConditionFalse {
		t.Fatalf("expected StorageReady=False, got %+v", found)
	}
	if found.Reason != "StorageUnreachable" {
		t.Errorf("reason = %q, want StorageUnreachable", found.Reason)
	}
}

func TestReconcileValidation_S3ListBucketsDiscovered(t *testing.T) {
	srv := httptest.NewServer(fakeS3ListBuckets(http.StatusOK, listBucketsXML))
	t.Cleanup(srv.Close)

	cfg := &costv1alpha1.CostManagementServiceConfig{
		ObjectMeta: metav1.ObjectMeta{Name: testCRName, Namespace: testNamespace},
		Spec:       bundledNoKafkaSpec(),
		Status: costv1alpha1.CostManagementServiceConfigStatus{
			DiscoveredConfig: &costv1alpha1.DiscoveredConfig{
				S3: &costv1alpha1.DiscoveredS3{
					Endpoint:   srv.URL,
					SecretName: s3TestSecret,
					Region:     "us-east-1",
				},
			},
		},
	}

	r := newValidationReconciler(t, s3CredsSecret(s3TestSecret))
	_, _ = r.reconcileValidation(context.Background(), cfg)

	found := findCondition(cfg.Status.Conditions, costv1alpha1.ConditionStorageReady)
	if found == nil || found.Status != metav1.ConditionTrue || found.Reason != "StorageReachable" {
		t.Fatalf("expected StorageReady=True StorageReachable for discovered S3, got %+v", found)
	}
}
