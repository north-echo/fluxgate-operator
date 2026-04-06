IMG ?= fluxgate-operator:latest
CONTROLLER_GEN ?= controller-gen

.PHONY: build test docker-build generate manifests install run fmt vet

## Build the operator binary
build: fmt vet
	go build -o bin/operator ./cmd/operator

## Run all tests
test: fmt vet
	go test ./...

## Build container image with podman
docker-build:
	podman build -t $(IMG) -f Containerfile .

## Generate DeepCopy and CRD manifests
generate:
	$(CONTROLLER_GEN) object paths="./api/..."

## Generate CRD YAML manifests
manifests:
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:artifacts:config=config/crd

## Install CRDs into the cluster
install: manifests
	kubectl apply -f config/crd/

## Run the operator locally against the current kubeconfig
run:
	go run ./cmd/operator

## Format code
fmt:
	go fmt ./...

## Run go vet
vet:
	go vet ./...
