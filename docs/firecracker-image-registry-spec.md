# Firecracker Image Factory And OCI Registry Spec

Status: design draft.

This spec defines a production path for building, certifying, publishing,
pulling, caching, and running Firecracker VM images through a real operator CLI
and an OCI-compatible registry.

The product sentence:

Secure CI and ops agents with VM isolation, container-speed startup,
cache-aware fleet scheduling, signed image provenance, and signed proof of
cleanup.

The short version:

- `firecrackeradm` is the operator CLI. It builds images from YAML, certifies
  them on real Firecracker hosts, pushes them as OCI artifacts, pulls them for
  inspection, manages hosts, and verifies proof bundles.
- `firecracker-hostd` owns host lifecycle. It pulls and verifies VM artifacts,
  hydrates them into local disk, creates COW job disks, leases warm snapshots,
  runs Firecracker, records evidence, and recovers orphaned resources.
- `firecracker-registry` starts as an OCI-compatible artifact registry profile
  plus optional metadata index, not as a bespoke distribution protocol.
- Local disk is always the hot path. S3 or Mountpoint S3 may distribute durable
  immutable objects across a fleet, but live writable build caches remain local
  and are synchronized through cache manifests, archives, CAS objects, or
  registry cache protocols.
- Scheduling is cache-aware. Jenkins, Torque, or another controller should
  prefer hosts that already have the selected image, snapshot, and requested
  build cache keys hot on local disk.
- Every successful and failed run produces an evidence bundle with image
  digests, certification status, boot timings, cache timings, Docker proof,
  cleanup proof, and artifact hashes.

## Design References

Reference standards and prior art:

- OCI Distribution Specification: registries expose standard `/v2/` manifest
  and blob APIs for content-addressed push and pull.
- OCI Image Manifest Specification: manifests reference a config descriptor and
  content layers by digest.
- Firecracker snapshotting: snapshots are memory and state files; disk files
  are managed by the integrator; snapshot files and rootfs artifacts must be
  authenticated and lifecycle-managed outside Firecracker.
- `ingresslabs/firecracker-sandbox`: good local ergonomics for kernel/rootfs
  build, VM config, networking, console, and snapshot operations. This spec
  turns that style of workflow into a signed, registry-backed, fleet-ready
  product surface.

## Goals

1. Build a VM image from a declarative YAML file with reproducible outputs.
2. Store Firecracker VM artifacts in an OCI-compatible registry so existing
   registry auth, replication, retention, digest pinning, and scanning tooling
   can be reused.
3. Certify images by booting real Firecracker VMs and running functional tests
   before an image is allowed into a production pool.
4. Support fast startup through hydrated local base images, COW disks, warm
   snapshots, and preboot pools.
5. Make image placement cache-aware at fleet scale.
6. Keep the local mode first-class: a single host with local disk and no S3
   must work by default.
7. Use S3 or Mountpoint S3 only as a fleet distribution and durable object
   layer, never as the live writable filesystem for job caches.
8. Make cleanup auditable. A stopped, failed, or aborted job must leave signed
   proof that derived disks, leases, tap devices, and Firecracker processes were
   cleaned while the immutable base image remains.
9. Provide an E2E certification profile that proves real jobs, Docker inside
   Firecracker, parallel agents, cleanup, restart recovery, and startup speed.

## Non-Goals

- Do not invent a new registry protocol for blobs and manifests.
- Do not mount S3 directly as `/var/lib/docker`, `~/.m2`, `~/.gradle`,
  `~/.npm`, `~/go/pkg/mod`, or Terraform provider cache directories inside
  jobs.
- Do not share one mutable Docker graph directory across concurrent VMs.
- Do not let Jenkins shell scripts own Firecracker process lifecycle forever.
  Jenkins should request leases from `firecracker-hostd`.
- Do not treat a tag as a trust anchor. Production scheduling must pin and log
  immutable digests.
- Do not make snapshot speed hide correctness. Warm restore must still prove
  identity reset, network reset, cache policy, and cleanup.

## Component Model

### `firecrackeradm`

`firecrackeradm` is the operator CLI.

Initial command surface:

```bash
firecrackeradm image build -f firecracker-image.yaml --out ./dist/fc-image
firecrackeradm image certify ./dist/fc-image --profile ci-docker
firecrackeradm image push ./dist/fc-image registry.example.com/fc/ubuntu-docker:24.04
firecrackeradm image pull registry.example.com/fc/ubuntu-docker@sha256:<digest> --out ./dist/pulled
firecrackeradm image inspect registry.example.com/fc/ubuntu-docker:24.04

firecrackeradm registry serve --listen :5000 --storage /var/lib/firecracker-registry
firecrackeradm hosts
firecrackeradm images
firecrackeradm certify host-141
firecrackeradm drain host-141
firecrackeradm verify-proof build-123
firecrackeradm support-bundle host-141
```

