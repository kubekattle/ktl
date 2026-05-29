# Firecracker kubeadm HA Lab

This helper no longer builds a LinuxKit + k3s lab. It now creates a
three-control-plane kubeadm cluster with stacked etcd and a real HA API
endpoint in front of it.

That change is deliberate. If you want upstream Kubernetes with kubeadm,
stacked etcd, and a shared control-plane endpoint, a small disk-backed guest OS
with systemd, containerd, kubelet, and kubeadm is the practical shape. The
repo helper now builds that lab directly on the Firecracker host.

The entrypoint is
[scripts/create-your-own-talos.sh](/Users/antonvkrylov/work/torque/scripts/create-your-own-talos.sh:1).

## What It Builds

- `3` Firecracker VMs that act as kubeadm control-plane nodes.
- stacked etcd, one member per control-plane node.
- a host-side HAProxy listener on `${API_LB_IP}:6443` as the shared Kubernetes
  API endpoint.
- Cilium as the default pod network.
- optional extra worker VMs when `WORKER_COUNT` is greater than `0`.

By default the cluster is:

- `CONTROL_PLANE_COUNT=3`
- `WORKER_COUNT=0`
- `KUBERNETES_MINOR=v1.35`
- `POD_CIDR=10.244.0.0/16`
- `SERVICE_CIDR=10.96.0.0/12`
- `NETWORK_PLUGIN=cilium`
- `CILIUM_VERSION=v1.19.4`

That default pairing is intentional: the helper now prefers a stable
Kubernetes/Cilium combination for the next remote validation pass rather than a
newer Kubernetes minor with an unverified Cilium prerelease.

## Host Prerequisites

Run the script as `root` on a Linux host with:

- Docker
- Firecracker
- `curl`, `ip`, `iptables`, `ssh`, `ssh-keygen`
- `unsquashfs` from `squashfs-tools`
- `mkfs.ext4`, `e2fsck`, `resize2fs` from `e2fsprogs`
- `mount`, `umount`, `truncate`, `chroot`
- outbound internet access for:
  - Firecracker guest kernel and Ubuntu rootfs assets
  - Kubernetes apt packages
  - Cilium CLI download and Cilium image pulls
  - optional Flannel manifest download when `NETWORK_PLUGIN=flannel`
  - HAProxy container image pull

The script downloads and caches the guest assets under `CACHE_ROOT`, so the
first run is the slow one.

On hosts that already have the older Firecracker lab assets under
`/opt/firecracker-sandbox-lab`, the helper now reuses those first:

- `/opt/firecracker-sandbox-lab/rootfs.ext4`
- `/opt/firecracker-sandbox-lab/vmlinux.bin`

That avoids rebuilding the base guest image from the Firecracker CI squashfs
when a known-good ext4 rootfs is already available.

## Quick Start

```bash
export RUN_ROOT=/var/lib/kubeadm-firecracker-ha
export CACHE_ROOT=/var/cache/kubeadm-firecracker-ha
export CONTROL_PLANE_COUNT=3
export WORKER_COUNT=0
export SUBNET_PREFIX=198.19.0
export BRIDGE_NAME=k8sha198
export TAP_PREFIX=k8sha198
export FIRECRACKER_BIN=/usr/local/bin/firecracker
export KUBERNETES_MINOR=v1.35
export POD_CIDR=10.244.0.0/16
export SERVICE_CIDR=10.96.0.0/12
export NETWORK_PLUGIN=cilium
export CILIUM_VERSION=v1.19.4
export API_LB_IP="${SUBNET_PREFIX}.5"
export API_LB_PORT=6443
```

Then run:

```bash
./scripts/create-your-own-talos.sh apply
```

The script does the full flow:

1. Downloads a Firecracker kernel and Ubuntu rootfs if needed.
2. Builds a reusable kubeadm-capable base image with containerd, kubelet,
   kubeadm, kubectl, etcdctl, and CNI plugins.
3. Boots the VMs and wires a bridge plus outbound NAT.
4. Starts a host-side HAProxy TCP load balancer for the API server.
5. Initializes the first control plane with `kubeadm init --upload-certs`.
6. Installs Cilium with the official Cilium CLI and waits for `cilium status`.
7. Joins the other two control-plane nodes with `--control-plane`.
8. Verifies node readiness, API endpoint fanout, and the three-member etcd
   cluster.
9. Runs a Kubernetes smoke test and saves the artifacts.

## Expected Result

`status` should show three Ready control-plane nodes:

```text
k8s-00   Ready   control-plane
k8s-01   Ready   control-plane
k8s-02   Ready   control-plane
```

The artifacts directory also includes:

- `etcd-members.txt`: stacked etcd membership proof.
- `kubeconfig.yaml`: admin kubeconfig that points at the HA API endpoint.
- `api-lb-version.json`: a host-side `curl` against `https://${API_LB_IP}:6443/version`.
- `cilium-status.txt`: `cilium status` output from the primary control plane.
- `smoke-pods.txt` and `smoke-job.log`: in-cluster DNS and HTTP proof.

## Access The Cluster

```bash
export KUBECONFIG="${RUN_ROOT}/artifacts/kubeconfig.yaml"
kubectl get nodes -o wide
kubectl get pods -A -o wide
kubectl get --raw /readyz?verbose
```

The kubeconfig already targets the HA endpoint exposed by host HAProxy.

## Optional Validation

If you add workers and want an extra Cilium-specific network pass, enable the
CLI connectivity suite:

```bash
export WORKER_COUNT=2
export CILIUM_CONNECTIVITY_TEST=1
./scripts/create-your-own-talos.sh apply
```

For a fallback kubeadm + Flannel run, set:

```bash
export NETWORK_PLUGIN=flannel
```

## Optional Workers

If you want schedulable workers in addition to the three control-plane nodes:

```bash
export WORKER_COUNT=2
./scripts/create-your-own-talos.sh apply
```

Without workers, the control-plane taints remain in place. The built-in smoke
manifests tolerate them so the validation still works, but ordinary workloads
will not schedule unless you either:

- add worker nodes, or
- remove the control-plane taint yourself.

## Status And Teardown

```bash
./scripts/create-your-own-talos.sh status
./scripts/create-your-own-talos.sh delete
```

`delete` stops the Firecracker VMs, removes the HAProxy container, deletes the
bridge and tap devices, removes the NAT rules, and deletes `RUN_ROOT`. It does
not delete `CACHE_ROOT`, so the downloaded guest assets and prepared base image
can be reused on the next run.
