# Testing ClusterRegistration Feature Locally

This guide provides comprehensive instructions for testing the ClusterRegistration feature locally using Helm charts.

## Overview

The ClusterRegistration feature enables Kubernetes-native cluster joining in KubeVela. Instead of using the `vela cluster join` CLI command, users can create a `ClusterRegistration` custom resource with kubeconfig content, and the cluster will be automatically registered.

**Key Requirements:**
- cluster-gateway must be running for ClusterRegistration to work
- Multi-cluster support must be enabled (`multicluster.enabled: true`)
- The ClusterRegistration controller only runs when `--enable-cluster-gateway=true`

## Testing Approaches

There are two main approaches for testing:

1. **[Recommended] Helm Chart Installation** - Complete deployment with cluster-gateway
2. **Manual Installation** - For faster iterative development

---

## Approach 1: Helm Chart Installation (Recommended)

This approach installs everything you need (vela-core + cluster-gateway + CRDs) in one command.

### Prerequisites

- A Kubernetes cluster (kind, minikube, k3s, or any cluster)
- Docker installed
- Helm 3 installed
- kubectl configured to access your cluster

### Step 1: Build Custom Docker Image

Build a Docker image containing your ClusterRegistration feature:

```bash
# From the kubevela root directory
make docker-build IMG=oamdev/vela-core:test-clusterreg
```

**For local clusters (kind/minikube), load the image:**

```bash
# For kind:
kind load docker-image oamdev/vela-core:test-clusterreg

# For minikube:
eval $(minikube docker-env)
make docker-build IMG=oamdev/vela-core:test-clusterreg

# For k3s/k3d:
k3d image import oamdev/vela-core:test-clusterreg
```

**For remote clusters or Docker Hub:**

```bash
# Tag and push to your registry
docker tag oamdev/vela-core:test-clusterreg <your-registry>/vela-core:test-clusterreg
docker push <your-registry>/vela-core:test-clusterreg
```

### Step 2: Create Custom values.yaml

Create a `test-values.yaml` file to override default settings:

```yaml
# test-values.yaml

# Use your custom image
image:
  repository: oamdev/vela-core  # or <your-registry>/vela-core
  tag: test-clusterreg
  pullPolicy: IfNotPresent  # Important for local images

# Enable multi-cluster (this installs cluster-gateway + ClusterRegistration CRD)
multicluster:
  enabled: true  # Default is true, but being explicit
  clusterGateway:
    replicaCount: 1
    image:
      repository: oamdev/cluster-gateway
      tag: v1.9.0-alpha.2
      pullPolicy: IfNotPresent
    resources:
      requests:
        cpu: 50m
        memory: 20Mi
      limits:
        cpu: 500m
        memory: 200Mi

# Enable detailed logging for debugging
logDebug: true
devLogs: true

# Optional: Reduce resource requests for local testing
resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 50m
    memory: 20Mi
```

### Step 3: Install the Helm Chart

```bash
# Create namespace
kubectl create namespace vela-system

# Install the chart
helm install vela-core ./charts/vela-core \
  --namespace vela-system \
  --values test-values.yaml \
  --wait \
  --timeout 5m

# Or if already installed, upgrade:
helm upgrade vela-core ./charts/vela-core \
  --namespace vela-system \
  --values test-values.yaml \
  --wait \
  --timeout 5m
```

### Step 4: Verify Installation

Run these checks to ensure everything is working:

```bash
# 1. Check that vela-core pod is running with your custom image
kubectl get pods -n vela-system
kubectl describe pod -n vela-system -l app.kubernetes.io/name=vela-core | grep Image:

# Expected output should show your image:
# Image: oamdev/vela-core:test-clusterreg

# 2. Check that cluster-gateway is running
kubectl get pods -n vela-system -l app=cluster-gateway

# Expected output:
# NAME                               READY   STATUS    RESTARTS   AGE
# cluster-gateway-xxxxxxxxxx-xxxxx   1/1     Running   0          1m

# 3. Verify cluster-gateway APIService is available
kubectl get apiservice v1alpha1.cluster.core.oam.dev

# Expected output:
# NAME                              SERVICE                        AVAILABLE   AGE
# v1alpha1.cluster.core.oam.dev     vela-system/cluster-gateway    True        1m

# 4. Check that ClusterRegistration CRD was installed
kubectl get crd clusterregistrations.core.oam.dev

# 5. Verify vela-core started the ClusterRegistration controller
kubectl logs -n vela-system -l app.kubernetes.io/name=vela-core --tail=50 | grep -i "clusterregistration\|cluster-gateway"

# Expected output should show:
# - "waiting for cluster gateway service" followed by "cluster gateway service is ready"
# - Controller setup logs
```