The CLI owns UX, config parsing, local OCI layout creation, remote registry
push/pull, and operator workflows. It should not own VM process supervision
after a host daemon exists.

### `firecracker-hostd`

`firecracker-hostd` is the production host daemon.

It owns:

- Firecracker process lifecycle.
- API socket lifecycle.
- tap networking and cleanup.
- local image cache hydration.
- COW disk creation and deletion.
- snapshot pool lease state.
- VM lease state on disk.
- job cache import/export through a cache gateway.
- orphan recovery after hostd restart or host reboot.
- `/metrics`, `/healthz`, `/support-bundle`, and evidence endpoints.

`firecracker-hostd` MUST expose an API where controllers ask for a lease and
receive SSH information, startup timing, cache timing, and evidence locations.

### `firecracker-registry`

The registry path has two layers:

1. The OCI distribution layer stores manifests and blobs.
2. The Firecracker metadata layer indexes compatibility, certification, SLO,
   image families, trust domains, and fleet residency.

The first production implementation SHOULD use an existing OCI registry or an
OCI library. A custom `firecracker-registry` service MAY exist for local labs
and metadata APIs, but any registry claiming OCI compatibility MUST pass OCI
Distribution conformance for the implemented categories.

### Scheduler Integration

Jenkins, Torque, or another scheduler should not pick a host only by CPU and
memory.

Placement inputs:

- image digest.
- kernel digest.
- rootfs digest.
- snapshot digest.
- required architecture and Firecracker version.
- requested CPU and memory.
- trust domain.
- cache keys and cache policy.
- host health.
- host warm pool capacity.
- current lease pressure.
- host-local cache residency.

Placement output MUST include placement reasons:

```json
{
  "host": "host-141",
  "image": "registry.example.com/fc/ubuntu-docker@sha256:...",
  "reasons": [
    "image-local-hit",
    "snapshot-warm-hit",
    "docker-cache-local-hit",
    "trust-domain-match",
    "capacity-available"
  ],
  "misses": [],
  "estimatedStartupMs": 830
}
```

## Image YAML Contract

The image file is the source document for building an immutable Firecracker VM
artifact.

Example:

```yaml
apiVersion: firecracker.io/v1alpha1
kind: FirecrackerImage
metadata:
  name: ubuntu-docker
  version: "24.04"
  labels:
    purpose: jenkins-agent
    trustDomain: ci/default

build:
  architecture: x86_64
  base:
    type: debootstrap
    suite: noble
    mirror: http://archive.ubuntu.com/ubuntu
  kernel:
    source: file://./kernels/vmlinux-6.8
    cmdline: console=ttyS0 reboot=k panic=1 pci=off
  rootfs:
    format: ext4
    size: 8Gi
    compression: zstd
  packages:
    apt:
      - ca-certificates
      - curl
      - git
      - openssh-server
      - docker.io
      - docker-buildx
      - openjdk-21-jre-headless
  files:
    - source: ./agent/bootstrap.sh
      target: /usr/local/bin/jenkins-agent-bootstrap
      mode: "0755"
  run:
    - systemctl enable ssh
    - systemctl enable docker
    - useradd -m -s /bin/bash jenkins
    - usermod -aG docker jenkins

runtime:
  cpus: 2
  memory: 2Gi
  network:
    mode: tap
  ssh:
    user: jenkins
    authorizedKeys: runtime-injected
  docker:
    required: true
    proofCommand: docker run --rm hello-world

cache:
  defaults:
    mode: local-hot
    exportOnSuccess: true
  paths:
    maven:
      guest: /home/jenkins/.m2/repository
      key: maven:${lockHash}
      strategy: archive-cas
    gradle:
      guest: /home/jenkins/.gradle/caches
      key: gradle:${lockHash}
      strategy: archive-cas
    npm:
      guest: /home/jenkins/.npm
      key: npm:${lockHash}
      strategy: archive-cas
    terraform:
      guest: /home/jenkins/.terraform.d/plugin-cache
      key: terraform:${lockHash}
      strategy: archive-cas
    docker:
      strategy: buildkit-registry
      ref: registry.example.com/cache/buildkit/ubuntu-docker

snapshot:
  warm: true
  name: ssh-docker-ready
  readiness:
    - systemctl is-active ssh
    - systemctl is-active docker
    - docker info
  sanitize:
    machineId: regenerate
    sshHostKeys: regenerate
    journal: truncate

certify:
  profiles:
    - name: ci-docker
      tests:
        - ssh-ready
        - docker-info
        - docker-build
        - docker-network
        - docker-volume
        - git-checkout
        - cache-import-export
        - cleanup-proof
  slo:
    coldBootP95: 15s
    warmRestoreP95: 1500ms

publish:
  repository: registry.example.com/fc/ubuntu-docker
  tags:
    - "24.04"
    - "24.04-docker"
  sign: cosign
  sbom: spdx
```

