DISCONTINUATION OF PROJECT

This project will no longer be maintained by Intel.

Intel has ceased development and contributions including, but not limited to, maintenance, bug fixes, new releases, or updates, to this project.  

Intel no longer accepts patches to this project.

If you have an ongoing need to use this project, are interested in independently developing it, or would like to maintain patches for the open source software community, please create your own fork of this project.  

Contact: webadmin@linux.intel.com
# Container Device Interface (CDI) for Kubernetes

**Documentation**

[Container Device Interface](https://docs.google.com/document/d/1Tc0Kc4GDWx1gFvGQbBUizudSuND6Kq8GiH7KVm_X5eg/edit#heading=h.5lakm98lya8j)

**Prerequisites**

Prerequisites for building and running CDI include:

- Appropriate hardware
- A fully configured [Kubernetes cluster](https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/) with Docker runtime
- A working [Go environment](https://golang.org/doc/install), of at least version v1.14.4

**Installation**

1. Build modified external-provisioner image csi-provisioner:cdi

```
$ git clone ssh://git@gitlab.devtools.intel.com:29418/kubernetes/device-plugins/external-provisioner.git
$ cd external-provisioner
$ make container
$ docker tag csi-provisioner csi-provisioner:cdi
```

2. Clone cdi repo
```
$ git clone ssh://git@gitlab.devtools.intel.com:29418/kubernetes/device-plugins/cdi.git
```

3. Install cfssl and cfssljson
```
$ sudo apt-get install golang-cfssl
```

4. Create cdi-registry-secrets and cdi-node-secrets
```
$ cd cdi
$ KUBECONFIG=~/.kube/config ./deploy/setup-ca-k8s.sh
```

5. Build cdi-driver:canary image
```
$ make image
```

6. Build runc wrapper
```
$ make cdi-runc
```

7. Replace runc with a wrapper
```
$ sudo cp /usr/bin/runc /usr/bin/runc.orig
$ sudo cp _output/cdi-runc /usr/bin/runc
```

8. Build deploy/kubernetes-1.18/cdi.yaml
```
$ make kustomize
```

9. Label device nodes
```
$ kubectl label node <node name> storage=cdi
```

10. Deploy cdi driver and its dependencies
```
$ kubectl create -f deploy/kubernetes-1.18/cdi.yaml
```

11. Create FPGA storage class and PVC
```
$ kubectl create -f example/arria10.nlb0/sc.yaml
$ kubectl create -f example/arria10.nlb0/pvc.yaml
```

12. Run example workload
```
$ kubectl create -f example/arria10.nlb0/pod.yaml
```