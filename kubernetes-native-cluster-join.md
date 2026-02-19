# Kubernetes-Native Cluster Joining in KubeVela: Current State and Future Feasibility

## Executive Summary

**Current State:** KubeVela **ALREADY SUPPORTS** a Kubernetes-native approach to joining clusters by creating Secret resources directly, without using the `vela join` CLI command. However, this approach is manual and lacks automation for credential extraction, validation, and post-registration tasks.

**Recommendation:** Implement a CRD-based approach with a dedicated controller (ClusterRegistration CR + controller) to provide a fully declarative, automated, GitOps-friendly cluster joining experience.

---

## Table of Contents

1. [Current Kubernetes-Native Approach](#current-kubernetes-native-approach)
2. [How It Works Today](#how-it-works-today)
3. [Limitations of Current Approach](#limitations-of-current-approach)
4. [Proposed Enhanced CRD-Based Approach](#proposed-enhanced-crd-based-approach)
5. [Architecture Design](#architecture-design)
6. [Implementation Feasibility](#implementation-feasibility)
7. [Migration Path](#migration-path)
8. [Comparison Matrix](#comparison-matrix)

---

## Current Kubernetes-Native Approach

### Overview

KubeVela's cluster discovery mechanism is **fully Kubernetes-native** and works by watching for Secrets with specific labels in the cluster-gateway namespace (default: `vela-system`).

**Key Discovery Files:**
- `/Users/co/kubevela_os/kubevela/pkg/multicluster/virtual_cluster.go:242-276`
- Function: `ListVirtualClusters()`, `FindVirtualClustersByLabels()`

**Discovery Mechanism:**
```go
// Line 242-249
func ListVirtualClusters(ctx context.Context, c client.Client) ([]VirtualCluster, error) {
    clusters, err := FindVirtualClustersByLabels(ctx, c, map[string]string{})
    return append([]VirtualCluster{*NewVirtualClusterFromLocal()}, clusters...), nil
}

// Line 251-276 - Discovers from two sources:
// 1. Secrets with label cluster.core.oam.dev/cluster-credential-type
// 2. ManagedCluster CRs (OCM integration)
```

**Result:** Any Secret created with the correct structure and label is **automatically discovered** and becomes a usable cluster in KubeVela.

---

## How It Works Today

### Method 1: Manual Secret Creation (Currently Supported)

#### Step 1: Extract Credentials from Kubeconfig

```bash
# Extract from kubeconfig manually
CLUSTER_NAME="my-remote-cluster"
API_SERVER=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')
CA_CERT=$(kubectl config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' | base64 -d)
TOKEN=$(kubectl config view --raw --minify -o jsonpath='{.users[0].user.token}')
```

#### Step 2: Create Cluster Secret in Hub Cluster

**For Service Account Token Authentication:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-remote-cluster
  namespace: vela-system
  labels:
    cluster.core.oam.dev/cluster-credential-type: ServiceAccountToken
  annotations:
    cluster.core.oam.dev/cluster-alias: "Production West"
    cluster.core.oam.dev/cluster-version: '{"major":"1","minor":"28","gitVersion":"v1.28.3"}'
type: Opaque
data:
  endpoint: aHR0cHM6Ly9hcGkucmVtb3RlLmNsdXN0ZXI6NjQ0Mw==  # base64(https://api.remote.cluster:6443)
  ca.crt: LS0tLS1CRUdJTi...                               # base64(CA certificate)
  token: ZXlKaGJHY2lPaUpT...                              # base64(service account token)
```

**For X.509 Certificate Authentication:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-remote-cluster
  namespace: vela-system
  labels:
    cluster.core.oam.dev/cluster-credential-type: X509Certificate
  annotations:
    cluster.core.oam.dev/cluster-alias: "Production East"
type: Opaque
data:
  endpoint: aHR0cHM6Ly9hcGkucmVtb3RlLmNsdXN0ZXI6NjQ0Mw==  # base64(https://api.remote.cluster:6443)
  ca.crt: LS0tLS1CRUdJTi...                               # base64(CA certificate)
  tls.crt: LS0tLS1CRUdJTi...                              # base64(client certificate)
  tls.key: LS0tLS1CRUdJTi...                              # base64(client key)
```

#### Step 3: Apply the Secret

```bash
kubectl apply -f cluster-secret.yaml
```

#### Step 4: Verify Discovery

```bash
# Cluster is automatically discovered
kubectl get clustergateways -n vela-system

# Or use vela CLI to list
vela cluster list
```

**Output:**
```
CLUSTER         TYPE            ENDPOINT
local           Internal        -
my-remote-cluster ServiceAccountToken  https://api.remote.cluster:6443
```

---

### Method 2: Script-Based Secret Creation

**Complete Script Example:**

```bash
#!/bin/bash
# create-cluster-secret.sh

set -e

CLUSTER_NAME="${1:?Cluster name required}"
KUBECONFIG_FILE="${2:?Kubeconfig file required}"
HUB_CONTEXT="${3:-$(kubectl config current-context)}"

echo "Creating cluster secret for: $CLUSTER_NAME"

# Switch to target cluster context
export KUBECONFIG="$KUBECONFIG_FILE"

# Extract cluster info
API_SERVER=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')
CA_CERT=$(kubectl config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
CLIENT_CERT=$(kubectl config view --raw --minify -o jsonpath='{.users[0].user.client-certificate-data}')
CLIENT_KEY=$(kubectl config view --raw --minify -o jsonpath='{.users[0].user.client-key-data}')
TOKEN=$(kubectl config view --raw --minify -o jsonpath='{.users[0].user.token}')

# Determine credential type
if [ -n "$TOKEN" ]; then
    CRED_TYPE="ServiceAccountToken"
    echo "Using ServiceAccountToken authentication"
else
    CRED_TYPE="X509Certificate"
    echo "Using X509Certificate authentication"
fi

# Get cluster version
K8S_VERSION=$(kubectl version -o json | jq -r '.serverVersion')

# Switch back to hub cluster
kubectl config use-context "$HUB_CONTEXT"

# Create secret
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: $CLUSTER_NAME
  namespace: vela-system
  labels:
    cluster.core.oam.dev/cluster-credential-type: $CRED_TYPE
  annotations:
    cluster.core.oam.dev/cluster-version: '$K8S_VERSION'
type: Opaque
data:
  endpoint: $(echo -n "$API_SERVER" | base64)
  ca.crt: $CA_CERT
$(if [ -n "$TOKEN" ]; then
    echo "  token: $(echo -n "$TOKEN" | base64)"
else
    echo "  tls.crt: $CLIENT_CERT"
    echo "  tls.key: $CLIENT_KEY"
fi)
EOF

echo "✓ Cluster secret created successfully"
echo "Verify with: kubectl get clustergateways -n vela-system"
```

**Usage:**
```bash
./create-cluster-secret.sh my-cluster ./cluster.kubeconfig
```

---

### Method 3: Helm/Kustomize Integration

**Helm Chart Approach:**

```yaml
# values.yaml
clusters:
  - name: production-west
    endpoint: https://prod-west.example.com:6443
    credentialType: ServiceAccountToken
    credentials:
      caCert: |
        -----BEGIN CERTIFICATE-----
        MIIDXTCCAkWgAwIBAgIJ...
        -----END CERTIFICATE-----
      token: "eyJhbGciOiJSUzI1NiIsImtpZCI..."
    labels:
      env: production
      region: us-west

  - name: staging-east
    endpoint: https://staging-east.example.com:6443
    credentialType: X509Certificate
    credentials:
      caCert: |
        -----BEGIN CERTIFICATE-----
        ...
      clientCert: |
        -----BEGIN CERTIFICATE-----
        ...
      clientKey: |
        -----BEGIN RSA PRIVATE KEY-----
        ...
```

**Helm Template:**

```yaml
# templates/cluster-secrets.yaml
{{- range .Values.clusters }}
---
apiVersion: v1
kind: Secret
metadata:
  name: {{ .name }}
  namespace: vela-system
  labels:
    cluster.core.oam.dev/cluster-credential-type: {{ .credentialType }}
    {{- with .labels }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
type: Opaque
data:
  endpoint: {{ .endpoint | b64enc | quote }}
  ca.crt: {{ .credentials.caCert | b64enc | quote }}
  {{- if eq .credentialType "ServiceAccountToken" }}
  token: {{ .credentials.token | b64enc | quote }}
  {{- else }}
  tls.crt: {{ .credentials.clientCert | b64enc | quote }}
  tls.key: {{ .credentials.clientKey | b64enc | quote }}
  {{- end }}
{{- end }}
```

---

### Discovery and Usage Flow

```
┌─────────────────────────────────────────────────────────────────┐
│ 1. User Creates Secret                                          │
│    • Manually via kubectl                                       │
│    • Script automation                                          │
│    • Helm/Kustomize deployment                                  │
│    • GitOps (ArgoCD/Flux)                                       │
└─────────────────┬───────────────────────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────────────────────┐
│ 2. Secret Exists in vela-system Namespace                       │
│    • Name: cluster name                                         │
│    • Label: cluster.core.oam.dev/cluster-credential-type        │
│    • Data: endpoint, ca.crt, token/tls.crt/tls.key             │
└─────────────────┬───────────────────────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. KubeVela Discovery (Automatic)                               │
│    • ListVirtualClusters() scans vela-system namespace          │
│    • Finds all secrets with credential type label              │
│    • Constructs VirtualCluster objects                          │
└─────────────────┬───────────────────────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────────────────────┐
│ 4. Cluster-Gateway APIService                                   │
│    • Registers as v1alpha1.cluster.core.oam.dev                 │
│    • Intercepts requests to /clusters/{name}/proxy             │
│    • Loads credentials from secret                              │
│    • Proxies to remote cluster                                  │
└─────────────────┬───────────────────────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────────────────────┐
│ 5. Applications Use Cluster                                     │
│    • Topology policies target cluster by name/labels           │
│    • Workflow steps deploy to cluster                           │
│    • Multi-cluster controllers distribute resources            │
└─────────────────────────────────────────────────────────────────┘
```

---

## Limitations of Current Approach

### 1. Manual Credential Extraction

**Problem:** Users must manually:
- Parse kubeconfig files
- Extract credentials (CA, token, certs)
- Base64 encode values
- Handle different authentication methods

**Impact:**
- Error-prone (copy-paste mistakes, encoding issues)
- Time-consuming
- Requires deep knowledge of kubeconfig structure

**Example Error Scenario:**
```bash
# User forgets to base64 encode
echo "endpoint: https://api.cluster:6443"  # ❌ Not base64 encoded
# Result: Cluster connection fails silently
```

---

### 2. No Validation

**Problem:** No pre-flight checks before cluster registration:
- API server reachability not verified
- Credentials validity not tested
- Cluster version not automatically detected
- Namespace existence not validated

**Impact:**
- Invalid clusters registered (fail at deployment time)
- Difficult to troubleshoot
- No feedback on credential correctness

**Code Reference:** `/Users/co/kubevela_os/kubevela/pkg/multicluster/cluster_management.go:89-98`

CLI validation that's missing in Secret approach:
```go
func (clusterConfig *KubeClusterConfig) Validate() error {
    switch clusterConfig.ClusterName {
    case "":
        return errors.Errorf("ClusterName cannot be empty")
    case ClusterLocalName:  // "local"
        return errors.Errorf("ClusterName cannot be `local`, reserved")
    }
    return nil
}
```

---

### 3. Missing Post-Registration Tasks

**Problem:** CLI performs automatic tasks that Secret creation skips:

| Task | CLI (`vela join`) | Secret Creation |
|------|-------------------|-----------------|
| Create namespace in managed cluster | ✅ Yes (with retry) | ❌ No |
| Query cluster version | ✅ Yes | ❌ No |
| Update apps with topology policy | ✅ Yes | ❌ No |
| Add custom labels | ✅ Yes | ❌ Manual |
| Duplicate cluster check | ✅ Yes (with prompt) | ❌ No |

**Code Reference:** `/Users/co/kubevela_os/kubevela/references/cli/cluster.go:251-320`

Missing automation:
```go
// CLI automatically updates apps after cluster join
func updateAppsWithTopologyPolicy(ctx context.Context, k8sClient client.Client, ...) error {
    // Lists all applications
    // Checks for topology policies with clusterLabelSelector
    // Triggers redeployment by updating publish version
}
```

---

### 4. No Exec Provider Support

**Problem:** Cannot use external credential providers:
- AWS: `aws eks get-token`
- GCP: `gke-gcloud-auth-plugin`
- Azure: `kubelogin`
- Custom plugins

**Impact:**
- Cloud provider integrations require manual token extraction
- Tokens expire (need rotation mechanism)
- No support for short-lived credentials

**Code Reference:** `/Users/co/kubevela_os/kubevela/pkg/multicluster/cluster_management.go:643-692`

CLI supports exec but Secret creation doesn't:
```go
func getTokenFromExec(execConfig *clientcmdapi.ExecConfig) (string, error) {
    // Executes external command to get token
    // Parses ExecCredential output
    // Returns short-lived token
}
```

---

### 5. Poor GitOps Experience

**Problem:** Secrets contain sensitive data (tokens, keys) that shouldn't be in Git:
- Security risk (credentials exposed)
- No secret rotation workflow
- Difficult to manage across environments

**Impact:**
- Cannot use GitOps fully (must encrypt secrets)
- Requires external secret management (Sealed Secrets, External Secrets Operator)
- Complex workflow for secret rotation

---

### 6. No Status Reporting

**Problem:** No feedback mechanism on cluster health:
- Cannot tell if cluster is reachable
- No connection status
- No error reporting for invalid credentials

**Desired Status:**
```yaml
status:
  phase: Ready | Failed | Pending
  conditions:
    - type: CredentialsValid
      status: "True"
    - type: APIServerReachable
      status: "True"
    - type: NamespaceCreated
      status: "True"
  message: "Cluster successfully registered"
  lastHeartbeatTime: "2026-02-06T10:30:00Z"
```

---

### 7. No Lifecycle Management

**Problem:** Manual cluster management tasks:
- Renaming clusters requires Secret rename (loses history)
- Adding labels requires Secret patch
- Removing clusters requires manual Secret deletion
- No audit trail of changes

---

## Proposed Enhanced CRD-Based Approach

### Overview

Introduce a new Custom Resource Definition: **`ClusterRegistration`** with a dedicated controller that automates the entire cluster joining process.

**Design Principle:** Provide a declarative, GitOps-friendly, fully automated cluster registration experience while maintaining backward compatibility with the existing Secret-based approach.

---

### CRD Design: ClusterRegistration

```yaml
apiVersion: multicluster.oam.dev/v1alpha1
kind: ClusterRegistration
metadata:
  name: production-west-cluster
  namespace: vela-system
spec:
  # Cluster name (defaults to metadata.name)
  clusterName: prod-west

  # Alias for display purposes
  alias: "Production West Coast"

  # Credential source (multiple options)
  credentialSource:
    # Option 1: Reference to existing Secret containing kubeconfig
    secretRef:
      name: prod-west-kubeconfig
      namespace: default
      key: kubeconfig  # Key in secret containing kubeconfig data

    # Option 2: Inline credentials (for pre-extracted credentials)
    # inline:
    #   endpoint: https://api.prod-west.example.com:6443
    #   credentialType: ServiceAccountToken
    #   caData: LS0tLS1CRUdJTi...
    #   tokenData: ZXlKaGJHY2lP...

    # Option 3: External secret reference (for External Secrets Operator)
    # externalSecretRef:
    #   name: cluster-credentials
    #   provider: aws-secrets-manager

  # Management engine
  engine: cluster-gateway  # or "ocm"

  # Post-registration configuration
  config:
    # Namespace to create in managed cluster
    createNamespace: vela-system

    # Enable in-cluster bootstrap (for OCM)
    inClusterBootstrap: false

    # Connection timeout
    connectionTimeout: 30s

    # Validate credentials before registration
    validateCredentials: true

  # Custom labels to add to cluster
  labels:
    env: production
    region: us-west
    tier: gold

  # Pause reconciliation
  suspend: false

status:
  # Overall phase
  phase: Ready  # Pending | Progressing | Ready | Failed

  # Detailed conditions
  conditions:
    - type: CredentialValid
      status: "True"
      reason: TokenVerified
      message: "Service account token is valid"
      lastTransitionTime: "2026-02-06T10:30:00Z"

    - type: APIServerReachable
      status: "True"
      reason: ConnectionSuccessful
      message: "Successfully connected to cluster API server"
      lastTransitionTime: "2026-02-06T10:30:15Z"

    - type: NamespaceCreated
      status: "True"
      reason: NamespaceExists
      message: "Namespace vela-system exists in managed cluster"
      lastTransitionTime: "2026-02-06T10:30:20Z"

    - type: ClusterRegistered
      status: "True"
      reason: SecretCreated
      message: "Cluster secret created in vela-system"
      lastTransitionTime: "2026-02-06T10:30:25Z"

  # Cluster information
  clusterInfo:
    version:
      major: "1"
      minor: "28"
      gitVersion: "v1.28.3"
      platform: "linux/amd64"
    endpoint: https://api.prod-west.example.com:6443
    credentialType: ServiceAccountToken

  # Observed generation
  observedGeneration: 1

  # Last reconciliation time
  lastReconcileTime: "2026-02-06T10:30:25Z"

  # Last heartbeat (cluster connectivity check)
  lastHeartbeatTime: "2026-02-06T10:35:00Z"

  # Error message (if phase is Failed)
  message: ""
```

---

### Controller Architecture

#### Component Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                     ClusterRegistration Controller                   │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │ 1. Reconciliation Loop                                        │  │
│  │    • Watch ClusterRegistration CRs                            │  │
│  │    • Trigger on Create/Update/Delete                          │  │
│  │    • Requeue on transient failures                            │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                              ↓                                       │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │ 2. Credential Extraction                                      │  │
│  │    • Load kubeconfig from secretRef/inline/external           │  │
│  │    • Parse kubeconfig structure                               │  │
│  │    • Handle exec providers (AWS, GCP, Azure)                  │  │
│  │    • Detect authentication method                             │  │
│  │    • Extract endpoint, CA, token/cert                         │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                              ↓                                       │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │ 3. Validation                                                 │  │
│  │    • Name not empty, not "local"                              │  │
│  │    • No duplicate cluster names                               │  │
│  │    • Credentials not empty                                    │  │
│  │    • If validateCredentials=true:                             │  │
│  │      ├─ Test API server connectivity                          │  │
│  │      ├─ Verify token/cert validity                            │  │
│  │      └─ Query cluster version                                 │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                              ↓                                       │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │ 4. Cluster Secret Creation                                    │  │
│  │    • Create/Update Secret in vela-system                      │  │
│  │    • Set ownerReference to ClusterRegistration                │  │
│  │    • Add credential type label                                │  │
│  │    • Add cluster version annotation                           │  │
│  │    • Add custom labels from spec.labels                       │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                              ↓                                       │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │ 5. Post-Registration Tasks                                    │  │
│  │    • Create namespace in managed cluster (with retry)         │  │
│  │    • Trigger app redeployment for topology policies           │  │
│  │    • Set cluster version annotation                           │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                              ↓                                       │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │ 6. Status Update                                              │  │
│  │    • Update conditions (CredentialValid, APIServerReachable)  │  │
│  │    • Set phase (Pending → Progressing → Ready/Failed)         │  │
│  │    • Record cluster info (version, endpoint)                  │  │
│  │    • Update lastReconcileTime                                 │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                              ↓                                       │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │ 7. Periodic Heartbeat (Optional)                              │  │
│  │    • Every 5 minutes, check cluster connectivity              │  │
│  │    • Update lastHeartbeatTime                                 │  │
│  │    • Update APIServerReachable condition                      │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

---

#### Reconciliation State Machine

```
┌─────────────┐
│   Created   │
└──────┬──────┘
       │
       ▼
┌─────────────┐   Validation Failed    ┌─────────────┐
│  Pending    ├───────────────────────▶│   Failed    │
└──────┬──────┘                        └─────────────┘
       │                                      ▲
       │ Validation Passed                    │
       ▼                                      │
┌─────────────┐                               │
│ Progressing │──────Registration Failed──────┘
└──────┬──────┘
       │
       │ Registration Successful
       ▼
┌─────────────┐   Connectivity Lost    ┌─────────────┐
│    Ready    ├───────────────────────▶│ Degraded    │
└──────┬──────┘                        └──────┬──────┘
       │                                      │
       │                                      │
       │                   Connectivity       │
       │◀─────────────── Restored ────────────┘
       │
       │ Deletion Requested
       ▼
┌─────────────┐
│  Deleting   │
└─────────────┘
```

---

### Controller Pseudocode

```go
package controllers

import (
    "context"
    "time"

    multiclusterv1alpha1 "github.com/oam-dev/kubevela/apis/multicluster/v1alpha1"
    "github.com/oam-dev/kubevela/pkg/multicluster"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterRegistrationReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

func (r *ClusterRegistrationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch ClusterRegistration
    clusterReg := &multiclusterv1alpha1.ClusterRegistration{}
    if err := r.Get(ctx, req.NamespacedName, clusterReg); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // Handle deletion
    if !clusterReg.DeletionTimestamp.IsZero() {
        return r.handleDeletion(ctx, clusterReg)
    }

    // Handle suspension
    if clusterReg.Spec.Suspend {
        return ctrl.Result{}, nil
    }

    // 2. Update status to Progressing
    clusterReg.Status.Phase = "Progressing"
    r.Status().Update(ctx, clusterReg)

    // 3. Load credentials
    kubeConfig, err := r.loadCredentials(ctx, clusterReg)
    if err != nil {
        r.updateCondition(clusterReg, "CredentialValid", "False", err.Error())
        clusterReg.Status.Phase = "Failed"
        r.Status().Update(ctx, clusterReg)
        return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
    }
    r.updateCondition(clusterReg, "CredentialValid", "True", "Credentials loaded successfully")

    // 4. Validate credentials (if enabled)
    if clusterReg.Spec.Config.ValidateCredentials {
        if err := r.validateConnection(ctx, kubeConfig); err != nil {
            r.updateCondition(clusterReg, "APIServerReachable", "False", err.Error())
            clusterReg.Status.Phase = "Failed"
            r.Status().Update(ctx, clusterReg)
            return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
        }
        r.updateCondition(clusterReg, "APIServerReachable", "True", "API server is reachable")
    }

    // 5. Create cluster secret
    clusterName := clusterReg.Spec.ClusterName
    if clusterName == "" {
        clusterName = clusterReg.Name
    }

    secret, err := r.createClusterSecret(ctx, clusterName, kubeConfig, clusterReg)
    if err != nil {
        r.updateCondition(clusterReg, "ClusterRegistered", "False", err.Error())
        clusterReg.Status.Phase = "Failed"
        r.Status().Update(ctx, clusterReg)
        return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
    }
    r.updateCondition(clusterReg, "ClusterRegistered", "True", "Cluster secret created")

    // 6. Post-registration tasks
    if clusterReg.Spec.Config.CreateNamespace != "" {
        if err := r.createNamespaceInCluster(ctx, clusterName, clusterReg.Spec.Config.CreateNamespace); err != nil {
            r.updateCondition(clusterReg, "NamespaceCreated", "False", err.Error())
            // Don't fail completely, just log warning
        } else {
            r.updateCondition(clusterReg, "NamespaceCreated", "True", "Namespace created successfully")
        }
    }

    // 7. Update apps with topology policy
    if err := r.updateAppsWithTopologyPolicy(ctx, clusterReg); err != nil {
        // Log warning but don't fail
    }

    // 8. Update status to Ready
    clusterReg.Status.Phase = "Ready"
    clusterReg.Status.ClusterInfo = r.extractClusterInfo(kubeConfig)
    clusterReg.Status.LastReconcileTime = metav1.Now()
    clusterReg.Status.LastHeartbeatTime = metav1.Now()
    r.Status().Update(ctx, clusterReg)

    // 9. Requeue for periodic heartbeat
    return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// Helper functions

func (r *ClusterRegistrationReconciler) loadCredentials(ctx context.Context, clusterReg *multiclusterv1alpha1.ClusterRegistration) (*KubeConfig, error) {
    source := clusterReg.Spec.CredentialSource

    if source.SecretRef != nil {
        // Load kubeconfig from secret
        secret := &corev1.Secret{}
        key := client.ObjectKey{
            Name:      source.SecretRef.Name,
            Namespace: source.SecretRef.Namespace,
        }
        if err := r.Get(ctx, key, secret); err != nil {
            return nil, err
        }

        kubeconfigData := secret.Data[source.SecretRef.Key]
        return multicluster.ParseKubeConfig(kubeconfigData)
    }

    if source.Inline != nil {
        // Use inline credentials
        return &KubeConfig{
            Endpoint:       source.Inline.Endpoint,
            CAData:         source.Inline.CAData,
            TokenData:      source.Inline.TokenData,
            ClientCertData: source.Inline.ClientCertData,
            ClientKeyData:  source.Inline.ClientKeyData,
        }, nil
    }

    return nil, fmt.Errorf("no credential source specified")
}

func (r *ClusterRegistrationReconciler) validateConnection(ctx context.Context, kubeConfig *KubeConfig) error {
    // Create client to remote cluster
    remoteClient, err := kubeConfig.ToRESTConfig()
    if err != nil {
        return err
    }

    // Test connectivity with timeout
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    // Try to get server version
    discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(remoteClient)
    _, err = discoveryClient.ServerVersion()
    return err
}

func (r *ClusterRegistrationReconciler) createClusterSecret(ctx context.Context, clusterName string, kubeConfig *KubeConfig, clusterReg *multiclusterv1alpha1.ClusterRegistration) (*corev1.Secret, error) {
    // Reuse existing logic from cluster_management.go
    clusterConfig := &multicluster.KubeClusterConfig{
        ClusterName:     clusterName,
        CreateNamespace: clusterReg.Spec.Config.CreateNamespace,
        Config:          kubeConfig.RawConfig,
        Cluster:         kubeConfig.Cluster,
        AuthInfo:        kubeConfig.AuthInfo,
    }

    return clusterConfig.CreateOrUpdateClusterSecret(ctx, r.Client)
}

func (r *ClusterRegistrationReconciler) handleDeletion(ctx context.Context, clusterReg *multiclusterv1alpha1.ClusterRegistration) (ctrl.Result, error) {
    // Delete cluster secret
    clusterName := clusterReg.Spec.ClusterName
    if clusterName == "" {
        clusterName = clusterReg.Name
    }

    secret := &corev1.Secret{}
    key := client.ObjectKey{
        Name:      clusterName,
        Namespace: multicluster.ClusterGatewaySecretNamespace,
    }

    if err := r.Delete(ctx, secret); client.IgnoreNotFound(err) != nil {
        return ctrl.Result{}, err
    }

    // Remove finalizer
    controllerutil.RemoveFinalizer(clusterReg, "multicluster.oam.dev/cluster-registration")
    return ctrl.Result{}, r.Update(ctx, clusterReg)
}

func (r *ClusterRegistrationReconciler) updateCondition(clusterReg *multiclusterv1alpha1.ClusterRegistration, condType, status, message string) {
    condition := metav1.Condition{
        Type:               condType,
        Status:             metav1.ConditionStatus(status),
        Reason:             condType + "Updated",
        Message:            message,
        LastTransitionTime: metav1.Now(),
    }

    meta.SetStatusCondition(&clusterReg.Status.Conditions, condition)
}

// SetupWithManager sets up the controller with the Manager
func (r *ClusterRegistrationReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&multiclusterv1alpha1.ClusterRegistration{}).
        Owns(&corev1.Secret{}).  // Watch owned secrets
        Complete(r)
}
```

---

## Implementation Feasibility

### Phase 1: Foundation (2-3 weeks)

**Tasks:**
1. **Define API (CRD)**
   - Create `apis/multicluster/v1alpha1/clusterregistration_types.go`
   - Add validation webhooks
   - Generate CRD manifests
   - **Effort:** 3-5 days

2. **Create Controller Scaffold**
   - Set up controller structure
   - Implement basic reconciliation loop
   - Add logging and metrics
   - **Effort:** 2-3 days

3. **Credential Loading**
   - Implement secretRef loading
   - Support inline credentials
   - Handle different auth types
   - **Effort:** 3-4 days

4. **Reuse Existing Logic**
   - Extract shared code from `pkg/multicluster/cluster_management.go`
   - Refactor into reusable packages
   - **Effort:** 2-3 days

---

### Phase 2: Core Functionality (2-3 weeks)

**Tasks:**
1. **Validation Logic**
   - Port validation from CLI
   - Add connectivity tests
   - Implement credential verification
   - **Effort:** 3-4 days

2. **Secret Creation**
   - Reuse `createOrUpdateClusterSecret()`
   - Add ownerReference management
   - Handle label/annotation propagation
   - **Effort:** 2-3 days

3. **Post-Registration Tasks**
   - Namespace creation with retry
   - Cluster version detection
   - App redeployment trigger
   - **Effort:** 3-4 days

4. **Status Management**
   - Condition updates
   - Phase transitions
   - Error reporting
   - **Effort:** 2-3 days

---

### Phase 3: Advanced Features (2-3 weeks)

**Tasks:**
1. **Exec Provider Support**
   - Port `getTokenFromExec()` logic
   - Add token caching
   - Implement refresh mechanism
   - **Effort:** 3-4 days

2. **External Secrets Integration**
   - Support External Secrets Operator
   - Add watch for external secret changes
   - **Effort:** 2-3 days

3. **Heartbeat Monitoring**
   - Periodic connectivity checks
   - Status degradation on failure
   - Alerting integration
   - **Effort:** 2-3 days

4. **Webhooks**
   - Validation webhook (name, duplicate check)
   - Mutating webhook (defaults, normalization)
   - **Effort:** 2-3 days

---

### Phase 4: Testing and Documentation (1-2 weeks)

**Tasks:**
1. **Unit Tests**
   - Controller logic tests
   - Credential loading tests
   - Validation tests
   - **Effort:** 3-4 days

2. **Integration Tests**
   - End-to-end cluster registration
   - Multi-auth type scenarios
   - Failure recovery tests
   - **Effort:** 3-4 days

3. **Documentation**
   - User guide (how to use CR)
   - Migration guide (from CLI/Secret)
   - API reference
   - **Effort:** 2-3 days

---

### Total Effort Estimate

| Phase | Duration | Team Size | Total Effort |
|-------|----------|-----------|--------------|
| Phase 1: Foundation | 2-3 weeks | 1-2 engineers | 15-20 person-days |
| Phase 2: Core | 2-3 weeks | 1-2 engineers | 15-20 person-days |
| Phase 3: Advanced | 2-3 weeks | 1-2 engineers | 15-20 person-days |
| Phase 4: Testing | 1-2 weeks | 1-2 engineers | 10-15 person-days |
| **Total** | **7-11 weeks** | **1-2 engineers** | **55-75 person-days** |

---

### Technical Feasibility: High ✅

**Reasons:**
1. **Reusable Code:** 70% of logic already exists in `pkg/multicluster/cluster_management.go`
2. **Proven Patterns:** Controller-runtime provides scaffolding and best practices
3. **Backward Compatible:** Can coexist with existing Secret-based approach
4. **No Breaking Changes:** Existing clusters continue working
5. **Well-Defined Scope:** Clear requirements and boundaries

---

### Risk Assessment

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| Backward compatibility issues | Low | High | Thorough testing, maintain Secret discovery |
| Performance with large cluster counts | Medium | Medium | Implement efficient caching, rate limiting |
| Exec provider complexity | Medium | Medium | Reuse existing CLI logic, thorough testing |
| External Secrets integration | Low | Low | Well-documented ESO API, community support |
| Migration complexity | Low | Medium | Provide migration tooling, clear documentation |

---

## Migration Path

### Strategy: Gradual Adoption

KubeVela will support **three cluster registration methods** simultaneously:

1. **CLI (`vela join`)** - Existing, continues to work
2. **Manual Secret** - Existing, continues to work
3. **ClusterRegistration CR** - New, recommended for GitOps

---

### Migration Steps

#### Step 1: Deploy New Controller (Day 1)

```bash
# Update KubeVela to version with ClusterRegistration CRD
helm upgrade vela-core vela-core/vela-core --version v1.x.x

# Verify CRD installed
kubectl get crd clusterregistrations.multicluster.oam.dev
```

---

#### Step 2: Convert Existing Secrets to CRs (Optional)

**Migration Tool: `vela cluster convert`**

```bash
# List all registered clusters
vela cluster list

# Convert specific cluster to CR
vela cluster convert prod-west --output yaml > prod-west-cr.yaml

# Apply CR
kubectl apply -f prod-west-cr.yaml
```

**Generated CR:**
```yaml
apiVersion: multicluster.oam.dev/v1alpha1
kind: ClusterRegistration
metadata:
  name: prod-west-cluster
  namespace: vela-system
spec:
  clusterName: prod-west
  credentialSource:
    inline:
      endpoint: https://api.prod-west.example.com:6443
      credentialType: ServiceAccountToken
      # Credentials copied from existing secret
      caData: LS0tLS...
      tokenData: ZXlKaG...
  config:
    createNamespace: vela-system
    validateCredentials: false  # Skip validation for existing clusters
  labels:
    migrated-from: secret
```

**Controller Behavior:**
- Detects existing cluster secret
- Takes ownership (adds ownerReference)
- Skips recreation (no downtime)
- Updates status based on existing state

---

#### Step 3: New Clusters Use CR (Day 1+)

```bash
# Create secret with kubeconfig
kubectl create secret generic staging-kubeconfig \
  --from-file=kubeconfig=./staging.kubeconfig \
  -n default

# Create ClusterRegistration
cat <<EOF | kubectl apply -f -
apiVersion: multicluster.oam.dev/v1alpha1
kind: ClusterRegistration
metadata:
  name: staging-cluster
  namespace: vela-system
spec:
  clusterName: staging
  credentialSource:
    secretRef:
      name: staging-kubeconfig
      namespace: default
      key: kubeconfig
  config:
    createNamespace: vela-system
    validateCredentials: true
  labels:
    env: staging
EOF
```

---

#### Step 4: GitOps Integration

**Flux Example:**

```yaml
# clusters/production/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - cluster-registration.yaml

# Flux will sync this to hub cluster
---
apiVersion: multicluster.oam.dev/v1alpha1
kind: ClusterRegistration
metadata:
  name: prod-cluster
  namespace: vela-system
spec:
  clusterName: production
  credentialSource:
    externalSecretRef:
      name: prod-cluster-credentials
      provider: aws-secrets-manager
  config:
    validateCredentials: true
  labels:
    managed-by: flux
    env: production
```

**ArgoCD Example:**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: cluster-registrations
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/org/kubevela-clusters
    targetRevision: main
    path: clusters
  destination:
    server: https://kubernetes.default.svc
    namespace: vela-system
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

---

### Backward Compatibility Guarantee

**Controller Design:**
```go
// In virtual_cluster.go:251-276
func FindVirtualClustersByLabels(ctx context.Context, c client.Client, labels map[string]string) ([]VirtualCluster, error) {
    // Method 1: Find from Secrets (existing)
    secretList := &corev1.SecretList{}
    // ... existing logic ...

    // Method 2: Find from ClusterRegistration CRs (new)
    crList := &multiclusterv1alpha1.ClusterRegistrationList{}
    c.List(ctx, crList, &client.ListOptions{Namespace: ClusterGatewaySecretNamespace})

    // Deduplicate: CR-managed secrets take precedence
    // Return merged list
}
```

**Result:** Clusters registered via any method are discovered and usable.

---

## Comparison Matrix

### Feature Comparison

| Feature | CLI (`vela join`) | Manual Secret | ClusterRegistration CR |
|---------|-------------------|---------------|------------------------|
| **Credential Extraction** | ✅ Automatic | ❌ Manual | ✅ Automatic |
| **Validation** | ✅ Pre-flight checks | ❌ None | ✅ Configurable |
| **Post-Registration Tasks** | ✅ Namespace, version, apps | ❌ None | ✅ Namespace, version, apps |
| **Exec Provider Support** | ✅ Yes | ❌ No | ✅ Yes (future) |
| **Status Reporting** | ❌ CLI output only | ❌ No | ✅ Kubernetes status |
| **GitOps Friendly** | ❌ Imperative | ⚠️ Secrets in Git | ✅ Declarative CR |
| **External Secrets** | ❌ No | ⚠️ Manual | ✅ ESO integration |
| **Audit Trail** | ❌ No | ❌ No | ✅ CR history |
| **Lifecycle Management** | ⚠️ CLI commands | ❌ Manual kubectl | ✅ CR updates |
| **Error Recovery** | ❌ Manual retry | ❌ Manual | ✅ Automatic retry |
| **Heartbeat Monitoring** | ❌ No | ❌ No | ✅ Yes (future) |
| **Label Management** | ✅ CLI flags | ⚠️ Manual patch | ✅ CR spec |
| **Cluster Renaming** | ✅ `vela cluster rename` | ❌ Secret rename | ✅ Update spec |
| **Duplicate Prevention** | ✅ Prompt | ❌ No | ✅ Validation webhook |
| **Learning Curve** | Low | Medium | Low-Medium |

---

### Use Case Recommendations

| Use Case | Recommended Method | Reason |
|----------|-------------------|--------|
| **Quick testing** | CLI | Fastest, immediate feedback |
| **Manual one-off** | CLI | Interactive, handles edge cases |
| **GitOps/IaC** | ClusterRegistration CR | Declarative, version controlled |
| **CI/CD pipeline** | ClusterRegistration CR | Automated, status checking |
| **Large-scale deployments** | ClusterRegistration CR | Standardized, auditable |
| **Legacy compatibility** | Manual Secret | No changes needed |
| **Cloud provider auth** | CLI or CR (future) | Exec provider support |
| **Sensitive environments** | CR + External Secrets | No secrets in Git |

---

## Implementation Roadmap

### Milestone 1: MVP (8 weeks)

**Deliverables:**
- ClusterRegistration CRD
- Basic controller (credential loading, secret creation)
- Status reporting
- Documentation

**Success Criteria:**
- Can register clusters via CR
- Existing Secret-based clusters continue working
- Status shows Ready/Failed state

---

### Milestone 2: Feature Parity (12 weeks)

**Deliverables:**
- All CLI features (validation, namespace creation, app updates)
- Migration tooling (`vela cluster convert`)
- Comprehensive testing

**Success Criteria:**
- Feature parity with `vela join` CLI
- Zero downtime migration path
- 90%+ test coverage

---

### Milestone 3: Advanced Features (16 weeks)

**Deliverables:**
- Exec provider support
- External Secrets integration
- Heartbeat monitoring
- Validation/mutating webhooks

**Success Criteria:**
- Supports all authentication methods
- GitOps-friendly with secret management
- Production-ready reliability

---

## Conclusion

### Key Findings

1. **Kubernetes-Native Approach EXISTS:** KubeVela already supports cluster registration via manual Secret creation, but it lacks automation and validation.

2. **CRD-Based Approach is FEASIBLE:** Implementing ClusterRegistration CR with a controller is technically viable with moderate effort (7-11 weeks).

3. **High Value Proposition:**
   - Declarative, GitOps-friendly
   - Automated credential extraction and validation
   - Status reporting and error handling
   - External secrets integration
   - Backward compatible

4. **Recommended Path:** Implement ClusterRegistration CR while maintaining backward compatibility with existing CLI and Secret-based approaches.

---

### Next Steps

1. **RFC/Proposal:** Submit design proposal to KubeVela community
2. **Prototype:** Build MVP controller (4 weeks)
3. **Community Feedback:** Gather input, iterate on design
4. **Implementation:** Full implementation per roadmap
5. **Documentation:** User guide, migration guide, examples
6. **Release:** Include in next minor version (v1.x.0)

---

**Document Version:** 1.0
**Created:** 2026-02-06
**Status:** Proposal / Feasibility Study
**Recommended Action:** Proceed with implementation