Required fields:

- `apiVersion`.
- `kind`.
- `metadata.name`.
- `metadata.version`.
- `build.architecture`.
- `build.kernel`.
- `build.rootfs`.
- `runtime.ssh.user`.

The builder MUST reject image YAML that does not produce a deterministic image
identity. Allowed sources must resolve to pinned digests, immutable file hashes,
or package repository snapshots before production signing.

## OCI Artifact Layout

Firecracker VM images MUST be published as OCI manifests or OCI artifacts with
Firecracker-specific media types.

The manifest SHOULD use:

- `application/vnd.oci.image.manifest.v1+json` as the manifest media type.
- `artifactType: application/vnd.firecracker.image.v1` when supported by the
  registry client and server.
- a config descriptor with Firecracker image metadata.
- layer descriptors for kernel, rootfs, VM config, snapshot files, SBOM,
  provenance, and certification evidence.

Media types:

```text
application/vnd.firecracker.image.config.v1+json
application/vnd.firecracker.kernel.v1
application/vnd.firecracker.rootfs.ext4.v1+zstd
application/vnd.firecracker.vmconfig.v1+json
application/vnd.firecracker.snapshot.state.v1+json
application/vnd.firecracker.snapshot.memory.v1+zstd
application/vnd.firecracker.certification.v1+json
application/vnd.firecracker.provenance.v1+json
application/vnd.firecracker.sbom.spdx.v1+json
application/vnd.firecracker.cleanup-proof.v1+json
```

Example config blob:

```json
{
  "schemaVersion": "firecracker.image.config/v1",
  "name": "ubuntu-docker",
  "version": "24.04",
  "architecture": "x86_64",
  "firecracker": {
    "minimumVersion": "1.7.0",
    "snapshotFormat": "1.0.0"
  },
  "runtime": {
    "cpus": 2,
    "memoryBytes": 2147483648,
    "sshUser": "jenkins",
    "dockerRequired": true
  },
  "artifacts": {
    "kernel": "sha256:...",
    "rootfs": "sha256:...",
    "vmConfig": "sha256:...",
    "snapshotState": "sha256:...",
    "snapshotMemory": "sha256:..."
  },
  "certification": {
    "required": true,
    "profile": "ci-docker",
    "digest": "sha256:..."
  }
}
```

Required annotations:

```text
org.opencontainers.image.title
org.opencontainers.image.version
org.opencontainers.image.created
io.firecracker.image.name
io.firecracker.image.version
io.firecracker.image.architecture
io.firecracker.image.kernel.digest
io.firecracker.image.rootfs.digest
io.firecracker.image.certification.digest
io.firecracker.image.snapshot.digest
```

Tags are convenience aliases. Controllers MUST resolve tags to digests before
leasing a VM and MUST record the digest in job evidence.

## Registry API

The OCI layer MUST support standard registry operations for the claimed
conformance level:

```text
GET  /v2/
HEAD /v2/<name>/manifests/<reference>
GET  /v2/<name>/manifests/<reference>
PUT  /v2/<name>/manifests/<reference>
HEAD /v2/<name>/blobs/<digest>
GET  /v2/<name>/blobs/<digest>
POST /v2/<name>/blobs/uploads/
PATCH /v2/<name>/blobs/uploads/<uuid>
PUT  /v2/<name>/blobs/uploads/<uuid>?digest=<digest>
GET  /v2/<name>/referrers/<digest>
```

The Firecracker metadata API is optional in local mode and recommended in fleet
mode:

```text
GET  /api/v1/images
GET  /api/v1/images/<repository>/<reference>
GET  /api/v1/images/<digest>/certifications
GET  /api/v1/images/<digest>/residency
POST /api/v1/certifications
POST /api/v1/residency
```

Metadata API records are derived from immutable registry digests and hostd
reports. The metadata layer MUST NOT become the source of truth for blob
contents.

## Local Host Cache

Local disk is the hot path.

Suggested layout:

