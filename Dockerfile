FROM alpine:3.6

ADD calico-node-controller /usr/bin/calico-node-controller
ENTRYPOINT ["/usr/bin/calico-node-controller"]