### Step 5: Create a Test ClusterRegistration

Now test the feature by creating a ClusterRegistration resource:

```bash
# Get your current cluster's kubeconfig
kubectl config view --raw --minify > /tmp/test-kubeconfig.yaml

# Create a ClusterRegistration
cat <<EOF | kubectl apply -f -
apiVersion: core.oam.dev/v1beta1
kind: ClusterRegistration
metadata:
  name: test-cluster
  namespace: vela-system
spec:
  clusterName: my-test-cluster
  alias: "My Test Cluster"
  kubeconfig: |
$(cat /tmp/test-kubeconfig.yaml | sed 's/^/    /')
  createNamespace: "vela-system"
  labels:
    env: test
    purpose: local-testing
    region: local
EOF
```

### Step 6: Monitor the Registration Process

Watch the ClusterRegistration as it progresses through phases:

```bash
# Watch status changes in real-time
kubectl get clusterregistration test-cluster -n vela-system -w

# Expected progression:
# NAME           CLUSTER            PHASE          ENDPOINT    AGE
# test-cluster   my-test-cluster    Progressing    ...         5s
# test-cluster   my-test-cluster    Ready          https://... 10s

# Check detailed status
kubectl describe clusterregistration test-cluster -n vela-system

# View controller logs
kubectl logs -n vela-system -l app.kubernetes.io/name=vela-core -f | grep -i clusterregistration

# Verify the cluster secret was created
kubectl get secret my-test-cluster -n vela-system

# Check secret details
kubectl get secret my-test-cluster -n vela-system -o yaml
```

### Step 7: Verify Cluster is Usable

```bash
# If you have vela CLI installed, check cluster list
vela cluster list

# Expected output:
# CLUSTER           TYPE    ENDPOINT                  ACCEPTED  LABELS
# my-test-cluster   X509    https://your-api-server   true      env=test,purpose=local-testing,region=local

# Check ClusterGateway resources
kubectl get clustergateways -n vela-system

# Try accessing the cluster through cluster-gateway
kubectl get --raw "/apis/cluster.core.oam.dev/v1alpha1/clustergateways/my-test-cluster/proxy/api/v1/namespaces"
```

### Step 8: Test Cleanup

Verify that deleting the ClusterRegistration properly cleans up:

```bash
# Delete the ClusterRegistration
kubectl delete clusterregistration test-cluster -n vela-system

# Verify the secret was removed
kubectl get secret my-test-cluster -n vela-system
# Should return: Error from server (NotFound)

# Verify ClusterGateway was removed
kubectl get clustergateways -n vela-system | grep my-test-cluster
# Should return nothing
```

### Uninstalling

To completely remove the installation:

```bash
# Uninstall the Helm release
helm uninstall vela-core -n vela-system

# Delete the namespace (optional)
kubectl delete namespace vela-system

# Remove CRDs (optional - they persist after uninstall)
kubectl delete crd clusterregistrations.core.oam.dev
```

---

## Approach 2: Manual Installation (Fast Iteration)

This approach is better for rapid development/debugging since you don't need to rebuild Docker images.

### Step 1: Install cluster-gateway

Use the provided installation script:

```bash
# From kubevela root directory
./hack/install-cluster-gateway-dev.sh

# Or install manually
kubectl apply -f https://raw.githubusercontent.com/oam-dev/cluster-gateway/master/config/crd/cluster.core.oam.dev_clustergatewayconfigurations.yaml
kubectl apply -f https://raw.githubusercontent.com/oam-dev/cluster-gateway/master/config/crd/cluster.core.oam.dev_clustergateways.yaml

# Follow the steps in the script to create deployment, service, RBAC, and APIService
```

### Step 2: Verify cluster-gateway is Ready

```bash
# Check pods
kubectl get pods -n vela-system | grep cluster-gateway

# Check APIService
kubectl get apiservice v1alpha1.cluster.core.oam.dev

# Should show AVAILABLE=True
```

### Step 3: Apply ClusterRegistration CRD