```text
/var/lib/firecracker-hostd/
  cache/
    oci/
      blobs/sha256/<digest>
      manifests/sha256/<digest>.json
    images/
      sha256-<image-digest>/
        image.json
        kernel
        rootfs.base
        vm-config.json
        certification.json
    snapshots/
      sha256-<snapshot-digest>/
        mem
        state
        metadata.json
    job-cache/
      cas/sha256/<digest>
      archives/<cache-key>.tar.zst
      manifests/<cache-key>.json
  leases/
    vm/<lease-id>.json
    snapshot/<lease-id>.json
  runs/
    <run-id>/
      evidence.json
      console.log
      firecracker.log
      cleanup-proof.json
```

Hostd hydration rules:

- Pull blobs by digest.
- Verify every blob hash before it enters the hydrated image directory.
- Decompress into a staging directory.
- fsync content and metadata where supported.
- Atomically rename staging into the final digest directory.
- Treat hydrated image directories as immutable.
- Create per-job COW disks from immutable base rootfs files.
- Delete derived COW disks at release, abort, failure, daemon restart recovery,
  or host reboot recovery.
- Never delete the base image merely because a job stopped.

## Distributed Cache Plane

There are three separate cache classes:

1. VM artifact cache: kernel, rootfs, VM config, snapshots, certification.
2. Build dependency cache: Maven, Gradle, npm, Go modules, Terraform providers.
3. Docker build cache: BuildKit layers and registry cache metadata.

They have different correctness rules.

### VM Artifact Cache

VM artifacts are immutable and content-addressed. They are safe to distribute
through OCI registries, S3, Mountpoint S3, object storage replication, or host
prewarming.

Mountpoint S3 may expose:

```text
/mnt/firecracker-cache/base-images/*.base
/mnt/firecracker-cache/snapshots/*.mem
/mnt/firecracker-cache/snapshots/*.state
/mnt/firecracker-cache/oci/blobs/sha256/*
```

Hostd still hydrates to local disk before running jobs:

```text
/var/lib/firecracker-hostd/cache/images/<digest>/rootfs.base
/var/lib/firecracker-hostd/cache/snapshots/<digest>/mem
/var/lib/firecracker-hostd/cache/snapshots/<digest>/state
```

This scales to hundreds of hosts when:

- objects are immutable and digest-addressed.
- hosts use local disk for runtime.
- prewarming is rate-limited and jittered.
- scheduler prefers local hits.
- object store GET pressure is measured.
- large rollouts use regional buckets, CDN, registry mirrors, or staged
  prefetch waves.

### Build Dependency Cache

Dependency caches are mutable from the tool point of view. They MUST NOT be
shared as live writable directories across VMs.

Supported model:

- Import a cache archive or CAS view at job start.
- Make it writable only inside the VM or job overlay.
- On successful job completion, export changed content into a new immutable
  archive or CAS manifest.
- Publish the new manifest atomically.
- Let future jobs choose the newest compatible manifest by key and policy.

Guest paths:

```text
/home/jenkins/.m2/repository
/home/jenkins/.gradle/caches
/home/jenkins/.npm
/home/jenkins/go/pkg/mod
/home/jenkins/.terraform.d/plugin-cache
```

Host-side backing:

```text
/var/lib/firecracker-hostd/cache/job-cache/archives/<key>.tar.zst
/var/lib/firecracker-hostd/cache/job-cache/cas/sha256/<digest>
/var/lib/firecracker-hostd/cache/job-cache/manifests/<key>.json
```

Distributed backing:

```text
s3://<bucket>/firecracker-cache/job-cache/archives/<key>.tar.zst
s3://<bucket>/firecracker-cache/job-cache/cas/sha256/<digest>
s3://<bucket>/firecracker-cache/job-cache/manifests/<key>.json
```

Mountpoint S3 can read and write immutable archives and CAS objects, but the
cache gateway owns concurrency, manifest locking, validation, and promotion.

### Docker Build Cache

Docker inside the VM needs special handling.

Default:

- Each VM gets its own writable `/var/lib/docker` on a per-job derived disk.
- That derived disk is deleted at cleanup.
- The image can include Docker and BuildKit binaries, but not a shared mutable
  Docker graph.

Recommended cache strategies:

1. BuildKit registry cache:

   ```bash
   docker buildx build \
     --cache-from type=registry,ref=registry.example.com/cache/api:buildkit \
     --cache-to type=registry,ref=registry.example.com/cache/api:buildkit,mode=max \
     .
   ```

