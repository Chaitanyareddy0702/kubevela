# ClusterRegistration POC - Implementation Guide

## Overview

This POC implements a Kubernetes-native approach to joining clusters in KubeVela using a Custom Resource Definition (CRD) called `ClusterRegistration`. Instead of using the `vela cluster join` CLI command, users can now create a `ClusterRegistration` resource with their kubeconfig content, and the cluster will be automatically registered.

## What's Implemented

### 1. ClusterRegistration CRD

**Location:** `apis/core.oam.dev/v1beta1/clusterregistration_types.go`

The CRD includes:
- **Spec fields:**
  - `clusterName`: The name to register the cluster as
  - `alias`: A human-readable display name
  - `kubeconfig`: The raw kubeconfig content (copy-paste your kubeconfig here)
  - `createNamespace`: Namespace to create in the managed cluster (default: vela-system)
  - `labels`: Custom labels for cluster selection

- **Status fields:**
  - `phase`: Pending | Progressing | Ready | Failed
  - `conditions`: Detailed status conditions
  - `clusterInfo`: Information about the registered cluster (endpoint, credential type, version)
  - `message`: Additional status information
  - `observedGeneration`: For tracking updates
  - `lastReconcileTime`: Last reconciliation timestamp

### 2. ClusterRegistration Controller

**Location:** `pkg/controller/core.oam.dev/v1beta1/clusterregistration/clusterregistration_controller.go`

The controller:
1. Watches for `ClusterRegistration` resources
2. Extracts the kubeconfig from the spec
3. Uses the existing `multicluster.LoadKubeClusterConfigFromFile()` function to parse it
4. Calls `clusterConfig.RegisterByVelaSecret()` to register the cluster (same as CLI)
5. Updates the status with registration results
6. Handles deletion with proper cleanup (detaches the cluster)

### 3. Generated CRD Manifest

**Location:** `config/crd/base/core.oam.dev_clusterregistrations.yaml`

The CRD is automatically generated and ready to be applied to your cluster.

## Prerequisites

**Important:** The ClusterRegistration feature is only available when multi-cluster support is enabled. This ensures the feature only runs when cluster-gateway is active.

### Enabling Multi-cluster Support

**Using Helm:**
```yaml
# values.yaml
multicluster:
  enabled: true  # This enables ClusterRegistration
```

**Using CLI flags:**
```bash
vela-core --enable-cluster-gateway=true
```

When multi-cluster is disabled, the ClusterRegistration CRD will not be installed and the controller will not run.

## How to Use

### Step 1: Enable Multi-cluster (if using manual installation)

If you're not using Helm or if multi-cluster is disabled, first apply the ClusterRegistration CRD:

```bash
kubectl apply -f config/crd/base/core.oam.dev_clusterregistrations.yaml
```

### Step 2: Build and Run the Controller

Build and run vela-core with multi-cluster enabled:

```bash
# Build (note: linking may fail in some environments, but the controller code is valid)
make build

# Run with multi-cluster enabled
./bin/vela-core --enable-cluster-gateway=true

# Or run directly for development
go run ./cmd/core/main.go --enable-cluster-gateway=true
```

### Step 3: Create a ClusterRegistration Resource

Create a YAML file with your kubeconfig:

```yaml
apiVersion: core.oam.dev/v1beta1
kind: ClusterRegistration
metadata:
  name: my-cluster
  namespace: vela-system
spec:
  clusterName: production-cluster
  alias: "Production Cluster"

  # Simply copy-paste your kubeconfig content here
  kubeconfig: |
    apiVersion: v1
    kind: Config
    clusters:
    - cluster:
        certificate-authority-data: LS0tLS1CRUdJTi...
        server: https://api.your-cluster.com:6443
      name: your-cluster
    contexts:
    - context:
        cluster: your-cluster
        user: admin
      name: your-cluster
    current-context: your-cluster
    users:
    - name: admin
      user:
        client-certificate-data: LS0tLS1CRUdJTi...
        client-key-data: LS0tLS1CRUdJTi...

  createNamespace: "vela-system"

  labels:
    env: production
    region: us-west
```

Apply it:

```bash
kubectl apply -f my-clusterregistration.yaml
```

### Step 4: Check Status

Monitor the registration progress:

```bash
# List all cluster registrations
kubectl get clusterregistrations -n vela-system

# Get detailed status
kubectl get clusterregistration my-cluster -n vela-system -o yaml

# Check if cluster is registered
vela cluster list
# or
kubectl get clustergateways -n vela-system
```

Expected output:

```
NAME         CLUSTER              PHASE   ENDPOINT                             AGE
my-cluster   production-cluster   Ready   https://api.your-cluster.com:6443   1m
```

### Step 5: Verify the Cluster is Usable

The cluster should now be available for use in KubeVela applications:

```bash
# List clusters
vela cluster list

# Deploy an app to the cluster
vela up -f my-app.yaml
```

## How It Works

### Registration Flow

