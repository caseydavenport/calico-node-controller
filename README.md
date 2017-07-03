# Calico Node Controller for Kubernetes

This is a prototype, WIP node controller for Kubernetes.  I'm not currently intending to continue
development / maintenance much further, but it might be a useful starting point for someone.

It watches Kubernetes nodes and ensures that the Calico etcd datastore remains in-sync.

Namely:
- It creates / updates any Calico Node resources when the Kubernetes representation changes.
- It deletes and Calico Node resources which do not match a Kubernetes Node.

This is useful when, for example, running a cluster in an AWS autoscaling group where nodes are
commonly created / deleted, and prevents stale data from lingering in etcd.

## TODOs:

- Limit to Kubernetes Nodes (currently this won't work on a mixed cluster).
- Handle disconnect / reconnect to etcd.
- Periodically populate cache to check for and overwrite modifications to Calico resources.
- Tests, tests, tests