2. BuildKit local cache through hostd cache gateway:

   ```bash
   docker buildx build \
     --cache-from type=local,src=/mnt/job-cache/buildkit/api \
     --cache-to type=local,dest=/mnt/job-cache/buildkit/api-new,mode=max \
     .
   ```

3. Remote BuildKit sidecar per host with registry or S3 cache exports.

The first version SHOULD prioritize BuildKit registry cache because it already
has content addressing and concurrency semantics. Hostd can still report cache
hit/miss timing by parsing BuildKit metadata or recording cache import/export
steps.

## Cache Policy

Cache policy is part of the lease request.

Example:

```json
{
  "image": "registry.example.com/fc/ubuntu-docker@sha256:...",
  "cache": {
    "docker": {
      "strategy": "buildkit-registry",
      "ref": "registry.example.com/cache/api:buildkit",
      "mode": "read-write-on-success"
    },
    "maven": {
      "key": "maven:sha256-lock-abc",
      "strategy": "archive-cas",
      "mode": "read-write-on-success"
    },
    "npm": {
      "key": "npm:sha256-lock-def",
      "strategy": "archive-cas",
      "mode": "read-only"
    }
  }
}
```

Policy modes:

- `disabled`: do not import or export.
- `read-only`: import only.
- `read-write-on-success`: import, then export only when the job succeeds.
- `read-write-always`: import and export even after failure. This is useful for
  diagnostic caches but MUST be opt-in.
- `ephemeral`: use a local cache only for the job and delete it afterward.

Every job evidence bundle MUST report cache policy, resolved keys, local hit or
miss, distributed hit or miss, import timing, export timing, bytes read, bytes
written, and promotion result.

## Snapshot And Warm Pool Model

Snapshot artifacts are optional image layers. Warm snapshot pools are runtime
state.

Terms:

- `snapshot artifact`: immutable memory and state files published with an image.
- `warm snapshot`: host-local hydrated snapshot ready for fast restore.
- `preboot VM`: already running paused or idle VM waiting for a job lease.
- `snapshot lease`: a reservation of a warm snapshot or preboot VM for one job.

Rules:

- Snapshot files MUST be tied to the exact Firecracker version, kernel digest,
  rootfs digest, VM config digest, CPU template, architecture, and memory size.
- Snapshot restore MUST reset per-VM identity: machine-id, SSH host keys when
  required, hostname, DHCP identity, MMDS data, job token, and agent secret.
- Network connections are not assumed to survive restore.
- Warm pool scheduler MUST maintain a target SLO, for example p95 startup under
  3 seconds, by auto-sizing pools based on queue pressure and measured misses.
- Snapshot leases MUST expire and be recovered after daemon restart.
- A preboot VM that has received job secrets MUST never be returned to a clean
  pool.

Expected benchmark evidence:

```json
{
  "image": "registry.example.com/fc/ubuntu-docker@sha256:...",
  "coldBootMs": 9200,
  "warmRestoreMs": 420,
  "sshReadyMs": 780,
  "dockerReadyMs": 950,
  "speedup": 11.8,
  "samples": 25,
  "p50WarmRestoreMs": 390,
  "p95WarmRestoreMs": 830
}
```

## Job Evidence Bundle

Each job MUST emit an artifact bundle.

Required files:

```text
evidence.json
vm-boot.log
firecracker-console.log
firecracker.log
ssh-timing.json
image-cache-timing.json
job-cache-timing.json
docker-proof.json
cleanup-proof.json
artifacts-manifest.json
```

`evidence.json` minimum fields:

```json
{
  "schemaVersion": "firecracker.job.evidence/v1",
  "runId": "build-123",
  "host": "host-141",
  "imageRef": "registry.example.com/fc/ubuntu-docker:24.04",
  "imageDigest": "sha256:...",
  "kernelDigest": "sha256:...",
  "rootfsDigest": "sha256:...",
  "snapshotDigest": "sha256:...",
  "leaseId": "lease-...",
  "startup": {
    "pullMs": 0,
    "hydrateMs": 0,
    "cowCreateMs": 74,
    "firecrackerStartMs": 220,
    "sshReadyMs": 810,
    "dockerReadyMs": 990
  },
  "cache": {
    "imageLocalHit": true,
    "snapshotWarmHit": true,
    "dockerCache": "hit",
    "mavenCache": "miss"
  },
  "cleanup": {
    "derivedDiskDeleted": true,
    "baseImagePresent": true,
    "tapDeleted": true,
    "processExited": true,
    "snapshotLeaseReleased": true,
    "proofDigest": "sha256:..."
  }
}
```

