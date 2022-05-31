#!/bin/bash

user_help () {
    echo "Collects all logs from the given namespace"
    echo "options:"
    echo "-n,   --namespace          The namespace the logs should be collected from."
    echo "-h,   --help               To show this help text"
    echo ""
    exit 0
}

read_arguments() {
    if [[ $# -lt 2 ]]
    then
        user_help
    fi

    while test $# -gt 0; do
           case "$1" in
                -h|--help)
                    user_help
                    ;;
                -sw|--serviceaccount-workspace)
                    shift
                    SOURCE_WS=$1
                    shift
                    ;;
                -sn|--serviceaccount-name)
                    shift
                    SA_NAME=$1
                    shift
                    ;;
                -tw|--target-workspace)
                    shift
                    TARGET_WS=$1
                    shift
                    ;;
                -l|--label)
                    shift
                    LABEL=$1
                    shift
                    ;;
                -sk|--source-kubeconfig)
                    shift
                    SOURCE_KUBECONFIG=$1
                    shift
                    ;;
                -ts|--target-secret)
                    shift
                    TARGET_SECRET=$1
                    shift
                    ;;
                -k|--type-kubeconfig)
                    shift
                    TYPE_KUBECONFIG=$1
                    shift
                    ;;
                -tk|--target-kubeconfig)
                    shift
                    TARGET_KUBECONFIG=$1
                    shift
                    ;;
                -tn|--target-namespace)
                    shift
                    NAMESPACE_NAME=$1
                    shift
                    ;;
                -sns|--source-namespace)
                    shift
                    SOURCE_NAMESPACE_NAME=$1
                    shift
                    ;;
                *)
                   echo "$1 is not a recognized flag!" >> /dev/stderr
                   user_help
                   exit -1
                   ;;
          esac
    done
}

read_arguments $@

set -ex


if [[ -n ${SOURCE_KUBECONFIG} ]]; then
  SOURCE_KUBECONFIG_PARAM="--kubeconfig ${SOURCE_KUBECONFIG}"
fi

if [[ -n ${SOURCE_NAMESPACE_NAME} ]]; then
  SOURCE_NS_PARAM="--namespace ${SOURCE_NAMESPACE_NAME}"
fi

if [[ -n "${SOURCE_WS}" ]]; then
  kubectl ws use "${SOURCE_WS}" ${SOURCE_KUBECONFIG_PARAM}
fi

kubectl create serviceaccount ${SA_NAME} ${SOURCE_KUBECONFIG_PARAM} ${SOURCE_NS_PARAM} || true

cat <<EOF | kubectl apply ${SOURCE_KUBECONFIG_PARAM} -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${SA_NAME}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
- kind: ServiceAccount
  name: ${SA_NAME}
  namespace: default
EOF

SECRET=$(kubectl get secrets -o name ${SOURCE_KUBECONFIG_PARAM} ${SOURCE_NS_PARAM} | grep "${SA_NAME}-token-")
SECRET_NAME=${SECRET##*/}
SECRET_FILE="/tmp/${SECRET_NAME}"

kubectl get secret ${SECRET_NAME} -o yaml ${SOURCE_KUBECONFIG_PARAM} ${SOURCE_NS_PARAM} | grep "^data:" -A 3 > ${SECRET_FILE}

CLUSTER_URL=$(kubectl config view | yq ".clusters[] | select(.name==\"$(kubectl config current-context 2>/dev/null)\")" | jq -r .cluster.server)
TARGET_SECRET=${TARGET_SECRET:-"for-${SA_NAME}-token"}

SOURCE_WS_NAME=${SOURCE_WS##*:}

echo "apiVersion: v1
kind: Secret
metadata:
  annotations:
    url: ${CLUSTER_URL}
  labels:
    workspace: ${LABEL}
    workspace-name: '${SOURCE_WS_NAME}'
  name: ${TARGET_SECRET}
  namespace: ${NAMESPACE_NAME}
" >> ${SECRET_FILE}

if [[ -n "${TARGET_WS}" ]]; then
  kubectl ws use "${TARGET_WS}"
fi

#cat ${SECRET_FILE}

if [[ "${TYPE_KUBECONFIG}" == "true" ]]; then
  TOKEN=$(cat ${SECRET_FILE} | yq -r '.data.token' | base64 -d)
  echo "apiVersion: v1
kind: Secret
metadata:
  name: ${TARGET_SECRET}
  namespace: ${NAMESPACE_NAME}
stringData:
  kubeconfig: |
    apiVersion: v1
    kind: Config
    clusters:
    - name: default-cluster
      cluster:
        certificate-authority-data:
        server: <<url>>
    contexts:
    - name: default-context
      context:
        cluster: default-cluster
        namespace: default
        user: default-user
    current-context: default-context
    users:
    - name: default-user
      user:
        token: <<token>>
" | sed "s|<<url>>|${CLUSTER_URL}|;s|<<token>>|${TOKEN}|" > ${SECRET_FILE}
fi

if [[ -n ${TARGET_KUBECONFIG} ]]; then
  KUBECONFIG_PARAM="--kubeconfig ${TARGET_KUBECONFIG}"
fi

kubectl apply -f ${SECRET_FILE} ${KUBECONFIG_PARAM}