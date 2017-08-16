FROM gcr.io/google_containers/debian-base-amd64:0.1
COPY dist/linux/amd64/calico-node-controller /calico-node-controller
ENTRYPOINT "/calico-node-controller"