Cleanup proof MUST be signed by hostd and include:

- host identity.
- hostd version.
- run ID and lease ID.
- image digest.
- derived disk path and deletion status.
- base image path and presence status.
- Firecracker PID and exit status.
- tap device and cleanup status.
- snapshot lease release status.
- cache export status.
- timestamp.
- payload digest.
- signature and public key ID.

## Security Model

Trust anchors:

- registry authentication and authorization.
- image signing keys.
- hostd identity keys.
- certification signer keys.
- optional transparency log or append-only proof log.

Required checks before a host runs an image:

1. Resolve tag to digest.
2. Pull manifest by digest.
3. Verify all blob digests.
4. Verify image signature when policy requires it.
5. Verify certification evidence matches image digest.
6. Verify host compatibility.
7. Verify trust domain policy.
8. Verify cache policy does not cross forbidden trust boundaries.

Trust domain examples:

```text
ci/default
ci/untrusted-pr
prod/release-signing
lab/firecracker-e2e
```

Cache objects MUST include a trust domain and producer identity. A cache written
by `ci/untrusted-pr` MUST NOT be imported into `prod/release-signing` unless an
explicit promotion policy validates and re-signs it.

## Host Join And Certification

Host installation:

```bash
firecracker-hostd install \
  --join https://jenkins.example.com \
  --token "$FIRECRACKER_JOIN_TOKEN" \
  --cache-dir /var/lib/firecracker-hostd/cache \
  --state-dir /var/lib/firecracker-hostd
```

Install validation MUST check:

- KVM availability.
- Firecracker binary availability and version.
- cgroups v2 when snapshots are enabled.
- tap networking tools or configured network backend.
- COW disk backend support.
- local cache directory permissions and capacity.
- registry connectivity.
- optional S3 or Mountpoint S3 connectivity.
- Jenkins or scheduler connectivity.
- ability to run a certification VM.

Certification output:

```json
{
  "host": "host-141",
  "kvm": true,
  "firecrackerVersion": "1.7.0",
  "network": true,
  "cowDisk": true,
  "registryPull": true,
  "localCache": true,
  "snapshotRestore": true,
  "dockerInsideVm": true,
  "cleanupProof": true,
  "parallelVmCapacity": 30,
  "certifiedAt": "2026-06-01T00:00:00Z"
}
```

## Jenkins Pipeline API

The Jenkins plugin should expose a Jenkins-native runtime, not only a cloud
backend.

Scripted pipeline:

```groovy
firecrackerAgent(
  image: 'registry.example.com/fc/ubuntu-docker:24.04',
  cpus: 2,
  memory: '2g',
  cache: [
    docker: 'buildkit-registry',
    maven: 'archive-cas'
  ],
  cachePolicy: 'read-write-on-success',
  evidence: 'archive',
  trustDomain: 'ci/default'
) {
  sh 'docker buildx build --cache-from type=registry,ref=registry.example.com/cache/api:buildkit .'
}
```

Declarative pipeline:

```groovy
pipeline {
  agent {
    firecracker {
      image 'registry.example.com/fc/ubuntu-docker:24.04'
      cpus 2
      memory '2g'
      cache docker: 'buildkit-registry', maven: 'archive-cas'
      cachePolicy 'read-write-on-success'
      evidence 'archive'
      trustDomain 'ci/default'
    }
  }
  stages {
    stage('Build') {
      steps {
        sh 'docker build .'
      }
    }
  }
}
```

Wrapper API:

```groovy
firecrackerCache(cache: [maven: 'maven:${pomHash}', docker: 'buildkit']) {
  sh 'mvn test'
}

firecrackerArchiveEvidence()
```

Matrix API:

```groovy
firecrackerMatrix(
  image: ['ubuntu-docker-24.04', 'debian-docker-12'],
  cpus: [2, 4]
) {
  sh './ci/test.sh'
}
```

The UI MUST let users select certified base images by name, tag, digest,
certification profile, and trust domain. The UI MUST show whether the selected
image is signed, certified, locally cached on any hosts, and compatible with
Docker-in-VM jobs.

## Firecracker Registry Product Shape

The registry experience should feel like this:

```bash
firecrackeradm image build -f images/ubuntu-docker.yaml --out dist/ubuntu-docker
firecrackeradm image certify dist/ubuntu-docker --host host-141 --profile ci-docker
firecrackeradm image push dist/ubuntu-docker registry.example.com/fc/ubuntu-docker:24.04

firecrackeradm images list --registry registry.example.com
firecrackeradm images inspect registry.example.com/fc/ubuntu-docker:24.04
firecrackeradm images prewarm registry.example.com/fc/ubuntu-docker:24.04 --hosts host-141,host-142
```

