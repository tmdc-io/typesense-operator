# Typesense Kubernetes Operator

The **Typesense Kubernetes Operator** is designed to manage the deployment and lifecycle of Typesense clusters within Kubernetes environments. 
The operator is developed in Go using [Operator SDK Framework](https://sdk.operatorframework.io/), an open source toolkit to manage Kubernetes native applications, called Operators, in an effective, automated, and scalable way. 

## Description

Key features of Typesense Kubernetes Operator include:

- **Custom Resource Management**: Provides a Kubernetes-native interface to define and manage Typesense cluster configurations using a CRD named `TypesenseCluster`.
- **Typesense Lifecycle Automation**: Simplifies deploying, scaling, and managing Typesense clusters. Handles aspects like:
    - bootstrap Typesense's Admin API Key creation as a `Secret`,
    - deploy Typesense as a `StatefulSet`,
    - provision Typesense services (headless & discovery `Services`),
    - actively discover and update Typesense's nodes list (quorum configuration mounted as `ConfigMap`),
    - place claims for Typesense `PersistentVolumes`
- **Raft Quorum Configuration & Recovery Automation**:
    - Continuous active (re)discovery of the quorum configuration reacting to changes in `ReplicaSet` **without the need of an additional sidecar container**,
    - Automatic recovery of a cluster that has lost quorum **without the need of manual intervention**.

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

![image](https://github.com/user-attachments/assets/a28357bf-199f-45e7-9ce4-9557043bfc20)

In production environments manual intervention can be sometimes impossible or even not desired and the downtime of a service like
Typesense might be completely out of the question. The Typesense Kubernetes Operator solves for that matter both of these problems:

### Problem 1: Quorum reconfiguration

The Typesense Kubernetes Operator takes over the whole lifecycle of Typesense Clusters in Kubernetes:

1. A random token is generated and stored as `base64` in a new `Secret`; it will be used later as the Admin Api key to boostrap Typesense cluster.
2. A `ConfigMap` is created; the endpoints of the cluster nodes as a single concatenated string are provided `data` of the `ConfigMap`. Every
new reconciliation loop identifies the new endpoints and updates the `ConfigMap`, which as we will see later is mounted in every Pod in the path
tha raft expects the quorum configuration. 

The FQDN of each endpoint of the headless service follows the naming convention: 

`{cluster-name}-sts-{pod-index}.{cluster-name}-sts-svc.{namespace}.svc.cluster.local:{peering-port}:{api-port}`

> [!IMPORTANT]
> **This eliminates completely the need of a sidecar** that translates endpoints of the headless `Service` to `Pod` IP addresses. 
> The FQDN of the endpoints are resolving to the new IP addresses automatically and raft will start contacting those endpoints 
> inside the next 30sec (polling interval of raft).

3. As next step the reconciler will create a headless `Service` that we are going to need for the `StatefulSet`, 
and a normal Kubernetes `Service` of type `ClusterIP` that we will use to expose the REST/API endpoints of Typesense cluster to other systems
4. A `StatefulSet` is being created. The quorum configuration that we keep in the ConfigMap is mounted a volume in every `Pod` 
under the `/usr/share/typesense/nodelist`. No `Pod` reloading is required when changes happen to the `ConfigMap`, raft will
pick up the changes automatically.

![image](https://github.com/user-attachments/assets/30b6989c-c872-46ef-8ece-86c5d4911667)

> [!NOTE]
> The interval of the reconciliation loops depends on the number of nodes, trying that way to give raft adequate 'breathing room'
> to perform its operations (leader election, log replication, bootstrapping etc.) before a new _quorum's health reconciliation_ starts.

5. The controller evaluates quorum's health by probing each node at `http://{nodeUrl}:{api-port}/health` and devises
an action plan for the next reconciliation loop according to the outcome. This is described in the next paragraph:

### Problem 2: Recovering a cluster that has lost quorum

**Left Path:**

1. Quorum reconciler is probing every node of the cluster at `http://{nodeUrl}:{api-port}/health`. If every node returns
`{ ok: true }` then the `ConditionReady` condition of the `TypesenseCluster` custom resource is set to `QuorumReady` which means the cluster 
is 100% healthy and ready to go.
2.
   - If the cluster size has already the desired size defined by the `TypesenseCluster` custom resource (in case was not downgraded during 
   another loop; we will explore that option later) the quorum reconciliation loop marks the `ConditionReady` condition of the `TypesenseCluster` 
   custom resource is set to `QuorumReady`exits and returns back to the controller loop.
   - If the cluster has been downgraded to a single instance during a previous reconciliation loop, the the quorum reconciliation 
   loop set the `ConditionReady` condition of the `TypesenseCluster` as `QuorumUpgraded` and returns control back to the controller loop
   which will attempt, in its next reconciliation loop, to restore the cluster size to the desired size defined by the `TypesenseCluster` custom resource,
   and let raft to identify the new quorum configuration and elect a new leader.
   - In the event that a node is running out of memory or disk, the health endpoint response will have an additional `resource_error` field
   that will be set to `OUT_OF_MEMORY` or `OUT_OF_DISK` respectively. In that very case, the quorum reconciler, 
   marks the `ConditionReady` condition of the `TypesenseCluster` as `QuorumNeedsIntervention`, signals a Kubernetes `Event` and returns control back to the controller. 
   In that and only case, **you need to manually intervene** by either changing the `resources` in `PodSpec` or the `storage` in `PersistentVolumeClaim` of the `StatefulSet` in order to provide
   new memory limits or storage size. That can easily happen by just changing and re-applying the respective `TypesenseCluster` manifest!


**Right Path:**

1. Quorum reconciler is probing every node of the cluster at `http://{nodeUrl}:{api-port}/health`. 
    - If the required number of nodes (minimum `(N-1)/2`) return `{ ok: true }` then the `ConditionReady` condition of the `TypesenseCluster` custom resource is set to `QuorumReady` which means the cluster
      is healthy and ready to go, although some nodes are unavailable, and the control is returned back to the controller's loop.
    - If the required number of nodes (minimum `(N-1)/2`) return `{ ok: false }` then the `ConditionReady` condition of the `TypesenseCluster` custom resource is set to `QuorumDowngrade` which means the cluster
      is declared unhealthy, and as a mitigation plan is downgraded to a **single instance cluster** with intent to let raft recover the quorum automatically.
      The quorum reconciliation loop then returns control back to the controller loop. 

> [!NOTE]
> In the next quorum reconciliation, the process will take the **Left Path**, that will eventually discover a healthy quorum, 
> nevertheless with the wrong amount of nodes; thing that will lead to setting the `ConditionReady` condition of the `TypesenseCluster` as `QuorumUpgraded`.
> What happens next is already described in the **Left Path**.
   
![image](https://github.com/user-attachments/assets/55fda493-d35a-405c-8a58-a6f9436a28db)

This scaling down and up of the `StatefulSet`, is in practice what would be necessary as "manual intervention" to recover
a cluster that has lost its quorum. Instead the controller takes over and does this **without interrupting the service** and without
requiring any action from the administrators of the cluster.

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