```bash
# Apply the CRD from your Helm chart templates
kubectl apply -f charts/vela-core/templates/clusterregistration-crd.yaml

# Or from generated CRD
kubectl apply -f config/crd/base/core.oam.dev_clusterregistrations.yaml

# Verify CRD is installed
kubectl get crd clusterregistrations.core.oam.dev
```

### Step 4: Run vela-core Locally

Run vela-core from source with cluster-gateway enabled:

```bash
# Using go run
go run ./cmd/core/main.go --enable-cluster-gateway=true

# Or build and run
make build
./bin/vela-core --enable-cluster-gateway=true

# Or use your IDE debugger with args: --enable-cluster-gateway=true
```

### Step 5: Test ClusterRegistration

Follow the same testing steps as in Approach 1 (Steps 5-8).

### Advantages of Manual Installation

- **Faster iteration**: No Docker build/push/pull cycle
- **Easier debugging**: Direct access to logs and debugger
- **Live reload**: Can restart the controller quickly
- **Code changes**: Immediate effect without image rebuilds

### Disadvantages of Manual Installation

- **Not production-like**: Doesn't test the full Helm deployment
- **Manual setup**: More steps to get started
- **Environment differences**: Running locally vs in-cluster

---

## Testing Scenarios

### Scenario 1: Basic Registration

Test registering a cluster with minimal configuration:

```yaml
apiVersion: core.oam.dev/v1beta1
kind: ClusterRegistration
metadata:
  name: basic-cluster
  namespace: vela-system
spec:
  clusterName: basic-test
  kubeconfig: |
    # Your kubeconfig here
```

**Expected result:** Cluster registers successfully with phase: Ready

### Scenario 2: Registration with Labels

Test cluster registration with custom labels:

```yaml
apiVersion: core.oam.dev/v1beta1
kind: ClusterRegistration
metadata:
  name: labeled-cluster
  namespace: vela-system
spec:
  clusterName: production-east
  alias: "Production East Coast"
  kubeconfig: |
    # Your kubeconfig here
  labels:
    env: production
    region: us-east-1
    tier: frontend
```

**Expected result:** Labels appear in the cluster secret and can be used for cluster selection

### Scenario 3: Invalid Kubeconfig

Test error handling with invalid kubeconfig:

```yaml
apiVersion: core.oam.dev/v1beta1
kind: ClusterRegistration
metadata:
  name: invalid-cluster
  namespace: vela-system
spec:
  clusterName: invalid-test
  kubeconfig: "invalid yaml content"
```

**Expected result:** Phase: Failed, status.message contains error details

### Scenario 4: Reserved Cluster Name

Test validation for reserved cluster names:

```yaml
apiVersion: core.oam.dev/v1beta1
kind: ClusterRegistration
metadata:
  name: local-cluster
  namespace: vela-system
spec:
  clusterName: local  # Reserved name
  kubeconfig: |
    # Your kubeconfig here
```

**Expected result:** Phase: Failed, status.message indicates "local" is reserved

### Scenario 5: Update ClusterRegistration

Test updating an existing ClusterRegistration:

```bash
# Create initial registration
kubectl apply -f examples/clusterregistration-sample.yaml

# Wait for Ready
kubectl wait --for=condition=Ready clusterregistration/my-cluster -n vela-system --timeout=60s

# Update labels
kubectl patch clusterregistration my-cluster -n vela-system --type=merge -p '
spec:
  labels:
    env: staging
    updated: "true"
'

# Watch for reconciliation
kubectl get clusterregistration my-cluster -n vela-system -w
```

**Expected result:** Controller reconciles and updates the cluster secret with new labels

### Scenario 6: Multi-Cluster Registration

Test registering multiple clusters simultaneously:

```bash
# Create multiple ClusterRegistrations
for i in {1..3}; do
  cat <<EOF | kubectl apply -f -
apiVersion: core.oam.dev/v1beta1
kind: ClusterRegistration
metadata:
  name: cluster-$i
  namespace: vela-system
spec:
  clusterName: test-cluster-$i
  kubeconfig: |
$(cat /tmp/test-kubeconfig.yaml | sed 's/^/    /')
  labels:
    index: "$i"
EOF
done

# Watch all registrations
kubectl get clusterregistrations -n vela-system -w
```

**Expected result:** All clusters register successfully without conflicts