`firecrackeradm images inspect` should show:

- tags and digest.
- artifact sizes.
- kernel/rootfs/snapshot digests.
- Firecracker version compatibility.
- architecture.
- certification profiles and results.
- SBOM and provenance availability.
- signing status.
- known host residency.
- expected cold and warm startup timings.

## End-To-End Certification Suite

The main real E2E command:

```bash
./mvnw verify -Pfirecracker-e2e
```

or for the hostd/adm module:

```bash
go test -tags=firecracker_e2e ./...
```

The E2E suite MUST be able to run against a real host:

```bash
FIRECRACKER_E2E_HOST=root@141.105.65.227 \
FIRECRACKER_E2E_PARALLEL_VMS=30 \
FIRECRACKER_E2E_REGISTRY=registry.example.com \
./scripts/firecracker-e2e.sh
```

Required scenarios:

1. Build image from YAML.
2. Push image to OCI registry.
3. Pull image by digest.
4. Certify image boots in real Firecracker.
5. Prove SSH readiness timing.
6. Prove Docker daemon starts inside the VM.
7. Run Docker build inside the VM.
8. Run Docker network scenario inside the VM.
9. Run Docker volume scenario inside the VM.
10. Checkout from Gitea inside the VM.
11. Build a real Terraform repository or similarly complex project.
12. Run Maven or Gradle cache import/export.
13. Run npm cache import/export.
14. Run Terraform provider cache import/export.
15. Run one failed job and prove cleanup.
16. Run one aborted job and prove cleanup.
17. Restart Jenkins or scheduler and prove lease recovery.
18. Restart hostd and prove orphan recovery.
19. Optional destructive profile: reboot host and prove recovery.
20. Run 20 to 30 VMs in parallel across one or more hosts.
21. Run multiple Jenkins agents concurrently and prove scheduling reasons.
22. Prove derived disks are deleted when jobs stop.
23. Prove base images remain after jobs stop.
24. Prove warm snapshot startup is faster than cold boot.
25. Archive evidence for every job.

Speed proof:

- collect at least 20 cold boot samples per image.
- collect at least 20 warm restore samples per image.
- report p50, p90, p95, p99.
- fail the certification profile if warm restore p95 exceeds the configured
  image SLO.
- record host model, kernel version, Firecracker version, cgroup mode, storage
  backend, and image digest.

Cleanup proof checks:

```text
derived_disk_deleted == true
base_image_present == true
firecracker_process_exited == true
tap_deleted == true
snapshot_lease_released == true
signature.valid == true
```

## Implementation Plan

### Phase 1: Spec Contracts And Local OCI Layout

Deliver:

- YAML structs and validation.
- `firecrackeradm image build --dry-run`.
- local OCI layout writer.
- digest calculation and artifact manifest.
- unit tests for schema validation, deterministic digesting, and media types.

Acceptance:

```bash
firecrackeradm image build -f testdata/images/ubuntu-docker.yaml --out ./dist/image --dry-run
firecrackeradm image inspect ./dist/image
```

### Phase 2: Registry Push/Pull

Deliver:

- `firecrackeradm image push`.
- `firecrackeradm image pull`.
- auth through standard registry config.
- local test registry or OCI registry test harness.
- digest-pinned pull.
- tag resolution recording.

Acceptance:

```bash
firecrackeradm registry serve --listen 127.0.0.1:5000 --storage ./runtime/registry
firecrackeradm image push ./dist/image 127.0.0.1:5000/fc/ubuntu-docker:dev
firecrackeradm image pull 127.0.0.1:5000/fc/ubuntu-docker:dev --out ./dist/pulled
diff <(firecrackeradm image inspect ./dist/image --format json) \
     <(firecrackeradm image inspect ./dist/pulled --format json)
```

### Phase 3: Hostd Pull And Hydration

Deliver:

- hostd image pull API.
- local cache layout.
- atomic hydration.
- local hit/miss metrics.
- support bundle entries.

Acceptance:

```bash
firecracker-hostd image pull 127.0.0.1:5000/fc/ubuntu-docker:dev
firecrackeradm hosts image-residency host-141
```

### Phase 4: Real Image Builder

Deliver:

- rootfs builder.
- package installer.
- file injection.
- kernel and VM config packaging.
- optional snapshot creation.
- SBOM and provenance.

Acceptance:

```bash
firecrackeradm image build -f images/ubuntu-docker.yaml --out dist/ubuntu-docker
firecrackeradm image certify dist/ubuntu-docker --host host-141 --profile ci-docker
```

