package Cx

import data.generic.common as common_lib
import data.generic.k8s as k8sLib

types := {"initContainers", "containers"}

allowed(type) {
  lower(type) == "runtimedefault"
}
allowed(type) {
  lower(type) == "localhost"
}

CxPolicy[result] {
  document := input.document[i]
  metadata := document.metadata

  specInfo := k8sLib.getSpecInfo(document)
  spec := specInfo.spec

  podSC := object.get(spec, "securityContext", {})
  podSeccomp := object.get(podSC, "seccompProfile", {})
  podType := object.get(podSeccomp, "type", "")

  container := specInfo.spec[types[t]][c]
  cSC := object.get(container, "securityContext", {})
  cSeccomp := object.get(cSC, "seccompProfile", {})
  cType := object.get(cSeccomp, "type", podType)

  not allowed(cType)

  result := {
    "documentId": document.id,
    "resourceType": document.kind,
    "resourceName": metadata.name,
    "searchKey": sprintf("metadata.name={{%s}}.%s.%s.name={{%s}}.securityContext.seccompProfile.type", [metadata.name, specInfo.path, types[t], container.name]),
    "issueType": "MissingAttribute",
    "keyExpectedValue": "seccompProfile.type should be RuntimeDefault (or Localhost)",
    "keyActualValue": sprintf("seccompProfile.type is %s", [cType]),
    "searchLine": common_lib.build_search_line(split(specInfo.path, "."), [types[t], c]),
  }
}