---

## Troubleshooting

### Issue 1: vela-core Stuck "Waiting for cluster gateway service"

**Symptoms:**
```
kubectl logs -n vela-system -l app.kubernetes.io/name=vela-core | grep gateway
# Output: "waiting for cluster gateway service..."
```

**Diagnosis:**
```bash
# Check if cluster-gateway pod is running
kubectl get pods -n vela-system -l app=cluster-gateway

# Check APIService status
kubectl get apiservice v1alpha1.cluster.core.oam.dev -o yaml
```

**Solutions:**
- Ensure cluster-gateway pod is Running
- Check APIService shows `status.conditions[?(@.type=="Available")].status == "True"`
- Verify Service exists: `kubectl get service cluster-gateway -n vela-system`
- Check cluster-gateway logs: `kubectl logs -n vela-system -l app=cluster-gateway`

### Issue 2: ClusterRegistration Stuck in "Progressing" Phase

**Symptoms:**
```bash
kubectl get clusterregistration test-cluster -n vela-system
# PHASE shows "Progressing" for more than 30 seconds
```

**Diagnosis:**
```bash
# Check status message
kubectl get clusterregistration test-cluster -n vela-system -o jsonpath='{.status.message}'

# Check controller logs
kubectl logs -n vela-system -l app.kubernetes.io/name=vela-core | grep -A 20 "test-cluster"

# Check events
kubectl describe clusterregistration test-cluster -n vela-system
```

**Common causes:**
1. **Invalid kubeconfig format** - Fix: Validate YAML syntax
2. **Invalid cluster credentials** - Fix: Verify kubeconfig has valid certs/tokens
3. **Network connectivity issues** - Fix: Check if API server is reachable
4. **Reserved cluster name ("local")** - Fix: Use a different cluster name

### Issue 3: ClusterRegistration Controller Not Running

**Symptoms:**
```bash
# Creating ClusterRegistration has no effect
kubectl logs -n vela-system -l app.kubernetes.io/name=vela-core | grep -i clusterregistration
# Returns nothing
```

**Diagnosis:**
```bash
# Check if multicluster is enabled
kubectl get pods -n vela-system -l app.kubernetes.io/name=vela-core -o jsonpath='{.items[0].spec.containers[0].args}'
# Should contain: --enable-cluster-gateway=true
```

**Solutions:**
- Ensure `multicluster.enabled: true` in Helm values
- Verify controller was started with `--enable-cluster-gateway=true` flag
- Check that ClusterRegistration controller is registered in logs at startup

### Issue 4: Image Pull Errors

**Symptoms:**
```bash
kubectl get pods -n vela-system
# Shows ImagePullBackOff or ErrImagePull
```

**Solutions:**

For local testing:
```yaml
# In test-values.yaml, use:
image:
  pullPolicy: IfNotPresent  # or Never
```

For kind:
```bash
# Reload the image
kind load docker-image oamdev/vela-core:test-clusterreg
# Restart the pod
kubectl rollout restart deployment vela-core -n vela-system
```

For minikube:
```bash
# Rebuild within minikube's Docker
eval $(minikube docker-env)
make docker-build IMG=oamdev/vela-core:test-clusterreg
```

### Issue 5: CRD Not Found

**Symptoms:**
```bash
kubectl apply -f my-clusterregistration.yaml
# Error: no matches for kind "ClusterRegistration" in version "core.oam.dev/v1beta1"
```

**Solutions:**
```bash
# Check if CRD exists
kubectl get crd clusterregistrations.core.oam.dev

# If not found, apply manually
kubectl apply -f charts/vela-core/templates/clusterregistration-crd.yaml

# Or reinstall Helm chart with multicluster.enabled=true
helm upgrade vela-core ./charts/vela-core -n vela-system --set multicluster.enabled=true
```

### Issue 6: Secret Not Created After Registration

**Symptoms:**
```bash
kubectl get clusterregistration test-cluster -n vela-system
# PHASE: Ready

kubectl get secret my-test-cluster -n vela-system
# Error: Not found
```

**Diagnosis:**
```bash
# Check controller logs for registration errors
kubectl logs -n vela-system -l app.kubernetes.io/name=vela-core | grep -i "RegisterByVelaSecret"

# Check if secret was created with different name
kubectl get secrets -n vela-system | grep -i test
```

