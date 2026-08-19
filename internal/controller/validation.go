package controller

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	costv1alpha1 "github.com/project-koku/koku-service-operator/api/v1alpha1"
	"github.com/project-koku/koku-service-operator/internal/resources"
)

const (
	validationTimeout = 5 * time.Second
	jwksBodyLimit     = 256 * 1024
)

// accessKeyKey would be less readable than the key name itself.
//
//nolint:goconst // Secret key names are intentionally literal — a constant like
var (
	// s3SecretKeys are the required keys in an objectStorage Secret.
	s3SecretKeys = []string{"access-key", "secret-key"}

	// dbSecretKeys lists the credential keys the operator expects in an
	// externally-provided database Secret (spec.database.secretName).
	dbSecretKeys = []string{
		"postgres-user", "postgres-password",
		"koku-user", "koku-password",
		"ros-user", "ros-password",
		"rbac-user", "rbac-password",
		"kruize-user", "kruize-password",
	}
)

// reconcileValidation probes all external dependencies and validates referenced
// Secrets before the migration gate. DB and Cache failures block the pipeline;
// Kafka, OIDC, and S3 set conditions without blocking (init containers inside
// pods handle late-starting infrastructure).
func (r *CostManagementServiceConfigReconciler) reconcileValidation(ctx context.Context, cfg *costv1alpha1.CostManagementServiceConfig) (Result, error) {
	allReady := true

	// --- External DB ---
	// Bundled DB is already gated in reconcileInfrastructure; probe only when external.
	if !costv1alpha1.BoolVal(cfg.Spec.Database.Deploy, true) {
		host := resources.DatabaseHost(cfg)
		port := cfg.Spec.Database.Port
		if port == 0 {
			port = 5432
		}
		addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
		if err := tcpProbe(addr, validationTimeout); err != nil {
			r.setCondition(cfg, costv1alpha1.ConditionDatabaseReady, metav1.ConditionFalse,
				"DatabaseUnreachable", fmt.Sprintf("TCP probe %s: %v", addr, err))
			allReady = false
		} else {
			// Validate secret keys when the user provided their own secret.
			if cfg.Spec.Database.SecretName != "" {
				required := dbSecretKeys
				if err := r.checkSecretKeys(ctx, cfg.Namespace, cfg.Spec.Database.SecretName, required); err != nil {
					r.setCondition(cfg, costv1alpha1.ConditionDatabaseReady, metav1.ConditionFalse,
						"DatabaseSecretInvalid", err.Error())
					allReady = false
				} else {
					r.setCondition(cfg, costv1alpha1.ConditionDatabaseReady, metav1.ConditionTrue,
						"DatabaseReachable", addr)
				}
			} else {
				r.setCondition(cfg, costv1alpha1.ConditionDatabaseReady, metav1.ConditionTrue,
					"DatabaseReachable", addr)
			}
		}
	}

	// --- External Cache ---
	if !costv1alpha1.BoolVal(cfg.Spec.Cache.Deploy, true) {
		host := resources.CacheHost(cfg)
		port := cfg.Spec.Cache.Port
		if port == 0 {
			port = 6379
		}
		addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
		if err := tcpProbe(addr, validationTimeout); err != nil {
			r.setCondition(cfg, costv1alpha1.ConditionCacheReady, metav1.ConditionFalse,
				"CacheUnreachable", fmt.Sprintf("TCP probe %s: %v", addr, err))
			allReady = false
		} else {
			if cfg.Spec.Cache.Auth.SecretName != "" {
				if err := r.checkSecretKeys(ctx, cfg.Namespace, cfg.Spec.Cache.Auth.SecretName, []string{"redis-password"}); err != nil {
					r.setCondition(cfg, costv1alpha1.ConditionCacheReady, metav1.ConditionFalse,
						"CacheSecretInvalid", err.Error())
					allReady = false
				} else {
					r.setCondition(cfg, costv1alpha1.ConditionCacheReady, metav1.ConditionTrue,
						"CacheReachable", addr)
				}
			} else {
				r.setCondition(cfg, costv1alpha1.ConditionCacheReady, metav1.ConditionTrue,
					"CacheReachable", addr)
			}
		}
	}

	// --- Kafka (always external; non-blocking) ---
	if bs := strings.TrimSpace(cfg.Spec.Kafka.BootstrapServers); bs != "" {
		if err := kafkaTCPProbe(bs, validationTimeout); err != nil {
			r.setCondition(cfg, costv1alpha1.ConditionKafkaReady, metav1.ConditionFalse,
				"KafkaUnreachable", err.Error())
		} else {
			if cfg.Spec.Kafka.SASL.ExistingSecret != "" {
				if err := r.checkSecretKeys(ctx, cfg.Namespace, cfg.Spec.Kafka.SASL.ExistingSecret, []string{"username", "password"}); err != nil { //nolint:goconst // Secret key names are clearer as literals
					r.setCondition(cfg, costv1alpha1.ConditionKafkaReady, metav1.ConditionFalse,
						"KafkaSASLSecretInvalid", err.Error())
				} else {
					r.setCondition(cfg, costv1alpha1.ConditionKafkaReady, metav1.ConditionTrue,
						"KafkaReachable", bs)
				}
			} else {
				r.setCondition(cfg, costv1alpha1.ConditionKafkaReady, metav1.ConditionTrue,
					"KafkaReachable", bs)
			}
		}
	}

	// --- S3 / ObjectStorage (non-blocking) ---
	// G2: Secret exists with access-key / secret-key.
	// G1: Signed ListBuckets against the resolved endpoint (user or discovered).
	r.validateObjectStorage(ctx, cfg)

	// --- OIDC / Keycloak (non-blocking; skipped when URL not explicitly set) ---
	if u := strings.TrimSpace(cfg.Spec.Auth.Keycloak.URL); u != "" {
		jwksURL := resources.KeycloakJWKSURL(cfg)
		insecure := cfg.Spec.Auth.Keycloak.TLS.InsecureSkipVerify
		if err := jwksProbe(ctx, jwksURL, insecure, validationTimeout); err != nil {
			r.setCondition(cfg, costv1alpha1.ConditionAuthReady, metav1.ConditionFalse,
				"OIDCUnreachable", fmt.Sprintf("JWKS %s: %v", jwksURL, err))
		} else {
			r.setCondition(cfg, costv1alpha1.ConditionAuthReady, metav1.ConditionTrue,
				"OIDCReachable", jwksURL)
		}
	}

	if !allReady {
		return Result{RequeueAfter: requeueSlow}, nil
	}
	return Result{}, nil
}

