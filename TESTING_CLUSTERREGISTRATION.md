# Testing ClusterRegistration Feature

This guide explains how to test the ClusterRegistration feature locally.

## Prerequisites

You need cluster-gateway running in your cluster for ClusterRegistration to work.

## Quick Setup

### Step 1: Install cluster-gateway
Install manually using Helm:

```bash
helm repo add kubevela https://kubevela.github.io/charts
helm repo update

# Install just cluster-gateway (not the full vela-core)
helm install cluster-gateway kubevela/vela-core \
  --create-namespace \
  --namespace vela-system \
  --set multicluster.enabled=true \
  --set multicluster.clusterGateway.replicaCount=1 \
  --set replicaCount=0
```

### Step 2: Verify cluster-gateway is running

```bash
# Check pods
kubectl get pods -n vela-system | grep cluster-gateway

# Check APIService
kubectl get apiservice v1alpha1.cluster.core.oam.dev

# Should show:
# NAME                              SERVICE                        AVAILABLE   AGE
# v1alpha1.cluster.core.oam.dev     vela-system/cluster-gateway    True        1m
```

### Step 3: Apply the ClusterRegistration CRD

```bash
kubectl apply -f config/crd/base/core.oam.dev_clusterregistrations.yaml
```

### Step 4: Run vela-core with multi-cluster enabled

#### Using Go:
```bash
go run ./cmd/core/main.go --enable-cluster-gateway=true
```

#### Using debugger:
Add `--enable-cluster-gateway=true` to your launch configuration.

#### Using compiled binary:
```bash
make build
./bin/vela-core --enable-cluster-gateway=true
```

### Step 5: Create a test ClusterRegistration

Create a test cluster registration (you can use your current cluster's kubeconfig):

```bash
# Get your current kubeconfig
kubectl config view --raw --minify > /tmp/test-kubeconfig.yaml

# Create ClusterRegistration resource
cat <<EOF | kubectl apply -f -
apiVersion: core.oam.dev/v1beta1
kind: ClusterRegistration
metadata:
  name: test-cluster
  namespace: vela-system
spec:
  clusterName: test-remote-cluster
  alias: "Test Remote Cluster"
  kubeconfig: |
$(cat /tmp/test-kubeconfig.yaml | sed 's/^/    /')
  createNamespace: "vela-system"
  labels:
    env: test
    purpose: development
EOF
```

### Step 6: Check the registration status

```bash
# Watch the ClusterRegistration status
kubectl get clusterregistration test-cluster -n vela-system -w

# Check detailed status
kubectl get clusterregistration test-cluster -n vela-system -o yaml

# Verify cluster was registered
kubectl get secrets -n vela-system | grep test-remote-cluster

# Or check via vela CLI (if installed)
vela cluster list
```

Expected output:
```
NAME           CLUSTER                PHASE   ENDPOINT                    AGE
test-cluster   test-remote-cluster    Ready   https://your-api-server     1m
```

## Troubleshooting

### Issue: "waiting for cluster gateway service"

**Problem:** vela-core is stuck waiting for cluster-gateway.

**Solution:** Make sure cluster-gateway is installed and the APIService is available:
```bash
kubectl get apiservice v1alpha1.cluster.core.oam.dev
```

If not available, reinstall cluster-gateway using the script above.

### Issue: ClusterRegistration stays in "Progressing" phase

**Problem:** The controller can't register the cluster.

**Solutions:**
1. Check controller logs:
   ```bash
   # If running in cluster
   kubectl logs -n vela-system -l app.kubernetes.io/name=vela-core

   # If running locally, check your terminal output
   ```

2. Check if the kubeconfig is valid:
   ```bash
   kubectl get clusterregistration test-cluster -n vela-system -o yaml
   # Look at .status.message for error details
   ```

3. Verify the cluster name is not "local" (reserved):
   ```bash
   kubectl get clusterregistration test-cluster -n vela-system -o jsonpath='{.spec.clusterName}'
   ```

### Issue: Controller not watching ClusterRegistration

**Problem:** Creating ClusterRegistration resources has no effect.

**Solution:** Make sure you started vela-core with `--enable-cluster-gateway=true` flag. The controller only registers when multi-cluster is enabled.

## Testing Cleanup

To clean up after testing:

```bash
# Delete the ClusterRegistration (this will automatically detach the cluster)
kubectl delete clusterregistration test-cluster -n vela-system

# Verify cluster was removed
kubectl get secrets -n vela-system | grep test-remote-cluster
# Should return nothing

# Uninstall cluster-gateway (optional)
kubectl delete deployment cluster-gateway -n vela-system
kubectl delete apiservice v1alpha1.cluster.core.oam.dev
```

## Testing with Multiple Clusters

If you have access to multiple Kubernetes clusters:

1. Get kubeconfig for each cluster
2. Create a ClusterRegistration for each
3. Verify all clusters show up in `vela cluster list`
4. Deploy a multi-cluster application to test cross-cluster functionality

## Next Steps

- Test the GitOps workflow by storing ClusterRegistration in Git
- Test with cloud provider kubeconfigs (EKS, GKE, AKS)
- Test the cleanup flow (delete ClusterRegistration and verify cluster is detached)
- Test updates (modify the ClusterRegistration and verify it reconciles)
