<!-- BEGIN MUNGE: UNVERSIONED_WARNING -->

<!-- BEGIN STRIP_FOR_RELEASE -->

<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">

<h2>PLEASE NOTE: This document applies to the HEAD of the source tree</h2>

If you are using a released version of Kubernetes, you should
refer to the docs that go with that version.

<strong>
The latest release of this document can be found
[here](http://releases.k8s.io/release-1.1/docs/proposals/compute-resource-metrics-api.md).

Documentation for other releases can be found at
[releases.k8s.io](http://releases.k8s.io).
</strong>
--

<!-- END STRIP_FOR_RELEASE -->

<!-- END MUNGE: UNVERSIONED_WARNING -->

# Kubernetes compute resource metrics API

*This is a design document for adding a new feature in Kubernetes, Compute Resource Metrics APIs.*

## Goals

Provide resource usage metrics on pods and nodes through the API server.
Use cases include the scheduler, end users and various flavors of auto-scaling.

These APIs are expected to be available on all Kubernetes deployments.
Core features like Quality of Service will depend on this API and QoS semantics should not change across deployments.

Expose `cpu` and `memory` metrics to begin with.
`disk` might be added in the future.

The minimum resolution will be a minute by default.
Users will have the ability to adjust the resolution in their deployments.

The scope is limited to statistical data only.
Historical timeseries metrics data is out of scope.

### API Requirements

- Provide node level metrics, all pod metrics (in single request), specific pod metrics
- Ability to authenticate node & pod metrics independently from each other
- Follow existing API conventions, fully compatible types able to be served by apiserver library, if necessary.

## Current state

No API exists for compute resource usage in Kubernetes.
Heapster aggregates metrics data from nodes and exposes them via REST endpoints.

## Use cases

The first user will be kubectl. The resource usage data can be shown to the
user via a periodically refreshing interface similar to `top` on Unix-like
systems. This info could let users assign resource limits more efficiently.

```
$ kubectl top kubernetes-node-abcd
POD                        CPU         MEM
monitoring-heapster-abcde  0.12 cores  302 MB
kube-ui-v1-nd7in           0.07 cores  130 MB
```

A second user will be the scheduler. To assign pods to nodes efficiently, the
scheduler needs to know the current free resources on each node.

Auto-scaling features like Horizontal Pod Autoscaler can also consume the Metrics APIs.

## Proposed endpoints

The metrics API will be its own [API group](api-group.md), and is shared by the
kubelet and cluster API. The metrics include the mean, max and a few
percentiles of the list of values, and will initially only be available through
the API server. The raw metrics include the stat samples from cAdvisor, and will
only be available through the kubelet. The types of metrics are detailed
[below](#schema). All endpoints are GET endpoints, rooted at
`/apis/metrics/v1alpha1/`

- `/` - discovery endpoint; type resource list
- `/nodes` - host metrics; type `[]metrics.Node`
  - `/nodes/{node}` - metrics for a specific node
- `/pods` - All pod metrics across all namespaces; type
  `[]metrics.Pod`
- `/namespaces/{namespace}/pods` - All pod metrics within namespace; type `[]metrics.Pod`
  - `/namespaces/{namespace}/pods/{pod}` - metrics for specific pod
- Unsupported paths return status not found (404)
  - `/namespaces/`
  - `/namespaces/{namespace}`

The following query parameters are supported:

- `pretty` - pretty print the response
- `labelSelector` - restrict the list of returned objects by labels (list endpoints only)
- `fieldSelector` - restrict the list of returned objects by fields (list endpoints only)

### Rationale

We are not adding new methods to pods and nodes, e.g. `/api/v1/namespaces/myns/pods/mypod/metrics`, for a number of reasons.
For example, having a separate endpoint allows fetching all the pod metrics in a single request.
The rate of change of the data is also too high to include in the pod resource.
A separate endpoint helps in storing metrics data in a metrics friendly backend storage.

In the future, if any uses cases are found that would benefit from RC, namespace or service aggregation, metrics at those levels could also be exposed taking advantage of the fact that Heapster already does aggregation and metrics for them.

## Schema

Types are colocated with other API groups in `/pkg/apis/metrics`, and follow api
groups conventions there.

```go
// Metrics are (initially) only available through the API server.
type Node struct {
  TypeMeta
  ObjectMeta              // Should include node name
  Machine MetricsWindow
  SystemContainers []Container
}
type Pod struct {
  TypeMeta
  ObjectMeta              // Should include pod name
  Containers []Container
}
type Container struct {
  TypeMeta
  ObjectMeta              // Should include container name
  Metrics Windows
}

// Last overlapping 10s, 1m, 1h and 1d as a start
// Updated every 10s, so the 10s window is sequential and the rest are
// rolling.
type Windows map[time.Duration]Metrics

type Metrics struct {
	// End time of all the time windows in Metrics
	EndTime unversioned.Time `json:"endtime"`

	Mean       ResourceUsage `json:"mean"`
	Max        ResourceUsage `json:"max"`
	NinetyFive ResourceUsage `json:"95th"`
}

type ResourceUsage map[resource.Type]resource.Quantity
```

## Implementation

There are multiple ways to serve the Metrics APIs from the API server.
The following are some of the options.

#### First class support in API Server

API server will have the functionality for Metrics API built into it.
Clients will be able to Get and Watch metrics for nodes and pods.
The API server will hold only the most recent Metrics sample.

If a pod gets deleted, the API server will get rid of any metrics it may
currently be holding for it.

The clients watching the metrics data may cache it for longer periods of time.

The primary issue with the approach is that etcd is not built to handle metrics data.
Assuming minutely resolution, 100 nodes and 100 pods per node, the API server will have to handle ~168 QPS.
Metrics don't have to be persisted.
Old metrics are not of much use.
Hence we can avoid storing to etcd and instead store in memory or to an alternate storage.
If the API server is distributed, the storage medium has to be chosen appropriately.

The source of the Metrics data can either be kubelet or heapster as of now.

##### Kubelet as the source

Kubelet has cAdvisor compiled into it.
Hence it has access to all the required metrics.
It is already built to be able to write to the API server.
Computing these metrics in the kubelet will help with scalability of the cluster, when compared to computing at the cluster level.

The primary issue with this approach is that of metrics persistence.
Kubelet has [no checkpointing](https://issues.k8s.io/489) functionlity as of now.
Kubelet stores metrics data in memory as of now.
Kubelet restarts will wipe out all historical metrics.
In-order to survive restarts, metrics data will have to be persisted either using a checkpointed file or by storing data to a local database.

##### Heapster as the source

Heapster queries all historical data 
Since heapster keeps data for a period of time, we could
  redirect requests to the API server to heapster instead of using etcd. This
  would also allow serving metrics other than the latest ones.

More information on kubelet checkpoints can be read on
.


<!-- BEGIN MUNGE: GENERATED_ANALYTICS -->
[![Analytics](https://kubernetes-site.appspot.com/UA-36037335-10/GitHub/docs/proposals/compute-resource-metrics-api.md?pixel)]()
<!-- END MUNGE: GENERATED_ANALYTICS -->