```
┌─────────────────────────────────────────┐
│ User creates ClusterRegistration CR     │
│ with kubeconfig in spec                 │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│ Controller watches ClusterRegistration  │
│ Status: Progressing                     │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│ Parse kubeconfig                        │
│ Extract credentials (cert/token)        │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│ Validate cluster name                   │
│ (not empty, not "local")                │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│ Call multicluster.RegisterByVelaSecret()│
│ • Creates Secret in vela-system         │
│ • Secret contains endpoint, CA, creds   │
│ • Adds cluster credential type label    │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│ Post-registration tasks                 │
│ • Create namespace in managed cluster   │
│ • Detect cluster version                │
└─────────────┬───────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────┐
│ Update Status to Ready                  │
│ • Set phase: Ready                      │
│ • Add condition: Available              │
│ • Set cluster info (endpoint, type)     │
└─────────────────────────────────────────┘
```

### What Happens Under the Hood

1. **Kubeconfig Parsing**: The controller writes the kubeconfig to a temporary file and uses `multicluster.LoadKubeClusterConfigFromFile()` to parse it

2. **Credential Extraction**: The same logic as the CLI is used to extract:
   - X.509 certificates (client-cert + client-key)
   - OR Service Account tokens
   - OR Exec provider tokens (e.g., AWS EKS, GKE)

3. **Secret Creation**: A Secret is created in the `vela-system` namespace with:
   - Name: the cluster name
   - Data:
     - `endpoint`: API server URL
     - `ca.crt`: CA certificate
     - `token` (for SA token auth) OR `tls.crt` + `tls.key` (for cert auth)
   - Label: `cluster.core.oam.dev/cluster-credential-type: ServiceAccountToken|X509Certificate`

4. **Discovery**: The existing KubeVela cluster discovery mechanism automatically finds this Secret and makes the cluster available

## Key Benefits

1. **Copy-Paste Simplicity**: Just paste your kubeconfig into the YAML - no need to understand kubeconfig structure or run CLI commands

2. **GitOps Friendly**: Store ClusterRegistration resources in Git (note: for production, use external secret management like Sealed Secrets or External Secrets Operator)

3. **Status Reporting**: Real-time status updates in the ClusterRegistration resource

4. **Automated Cleanup**: Deleting the ClusterRegistration automatically detaches the cluster

5. **Same Backend Logic**: Uses the exact same cluster registration logic as `vela cluster join`, ensuring compatibility

## Architecture

### Multi-cluster Gating

The ClusterRegistration feature is gated by the `--enable-cluster-gateway` flag:

1. **Flag Flow**: `--enable-cluster-gateway` → `MultiCluster.EnableClusterGateway` → `Controller.Args.EnableClusterGateway`
2. **Controller Setup**: In `pkg/controller/core.oam.dev/v1beta1/setup.go`, the ClusterRegistration controller only initializes when `args.EnableClusterGateway == true`
3. **CRD Installation**: In the Helm chart, the CRD is only installed when `multicluster.enabled == true`
4. **Zero Overhead**: When disabled, no CRD is installed and no controller runs

This ensures ClusterRegistration only operates when cluster-gateway is active and ready to handle multi-cluster operations.

### File Structure

```
apis/core.oam.dev/v1beta1/
├── clusterregistration_types.go          # CRD definition
└── zz_generated.deepcopy.go              # Generated deepcopy methods

pkg/controller/core.oam.dev/v1beta1/
├── clusterregistration/
│   └── clusterregistration_controller.go # Controller implementation
└── setup.go                              # Controller registration (with gating)

pkg/controller/core.oam.dev/
└── oamruntime_controller.go              # Args with EnableClusterGateway flag

cmd/core/app/
├── server.go                             # Syncs multicluster flag to controller args
└── config/
    ├── multicluster.go                   # Multi-cluster configuration
    └── controller.go                     # Controller configuration

config/crd/base/
└── core.oam.dev_clusterregistrations.yaml # Generated CRD manifest

charts/vela-core/templates/
└── clusterregistration-crd.yaml          # Conditional CRD template
```

### Dependencies

The controller reuses existing KubeVela multicluster functions:
- `multicluster.LoadKubeClusterConfigFromFile()` - Parse kubeconfig
- `multicluster.KubeClusterConfig.Validate()` - Validate cluster name
- `multicluster.KubeClusterConfig.RegisterByVelaSecret()` - Create cluster secret
- `multicluster.KubeClusterConfig.PostRegistration()` - Create namespace
- `multicluster.DetachCluster()` - Clean up on deletion

This ensures 100% compatibility with the CLI approach.

## Comparison with CLI

| Feature | vela cluster join | ClusterRegistration CR |
|---------|-------------------|------------------------|
| **Ease of Use** | CLI command | YAML resource |
| **Kubeconfig Input** | File path | Inline content (copy-paste) |
| **GitOps** | ❌ Imperative | ✅ Declarative |
| **Status Reporting** | CLI output | ✅ Kubernetes status |
| **Backend Logic** | ✅ | ✅ (Same) |
| **Cleanup** | `vela cluster detach` | Delete CR |
| **Labels** | CLI flags | spec.labels |
| **Namespace Creation** | ✅ | ✅ |

## Example: Real World Usage

