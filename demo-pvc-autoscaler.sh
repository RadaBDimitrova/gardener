#!/bin/bash

make kind-down && make kind-up && make gardener-up

kubectl patch storageclass standard --patch '{"allowVolumeExpansion": true}'

kubectl apply -f example/provider-local/shoot-workerless.yaml

NAMESPACE=garden-local ./hack/usage/wait-for.sh shoot local-wl APIServerAvailable ControlPlaneHealthy ObservabilityComponentsHealthy SystemComponentsHealthy

echo "Getting PVCA resources should not work:"
kubectl get pvca -A

kubectl apply -f configmap-enable.yaml

gardenlet_deployment=$(kubectl get deployment -n garden -l role=gardenlet -o name | head -1)

kubectl patch "$gardenlet_deployment" -n garden --type='json' -p='[
  {
    "op": "replace", 
    "path": "/spec/template/spec/volumes/2/configMap/name",
    "value": "gardenlet-configmap-9cc9855d"
  }
]'

kubectl rollout status "$gardenlet_deployment" -n garden --timeout=60s 2>/dev/null

sleep 30

echo "Waiting for PVC autoscaler deployment to be ready..."
kubectl wait --for=condition=Available deployment/pvc-autoscaler -n garden --timeout=120s

echo "Getting PVCA resources should work:"
kubectl wait --for condition=established crd/persistentvolumeclaimautoscalers.autoscaling.gardener.cloud --timeout=60s


echo "Reconciling shoot for pvca list to be updated:"
kubectl annotate -n garden-local shoot local-wl gardener.cloud/operation=reconcile

NAMESPACE=garden-local ./hack/usage/wait-for.sh shoot local-wl APIServerAvailable ControlPlaneHealthy ObservabilityComponentsHealthy SystemComponentsHealthy

echo "Getting PVCA resources should now show shoot's pvcas also:"
kubectl get pvca -A

kubectl apply -f example/provider-local/shoot2.yaml

NAMESPACE=garden-local ./hack/usage/wait-for.sh shoot local-wl-2 APIServerAvailable ControlPlaneHealthy ObservabilityComponentsHealthy SystemComponentsHealthy
kubectl wait --for=condition=Ready shoot/local-wl-2 -n garden-local --timeout=30s 2>/dev/null

echo "Getting PVCA resources should show both shoots' pvcas:"
kubectl get pvca -A

echo "Getting MRs:"
kubectl get mr -l test-delete=true -A

echo "Disabling pvc-autoscaler:"
kubectl patch "$gardenlet_deployment" -n garden --type='json' -p='[
  {
    "op": "replace", 
    "path": "/spec/template/spec/volumes/2/configMap/name",
    "value": "gardenlet-configmap-8cc9855d"
  }
]'

kubectl rollout status "$gardenlet_deployment" -n garden --timeout=60s 2>/dev/null

kubectl annotate -n garden-local shoot local-wl gardener.cloud/operation=reconcile
kubectl wait --for=condition=Ready shoot/local-wl -n garden-local --timeout=30s 2>/dev/null
kubectl annotate -n garden-local shoot local-wl-2 gardener.cloud/operation=reconcile
kubectl wait --for=condition=Ready shoot/local-wl-2 -n garden-local --timeout=30s 2>/dev/null

NAMESPACE=garden-local ./hack/usage/wait-for.sh shoot local-wl APIServerAvailable ControlPlaneHealthy ObservabilityComponentsHealthy SystemComponentsHealthy
NAMESPACE=garden-local ./hack/usage/wait-for.sh shoot local-wl-2 APIServerAvailable ControlPlaneHealthy ObservabilityComponentsHealthy SystemComponentsHealthy

echo "Getting MRs shouldn't work:"
kubectl get mr -l test-delete=true -A



