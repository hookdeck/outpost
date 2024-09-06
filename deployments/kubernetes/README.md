# Development Kubernetes setup for EventKit

This is the documentation guide to set up a Kubernetes (K8s) cluster running EventKit with Redis. We'll use [Bitnami Redis Helm chart](https://artifacthub.io/packages/helm/bitnami/redis) to get a master-replica Redis cluster.

## Installations

- [Docker](https://docs.docker.com/engine/install/)
- [Minikube](https://minikube.sigs.k8s.io/docs/start)
- [Kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm](https://helm.sh/docs/intro/install/)

After installing Helm, you need to add Bitnami's Helm chart repositories to use one of Bitnami's charts.

```sh
$ helm repo add bitnami https://charts.bitnami.com/bitnami

# Verify the successul installation
$ helm search repo bitnami
```

## Steps

### Local K8s cluster setup

**Step 1:** Start a local Kubernetes cluster with Minikube

- Start Kubernetes cluster with Minikube:

```sh
$ minikube start
```

- Verify that your cluster is running by proxying to the dashboard. You can use this dashboard to visually verify that things are working as we progress through this setup guide.

```sh
$ minikube dashboard --url
```

**Step 2:** (Optional) Create new namespace for easy cleanup later

By default, everything you install on your Kubernetes cluster is within the `default` namespace. You can create a new namespace for this project to separate its environment with other projects or for easy cleanup later.

```sh
$ kubectl create namespace myeventkit

# Configure Minikube to use this namespace by default for future commands
$ kubectl config set-context --current --namespace=myeventkit
```

### Redis installation

**Step 3:** Install Bitnami's Redis Helm chart

We'll use `eventkit-redis` as the name of the Redis installation. You can use whichever name makes sense to you. Please make sure to substitue `eventkit-redis` with your Redis's installation name later on when applicable.

```sh
$ helm install eventkit-redis bitnami/redis
```

Upon a successul installation, Helm will print some steps to connect to your Redis cluster. Please verify that Redis is working correctly. Here's a simplified version, if you'd like to follow along with these steps instead.

```sh
# Get your Redis password
$ kubectl get secret --namespace another eventkit-redis -o jsonpath="{.data.redis-password}" | base64 -d
<the_password>%

# Exec `$ redis-cli` at the master node
$ kubectl exec --tty -i eventkit-redis-master-0 -- redis-cli
127.0.0.1:6379> PING
(error) NOAUTH Authentication required.
127.0.0.1:6379> AUTH <the_password>
OK
127.0.0.1:6379> PING
PONG
127.0.0.1:6379> exit
```

### EventKit installation

**Step 4**: Build & load EventKit image

Because there's no EventKit image in DockerHub yet, we need to build the image locally and load it in Minikube's registry.

Build EventKit:

```
# at .../hookdeck/EventKit directory
$ docker build -t hookdeck/eventkit -f build/Dockerfile .

# verify that the built image exists
$ docker image ls | grep -w hookdeck/eventkit
```

Load image to Minikube

```sh
# load
$ minikube load image hookdeck/eventkit

# verify
$ minikube image ls | grep -w hookdeck/eventkit
```

**Step 5**: Install EventKit using Helm chart

This installation assumes you have followed the steps above to set up Redis cluster with Bitnami. We're using the Redis's password secret by providing the custom `values.yaml` file. If you use a different Redis setup, please feel free to provide your own values when installing the chart.

```sh
# Navigate to the right directory
# at .../hookdeck/EventKit
$ cd deployments/kubernetes

# at .../hookdeck/EventKit/deployments/kubernetes

$ helm install eventkit charts/eventkit -f values.yaml
NAME: eventkit
LAST DEPLOYED: ...
NAMESPACE: myeventkit
STATUS: deployed
REVISION: 1
TEST SUITE: None
```

Congrats, you've successfully deployed EventKit to your local Kubernetes cluster.

### Interact with the cluster

Let's verify and interact with your EventKit service.

Get the URL of the EventKit service:

```sh
$ minikube service eventkit --url -n myeventkit
http://127.0.0.1:50454
```

You should get a URL that looks something like `http://127.0.0.1:50454`. The exact port may change every time you run this command. Let's keep this terminal opened and interace with your EventKit service:

```sh
$ curl -v http://127.0.0.1:50454
*   Trying 127.0.0.1:50454...
* Connected to 127.0.0.1 (127.0.0.1) port 50454
> GET /healthz HTTP/1.1
> Host: 127.0.0.1:50454
> User-Agent: curl/8.6.0
> Accept: */*
>
< HTTP/1.1 200 OK
< Date: Thu, 05 Sep 2024 12:58:33 GMT
< Content-Length: 0
<
* Connection #0 to host 127.0.0.1 left intact
```

To tail the log of the API service:

```sh
$ kubectl get pod | grep -w eventkit-api
eventkit-api-5dfd69c689-h8698        1/1     Running   0          10m

# getting the pod name above
$ kubectl logs eventkit-api-5dfd69c689-h8698 -f
```

You should see your previous request along with any future requests you make. You can try some more:

```sh
# create a new tenant
$ curl -v -X PUT http://127.0.0.1:50454/123

# retrieve said tenant
$ curl -v http://127.0.0.1:50454/123

# ...
```

You can verify that the data exists in Redis:

```sh
$ kubectl exec --tty -i eventkit-redis-master-0 -- redis-cli
127.0.0.1:6379> AUTH TpxpTpiTpS
OK
127.0.0.1:6379> HGETALL tenant:123
1) "id"
2) "123"
3) "created_at"
4) "2024-09-05T13:03:37.966761586Z"
```

### Clean up

If you put everything under a namespace, cleaning up is as simple as deleting the namespace.

```sh
$ kubectl delete namespaces myeventkit

# (optional) reset kubectl config to use default or another namespace
$ kubectl config set-context --current --namespace=default
```

And that's about it, you should be good to go.

In the unfortunate event that you didn't use namespace earlier:

```sh
$ kubectl delete all --all
```

However, this command doesn't include some resources like `configmaps` or `secrets`, so you may need to use other tools (Minikube dashboard) to manually delete them.

## TODO for production Helm chart

- [] Support [Ingress](https://kubernetes.io/docs/concepts/services-networking/ingress/)
- [] Consider supporting Redis (and ClickHouse) directly
- [] Full Chart.yaml (see [Bitnami's Redis chart](https://github.com/bitnami/charts/blob/main/bitnami/redis/Chart.yaml) for reference)
- [] NOTES.txt
- [] values.schema.json