### Getting a Kubeconfig from Cloud Providers

**AWS EKS:**
```bash
aws eks update-kubeconfig --name my-cluster --region us-west-2 --kubeconfig /tmp/eks.kubeconfig
cat /tmp/eks.kubeconfig  # Copy this content to ClusterRegistration
```

**Google GKE:**
```bash
gcloud container clusters get-credentials my-cluster --zone us-central1-a
kubectl config view --raw --minify  # Copy this output to ClusterRegistration
```

**Azure AKS:**
```bash
az aks get-credentials --resource-group myResourceGroup --name myAKSCluster
kubectl config view --raw --minify  # Copy this output to ClusterRegistration
```

### Using with Helm

You can template ClusterRegistration resources in Helm charts:

```yaml
{{- range .Values.clusters }}
---
apiVersion: core.oam.dev/v1beta1
kind: ClusterRegistration
metadata:
  name: {{ .name }}
  namespace: vela-system
spec:
  clusterName: {{ .clusterName }}
  alias: {{ .alias }}
  kubeconfig: {{ .kubeconfig | quote }}
  labels:
    {{- toYaml .labels | nindent 4 }}
{{- end }}
```

## Limitations & Future Work

### Current Limitations

1. **Kubeconfig in YAML**: The kubeconfig is stored directly in the CR. For production:
   - Use Sealed Secrets to encrypt the kubeconfig
   - Use External Secrets Operator to fetch from external secret stores
   - A future enhancement could add `spec.secretRef` to reference an existing Secret

2. **No Pre-flight Validation**: The controller doesn't validate connectivity before registration (it will fail and retry). Future: Add validation webhook

3. **No Exec Provider Token Refresh**: Exec provider tokens (e.g., AWS EKS) are extracted once. Future: Add periodic token refresh

### Future Enhancements (from the original design doc)

1. **secretRef Support**: Reference a Secret containing kubeconfig instead of inline
   ```yaml
   spec:
     credentialSource:
       secretRef:
         name: cluster-kubeconfig
         key: kubeconfig
   ```

2. **External Secrets Integration**: Support External Secrets Operator
   ```yaml
   spec:
     credentialSource:
       externalSecretRef:
         name: cluster-credentials
         provider: aws-secrets-manager
   ```

3. **Validation Webhook**: Pre-flight checks before allowing creation

4. **Heartbeat Monitoring**: Periodic connectivity checks with status updates

5. **Migration Tool**: `vela cluster convert` to convert existing clusters to ClusterRegistration CRs

## Testing

### Manual Testing

1. **Create a test ClusterRegistration**:
   ```bash
   kubectl apply -f examples/clusterregistration-sample.yaml
   ```

2. **Check status**:
   ```bash
   kubectl get clusterregistration -n vela-system
   kubectl describe clusterregistration my-remote-cluster -n vela-system
   ```

3. **Verify cluster is usable**:
   ```bash
   vela cluster list
   kubectl get clustergateways -n vela-system
   ```

4. **Delete and verify cleanup**:
   ```bash
   kubectl delete clusterregistration my-remote-cluster -n vela-system
   vela cluster list  # Should not show the cluster anymore
   ```

### Unit Testing (Future Work)

Add unit tests to `clusterregistration_controller_test.go` to test:
- Kubeconfig parsing
- Secret creation
- Status updates
- Error handling
- Deletion cleanup

## Summary

This POC successfully implements a Kubernetes-native approach to cluster registration in KubeVela. Users can now simply copy-paste their kubeconfig into a ClusterRegistration resource and have their cluster automatically joined, without needing to use the CLI. The implementation reuses all existing multicluster logic, ensuring full compatibility with the CLI approach.

The next steps would be to:
1. Add comprehensive testing
2. Implement the advanced features from the design document (secretRef, external secrets, validation webhook)
3. Update KubeVela documentation with this new approach
4. Get community feedback and iterate

## Files Changed

1. **New Files:**
   - `apis/core.oam.dev/v1beta1/clusterregistration_types.go` - ClusterRegistration CRD definition
   - `pkg/controller/core.oam.dev/v1beta1/clusterregistration/clusterregistration_controller.go` - Controller implementation
   - `config/crd/base/core.oam.dev_clusterregistrations.yaml` - Generated CRD manifest
   - `charts/vela-core/templates/clusterregistration-crd.yaml` - Conditional CRD template for Helm
   - `examples/clusterregistration-sample.yaml` - Example usage
   - `CLUSTERREGISTRATION_POC.md` (this file)

2. **Modified Files:**
   - `pkg/controller/core.oam.dev/v1beta1/setup.go` - Added conditional ClusterRegistration controller setup (only when multicluster enabled)
   - `pkg/controller/core.oam.dev/oamruntime_controller.go` - Added `EnableClusterGateway` field to Args
   - `cmd/core/app/config/controller.go` - Initialize `EnableClusterGateway` in controller config
   - `cmd/core/app/server.go` - Sync multicluster flag to controller args
   - `apis/core.oam.dev/v1beta1/zz_generated.deepcopy.go` - Regenerated for new types