// tcpProbe opens and immediately closes a TCP connection to addr.
func tcpProbe(addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

// kafkaTCPProbe probes each broker in a comma-separated bootstrap-servers list
// and returns nil as soon as one is reachable.
func kafkaTCPProbe(bootstrapServers string, timeout time.Duration) error {
	var last error
	for b := range strings.SplitSeq(bootstrapServers, ",") {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		if err := tcpProbe(b, timeout); err != nil {
			last = err
			continue
		}
		return nil // first reachable broker
	}
	if last != nil {
		return fmt.Errorf("no reachable Kafka broker in %q: %w", bootstrapServers, last)
	}
	return fmt.Errorf("bootstrap-servers %q is empty", bootstrapServers)
}

// jwksProbe GETs the OIDC JWKS URL and requires HTTP 2xx plus a JSON body
// with a non-empty keys array (what Envoy needs to validate JWTs).
// 4xx is an error: 401/403 means the endpoint is misconfigured; 404 means
// the JWKS URL or realm is wrong. Uses the reconcile context so shutdown
// cancels an in-flight probe.
func jwksProbe(ctx context.Context, rawURL string, insecureSkipVerify bool, timeout time.Duration) error {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}
	transport := base.Clone()
	if insecureSkipVerify {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec // user-opted-in
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		transport.CloseIdleConnections()
		return err
	}
	defer func() {
		_ = resp.Body.Close()
		transport.CloseIdleConnections()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected HTTP status %d (want 2xx)", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, jwksBodyLimit+1))
	if err != nil {
		return fmt.Errorf("read JWKS body: %w", err)
	}
	if len(body) > jwksBodyLimit {
		return fmt.Errorf("JWKS response exceeds %d bytes", jwksBodyLimit)
	}
	var doc struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("JWKS is not valid JSON: %w", err)
	}
	if len(doc.Keys) == 0 {
		return fmt.Errorf("JWKS keys array is empty")
	}
	return nil
}

// checkSecretKeys verifies that a named Secret exists and contains all required keys
// with non-empty values.
func (r *CostManagementServiceConfigReconciler) checkSecretKeys(ctx context.Context, namespace, name string, required []string) error {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("secret %q not found", name)
		}
		return fmt.Errorf("get secret %q: %w", name, err)
	}
	var missing []string
	for _, key := range required {
		if len(secret.Data[key]) == 0 {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("secret %q missing keys: %s", name, strings.Join(missing, ", "))
	}
	return nil
}
