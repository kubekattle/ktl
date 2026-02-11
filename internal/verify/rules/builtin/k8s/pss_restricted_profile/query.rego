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

ignore_check(document, code) {
  ktl := object.get(document, "ktl", {})
  list := object.get(ktl, "ignoreChecks", [])
  some i
  lower(list[i]) == lower(code)
}

fail_ape(document) {
  not ignore_check(document, "ape")
  specInfo := k8sLib.getSpecInfo(document)
  container := specInfo.spec[types[t]][_]
  sc := object.get(container, "securityContext", {})
  ape := object.get(sc, "allowPrivilegeEscalation", true)
  ape != false
}

fail_drop_all(document) {
  not ignore_check(document, "drop_all")
  specInfo := k8sLib.getSpecInfo(document)
  container := specInfo.spec[types[t]][_]
  sc := object.get(container, "securityContext", {})
  caps := object.get(sc, "capabilities", {})
  drop := object.get(caps, "drop", [])
  not common_lib.inArray(drop, "ALL")
}

fail_no_add_caps(document) {
  not ignore_check(document, "no_add_caps")
  specInfo := k8sLib.getSpecInfo(document)
  container := specInfo.spec[types[t]][_]
  sc := object.get(container, "securityContext", {})
  caps := object.get(sc, "capabilities", {})
  add := object.get(caps, "add", [])
  count(add) > 0
}

fail_run_as_non_root(document) {
  not ignore_check(document, "run_as_non_root")
  specInfo := k8sLib.getSpecInfo(document)
  container := specInfo.spec[types[t]][_]
  sc := object.get(container, "securityContext", {})
  r := object.get(sc, "runAsNonRoot", false)
  r != true
}

fail_seccomp(document) {
  not ignore_check(document, "seccomp")
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

fail_no_unconfined(document) {
  not ignore_check(document, "no_unconfined")
  specInfo := k8sLib.getSpecInfo(document)
  spec := specInfo.spec
  podSC := object.get(spec, "securityContext", {})
  podSeccomp := object.get(podSC, "seccompProfile", {})
  podType := object.get(podSeccomp, "type", "")
  is_unconfined(podType)
}

fail_no_unconfined(document) {
  not ignore_check(document, "no_unconfined")
  specInfo := k8sLib.getSpecInfo(document)
  container := specInfo.spec[types[t]][_]
  sc := object.get(container, "securityContext", {})
  sp := object.get(sc, "seccompProfile", {})
  ct := object.get(sp, "type", "")
  is_unconfined(ct)
}

fail_not_privileged(document) {
  not ignore_check(document, "not_privileged")
  specInfo := k8sLib.getSpecInfo(document)
  container := specInfo.spec[types[t]][_]
  sc := object.get(container, "securityContext", {})
  priv := object.get(sc, "privileged", false)
  priv == true
}

fail_host_namespaces(document) {
  not ignore_check(document, "host_namespaces")
  specInfo := k8sLib.getSpecInfo(document)
  spec := specInfo.spec
  object.get(spec, "hostNetwork", false) == true
}
fail_host_namespaces(document) {
  not ignore_check(document, "host_namespaces")
  specInfo := k8sLib.getSpecInfo(document)
  spec := specInfo.spec
  object.get(spec, "hostPID", false) == true
}
fail_host_namespaces(document) {
  not ignore_check(document, "host_namespaces")
  specInfo := k8sLib.getSpecInfo(document)
  spec := specInfo.spec
  object.get(spec, "hostIPC", false) == true
}

checks_failed(document) = parts {
  p1 := [x | x := "ape"; fail_ape(document)]
  p2 := [x | x := "drop_all"; fail_drop_all(document)]
  p3 := [x | x := "no_add_caps"; fail_no_add_caps(document)]
  p4 := [x | x := "run_as_non_root"; fail_run_as_non_root(document)]
  p5 := [x | x := "seccomp"; fail_seccomp(document)]
  p6 := [x | x := "no_unconfined"; fail_no_unconfined(document)]
  p7 := [x | x := "not_privileged"; fail_not_privileged(document)]
  p8 := [x | x := "host_namespaces"; fail_host_namespaces(document)]

  parts := array.concat(array.concat(array.concat(array.concat(array.concat(array.concat(array.concat(p1, p2), p3), p4), p5), p6), p7), p8)
}

CxPolicy[result] {
  document := input.document[i]
  metadata := document.metadata

  specInfo := k8sLib.getSpecInfo(document)
  checks := checks_failed(document)
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
    "searchLine": common_lib.build_search_line(split(specInfo.path, "."), []),
    "ktlChecksFailed": checks
  }
}

