# Typesense Kubernetes Operator

The **Typesense Kubernetes Operator** is designed to manage the deployment and lifecycle of Typesense clusters within Kubernetes environments. 
The operator is developed in Go using [Operator SDK Framework](https://sdk.operatorframework.io/), an open source toolkit to manage Kubernetes native applications, called Operators, in an effective, automated, and scalable way. 

## Description

Key features of Typesense Kubernetes Operator include:

- **Custom Resource Management**: Provides a Kubernetes-native interface to define and manage Typesense cluster configurations using a CRD named `TypesenseCluster`.
- **Typesense Lifecycle Automation**: Simplifies deploying, scaling, and managing Typesense clusters. Handles aspects like:
    - Typesense bootstrapping and Admin API Key creation as a `Secret`,
    - Typesense deployment as a `StatefulSet`,
    - Typesense services (headless & discovery `Services`),
    - Typesense nodes list **active** discovery (quorum configuration mounted as `ConfigMap`),
    - Typesense filesystem as `PersistentVolumes`
- **Raft Lifecycle Automation**:
    - Continuous active (re)discovery of the quorum configuration reacting to changes in `ReplicaSet` **without the need of an additional sidecar container**,
    - Automatic recovery a cluster that has lost quorum **without the need of manual intervention**.

### Background

Typesense is using raft in the background to establish its clusters. Raft is a consensus algorithm based on the 
paper "[Raft: In Search of an Understandable Consensus Algorithm](https://raft.github.io/raft.pdf)".

Raft nodes operate in one of three possible states: _follower_, _candidate_, or _leader_. Every new node always joins the 
quorum as a follower. Followers can receive log entries from the leader and participate in voting for electing a leader. If no 
log entries are received for a specified period of time, a follower transitions to the candidate state. As a candidate, the node 
can accept votes from its peers nodes. Upon receiving a majority of votes, the candidate is becoming the leader of the quorum. 
The leader’s responsibilities include handling new log entries and replicating them to other nodes. 

Another thing to consider is what happens when the node set changes, when nodes join or leave the cluster. 
If a quorum of nodes is **available**, raft can dynamically modify the node set without any issue (this happens every 30sec). 
But if the cluster cannot form a quorum, then problems start to appear or better to pile up. A cluster with `N` nodes can tolerate 
a failure of at most `(N-1)/2` nodes without losing its quorum. If the available nodes go below this threshold then two events
are taking place:

- raft declares the whole cluster as **unavailable** (no leader can be elected, no more log entries can be processed)
- the remaining nodes are restarted in bootstrap mode

In a Kubernetes environment, the nodes are actually `Pods` which are rather volatile by nature and their lifetime is quite ephemeral and subjects 
to potential restarts, and that puts the whole concept of raft protocol consensus under a tough spot. As we can read in the official 
documentation of Typesense when it comes to [recovering a cluster that has lost quorum](https://typesense.org/docs/guide/high-availability.html#recovering-a-cluster-that-has-lost-quorum),
it is explicitly stated:

> If a Typesense cluster loses more than `(N-1)/2` nodes at the same time, the cluster becomes unstable because it loses quorum 
and the remaining node(s) cannot safely build consensus on which node is the leader. To avoid a potential split brain issue, 
Typesense then stops accepting writes and reads **until some manual verification and intervention is done**.

In production environments manual intervention can be sometimes impossible or even not desired and the downtime of a service like
Typesense might be completely out of the question. The Typesense Kubernetes Operator solves for that matter both of these problems.

### Problem 1: Quorum reconfiguration

The Typesense Kubernetes Operator takes over the whole lifecycle of Typesense Clusters in Kubernetes. 

![image](https://github.com/user-attachments/assets/9028a0f8-5ae5-4f9e-a83c-8a7e8f0e2f25)



reconciliation interval depend on the number of nodes, try that way to give breathing room to raft to perform its operations (leader election, log replication, bootstrapping etc.)

### Problem 2: Recovering a cluster that has lost quorum


![image](https://github.com/user-attachments/assets/0212cba0-c677-41df-a4f9-a41ca4eb6a8a)



## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running on the cluster
1. Install Instances of Custom Resources:

```sh
kubectl apply -f config/samples/
```

2. Build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/kube-dosbox:<tag>
```

3. Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/kube-dosbox:<tag>
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
UnDeploy the controller from the cluster:

```sh
make undeploy
```

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/),
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.

### Test It Out
1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
