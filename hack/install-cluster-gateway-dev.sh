#!/bin/bash
# Quick cluster-gateway installation for local testing

echo "Installing cluster-gateway from the official repo..."

# Install cluster-gateway CRDs and deployment
kubectl apply -f https://raw.githubusercontent.com/oam-dev/cluster-gateway/master/config/crd/cluster.core.oam.dev_clustergatewayconfigurations.yaml
kubectl apply -f https://raw.githubusercontent.com/oam-dev/cluster-gateway/master/config/crd/cluster.core.oam.dev_clustergateways.yaml

# Create namespace
kubectl create namespace vela-system --dry-run=client -o yaml | kubectl apply -f -

# Install cluster-gateway deployment
cat <<YAML | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cluster-gateway
  namespace: vela-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: cluster-gateway
  template:
    metadata:
      labels:
        app: cluster-gateway
    spec:
      serviceAccountName: cluster-gateway
      containers:
      - name: cluster-gateway
        image: oamdev/cluster-gateway:v1.9.0-alpha.2
        imagePullPolicy: IfNotPresent
        args:
        - apiserver
        - --secure-port=9443
        - --secret-namespace=vela-system
        ports:
        - containerPort: 9443
          name: https
          protocol: TCP
        resources:
          limits:
            cpu: 500m
            memory: 200Mi
          requests:
            cpu: 50m
            memory: 20Mi
---
apiVersion: v1
kind: Service
metadata:
  name: cluster-gateway
  namespace: vela-system
spec:
  ports:
  - port: 9443
    protocol: TCP
    targetPort: 9443
  selector:
    app: cluster-gateway
  type: ClusterIP
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: cluster-gateway
  namespace: vela-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cluster-gateway
rules:
- apiGroups:
  - ""
  resources:
  - secrets
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - cluster.core.oam.dev
  resources:
  - clustergateways
  verbs:
  - get
  - list
  - watch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: cluster-gateway
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-gateway
subjects:
- kind: ServiceAccount
  name: cluster-gateway
  namespace: vela-system
---
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: v1alpha1.cluster.core.oam.dev
spec:
  group: cluster.core.oam.dev
  groupPriorityMinimum: 2000
  service:
    name: cluster-gateway
    namespace: vela-system
    port: 9443
  version: v1alpha1
  versionPriority: 10
  insecureSkipTLSVerify: true
YAML

echo "Waiting for cluster-gateway to be ready..."
kubectl wait --for=condition=available --timeout=120s deployment/cluster-gateway -n vela-system

echo "Verifying cluster-gateway installation..."
kubectl get apiservice v1alpha1.cluster.core.oam.dev

echo ""
echo "✅ Cluster-gateway installed successfully!"
echo ""
echo "You can now run vela-core with --enable-cluster-gateway flag"
