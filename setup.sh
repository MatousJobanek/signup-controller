#!/bin/bash

set -e

kubectl ws root
kubectl ws create synctarget --type Universal --enter
kubectl kcp workload sync my-crc --syncer-image ghcr.io/kcp-dev/kcp/syncer:main --resources=services,routes.route.openshift.io -o ~/crc/syncer.yaml
kubectl apply -f ~/crc/syncer.yaml --kubeconfig ~/.kube/config

echo "waiting for sync target to be ready"
while [[ -z "$(kubectl get synctargets.workload.kcp.dev -o wide | grep True)" ]]; do
  echo -n "."
  sleep 1
done
echo ""

kubectl ws root
kubectl ws create plane --type Organization

cat <<EOF | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: all-users-access-plane
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: access-plane
subjects:
- apiGroup: rbac.authorization.k8s.io
  kind: Group
  name: system:authenticated
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: access-plane
rules:
- apiGroups:
  - tenancy.kcp.dev
  resourceNames:
  - plane
  resources:
  - clusterworkspaces/content
  verbs:
  - access
EOF

kubectl ws plane
kubectl ws create usersignup

cat <<EOF | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: all-users-access-usersignup
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: access-usersignup
subjects:
- apiGroup: rbac.authorization.k8s.io
  kind: Group
  name: system:authenticated
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: access-usersignup
rules:
- apiGroups:
  - tenancy.kcp.dev
  resourceNames:
  - usersignup
  resources:
  - clusterworkspaces/content
  verbs:
  - access
EOF

kubectl ws usersignup

cat <<EOF | kubectl apply -f -
apiVersion: apis.kcp.dev/v1alpha1
kind: APIBinding
metadata:
  name: crc
spec:
  reference:
    workspace:
      exportName: kubernetes
      path: root:synctarget
EOF

echo "waiting for APIBinding to be bound"
while [[ -z "$(kubectl get apibindings.apis.kcp.dev crc -o jsonpath="{.status.phase}" | grep Bound)" ]]; do
  echo -n "."
  sleep 1
done
echo ""

kubectl apply -f ~/go-workspace/src/github.com/codeready-toolchain/workspace-initializer/deploy/manager.yaml
kubectl apply -f ~/go-workspace/src/github.com/codeready-toolchain/signup-controller/deploy/manager.yaml
kubectl apply -f ~/go-workspace/src/github.com/codeready-toolchain/registration-service/deploy/registration-service.yaml


echo "waiting for registration service route to be available"
while [[ -z "$(kubectl get route rs -n user-registration-service -o=jsonpath='{.status.ingress[*].host}')" ]]; do
  echo -n "."
  sleep 1
done
echo ""

echo "====================================================================================="
echo ""
echo https://$(kubectl get route rs -n user-registration-service -o=jsonpath='{.status.ingress[*].host}')
echo ""
echo "====================================================================================="

kubectl ws root
kubectl ws create has --type universal
cat <<EOF | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: all-users-access-has
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: access-has
subjects:
- apiGroup: rbac.authorization.k8s.io
  kind: Group
  name: system:authenticated
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: access-has
rules:
- apiGroups:
  - tenancy.kcp.dev
  resourceNames:
  - has
  resources:
  - clusterworkspaces/content
  verbs:
  - access
EOF

kubectl ws has
cat <<EOF | kubectl apply -f -
apiVersion: apis.kcp.dev/v1alpha1
kind: APIBinding
metadata:
  name: crc
spec:
  reference:
    workspace:
      exportName: kubernetes
      path: root:synctarget
EOF

echo "waiting for APIBinding to be bound"
while [[ -z "$(kubectl get apibindings.apis.kcp.dev crc -o jsonpath="{.status.phase}" | grep Bound)" ]]; do
  echo -n "."
  sleep 1
done

kubectl create ns application-service-system
kubectl create secret generic has-github-token --from-literal=token="some-token" -n application-service-system
make -C ~/go-workspace/src/github.com/redhat-appstudio/application-service deploy-kcp