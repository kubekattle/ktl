package secretstore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

const (
	vaultAuthToken      = "token"
	vaultAuthAppRole    = "approle"
	vaultAuthKubernetes = "kubernetes"
	vaultAuthAWS        = "aws"

	defaultKubernetesTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

type vaultAuthConfig struct {
	method              string
	mount               string
	token               string
	roleID              string
	secretID            string
	kubernetesRole      string
	kubernetesToken     string
	kubernetesTokenPath string
	awsRole             string
	awsRegion           string
	awsHeaderValue      string
}

func buildVaultAuthConfig(cfg ProviderConfig) (vaultAuthConfig, error) {
	method, err := normalizeVaultAuthMethod(cfg.AuthMethod)
	if err != nil {
		return vaultAuthConfig{}, err
	}
	token := strings.TrimSpace(cfg.Token)
	roleID := strings.TrimSpace(cfg.RoleID)
	secretID := strings.TrimSpace(cfg.SecretID)
	kubernetesRole := strings.TrimSpace(cfg.KubernetesRole)
	kubernetesToken := strings.TrimSpace(cfg.KubernetesToken)
	kubernetesTokenPath := strings.TrimSpace(cfg.KubernetesTokenPath)
	awsRole := strings.TrimSpace(cfg.AWSRole)
	awsRegion := strings.TrimSpace(cfg.AWSRegion)
	awsHeaderValue := strings.TrimSpace(cfg.AWSHeaderValue)

	if method == "" {
		switch {
		case token != "":
			method = vaultAuthToken
		case roleID != "" || secretID != "":
			method = vaultAuthAppRole
		case kubernetesRole != "":
			method = vaultAuthKubernetes
		case awsRole != "":
			method = vaultAuthAWS
		default:
			method = vaultAuthToken
		}
	}
	authMount := strings.Trim(strings.TrimSpace(cfg.AuthMount), "/")
	if authMount == "" {
		authMount = defaultVaultAuthMount(method)
	}
	out := vaultAuthConfig{
		method:              method,
		mount:               authMount,
		token:               token,
		roleID:              roleID,
		secretID:            secretID,
		kubernetesRole:      kubernetesRole,
		kubernetesToken:     kubernetesToken,
		kubernetesTokenPath: kubernetesTokenPath,
		awsRole:             awsRole,
		awsRegion:           awsRegion,
		awsHeaderValue:      awsHeaderValue,
	}
	switch method {
	case vaultAuthToken:
		if out.token == "" {
			return vaultAuthConfig{}, fmt.Errorf("vault token is required")
		}
	case vaultAuthAppRole:
		if out.roleID == "" || out.secretID == "" {
			return vaultAuthConfig{}, fmt.Errorf("vault approle auth requires roleId and secretId")
		}
	case vaultAuthKubernetes:
		if out.kubernetesRole == "" {
			return vaultAuthConfig{}, fmt.Errorf("vault kubernetes auth requires kubernetesRole")
		}
		if out.kubernetesToken == "" && out.kubernetesTokenPath == "" {
			out.kubernetesTokenPath = defaultKubernetesTokenPath
		}
	case vaultAuthAWS:
		if out.awsRole == "" {
			return vaultAuthConfig{}, fmt.Errorf("vault aws auth requires awsRole")
		}
	default:
		return vaultAuthConfig{}, fmt.Errorf("vault auth method %q is not supported", cfg.AuthMethod)
	}
	return out, nil
}

func normalizeVaultAuthMethod(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "":
		return "", nil
	case "token":
		return vaultAuthToken, nil
	case "approle", "app-role", "app_role":
		return vaultAuthAppRole, nil
	case "kubernetes", "k8s":
		return vaultAuthKubernetes, nil
	case "aws", "aws-iam", "iam":
		return vaultAuthAWS, nil
	default:
		return "", fmt.Errorf("unsupported vault auth method %q", raw)
	}
}

func defaultVaultAuthMount(method string) string {
	switch method {
	case vaultAuthAppRole:
		return "approle"
	case vaultAuthKubernetes:
		return "kubernetes"
	case vaultAuthAWS:
		return "aws"
	default:
		return ""
	}
}

func (p *vaultProvider) ensureAuth(ctx context.Context) error {
	if p == nil {
		return fmt.Errorf("vault provider is not initialized")
	}
	if p.auth.method == vaultAuthToken {
		return nil
	}
	p.authOnce.Do(func() {
		p.authErr = p.login(ctx)
	})
	return p.authErr
}