**Solutions:**
- Verify spec.clusterName in ClusterRegistration matches expected secret name
- Check controller has permissions to create secrets in vela-system namespace
- Review controller logs for permission errors

### Issue 7: Cluster Not Accessible After Registration

**Symptoms:**
```bash
# Secret exists but can't access cluster
kubectl get secret my-test-cluster -n vela-system  # OK
kubectl get --raw "/apis/cluster.core.oam.dev/v1alpha1/clustergateways/my-test-cluster/proxy/api/v1/namespaces"
# Error: unable to access cluster
```

**Diagnosis:**
```bash
# Check ClusterGateway resource
kubectl get clustergateway my-test-cluster -n vela-system -o yaml

# Check secret has correct data fields
kubectl get secret my-test-cluster -n vela-system -o jsonpath='{.data}' | jq
```

**Solutions:**
- Verify secret contains: `endpoint`, `ca.crt`, and either `token` or `tls.crt`+`tls.key`
- Check cluster credentials are still valid (not expired)
- Test kubeconfig directly: `kubectl --kubeconfig=/tmp/test-kubeconfig.yaml get nodes`

---

## Debugging Tips

### Enable Verbose Logging

```yaml
# In test-values.yaml
logDebug: true
devLogs: true
```

Or via command line:
```bash
go run ./cmd/core/main.go --enable-cluster-gateway=true --v=5
```

### Watch Controller Logs

```bash
# Follow logs in real-time
kubectl logs -n vela-system -l app.kubernetes.io/name=vela-core -f

# Filter for ClusterRegistration events
kubectl logs -n vela-system -l app.kubernetes.io/name=vela-core -f | grep -i clusterregistration

# View last 100 lines
kubectl logs -n vela-system -l app.kubernetes.io/name=vela-core --tail=100
```

### Use kubectl describe

```bash
# Get detailed information including events
kubectl describe clusterregistration test-cluster -n vela-system
```

### Check Resource Status

```bash
# Get status in JSON format for inspection
kubectl get clusterregistration test-cluster -n vela-system -o json | jq '.status'

# Check conditions
kubectl get clusterregistration test-cluster -n vela-system -o jsonpath='{.status.conditions}' | jq
```

### Test Kubeconfig Manually

```bash
# Save kubeconfig from ClusterRegistration
kubectl get clusterregistration test-cluster -n vela-system -o jsonpath='{.spec.kubeconfig}' > /tmp/extracted-kubeconfig.yaml

# Test it directly
kubectl --kubeconfig=/tmp/extracted-kubeconfig.yaml get nodes

# This helps isolate whether the issue is with the kubeconfig or the controller
```

### Restart Controller

```bash
# If running via Helm
kubectl rollout restart deployment vela-core -n vela-system

# If running locally, just Ctrl+C and restart
```

---

## Performance Testing

### Test Many Clusters

```bash
# Create 10 ClusterRegistrations
for i in {1..10}; do
  cat <<EOF | kubectl apply -f -
apiVersion: core.oam.dev/v1beta1
kind: ClusterRegistration
metadata:
  name: perf-cluster-$i
  namespace: vela-system
spec:
  clusterName: perf-test-$i
  kubeconfig: |
$(cat /tmp/test-kubeconfig.yaml | sed 's/^/    /')
  labels:
    batch: "perf-test"
    index: "$i"
EOF
done

# Monitor registration speed
time kubectl wait --for=condition=Ready clusterregistration -n vela-system -l batch=perf-test --timeout=5m

# Cleanup
kubectl delete clusterregistration -n vela-system -l batch=perf-test
```

### Monitor Resource Usage

```bash
# Watch pod resource usage
kubectl top pod -n vela-system

# Watch node resource usage
kubectl top node

# Check detailed metrics
kubectl get --raw /metrics | grep vela
```

---

## CI/CD Testing

### Automated Test Script

Create a test script for CI/CD pipelines:

