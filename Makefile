all: controller

controller:
	mkdir -p dist 
	go build -o dist/node-controller ./controller.go