func (p *vaultProvider) login(ctx context.Context) error {
	switch p.auth.method {
	case vaultAuthAppRole:
		return p.loginWithData(ctx, map[string]interface{}{
			"role_id":   p.auth.roleID,
			"secret_id": p.auth.secretID,
		})
	case vaultAuthKubernetes:
		token := strings.TrimSpace(p.auth.kubernetesToken)
		if token == "" {
			path := p.auth.kubernetesTokenPath
			if strings.TrimSpace(path) == "" {
				path = defaultKubernetesTokenPath
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read kubernetes token: %w", err)
			}
			token = strings.TrimSpace(string(raw))
		}
		if token == "" {
			return fmt.Errorf("kubernetes token is empty")
		}
		return p.loginWithData(ctx, map[string]interface{}{
			"role": p.auth.kubernetesRole,
			"jwt":  token,
		})
	case vaultAuthAWS:
		payload, err := buildAWSLoginPayload(ctx, p.auth)
		if err != nil {
			return err
		}
		return p.loginWithData(ctx, payload)
	default:
		return nil
	}
}

func (p *vaultProvider) loginWithData(ctx context.Context, data map[string]interface{}) error {
	if p == nil {
		return fmt.Errorf("vault provider is not initialized")
	}
	path := fmt.Sprintf("auth/%s/login", strings.Trim(p.auth.mount, "/"))
	secret, err := p.client.Logical().WriteWithContext(ctx, path, data)
	if err != nil {
		return err
	}
	if secret == nil || secret.Auth == nil || strings.TrimSpace(secret.Auth.ClientToken) == "" {
		return fmt.Errorf("vault auth %s did not return a client token", p.auth.method)
	}
	p.client.SetToken(secret.Auth.ClientToken)
	return nil
}

func buildAWSLoginPayload(ctx context.Context, cfg vaultAuthConfig) (map[string]interface{}, error) {
	region := strings.TrimSpace(cfg.awsRegion)
	if region == "" {
		region = strings.TrimSpace(os.Getenv("AWS_REGION"))
	}
	if region == "" {
		region = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION"))
	}
	if region == "" {
		return nil, fmt.Errorf("aws region is required for vault auth (set awsRegion or AWS_REGION)")
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	creds, err := awsCfg.Credentials.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieve aws credentials: %w", err)
	}
	body := "Action=GetCallerIdentity&Version=2011-06-15"
	req, err := http.NewRequest(http.MethodPost, "https://sts.amazonaws.com/", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build sts request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.Header.Set("Host", "sts.amazonaws.com")
	if cfg.awsHeaderValue != "" {
		req.Header.Set("X-Vault-AWS-IAM-Server-ID", cfg.awsHeaderValue)
	}
	payloadHash := sha256.Sum256([]byte(body))
	signer := v4.NewSigner()
	if err := signer.SignHTTP(ctx, creds, req, hex.EncodeToString(payloadHash[:]), "sts", region, time.Now()); err != nil {
		return nil, fmt.Errorf("sign sts request: %w", err)
	}
	headers := map[string][]string{}
	for key, values := range req.Header {
		headers[key] = values
	}
	if req.Host != "" {
		headers["Host"] = []string{req.Host}
	}
	headerJSON, err := json.Marshal(headers)
	if err != nil {
		return nil, fmt.Errorf("encode aws headers: %w", err)
	}
	return map[string]interface{}{
		"role":                    cfg.awsRole,
		"iam_http_request_method": req.Method,
		"iam_request_url":         base64.StdEncoding.EncodeToString([]byte(req.URL.String())),
		"iam_request_body":        base64.StdEncoding.EncodeToString([]byte(body)),
		"iam_request_headers":     base64.StdEncoding.EncodeToString(headerJSON),
	}, nil
}

func coerceStringList(value interface{}) ([]string, error) {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), nil
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			str, ok := item.(string)
			if !ok {
				continue
			}
			out = append(out, str)
		}
		if len(out) == 0 && len(typed) > 0 {
			return nil, fmt.Errorf("secret keys must be strings")
		}
		return out, nil
	default:
		return nil, fmt.Errorf("secret keys must be a string list")
	}
}