```bash
#!/bin/bash
# test-clusterregistration.sh

set -e

echo "Step 1: Installing Helm chart..."
helm install vela-core ./charts/vela-core \
  --namespace vela-system \
  --create-namespace \
  --set image.tag=test-clusterreg \
  --set image.pullPolicy=IfNotPresent \
  --set multicluster.enabled=true \
  --wait \
  --timeout 5m

echo "Step 2: Verifying installation..."
kubectl wait --for=condition=available deployment/vela-core -n vela-system --timeout=2m
kubectl wait --for=condition=available deployment/cluster-gateway -n vela-system --timeout=2m

echo "Step 3: Creating test ClusterRegistration..."
kubectl apply -f examples/clusterregistration-sample.yaml

echo "Step 4: Waiting for registration to complete..."
kubectl wait --for=condition=Ready clusterregistration/test-cluster -n vela-system --timeout=2m

echo "Step 5: Verifying cluster secret was created..."
kubectl get secret test-remote-cluster -n vela-system

echo "Step 6: Testing cleanup..."
kubectl delete clusterregistration test-cluster -n vela-system
sleep 5
! kubectl get secret test-remote-cluster -n vela-system 2>/dev/null || (echo "Secret was not deleted!" && exit 1)

echo "✅ All tests passed!"
```

### GitHub Actions Example

```yaml
# .github/workflows/test-clusterregistration.yaml
name: Test ClusterRegistration

on:
  pull_request:
    paths:
      - 'apis/core.oam.dev/v1beta1/clusterregistration_types.go'
      - 'pkg/controller/core.oam.dev/v1beta1/clusterregistration/**'
      - 'charts/vela-core/templates/clusterregistration-crd.yaml'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.22'

      - name: Create kind cluster
        uses: helm/kind-action@v1

      - name: Build and load image
        run: |
          make docker-build IMG=oamdev/vela-core:test-clusterreg
          kind load docker-image oamdev/vela-core:test-clusterreg

      - name: Run tests
        run: ./test-clusterregistration.sh
```

---

## Best Practices

### Development Workflow

1. **Make code changes** to ClusterRegistration controller
2. **Run locally** using Approach 2 (Manual) for quick iteration
3. **Test with Approach 1** (Helm) before creating PR
4. **Run automated tests** in CI/CD pipeline
5. **Test on real multi-cluster setup** before merging

### Testing Checklist

Before submitting a PR, verify:

- [ ] ClusterRegistration CRD is valid and applies successfully
- [ ] Controller starts correctly with `--enable-cluster-gateway=true`
- [ ] Controller does NOT start when flag is false (feature is properly gated)
- [ ] Basic cluster registration works (Phase: Ready)
- [ ] Labels are properly applied to cluster secret
- [ ] Invalid kubeconfig is handled gracefully (Phase: Failed)
- [ ] Reserved names ("local") are rejected
- [ ] Cleanup works (deleting CR removes secret)
- [ ] Multiple clusters can be registered simultaneously
- [ ] Updates to ClusterRegistration trigger reconciliation
- [ ] Status conditions are properly set
- [ ] Helm chart installs successfully with `multicluster.enabled: true`
- [ ] Helm chart properly gates CRD installation

### Security Considerations

When testing:

- **Never commit kubeconfig content** to Git
- Use test clusters with limited permissions
- Rotate credentials after testing
- For production testing, use external secret management (Sealed Secrets, External Secrets Operator)

---

## Next Steps

After successful local testing:

1. **Write unit tests** for the controller
2. **Add integration tests** to the test suite
3. **Test with real cloud providers** (EKS, GKE, AKS)
4. **Document edge cases** discovered during testing
5. **Update user documentation** with examples
6. **Consider implementing**:
   - Validation webhook for pre-flight checks
   - Support for `secretRef` instead of inline kubeconfig
   - Periodic connectivity health checks
   - Metrics for cluster registration operations

---

## Additional Resources

- [KubeVela Multi-cluster Documentation](https://kubevela.io/docs/platform-engineers/system-operation/managing-clusters)
- [cluster-gateway Repository](https://github.com/oam-dev/cluster-gateway)
- [ClusterRegistration POC Documentation](./CLUSTERREGISTRATION_POC.md)
- [Original Testing Guide](./TESTING_CLUSTERREGISTRATION.md)

---

## Getting Help

If you encounter issues not covered in this guide:

1. Check controller logs: `kubectl logs -n vela-system -l app.kubernetes.io/name=vela-core`
2. Check cluster-gateway logs: `kubectl logs -n vela-system -l app=cluster-gateway`
3. Review the [KubeVela community Slack](https://cloud-native.slack.com/archives/C01BLQ3HTJA)
4. Open an issue in the [KubeVela repository](https://github.com/kubevela/kubevela/issues)
