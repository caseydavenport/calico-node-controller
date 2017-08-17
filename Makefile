all: controller # Builds controller

DOCKER_REGISTRY?=caseydavenport

CONTROLLER_VERSION=0.0.1

.PHONY: dependencies
dependencies: # Retrieve dependencies
	glide install

controller: dependencies # Build controller
	mkdir -p dist 
	go build -o dist/node-controller ./controller.go

.PHONY: controller-linux
controller-linux: dependencies # Builds linux version for container
	GOOS=linux GOARCH=amd64 go build -o dist/linux/amd64/calico-node-controller

.PHONY: docker
docker: controller-linux # Builds docker container
	docker build -t ${DOCKER_REGISTRY}/calico-node-controller:${CONTROLLER_VERSION} .

.PHONY: push-docker
push-docker: docker # Pushes docker container to registry
	docker push ${DOCKER_REGISTRY}/calico-node-controller:${CONTROLLER_VERSION}

.PHONY: help
help: # Show this help
	@{ \
	echo 'Targets:'; \
	echo ''; \
	grep '^[a-z/.-]*: .*# .*' Makefile \
	| sort \
	| sed 's/: \(.*\) # \(.*\)/ - \2 (deps: \1)/' `: fmt targets w/ deps` \
	| sed 's/:.*#/ -/'                            `: fmt targets w/o deps` \
	| sed 's/^/    /'                             `: indent`; \
	echo ''; \
	} 1>&2; \