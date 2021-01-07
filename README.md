# kube-image-bouncer

A simple webhook endpoint server that can be used to validate the images being created inside of the kubernetes cluster (created by kubeadm and tested on version 1.20.0), see the original repo: [kube-image-bouncer](https://github.com/flavio/kube-image-bouncer) for a vanilla implementation.

It works with two different types of [Kubernetes admission controller](https://kubernetes.io/docs/admin/admission-controllers/):

  * [ImagePolicyWebhook](https://kubernetes.io/docs/admin/admission-controllers/#imagepolicywebhook)
  * [GenericAdmissionWebhook](https://v1-8.docs.kubernetes.io/docs/admin/admission-controllers/#genericadmissionwebhook-alpha) (which starting from Kubernetes 1.9 has been renamed
[ValidatingAdmissionWebhook](https://kubernetes.io/docs/admin/admission-controllers/#validatingadmissionwebhook-alpha-in-18-beta-in-19).

This admission controller will reject all the pods that are using images with the `latest` tag.

## Disclaimer

I personally find the documentation of these admission controllers vague,
confusing and missing some details.

In this example I had to adapt things to use the ValidatingWebhookConfiguration from the latest version:
[Dynamic Admission Control](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#configure-admission-webhooks-on-the-fly)
which has more details.

# Comparison

The [ImagePolicyWebhook](https://kubernetes.io/docs/admin/admission-controllers/#imagepolicywebhook)
is an admission controller that evaluates only images.

Good things about `ImagePolicyWebhook`:

  * The API server can be instructed to reject the images if the webhook
    endpoint is not reachable.

Bad things about `ImagePolicyWebhook`:

  * More configuration files are expected on the API server node(s) compared to
    `ValidatingWebhookConfiguration`.
  * It's a bit tricky to deploy the service providing the webhook endpoint on the
    kubernetes cluster (more on that later).


[ValidatingAdmissionWebhook](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers)
can evaluate all kind of resources.

Good things about `ValidatingAdmissionWebhook`:

  * Only two changes are required on the kubernetes master node(s).
  * Part of the configuration is defined by a Kubernetes object.
  * It's incredibly easy to deploy the service providing the webhook endpoint
    on the kubernetes cluster.

Bad things about `ValidatingAdmissionWebhook`:

  * Starting from the 1.8 release it's no longer possible to instruct the API
    server to reject the images if the webhook endpoint is
    not reachable. Hence, when the endpoint is not reachable, all the resources
    are going to be automatically accepted.

# Building

To build the project just do:

```
$ go get github.com/kainlite/kube-image-bouncer
```

The project dependencies are tracked inside of this repository and are managed
using [dep](https://github.com/golang/dep).

This application is distributed also as a [Docker image](https://hub.docker.com/r/kainlite/kube-image-bouncer/):

```
$ docker pull kainlite/kube-image-bouncer
```

# Deployment of `ImagePolicyWebhook`

There are two possible ways to deploy this controller (webhook), for this to work you will need to create the certificates as explained below, but first
we need to take care of other details add this to your hosts file in the master or where the bouncer will run:

We use this name because it has to match with the names from the certificate, since this will run outside kuberntes and it could even be externally available, we just fake it with a hosts entry
```
echo "127.0.0.1 image-bouncer-webhook.default.svc" >> /etc/hosts
```

Then (go generate the cert in the step then come back), be aware that you need to be sitting in the folder with the certs for that to work:
```
docker run --rm -v `pwd`/server-key.pem:/certs/server-key.pem:ro -v `pwd`/server.crt:/certs/server.crt:ro -p 1323:1323 --network host kainlite/kube-image-bouncer -k /certs/server-key.pem -c /certs/server.crt
```

Also in the apiserver you need to update it with these settings:
```
--admission-control-config-file=/etc/kubernetes/kube-image-bouncer/admission_configuration.json
--enable-admission-plugins=ImagePolicyWebhook
```

If you did this method you don't need to create the `validating-webhook-configuration.yaml` resource nor apply the kubernetes deployment to run in the cluster.

## Kubernetes master node(s)

Ensure the `ImagePolicyWebhook` admission controller is enabled. Refer to
the [official](https://kubernetes.io/docs/admin/admission-controllers/#imagepolicywebhook)
documentation.

Create an admission control configuration file named
`/etc/kubernetes/admission_configuration.json` file with the following
contents:

```json
{
  "imagePolicy": {
     "kubeConfigFile": "/etc/kubernetes/kube-image-bouncer/kube-image-bouncer.yml",
     "allowTTL": 50,
     "denyTTL": 50,
     "retryBackoff": 500,
     "defaultAllow": false
  }
}
```

**Note well:** this configuration file will automatically reject all the images
if the server referenced by the webhook configuration is not reachable
(see the `defaultAllow: false` directive).

Create a kubeconfig file `/etc/kubernetes/kube-image-bouncer/kube-image-bouncer.yml` with the
following contents:

```yaml
apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority: /etc/kubernetes/kube-image-bouncer/pki/server.crt
    server: https://image-bouncer-webhook.default.svc:1323/image_policy
  name: bouncer_webhook
contexts:
- context:
    cluster: bouncer_webhook
    user: api-server
  name: bouncer_validator
current-context: bouncer_validator
preferences: {}
users:
- name: api-server
  user:
    client-certificate: /etc/kubernetes/pki/apiserver.crt
    client-key:  /etc/kubernetes/pki/apiserver.key
```

This configuration file instructs the API server to reach the webhook server
at `https://bouncer.local.lan:1323` and use its `/image_policy` endpoint.

We're reusing the certificates from the apiserver and the one for kube-image-bouncer we will generate in the next step.

# Deployment of `ValidatingAdmissionWebhook`

If you are using kubeadm you can rely on the CA already created for you like this:

Create a CSR:
```
cat <<EOF | cfssl genkey - | cfssljson -bare server
{
  "hosts": [
    "image-bouncer-webhook.default.svc",
    "image-bouncer-webhook.default.svc.cluster.local",
    "image-bouncer-webhook.default.pod.cluster.local",
    "192.0.2.24",
    "10.0.34.2"
  ],
  "CN": "system:node:image-bouncer-webhook.default.pod.cluster.local",
  "key": {
    "algo": "ecdsa",
    "size": 256
  },
  "names": [
    {
      "O": "system:nodes"
    }
  ]
}
EOF
```

Then apply it to the cluster
```
cat <<EOF | kubectl apply -f -
apiVersion: certificates.k8s.io/v1
kind: CertificateSigningRequest
metadata:
  name: image-bouncer-webhook.default
spec:
  request: $(cat server.csr | base64 | tr -d '\n')
  signerName: kubernetes.io/kubelet-serving
  usages:
  - digital signature
  - key encipherment
  - server auth
EOF
```

Approve and get your certificate ready to use
```
kubectl get csr image-bouncer-webhook.default -o jsonpath='{.status.certificate}' | base64 --decode > server.crt
```

You can also generate the `validating-webhook-configuration.yaml` file, like this:
```
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: image-bouncer-webook
webhooks:
  - name: image-bouncer-webhook.default.svc
    rules:
      - apiGroups:
          - ""
        apiVersions:
          - v1
        operations:
          - CREATE
        resources:
          - pods
    failurePolicy: Ignore
    sideEffects: None
    admissionReviewVersions: ["v1", "v1beta1"]
    clientConfig:
      caBundle: $(kubectl get csr image-bouncer-webhook.default -o jsonpath='{.status.certificate}')
      service:
        name: image-bouncer-webhook
        namespace: default
```
This could be easily automated, but since this is an example that should be enough to make it work.

## Define Kubernetes objects

First of all you have to create a tls secret holding the webhook certificate
and key (we just generated this in the previous step):

```
kubectl create secret tls tls-image-bouncer-webhook \
  --key server-key.pem \
  --cert server.pem
```

Then create a kubernetes deployment for the `image-bouncer-webhook`:

```
kubectl apply -f kubernetes/image-bouncer-webhook.yaml
```

Finally create `ValidatingWebhookConfiguration` that makes use of
our webhook endpoint:

```
kubectl apply -f kubernetes/validating-webhook-configuration.yaml
```

**Note well:** the `ValidatingWebhookConfiguration` resource defined inside of
`validating-webhook-configuration.yaml` includes a CA certificate. This
is the `server.crt` converted to base64.

As reported by the upstream docs:

> After you create the validating webhook configuration, the system will take a few seconds to honor the new configuration.

## Profit!

It doesn't matter which kind of admission controller you created, the
behaviour will be the same.

Create a `nginx-versioned.yml` file:

```yml
apiVersion: v1
kind: ReplicationController
metadata:
  name: nginx-versioned
spec:
  replicas: 1
  selector:
    app: nginx-versioned
  template:
    metadata:
      name: nginx-versioned
      labels:
        app: nginx-versioned
    spec:
      containers:
      - name: nginx-versioned
        image: nginx:1.13.8
        ports:
        - containerPort: 80
```

Then create the resource:

```
$ kubectl create -f nginx-versioned.yml
```
Ensure the replication controller is actually running:

```
$ kubectl get rc
NAME              DESIRED   CURRENT   READY     AGE
nginx-versioned   1         1         0         2h
```


Now create a `nginx-latest.yml` file:

```yml
apiVersion: v1
kind: ReplicationController
metadata:
  name: nginx-latest
spec:
  replicas: 1
  selector:
    app: nginx-latest
  template:
    metadata:
      name: nginx-latest
      labels:
        app: nginx-latest
    spec:
      containers:
      - name: nginx-latest
        image: nginx
        ports:
        - containerPort: 80
```

Then create the resource:

```
$ kubectl create -f nginx-latest.yml
```

This time the replication controller won't have all the desired pods running:

```
$ kubectl get rc
NAME              DESIRED   CURRENT   READY     AGE
nginx-latest      1         0         0         4s
nginx-versioned   1         1         0         2h
```

Get more details about the `nginx-versioned` replication controller:

```
$ kubectl describe rc nginx-latest
Name:         nginx-latest
Namespace:    default
Selector:     app=nginx-latest
Labels:       app=nginx-latest
Annotations:  <none>
Replicas:     0 current / 1 desired
Pods Status:  0 Running / 0 Waiting / 0 Succeeded / 0 Failed
Pod Template:
  Labels:  app=nginx-latest
  Containers:
   nginx-latest:
    Image:        nginx
    Port:         80/TCP
    Environment:  <none>
    Mounts:       <none>
  Volumes:        <none>
Conditions:
  Type             Status  Reason
  ----             ------  ------
  ReplicaFailure   True    FailedCreate
Events:
  Type     Reason        Age                From                    Message
  ----     ------        ----               ----                    -------
  Warning  FailedCreate  22s (x14 over 1m)  replication-controller  Error creating: pods "nginx-latest-" is forbidden: image policy webhook backend denied one or more images: Images using latest tag are not allowed

```

The culprit is inside of the latest line of the output, the pod creation has
been forbidden by our admission controller with the following message:

> Images using latest tag are not allowed

# Caveats of `ImagePolicyWebhook`

The admission controller is used to vet **all** the containers scheduled to run
inside of the cluster. That includes containers providing core services like
kube-dns, dex, kubedash,... If the image bouncer service is unreachable these
services won't be accepted inside of the cluster (because we set `defaultAllow` to
`false` inside of `/etc/kubernetes/admission_configuration.json`).

We could run the image bouncer on top of the kubernetes cluster, but that
would require its container to be accepted into the cluster, which leads to
a *"chicken-egg"* problem.
