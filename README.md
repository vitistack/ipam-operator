# Develop IPAM-Operator locally

## Prerequisites
- go version v1.25.3+
- docker version 4.45.0+.
- kubectl version v1.34.1+.
- Access to a Kubernetes v1.34.1+ cluster.
- Deployed IPAM-API in Docker

## Install Kubernetes Cluster, f.ex Sidero Talos

```sh
brew install siderolabs/tap/talosctl
talosctl cluster create
```

## Install & Configure Metallb Operator
```sh
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.15.2/config/manifests/metallb-native.yaml
kubectl apply -f ./hack/metallb.yaml
```

## Create required namespace (ipam-system) in Kubernetes Cluster
```sh
kubectl apply -f ./hack/namespace.yaml
```

## Create Certificate for IPAM-Operator
IPAM-Operator checks for certificate in folder /tmp/k8s-webhook-server/serving-certs, when running locally.
```sh
make generate-certs
```

## Update local webhook manifest file with encoded certificate and apply manifest to Kubernetes Cluster
```sh
base64 -i /tmp/k8s-webhook-server/serving-certs/tls.crt
```

Replace `caBundle` (two occurrences) in file ./config/webhook/manifests-local.yaml with encoded certificate

```sh
kubectl apply -f ./config/webhook/manifests-local.yaml
```

## Run Controller locally
```sh
make run
```

# Examples

## Create a service
```sh
kubectl apply -f ./hack/service.yaml
```

## Create a secret
```sh
kubectl apply -f ./hack/my-secret.yaml
```

# ArgoCD

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ipam-operator
  namespace: argocd
spec:
  project: default
  source:
    path: .
    repoURL: oci://ncr.sky.nhn.no/ghcr/vitistack/helm/ipam-operator
    targetRevision: 2.*
    helm:
      valueFiles:
          - values.prod.yaml
  destination:
    server: "https://kubernetes.default.svc"
    namespace: ipam-system
  syncPolicy:
      automated:
          selfHeal: true
          prune: true
      syncOptions:
      - CreateNamespace=true
```

If you would like to listen to only release candidates of the application, please replace `.spec.source.targetRevision` with f.ex `2.*-0`.
The notation will support releases with prefix `2.0.1-rc2`.

# License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.