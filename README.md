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

### Custom Resource Definition

Typesense Kubernetes Operator is controlling the lifecycle of multiple Typesense instances in the same Kubernetes cluster by 
introducing `TypesenseCluster`, a new Custom Resource Definition:  

![image](https://github.com/user-attachments/assets/23e40781-ca21-4297-93bf-2b5dbebc7e0e)

The specification of the CRD includes the following properties:

- `image`: the Typesense docker image to use
- `replicas`: the size of the cluster, defaults to **1**
- `apiPort`: the REST/API port, defaults to `8108`
- `peeringPort`: the peering port, defaults to `8107`
- `resetPeersOnError`: whether to reset nodes in error state or not, defaults to `true`
- `corsDomains`: domains that would be allowed for CORS calls, optional.
- `storage.size`: the size of the underlying `PersistentVolume`, defaults to `100Mi`
- `storage.storageClassName`: the storage class to use, defaults to `standard`

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

In production environments, manual intervention is sometimes impossible or undesirable, and downtime for a service like 
Typesense may be unacceptable. The Typesense Kubernetes Operator addresses both of these challenges.

### Problem 1: Quorum reconfiguration


The Typesense Kubernetes Operator manages the entire lifecycle of Typesense Clusters within Kubernetes:

1. A random token is generated and stored as a base64-encoded value in a new `Secret`. This token serves as the Admin API key for bootstrapping the Typesense cluster.
2. A `ConfigMap` is created, containing the endpoints of the cluster nodes as a single concatenated string in its `data` field. 
During each reconciliation loop, the operator identifies any changes in endpoints and updates the `ConfigMap`. This `ConfigMap` 
is mounted in every `Pod` at the path where raft expects the quorum configuration, ensuring quorum configuration stays always updated.
The Fully Qualified Domain Name (FQDN) for each endpoint of the headless service adheres to the following naming convention:

`{cluster-name}-sts-{pod-index}.{cluster-name}-sts-svc.{namespace}.svc.cluster.local:{peering-port}:{api-port}`

> [!IMPORTANT]
> **This completely eliminates the need for a sidecar** to translate the endpoints of the headless `Service` into `Pod` IP addresses. 
> The FQDN of the endpoints automatically resolves to the new IP addresses, and raft will begin contacting these endpoints 
> within its 30-second polling interval.

3. Next, the reconciler creates a headless `Service` required for the `StatefulSet`, along with a standard Kubernetes 
Service of type `ClusterIP`. The latter exposes the REST/API endpoints of the Typesense cluster to external systems.
4. A `StatefulSet` is then created. The quorum configuration stored in the `ConfigMap` is mounted as a volume in each `Pod`
under `/usr/share/typesense/nodelist`. No `Pod` restart is necessary when the `ConfigMap` changes, as raft automatically 
detects and applies the updates.

![image](https://github.com/user-attachments/assets/30b6989c-c872-46ef-8ece-86c5d4911667)

> [!NOTE]
> The interval between reconciliation loops depends on the number of nodes. This approach ensures raft has sufficient 
> "breathing room" to carry out its operations—such as leader election, log replication, and bootstrapping—before the 
> next quorum health reconciliation begins.

5. The controller assesses the quorum's health by probing each node at `http://{nodeUrl}:{api-port}/health`. Based on the
results, it formulates an action plan for the next reconciliation loop. This process is detailed in the following section:

### Problem 2: Recovering a cluster that has lost quorum

**Left Path:**

1. The quorum reconciler probes each cluster node at `http://{nodeUrl}:{api-port}/health`. If every node responds with `{ ok: true }`, 
    the `ConditionReady` status of the `TypesenseCluster` custom resource is updated to `QuorumReady`, indicating that the cluster is fully healthy and operational.
2.
   - If the cluster size matches the desired size defined in the `TypesenseCluster` custom resource (and was not downgraded
   during a previous loop—this scenario will be discussed later), the quorum reconciliation loop sets the `ConditionReady` 
   status of the `TypesenseCluster` custom resource to `QuorumReady`, exits, and hands control back to the main controller loop.
   - If the cluster was downgraded to a single instance during a previous reconciliation loop, the quorum reconciliation loop
   sets the `ConditionReady` status of the `TypesenseCluster` custom resource to `QuorumUpgraded`. It then returns control 
   to the main controller loop, which will attempt to restore the cluster to the desired size defined in the `TypesenseCluster`
   custom resource during the next reconciliation loop. Raft will then identify the new quorum configuration and elect a new leader.
   - If a node runs out of memory or disk, the health endpoint response will include an additional `resource_error` field, 
   set to either `OUT_OF_MEMORY` or `OUT_OF_DISK`, depending on the issue. In this case, the quorum reconciler marks the
   `ConditionReady` status of the `TypesenseCluster` as `QuorumNeedsIntervention`, triggers a Kubernetes `Event`, and 
   returns control to the main controller loop. **In this scenario, manual intervention is required**. You must adjust the 
   resources in the `PodSpec` or the storage in the `PersistentVolumeClaim` of the `StatefulSet` to provide new memory limits 
   or increased storage size. This can be done by modifying and re-applying the corresponding `TypesenseCluster` manifest.

**Right Path:**

1. The quorum reconciler probes each node of the cluster at http://{nodeUrl}:{api-port}/health. 
   - If the required number of nodes (at least `(N-1)/2`) return `{ ok: true }`, the `ConditionReady` status of the 
   `TypesenseCluster` custom resource is set to `QuorumReady`, indicating that the cluster is healthy and operational, 
   **even if** some nodes are unavailable. Control is then returned to the main controller loop.
   - If the required number of nodes (at least `(N-1)/2`) return `{ ok: false }`, the `ConditionReady` status of the 
   `TypesenseCluster` custom resource is set to `QuorumDowngrade`, marking the cluster as unhealthy. As part of the 
   mitigation plan, the cluster is scheduled for a downgrade to a single instance, with the intent to allow raft to automatically recover the quorum. 
   The quorum reconciliation loop then returns control to the main controller loop.
   - In the next quorum reconciliation, the process will take the **Left Path**, that will eventually discover a healthy quorum, 
     nevertheless with the wrong amount of nodes; thing that will lead to setting the `ConditionReady` condition of the `TypesenseCluster` as `QuorumUpgraded`.
     What happens next is already described in the **Left Path**.
   
![image](https://github.com/user-attachments/assets/55fda493-d35a-405c-8a58-a6f9436a28db)

> [!IMPORTANT]
> The scaling down and up of the `StatefulSet` would typically be the manual intervention needed to recover a cluster that has lost its quorum. 
> **However**, the controller automates this process, as long as is not a memory or disk capacity issue, ensuring no service
> interruption and **eliminating the need for any administrator action**.

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running on the cluster

1. Build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/typesense-operator:<tag>
```

2. Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/typesense-operator:<tag>
```

3. Install Instances of Custom Resources:

```sh
kubectl apply -f config/samples/
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
make generate && make manifests
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