### Phase 5: Jenkins And Pipeline Integration

Deliver:

- image selector UI.
- `firecrackerAgent`.
- `firecrackerCache`.
- `firecrackerArchiveEvidence`.
- `firecrackerMatrix`.
- Declarative `agent { firecracker { ... } }`.
- evidence archiving by default.

Acceptance:

```groovy
firecrackerAgent(image: 'ubuntu-docker-24.04', cache: [docker: 'buildkit']) {
  sh 'docker build .'
}
```

### Phase 6: Warm Snapshots And SLO Scheduler

Deliver:

- snapshot artifact support.
- warm snapshot lease path.
- preboot pool manager.
- SLO-driven pool sizing.
- placement reasons.

Acceptance:

- p95 startup under configured SLO for warm pool jobs.
- measured cold versus warm speedup in evidence.
- no secret reuse across snapshot leases.

### Phase 7: Distributed Cache Gateway

Deliver:

- host-local job cache gateway.
- archive/CAS import/export.
- optional S3/Mountpoint backing for immutable cache objects.
- BuildKit registry cache integration.
- cache timing evidence.

Acceptance:

- Maven/Gradle/npm/Terraform caches import and export correctly.
- Docker build cache hits across hosts through registry cache.
- S3/Mountpoint is never mounted as the live writable tool cache.

### Phase 8: Fleet Scale And Recovery

Deliver:

- multi-host scheduler.
- host drain.
- host reboot recovery.
- 20 to 30 parallel VM proof.
- quotas and trust domains.
- registry residency index.

Acceptance:

- scheduler chooses local-hit hosts when possible.
- failed and aborted jobs clean derived resources.
- hostd restart recovers leases.
- optional host reboot test recovers orphaned resources.

## Test Matrix

Unit tests:

- YAML parser accepts valid image specs.
- YAML parser rejects missing required fields.
- media type list is stable.
- digest calculation is deterministic.
- tag references resolve to immutable digests.
- cache policy validation rejects unsafe trust-domain crossings.
- cleanup proof signature verifies.
- placement reason calculation is deterministic.

Integration tests:

- local OCI layout round trip.
- local registry push/pull round trip.
- hostd hydration from registry.
- cache archive import/export.
- BuildKit registry cache config generation.
- support bundle includes image and cache evidence.

Real E2E tests:

- image build and certify on a KVM host.
- Jenkins job on one Firecracker VM.
- Jenkins job with Docker build inside Firecracker.
- Jenkins job with Docker network and volume inside Firecracker.
- Jenkins job with Gitea checkout.
- complex Terraform build/test job.
- 10 scenario job suite.
- 20 to 30 parallel VM suite.
- failed job cleanup.
- aborted job cleanup.
- hostd restart recovery.
- Jenkins restart recovery.
- optional host reboot recovery.

## Open Design Decisions

1. Registry client library:
   - Preferred: ORAS or `go-containerregistry` for push/pull and auth.
   - Requirement: support non-container artifacts and digest-pinned pulls.

2. Registry service:
   - Preferred first production path: use an existing OCI registry.
   - Local lab path: small `firecrackeradm registry serve` for tests.
   - Full custom registry only if conformance, auth, retention, and operations
     are funded as product work.

3. Rootfs builder:
   - Options: debootstrap, mmdebstrap, distro-specific cloud images, or
     container-to-rootfs conversion.
   - Requirement: record exact source digests and package snapshot metadata.

4. Snapshot portability:
   - Snapshots are fast but compatibility-sensitive.
   - Policy should default to host-family compatibility, not global portability.

5. Cache gateway transport:
   - Options: SSH copy, vsock, HTTP from guest, 9p, block device, or agent
     sidecar.
   - First version should prefer simple archive import/export over live mounts.

6. Signing:
   - Image signing can use cosign or an internal ed25519 proof format.
   - Cleanup proof should remain hostd-signed.
   - Certification proof should be signed by the certifier identity.

## Hard Requirements Before Production

- OCI artifacts are immutable and digest verified.
- Host-local hydrated image directories are immutable.
- Derived disks are per job and always cleaned.
- Base images are retained unless explicitly garbage-collected.
- Writable caches are never shared concurrently without a protocol.
- S3/Mountpoint is not used as a live writable tool cache.
- Snapshot restores reset per-job identity and secrets.
- Scheduler records placement reasons.
- Every job archives evidence.
- Every cleanup proof is signed.
- The E2E suite runs real Firecracker jobs, not mocks only.
