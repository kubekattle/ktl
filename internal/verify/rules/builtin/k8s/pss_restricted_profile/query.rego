package Cx

import data.generic.common as common_lib
import data.generic.k8s as k8sLib

types := {"initContainers", "containers"}

allowed_seccomp(t) {
  lower(t) == "runtimedefault"
}
allowed_seccomp(t) {
  lower(t) == "localhost"
}

is_unconfined(t) {
  lower(t) == "unconfined"
}

missing_ape(document) {
  specInfo := k8sLib.getSpecInfo(document)
  container := specInfo.spec[types[t]][_]
  sc := object.get(container, "securityContext", {})
  ape := object.get(sc, "allowPrivilegeEscalation", true)
  ape != false
}

missing_drop_all(document) {
  specInfo := k8sLib.getSpecInfo(document)
  container := specInfo.spec[types[t]][_]
  sc := object.get(container, "securityContext", {})
  caps := object.get(sc, "capabilities", {})
  drop := object.get(caps, "drop", [])
  not common_lib.inArray(drop, "ALL")
}

missing_run_as_non_root(document) {
  specInfo := k8sLib.getSpecInfo(document)
  container := specInfo.spec[types[t]][_]
  sc := object.get(container, "securityContext", {})
  r := object.get(sc, "runAsNonRoot", false)
  r != true
}

missing_read_only_rootfs(document) {
  specInfo := k8sLib.getSpecInfo(document)
  container := specInfo.spec[types[t]][_]
  sc := object.get(container, "securityContext", {})
  ro := object.get(sc, "readOnlyRootFilesystem", false)
  ro != true
}

missing_seccomp_runtime_default(document) {
  specInfo := k8sLib.getSpecInfo(document)
  spec := specInfo.spec
  podSC := object.get(spec, "securityContext", {})
  podSeccomp := object.get(podSC, "seccompProfile", {})
  podType := object.get(podSeccomp, "type", "")

  container := specInfo.spec[types[t]][_]
  sc := object.get(container, "securityContext", {})
  sp := object.get(sc, "seccompProfile", {})
  ct := object.get(sp, "type", podType)
  not allowed_seccomp(ct)
}

has_unconfined(document) {
  specInfo := k8sLib.getSpecInfo(document)
  spec := specInfo.spec
  podSC := object.get(spec, "securityContext", {})
  podSeccomp := object.get(podSC, "seccompProfile", {})
  podType := object.get(podSeccomp, "type", "")
  is_unconfined(podType)
}

has_unconfined(document) {
  specInfo := k8sLib.getSpecInfo(document)
  container := specInfo.spec[types[t]][_]
  sc := object.get(container, "securityContext", {})
  sp := object.get(sc, "seccompProfile", {})
  ct := object.get(sp, "type", "")
  is_unconfined(ct)
}

missing_checks(document) = parts {
  p1 := [x | x := "allowPrivilegeEscalation=false"; missing_ape(document)]
  p2 := [x | x := "capabilities.drop includes ALL"; missing_drop_all(document)]
  p3 := [x | x := "runAsNonRoot=true"; missing_run_as_non_root(document)]
  p4 := [x | x := "readOnlyRootFilesystem=true"; missing_read_only_rootfs(document)]
  p5 := [x | x := "seccompProfile.type=RuntimeDefault"; missing_seccomp_runtime_default(document)]
  p6 := [x | x := "seccompProfile.type must not be Unconfined"; has_unconfined(document)]

  parts := array.concat(array.concat(array.concat(array.concat(array.concat(p1, p2), p3), p4), p5), p6)
}

CxPolicy[result] {
  document := input.document[i]
  metadata := document.metadata

  specInfo := k8sLib.getSpecInfo(document)
  checks := missing_checks(document)
  count(checks) > 0

  msg := sprintf("PSS restricted profile failed: %s", [concat(", ", checks)])

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "message": msg,
    "issueType": "IncorrectValue",
    "searchKey": sprintf("metadata.name={{%s}}.%s", [metadata.name, specInfo.path]),
    "keyExpectedValue": "restricted profile requirements met",
    "keyActualValue": msg,
    "searchLine": common_lib.build_search_line(split(specInfo.path, "."), [])
  }
}
